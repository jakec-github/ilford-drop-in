"""Problem: the normalised solver view of an AllocationInput.

Derives per-group arithmetic (ordinary_size, has_team_lead, male_count)
by counting members — no grouping logic; groups arrive fully built from
Go. Preallocation resolution (volunteer id -> owning group) and its
error cases live here because multiple constraints need the resolved
pairs (e.g. availability exempts preallocated pairs).
"""

from __future__ import annotations

from dataclasses import dataclass

from .domain import GENDER_MALE, AllocationInput, Group, ShiftSpec


class ProblemError(ValueError):
    """Raised when the input is well-formed JSON but semantically invalid
    (e.g. a preallocated volunteer id that doesn't exist)."""


@dataclass(frozen=True)
class GroupView:
    """A group plus the derived numbers the constraints need."""

    group: Group
    ordinary_size: int  # members who are not team leads (seats they occupy)
    has_team_lead: bool
    male_count: int

    @property
    def key(self) -> str:
        return self.group.group_key


class Problem:
    """Normalised, validated view of the allocation problem.

    Attributes:
        groups: GroupView per input group, input order preserved.
        shifts: the input shift specs, index order.
        max_allocation_count: Go-computed cap on allocations per group.
        preallocated_pairs: {(group_key, shift_index)} that MUST be
            allocated — from both volunteer and team-lead preallocations.
        preallocated_team_lead: {shift_index: volunteer_id} designated TL.
        last_historical_group_keys: group keys present on the most recent
            historical shift (back-to-back boundary with the previous rota).
    """

    def __init__(self, input_: AllocationInput) -> None:
        self.input = input_
        self.shifts: tuple[ShiftSpec, ...] = input_.shifts
        self.max_allocation_count: int = input_.max_allocation_count
        self.groups: tuple[GroupView, ...] = tuple(
            GroupView(
                group=g,
                ordinary_size=sum(1 for m in g.members if not m.is_team_lead),
                has_team_lead=any(m.is_team_lead for m in g.members),
                male_count=sum(1 for m in g.members if m.gender == GENDER_MALE),
            )
            for g in input_.groups
        )
        self.group_by_key: dict[str, GroupView] = {gv.key: gv for gv in self.groups}

        # volunteer id -> (owning group key, is_team_lead)
        self._member_index: dict[str, tuple[str, bool]] = {}
        for gv in self.groups:
            for m in gv.group.members:
                if m.id in self._member_index:
                    raise ProblemError(
                        f"volunteer id '{m.id}' appears in more than one group"
                    )
                self._member_index[m.id] = (gv.key, m.is_team_lead)

        self.preallocated_pairs: set[tuple[str, int]] = set()
        self.preallocated_team_lead: dict[int, str] = {}
        self._resolve_preallocations()

        self.last_historical_group_keys: frozenset[str] = frozenset(
            input_.historical_shifts[-1].group_keys if input_.historical_shifts else ()
        )

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
