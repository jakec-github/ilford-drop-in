"""Shared builders for allocator tests.

make_group/make_shift/make_input build minimal valid inputs; solve_with
solves with an explicit constraint/preference subset so each test file
exercises exactly one module (plus the grouping constraint, which
replaces the old structural group-atomicity, and the placeholder
objective so solutions are non-empty).
"""

from __future__ import annotations

from typing import Sequence

from pyallocator.api import solve
from pyallocator.domain import (
    AllocationInput,
    AllocationOutput,
    Group,
    HistoricalShift,
    Member,
    ShiftSpec,
)
from pyallocator.constraints import grouping
from pyallocator.preferences import maximize_allocations


def make_member(
    member_id: str,
    *,
    gender: str = "Female",
    is_team_lead: bool = False,
) -> Member:
    return Member(
        id=member_id,
        first_name=member_id.capitalize(),
        last_name="Test",
        display_name=member_id.capitalize(),
        gender=gender,
        is_team_lead=is_team_lead,
    )


def make_group(
    key: str,
    *,
    members: Sequence[Member] | None = None,
    available: Sequence[int] = (),
    historical_count: int = 0,
    team_lead: bool = False,
) -> Group:
    if members is None:
        members = [make_member(key, is_team_lead=team_lead)]
    return Group(
        group_key=key,
        members=tuple(members),
        available_shift_indices=tuple(available),
        historical_allocation_count=historical_count,
    )


def make_shift(
    index: int,
    *,
    size: int = 4,
    closed: bool = False,
    custom_preallocations: Sequence[str] = (),
    preallocated_volunteer_ids: Sequence[str] = (),
    preallocated_team_lead_id: str = "",
) -> ShiftSpec:
    return ShiftSpec(
        index=index,
        date=f"2026-07-{13 + 7 * index:02d}",
        size=size,
        closed=closed,
        custom_preallocations=tuple(custom_preallocations),
        preallocated_volunteer_ids=tuple(preallocated_volunteer_ids),
        preallocated_team_lead_id=preallocated_team_lead_id,
    )


def make_input(
    groups: Sequence[Group],
    shifts: Sequence[ShiftSpec],
    *,
    max_allocation_count: int = 99,
    historical_shifts: Sequence[HistoricalShift] = (),
) -> AllocationInput:
    return AllocationInput(
        max_allocation_count=max_allocation_count,
        shifts=tuple(shifts),
        groups=tuple(groups),
        historical_shifts=tuple(historical_shifts),
    )


def solve_with(
    input_: AllocationInput,
    constraints: Sequence = (),
    preferences: Sequence | None = None,
) -> AllocationOutput:
    """Solve with ONLY the given constraints, plus grouping (group
    atomicity used to be structural; now it's a constraint every test
    relies on). Defaults to the placeholder maximise-allocations
    objective so the solver doesn't return the trivially-feasible
    empty rota."""
    if preferences is None:
        preferences = [maximize_allocations.PREFERENCE]
    return solve(
        input_,
        constraints=[grouping.CONSTRAINT, *constraints],
        preferences=list(preferences),
    )


def allocations_by_shift(output: AllocationOutput) -> dict[int, tuple[str, ...]]:
    """shift index -> allocated group keys."""
    return {s.index: s.allocated_group_keys for s in output.shifts}
