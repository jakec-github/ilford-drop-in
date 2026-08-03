"""Problem: the normalised solver view of an AllocationInput.

The assignment unit is the individual volunteer: groups arriving from
Go are flattened into VolunteerViews (canonical order: group input
order, then member order), and group atomicity is enforced by the
grouping constraint rather than the variable structure. Availability
is group-resolved in Go, so every member inherits its group's shifts.
Preallocation resolution (volunteer id -> owning group) and its error
cases live here because multiple constraints need the resolved pairs
(e.g. availability exempts preallocated groups).
"""

from __future__ import annotations

from dataclasses import dataclass

from .domain import GENDER_MALE, AllocationInput, Group, Member, Role, ShiftSpec


class ProblemError(ValueError):
    """Raised when the input is well-formed JSON but semantically invalid
    (e.g. a preallocated volunteer id that doesn't exist)."""


@dataclass(frozen=True)
class VolunteerView:
    """One volunteer plus the derived facts the constraints need."""

    member: Member
    group_key: str
    available_shift_indices: frozenset[int]  # inherited from the group
    # Whether this volunteer holds a capped Role. Temporary: it stands in for
    # the lead concept while the solver still designates a lead after the
    # fact, and goes when role variables arrive.
    is_team_lead: bool = False

    @property
    def id(self) -> str:
        return self.member.id

    @property
    def roles(self) -> tuple[str, ...]:
        return self.member.roles

    def holds(self, role: str) -> bool:
        return role in self.member.roles

    @property
    def is_male(self) -> bool:
        return self.member.gender == GENDER_MALE

    @property
    def seat_cost(self) -> int:
        """Ordinary seats this volunteer occupies: team leads never
        count toward shift size."""
        return 0 if self.is_team_lead else 1


