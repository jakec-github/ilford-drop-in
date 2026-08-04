"""End-to-end solve at realistic scale, modeled on the Go allocator's
e2e scenario (pkg/core/allocator/e2e/allocator_test.go): ~24 volunteers,
7 shifts, couples, team-lead couples, a closed shift, a volunteer
preallocation that overrides availability, a preallocated team lead and
a custom (free-text) preallocation.

Two oracles here, answering different questions.

verify_solution re-checks every hard rule directly against the input,
independently of CP-SAT — "is this rota legal?". It covers
DEFAULT_CONSTRAINTS only: one_shift_per_month is a STRICT constraint the
solver does not apply, so verifying it here would assert a rule the rota
is not built to keep.

The golden in testdata/e2e_rota.json answers "is this the same rota as
before?". Many rotas are legal, so verify_solution alone would not notice
a refactor quietly reshuffling who works when. See rota_content for why
it is stored as content rather than as the JSON contract, and
solver_environment for why the rota half of it is machine-specific.
"""

from __future__ import annotations

import json
import os
import platform
from importlib.metadata import version
from pathlib import Path

import pytest
from conftest import DEFAULT_ROLES, SERVICE_VOLUNTEER, TEAM_LEAD, make_member, make_shift
from pyallocator.api import solve
from pyallocator.domain import (
    AllocationInput,
    AllocationOutput,
    Group,
    HistoricalShift,
    Member,
)

GOLDEN_PATH = Path(__file__).parent / "testdata" / "e2e_rota.json"


# The spec no longer carries a size or three preallocation lists; it carries a
# Shape and one role-named pin list. These read the same facts back out, so
# verify_solution keeps checking exactly the rules it checked before.
def spec_size(spec) -> int:
    return sum(s.count for s in spec.shape if s.role == SERVICE_VOLUNTEER)


def spec_customs(spec) -> list[str]:
    return [p.custom for p in spec.preallocations if p.custom]


def spec_team_lead_id(spec) -> str:
    for pin in spec.preallocations:
        if pin.role == TEAM_LEAD and pin.volunteer_id:
            return pin.volunteer_id
    return ""


def spec_volunteer_ids(spec) -> list[str]:
    return [
        p.volunteer_id
        for p in spec.preallocations
        if p.volunteer_id and p.role != TEAM_LEAD
    ]


def is_lead(member) -> bool:
    return TEAM_LEAD in member.roles


def verify_solution(inp: AllocationInput, out: AllocationOutput) -> list[str]:
    """Return a list of hard-rule violations (empty = valid rota)."""
    problems: list[str] = []
    groups = {g.group_key: g for g in inp.groups}
    preallocated_pairs = set()
    member_to_group = {m.id: g.group_key for g in inp.groups for m in g.members}
    for spec in inp.shifts:
        if spec_team_lead_id(spec):
            preallocated_pairs.add(
                (member_to_group[spec_team_lead_id(spec)], spec.index)
            )
        for vid in spec_volunteer_ids(spec):
            preallocated_pairs.add((member_to_group[vid], spec.index))

    allocated: dict[str, list[int]] = {key: [] for key in groups}
    for spec, shift in zip(inp.shifts, out.shifts):
        if (spec.index, spec.date, spec_size(spec), spec.closed) != (
            shift.index,
            shift.date,
            shift.size,
            shift.closed,
        ):
            problems.append(f"shift {spec.index}: spec fields not echoed faithfully")
        if spec_customs(spec) != list(shift.custom_preallocations):
            problems.append(f"shift {spec.index}: custom preallocations not echoed")

        keys = shift.allocated_group_keys
        if len(set(keys)) != len(keys):
            problems.append(f"shift {shift.index}: duplicate group allocation")
        if spec.closed and keys:
            problems.append(f"shift {shift.index}: closed but allocated")

        # Who sits in an ordinary Seat is a solver decision now, not a
        # property of the Roles someone holds: a second team-lead holder
        # works the shift as a Service volunteer. So read it off the
        # output rather than deriving it from the input.
        ordinary = len(shift.volunteer_ids)
        males = 0
        expected_ids: set[str] = set()
        for key in keys:
            group = groups[key]
            allocated[key].append(shift.index)
            expected_ids.update(m.id for m in group.members)
            males += sum(1 for m in group.members if m.gender == "Male")
            if (
                shift.index not in group.available_shift_indices
                and (key, shift.index) not in preallocated_pairs
            ):
                problems.append(f"shift {shift.index}: {key} not available")

        # No male => a Seat must stay open (the TL Seat or an ordinary
        # one) so the rota creator can add one manually.
        if not spec.closed and males == 0 and shift.team_lead_id:
            budget = max(0, spec_size(spec) - len(spec_customs(spec)))
            if ordinary >= budget:
                problems.append(
                    f"shift {shift.index}: no male and no open slot to add one"
                )

        if not spec.closed and ordinary > max(
            0, spec_size(spec) - len(spec_customs(spec))
        ):
            problems.append(f"shift {shift.index}: over capacity ({ordinary})")

        # volunteer_ids + team_lead_id must be exactly the allocated members.
        reported = set(shift.volunteer_ids) | ({shift.team_lead_id} - {""})
        if reported != expected_ids:
            problems.append(
                f"shift {shift.index}: reported ids {sorted(reported)} != "
                f"allocated group members {sorted(expected_ids)}"
            )
        if shift.team_lead_id:
            tl_group = member_to_group.get(shift.team_lead_id)
            is_tl = any(
                m.id == shift.team_lead_id and is_lead(m)
                for g in inp.groups
                for m in g.members
            )
            if tl_group not in keys or not is_tl:
                problems.append(f"shift {shift.index}: bad team lead designation")
        if spec_team_lead_id(spec) and shift.team_lead_id != spec_team_lead_id(spec):
            problems.append(f"shift {shift.index}: preallocated TL not designated")

    for group_key, shift_index in preallocated_pairs:
        if shift_index not in allocated[group_key]:
            problems.append(f"preallocation ({group_key}, {shift_index}) not honoured")

    last_historical = (
        set(inp.historical_shifts[-1].group_keys) if inp.historical_shifts else set()
    )
    for key, indices in allocated.items():
        if len(indices) > inp.max_allocation_count:
            problems.append(f"{key}: exceeds max allocation count")
        indices = sorted(indices)
        for a, b in zip(indices, indices[1:]):
            if b - a == 1:
                problems.append(f"{key}: back-to-back shifts {a},{b}")
        if 0 in indices and key in last_historical:
            problems.append(f"{key}: on last historical shift AND shift 0")

    return problems


