"""Shared builders for allocator tests.

make_group/make_shift/make_input build minimal valid inputs; solve_with
solves with an explicit constraint/preference subset so each test file
exercises exactly one module (plus the grouping constraint, which
replaces the old structural group-atomicity, and the placeholder
objective so solutions are non-empty).

The builders take sizes, team-lead flags and pins the way the contract used
to express them, and translate to Roles, Seats and role-named pins here. That
keeps every test stating what it is actually about, and means the translation
is written once rather than in forty places. team_lead_id/volunteer_ids/
customs read the solved shift back the same way round.
"""

from __future__ import annotations

from dataclasses import replace
from typing import Sequence

from pyallocator.api import solve
from pyallocator.domain import (
    AllocationInput,
    AllocationOutput,
    Group,
    HistoricalShift,
    Member,
    OutputShift,
    Preallocation,
    Role,
    Rule,
    Seat,
    ShiftSpec,
)
from pyallocator.constraints import grouping, rule_constraints
from pyallocator.preferences import maximize_allocations

# The two Roles the system had before they were configurable. Tests that do
# not care about Roles get these.
TEAM_LEAD = "Team lead"
SERVICE_VOLUNTEER = "Service volunteer"
DEFAULT_ROLES = (
    Role(name=TEAM_LEAD, priority=1),
    Role(name=SERVICE_VOLUNTEER, priority=2),
)

# How many Seats each Role gets in a built Shape. A Role not named here is
# asked for `size` of, which is what the pre-Roles contract's single size
# bought; Team lead asks for one, as every Shape the app writes does.
DEFAULT_SEATS = {TEAM_LEAD: 1}


def make_member(
    member_id: str,
    *,
    gender: str = "Female",
    is_team_lead: bool = False,
    roles: Sequence[str] | None = None,
) -> Member:
    # A team lead holds Service volunteer too — the roster's migration note,
    # and what keeps leads eligible for ordinary Seats.
    if roles is None:
        roles = (TEAM_LEAD, SERVICE_VOLUNTEER) if is_team_lead else (SERVICE_VOLUNTEER,)
    return Member(
        id=member_id,
        first_name=member_id.capitalize(),
        last_name="Test",
        display_name=member_id.capitalize(),
        gender=gender,
        roles=tuple(roles),
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
    date: str | None = None,
    size: int = 4,
    closed: bool = False,
    custom_preallocations: Sequence[str] = (),
    preallocated_volunteer_ids: Sequence[str] = (),
    preallocated_team_lead_id: str = "",
    roles: Sequence[Role] = DEFAULT_ROLES,
) -> ShiftSpec:
    # size buys Seats in every Role DEFAULT_SEATS does not fix a count for.
    shape = tuple(
        Seat(role=r.name, count=DEFAULT_SEATS.get(r.name, size)) for r in roles
    )
    pins = [
        Preallocation(volunteer_id="", custom=c, role=SERVICE_VOLUNTEER)
        for c in custom_preallocations
    ]
    pins += [
        Preallocation(volunteer_id=v, custom="", role=SERVICE_VOLUNTEER)
        for v in preallocated_volunteer_ids
    ]
    if preallocated_team_lead_id:
        pins.append(
            Preallocation(
                volunteer_id=preallocated_team_lead_id, custom="", role=TEAM_LEAD
            )
        )
    # Default dates are weekly in July; pass date= to cross a month boundary.
    return ShiftSpec(
        index=index,
        date=date if date is not None else f"2026-07-{13 + 7 * index:02d}",
        closed=closed,
        shape=shape,
        preallocations=tuple(pins),
    )


def make_input(
    groups: Sequence[Group],
    shifts: Sequence[ShiftSpec],
    *,
    historical_shifts: Sequence[HistoricalShift] = (),
    roles: Sequence[Role] = DEFAULT_ROLES,
    rules: Sequence[Rule | tuple[str, int]] = (),
) -> AllocationInput:
    """rules takes a Rule, or a (kind, value) pair for a rule covering
    everyone (see covering_everyone)."""
    return AllocationInput(
        shifts=tuple(shifts),
        groups=tuple(groups),
        roles=tuple(roles),
        rules=tuple(
            r if isinstance(r, Rule) else covering_everyone(groups, r)[0] for r in rules
        ),
        historical_shifts=tuple(historical_shifts),
    )


# The Shift Rule kinds: they carry no Scope, so Go sends them naming nobody.
SHIFT_KINDS = {"shift_requires"}

# The choice a pair names nothing of: a shift_requires rule from a pair asks
# for a male, as the rule did before it took a gender.
PAIR_OPTIONS = {"shift_requires": "Male"}


def covering_everyone(groups: Sequence[Group], *rules: tuple[str, int]) -> tuple[Rule, ...]:
    """Rules from (kind, value) pairs, each covering every volunteer in
    `groups` — the Scope most tests mean, resolved the way Go resolves an
    empty one."""
    everyone = tuple(m.id for g in groups for m in g.members)
    return tuple(
        Rule(
            kind=kind,
            volunteer_ids=() if kind in SHIFT_KINDS else everyone,
            value=value,
            option=PAIR_OPTIONS.get(kind, ""),
        )
        for kind, value in rules
    )


def with_rules(input_: AllocationInput, *rules: tuple[str, int]) -> AllocationInput:
    """The input with these rules, each covering everyone in it."""
    return replace(input_, rules=covering_everyone(input_.groups, *rules))


def rules_of(input_: AllocationInput) -> list:
    """The constraints the input's rules make, for solve_with."""
    return rule_constraints(input_.rules)


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


def team_lead_id(shift: OutputShift) -> str:
    """Whoever the solver put in the Team lead Seat, "" if nobody did."""
    for a in shift.assignments:
        if a.role == TEAM_LEAD and a.volunteer_id:
            return a.volunteer_id
    return ""


def volunteer_ids(shift: OutputShift) -> tuple[str, ...]:
    """The volunteers in ordinary Seats, in assignment order."""
    return tuple(
        a.volunteer_id
        for a in shift.assignments
        if a.volunteer_id and a.role != TEAM_LEAD
    )


def customs(shift: OutputShift) -> tuple[str, ...]:
    """The custom (free-text) entries echoed back, in assignment order."""
    return tuple(a.custom for a in shift.assignments if a.custom)