class Problem:
    """Normalised, validated view of the allocation problem.

    Attributes:
        volunteers: VolunteerView per member, canonical order (group
            input order, then member order within the group).
        groups: the input groups, input order preserved — grouping,
            fairness history and extraction still need group structure.
        group_by_key: {group_key: Group}.
        shifts: the input shift specs, index order.
        roles: the configured Roles, priority order.
        role_by_name: {name: Role}.
        uncapped_role: the single Role with no ceiling.
        lead_role: the highest-priority capped Role, or None. Temporary —
            it is what the pre-Roles "team lead" meant, and the solver still
            designates a lead after the fact.
        requires_male: whether the male-cover rule applies.
        max_allocation_count: Go-computed cap on allocations per volunteer.
        preallocated_pairs: {(group_key, shift_index)} that MUST be
            allocated — from both volunteer and team-lead preallocations.
        preallocated_team_lead: {shift_index: volunteer_id} designated TL.
        last_historical_group_keys: group keys present on the most recent
            historical shift (back-to-back boundary with the previous rota).
        historical_group_months: {group_key: frozenset of YYYY-MM months} the
            group already worked in history (the one-shift-per-month rule bars a
            group from any current shift in a month it already worked).
    """

    def __init__(self, input_: AllocationInput) -> None:
        self.input = input_
        self.shifts: tuple[ShiftSpec, ...] = input_.shifts
        self.max_allocation_count: int = input_.max_allocation_count
        self.groups: tuple[Group, ...] = input_.groups
        self.group_by_key: dict[str, Group] = {g.group_key: g for g in self.groups}

        self.roles: tuple[Role, ...] = tuple(
            sorted(input_.roles, key=lambda r: r.priority)
        )
        self.role_by_name: dict[str, Role] = {r.name: r for r in self.roles}
        uncapped = [r for r in self.roles if not r.capped]
        if len(uncapped) != 1:
            raise ProblemError(
                f"expected exactly one uncapped role, got {len(uncapped)}"
            )
        self.uncapped_role: Role = uncapped[0]
        capped = [r for r in self.roles if r.capped]
        self.lead_role: Role | None = capped[0] if capped else None
        self.requires_male: bool = input_.requires_male

        lead_name = self.lead_role.name if self.lead_role else None

        volunteers: list[VolunteerView] = []
        # volunteer id -> (owning group key, roles held)
        self._member_index: dict[str, tuple[str, tuple[str, ...]]] = {}
        for group in self.groups:
            if not group.members:
                raise ProblemError(f"group '{group.group_key}' has no members")
            available = frozenset(group.available_shift_indices)
            for m in group.members:
                if m.id in self._member_index:
                    raise ProblemError(
                        f"volunteer id '{m.id}' appears in more than one group"
                    )
                self._member_index[m.id] = (group.group_key, m.roles)
                volunteers.append(
                    VolunteerView(
                        member=m,
                        group_key=group.group_key,
                        available_shift_indices=available,
                        is_team_lead=lead_name is not None and lead_name in m.roles,
                    )
                )
        self.volunteers: tuple[VolunteerView, ...] = tuple(volunteers)

        self.preallocated_pairs: set[tuple[str, int]] = set()
        self.preallocated_team_lead: dict[int, str] = {}
        self._resolve_preallocations()

        self.last_historical_group_keys: frozenset[str] = frozenset(
            input_.historical_shifts[-1].group_keys if input_.historical_shifts else ()
        )

        # group_key -> months (YYYY-MM) that group already worked in history.
        months: dict[str, set[str]] = {}
        for hs in input_.historical_shifts:
            month = hs.date[:7]
            for group_key in hs.group_keys:
                months.setdefault(group_key, set()).add(month)
        self.historical_group_months: dict[str, frozenset[str]] = {
            k: frozenset(v) for k, v in months.items()
        }

    def seats_for(self, shift: ShiftSpec, role: str) -> int:
        """How many Seats this shift's Shape asks for in the named Role."""
        return sum(seat.count for seat in shift.shape if seat.role == role)

    def shift_size(self, shift: ShiftSpec) -> int:
        """The shift's ordinary-volunteer target: its uncapped Role's Seats.

        Temporary. It reproduces the single `size` the contract used to carry,
        so the constraints that still think in sizes keep working while the
        Shape is being wired through.
        """
        return self.seats_for(shift, self.uncapped_role.name)

    def shift_customs(self, shift: ShiftSpec) -> tuple[str, ...]:
        """The shift's custom (non-volunteer) pins, in input order.

        Temporary, for the same reason as shift_size: these all occupy
        ordinary Seats today, whatever Role they name.
        """
        return tuple(p.custom for p in shift.preallocations if p.custom)

    def _resolve_preallocations(self) -> None:
        lead_name = self.lead_role.name if self.lead_role else None

        for shift in self.shifts:
            # Go strips preallocations from closed shifts before sending;
            # reject rather than silently ignore if any slip through.
            if shift.closed and shift.preallocations:
                raise ProblemError(
                    f"shift {shift.index} is closed but has preallocations"
                )

            for pin in shift.preallocations:
                if not pin.volunteer_id:
                    # A custom entry names a Role but no person, so there is no
                    # group to force onto the shift; it just occupies a Seat.
                    continue

                entry = self._member_index.get(pin.volunteer_id)
                if entry is None:
                    raise ProblemError(
                        f"preallocated volunteer '{pin.volunteer_id}' on shift "
                        f"{shift.index} does not match any volunteer"
                    )
                group_key, roles = entry
                if pin.role not in roles:
                    raise ProblemError(
                        f"preallocated volunteer '{pin.volunteer_id}' on shift "
                        f"{shift.index} does not hold role '{pin.role}'"
                    )

                # Temporary: the lead is still designated after the solve, so
                # a pin on the capped Role has to be remembered separately.
                # It goes when the solver assigns Roles itself.
                if pin.role == lead_name:
                    self.preallocated_team_lead[shift.index] = pin.volunteer_id

                # Multiple ids from the same group dedupe to one pair —
                # the whole group comes as a unit anyway.
                self.preallocated_pairs.add((group_key, shift.index))