def _couple(key: str, lead_id: str, partner_id: str, *, lead_gender="Female", partner_gender="Male", available=()) -> Group:
    members = (
        Member(
            lead_id,
            lead_id.capitalize(),
            "Test",
            lead_id.capitalize(),
            lead_gender,
            (TEAM_LEAD, SERVICE_VOLUNTEER),
        ),
        Member(
            partner_id,
            partner_id.capitalize(),
            "Test",
            partner_id.capitalize(),
            partner_gender,
            (SERVICE_VOLUNTEER,),
        ),
    )
    return Group(key, members, tuple(available), 0)


def _plain_couple(key: str, a: str, b: str, *, available=()) -> Group:
    members = (make_member(a), make_member(b, gender="Male"))
    return Group(key, members, tuple(available), 0)


def _individual(name_id: str, *, available=(), gender="Female") -> Group:
    first = name_id.capitalize()
    member = Member(name_id, first, "Green", first, gender, (SERVICE_VOLUNTEER,))
    return Group(f"{first} Green", (member,), tuple(available), 0)


def make_e2e_input() -> AllocationInput:
    groups = (
        _couple("couple_alice_bob", "alice", "bob", available=[0, 2, 4, 6]),
        _couple("couple_george_helen", "george", "helen", available=[1, 4, 6]),
        _couple("couple_karen_larry", "karen", "larry", available=[0, 4, 6]),
        _couple("couple_wendy_xavier", "wendy", "xavier", available=[3]),
        _plain_couple("couple_eve_frank", "eve", "frank", available=[0, 4, 6]),
        _plain_couple("couple_mike_nancy", "mike", "nancy", available=[1, 6]),
        _individual("charlie", available=[2], gender="Male"),
        _individual("diana", available=[0, 1, 4, 6]),
        _individual("ivan", available=[3], gender="Male"),
        _individual("judy", available=[1, 2, 6]),
        _individual("oliver", available=[0, 2, 4, 6], gender="Male"),
        _individual("paula", available=[0, 2, 4]),
        _individual("quinn", available=[1, 4, 6], gender="Male"),
        _individual("rachel", available=[4, 6]),
        _individual("steve", available=[0, 1, 2], gender="Male"),
        _individual("tina", available=[2, 6]),
        _individual("uma", available=[2]),
        _individual("victor", available=[1, 2, 4], gender="Male"),
    )
    shifts = (
        make_shift(0, size=3),  # override-enlarged first shift
        make_shift(1, size=2, preallocated_team_lead_id="george"),
        make_shift(2, size=2),
        make_shift(3, size=2),
        # charlie is NOT available for shift 4: preallocation overrides.
        make_shift(4, size=2, preallocated_volunteer_ids=["charlie"]),
        make_shift(5, size=2, closed=True),
        make_shift(6, size=2, custom_preallocations=["external_john"]),
    )
    historical = (
        HistoricalShift(date="2026-06-29", group_keys=("couple_mike_nancy",)),
        # Alice/Bob and Diana worked the shift immediately before this rota.
        HistoricalShift(date="2026-07-06", group_keys=("couple_alice_bob", "Diana Green")),
    )
    return AllocationInput(
        max_allocation_count=2,  # 33% of 7 shifts, as computed in Go
        shifts=shifts,
        groups=groups,
        roles=DEFAULT_ROLES,
        requires_male=True,
        historical_shifts=historical,
    )


