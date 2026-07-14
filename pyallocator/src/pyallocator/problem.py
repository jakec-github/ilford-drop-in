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

from .domain import GENDER_MALE, AllocationInput, Group, Member, ShiftSpec


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
    def is_team_lead(self) -> bool:
        return self.member.is_team_lead

    @property
    def is_male(self) -> bool:
        return self.member.gender == GENDER_MALE

    @property
    def seat_cost(self) -> int:
        """Ordinary seats this volunteer occupies: team leads never
        count toward shift size."""
        return 0 if self.member.is_team_lead else 1


class Problem:
    """Normalised, validated view of the allocation problem.

    Attributes:
        volunteers: VolunteerView per member, canonical order (group
            input order, then member order within the group).
        groups: the input groups, input order preserved — grouping,
            fairness history and extraction still need group structure.
        group_by_key: {group_key: Group}.
        shifts: the input shift specs, index order.
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

        volunteers: list[VolunteerView] = []
        # volunteer id -> (owning group key, is_team_lead)
        self._member_index: dict[str, tuple[str, bool]] = {}
        for group in self.groups:
            if not group.members:
                raise ProblemError(f"group '{group.group_key}' has no members")
            available = frozenset(group.available_shift_indices)
            for m in group.members:
                if m.id in self._member_index:
                    raise ProblemError(
                        f"volunteer id '{m.id}' appears in more than one group"
                    )
                self._member_index[m.id] = (group.group_key, m.is_team_lead)
                volunteers.append(
                    VolunteerView(
                        member=m,
                        group_key=group.group_key,
                        available_shift_indices=available,
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

    def _resolve_preallocations(self) -> None:
        for shift in self.shifts:
            # Go strips preallocations from closed shifts before sending;
            # reject rather than silently ignore if any slip through.
            if shift.closed and (
                shift.preallocated_volunteer_ids or shift.preallocated_team_lead_id
            ):
                raise ProblemError(
                    f"shift {shift.index} is closed but has preallocations"
                )

            if shift.preallocated_team_lead_id:
                vol_id = shift.preallocated_team_lead_id
                entry = self._member_index.get(vol_id)
                if entry is None:
                    raise ProblemError(
                        f"preallocated team lead '{vol_id}' on shift {shift.index} "
                        "does not match any volunteer"
                    )
                group_key, is_team_lead = entry
                if not is_team_lead:
                    raise ProblemError(
                        f"preallocated team lead '{vol_id}' on shift {shift.index} "
                        "is not a team lead"
                    )
                self.preallocated_team_lead[shift.index] = vol_id
                self.preallocated_pairs.add((group_key, shift.index))

            for vol_id in shift.preallocated_volunteer_ids:
                entry = self._member_index.get(vol_id)
                if entry is None:
                    raise ProblemError(
                        f"preallocated volunteer '{vol_id}' on shift {shift.index} "
                        "does not match any volunteer"
                    )
                group_key, _ = entry
                # Multiple ids from the same group dedupe to one pair —
                # the whole group comes as a unit anyway.
                self.preallocated_pairs.add((group_key, shift.index))
