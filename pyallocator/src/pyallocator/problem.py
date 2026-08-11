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
        max_allocation_count: Go-computed cap on allocations per volunteer.
        preallocated_pairs: {(group_key, shift_index)} that MUST be
            allocated — from both volunteer and team-lead preallocations.
        preallocated_roles: {(volunteer_id, shift_index): role} the pinned
            person must fill — and, through may_fill, may fill there even
            if they do not hold the Role. Their group-mates are in
            preallocated_pairs but not here: they attend, and the solver
            picks their Seat.
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

        # No Role is singled out. There used to be a check here that exactly
        # one Role was uncapped, because the contract carried a single size
        # that only such a Role could account for; a Shape states every Role's
        # count, so any set of Roles is now a set the solver can work with
        # (#185).

        volunteers: list[VolunteerView] = []
        # volunteer id -> owning group key
        self._group_key_by_member: dict[str, str] = {}
        for group in self.groups:
            if not group.members:
                raise ProblemError(f"group '{group.group_key}' has no members")
            available = frozenset(group.available_shift_indices)
            for m in group.members:
                if m.id in self._group_key_by_member:
                    raise ProblemError(
                        f"volunteer id '{m.id}' appears in more than one group"
                    )
                self._group_key_by_member[m.id] = group.group_key
                volunteers.append(
                    VolunteerView(
                        member=m,
                        group_key=group.group_key,
                        available_shift_indices=available,
                    )
                )
        self.volunteers: tuple[VolunteerView, ...] = tuple(volunteers)

        self.preallocated_pairs: set[tuple[str, int]] = set()
        self.preallocated_roles: dict[tuple[str, int], str] = {}
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

    def may_fill(self, volunteer: VolunteerView, shift_index: int, role: str) -> bool:
        """Whether this volunteer may take a Seat in this Role on this shift.

        Holding the Role is the ordinary answer. A preallocation is the
        other: a pin records a decision already taken off-system — someone
        has been asked to do this job on this shift — so it grants the Seat
        rather than being refused for a roster that has not caught up.

        The grant is deliberately the narrowest thing that makes the pin
        work. It names one shift, and the Roles the volunteer holds are
        untouched, so every other shift still sees them exactly as before;
        widening their Roles instead would change who the solver may pick
        them as across the whole rota.
        """
        return (
            volunteer.holds(role)
            or self.preallocated_roles.get((volunteer.id, shift_index)) == role
        )

    def seats_for(self, shift: ShiftSpec, role: str) -> int:
        """How many Seats this shift's Shape asks for in the named Role."""
        return sum(seat.count for seat in shift.shape if seat.role == role)

    def customs_for(self, shift: ShiftSpec, role: str) -> tuple[str, ...]:
        """The shift's custom (non-volunteer) pins on the named Role.

        A custom entry is not a solver decision, so it occupies one of its
        Role's Seats before the solver sees the Shape.
        """
        return tuple(
            p.custom for p in shift.preallocations if p.custom and p.role == role
        )

    def _resolve_preallocations(self) -> None:
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

                group_key = self._group_key_by_member.get(pin.volunteer_id)
                if group_key is None:
                    raise ProblemError(
                        f"preallocated volunteer '{pin.volunteer_id}' on shift "
                        f"{shift.index} does not match any volunteer"
                    )
                if self.seats_for(shift, pin.role) < 1:
                    raise ProblemError(
                        f"preallocated volunteer '{pin.volunteer_id}' on shift "
                        f"{shift.index} is pinned to role '{pin.role}', which "
                        "that shift has no Seat for"
                    )
                self.preallocated_roles[(pin.volunteer_id, shift.index)] = pin.role

                # Multiple ids from the same group dedupe to one pair —
                # the whole group comes as a unit anyway.
                self.preallocated_pairs.add((group_key, shift.index))