def test_end_to_end_scenario():
    inp = make_e2e_input()
    out = solve(inp)

    assert out.success, out.solver_status
    assert out.solver_status == "OPTIMAL"
    assert verify_solution(inp, out) == []

    by_shift = {s.index: s for s in out.shifts}
    # Closed shift untouched.
    assert by_shift[5].allocated_group_keys == ()
    # Custom preallocation echoed back for persistence.
    assert by_shift[6].custom_preallocations == ("external_john",)
    # Preallocated team lead designated on shift 1.
    assert by_shift[1].team_lead_id == "george"
    # Availability-overriding volunteer preallocation honoured.
    assert "Charlie Green" in by_shift[4].allocated_group_keys
    # Back-to-back boundary: Alice/Bob and Diana sat out shift 0.
    assert "couple_alice_bob" not in by_shift[0].allocated_group_keys
    assert "Diana Green" not in by_shift[0].allocated_group_keys

    assert out.diagnostics is not None
    assert out.diagnostics.num_groups == len(inp.groups)
    # A lower bound, not an equality: num_variables counts whatever the
    # model is made of, and every (volunteer, shift) pair needs at least
    # one variable to decide it.
    num_volunteers = sum(len(g.members) for g in inp.groups)
    assert out.diagnostics.num_variables >= num_volunteers * len(inp.shifts)
    assert set(out.diagnostics.constraints_applied) == {
        "no_duplicate_allocation",
        "grouping",
        "availability",
        "max_frequency",
        "seat_capacity",
        "male_required",
        "no_back_to_back",
        "closed_shifts",
        "preallocations",
    }


def rota_content(out: AllocationOutput) -> dict:
    """The solved rota as *content* — who works when, in what job.

    Deliberately not the JSON contract. The contract is about to be
    reshaped (team_lead_id and volunteer_ids give way to role-tagged
    assignments in #89), and a golden written in contract shape would
    have to be regenerated for every rename, which is exactly when it
    stops being able to tell a rename from a behaviour change. Read this
    function as the mapping from whatever the contract currently says to
    the rota a volunteer would recognise; rewrite it when the contract
    moves, and the golden file should not move with it.
    """
    return {
        str(shift.index): {
            "team_lead": shift.team_lead_id,
            "volunteers": sorted(shift.volunteer_ids),
            "customs": list(shift.custom_preallocations),
        }
        for shift in out.shifts
    }


def solver_environment() -> dict:
    """What a recorded rota is, and is not, reproducible against.

    solver.py pins the seed and a single worker, so CP-SAT is
    deterministic — on one build of it. Across builds it is not: the
    e2e scenario has many equally-optimal rotas, and a Mac and a Linux
    runner agree on the objective value while returning different ones.
    So the rota is pinned per environment and the objective everywhere.
    """
    return {
        "ortools": version("ortools"),
        "platform": f"{platform.system()}-{platform.machine()}".lower(),
    }


def test_end_to_end_matches_golden_rota():
    """The behaviour-preservation oracle for the Roles rewrite (#89).

    Many rotas satisfy every hard rule, so verify_solution cannot tell a
    refactor that preserves behaviour from one that quietly reshuffles
    the rota. This can. A diff here means the allocator now produces a
    different rota from the same input: inspect it by hand and either fix
    the change or accept it explicitly, never regenerate reflexively.

    Regenerate with UPDATE_GOLDEN=1 once the change is understood. That
    also re-records the environment, so regenerate where you develop.
    """
    inp = make_e2e_input()
    out = solve(inp)
    assert out.success, out.solver_status

    environment = solver_environment()
    actual = {
        "environment": environment,
        "objective_value": out.objective_value,
        "shifts": rota_content(out),
    }
    if os.environ.get("UPDATE_GOLDEN"):
        GOLDEN_PATH.write_text(json.dumps(actual, indent=2, sort_keys=True) + "\n")
    golden = json.loads(GOLDEN_PATH.read_text())

    # The optimum is a property of the model, not of the solver build, so
    # this holds everywhere — including CI, which cannot check the rota.
    # A change here is a behaviour change wherever it is seen.
    assert out.objective_value == golden["objective_value"]

    if environment != golden["environment"]:
        pytest.skip(
            f"rota recorded on {golden['environment']}, running on {environment}"
            " — equally-optimal rotas differ between CP-SAT builds, so only the"
            " objective value is comparable here"
        )
    assert actual["shifts"] == golden["shifts"]


def test_end_to_end_is_deterministic():
    inp = make_e2e_input()
    first = solve(inp)
    second = solve(inp)
    # solve_time_seconds naturally varies; the rota itself must not.
    assert first.shifts == second.shifts
    assert first.objective_value == second.objective_value


def test_infeasible_reported_not_crashed():
    # Two individuals preallocated onto a size-1 shift: capacity cannot
    # hold, so the model is INFEASIBLE — a well-formed result.
    inp = AllocationInput(
        max_allocation_count=2,
        shifts=(make_shift(0, size=1, preallocated_volunteer_ids=["a", "b"]),),
        groups=(
            _individual("a", available=[0]),
            _individual("b", available=[0]),
        ),
        roles=DEFAULT_ROLES,
        requires_male=True,
        historical_shifts=(),
    )
    out = solve(inp)
    assert not out.success
    assert out.solver_status == "INFEASIBLE"
    assert out.shifts == ()
    assert out.error == ""
