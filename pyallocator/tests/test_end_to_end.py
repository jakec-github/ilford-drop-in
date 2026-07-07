"""End-to-end solve at realistic scale, modeled on the Go allocator's
e2e scenario (pkg/core/allocator/e2e/allocator_test.go): ~24 volunteers,
7 shifts, couples, team-lead couples, a closed shift, a volunteer
preallocation that overrides availability, a preallocated team lead and
a custom (free-text) preallocation.

verify_solution re-checks every hard rule directly against the input,
independently of CP-SAT — a regression oracle for future changes.
"""

from __future__ import annotations

from conftest import make_member, make_shift
from pyallocator.api import solve
from pyallocator.domain import (
    AllocationInput,
    AllocationOutput,
    Group,
    HistoricalShift,
    Member,
)


def verify_solution(inp: AllocationInput, out: AllocationOutput) -> list[str]:
    """Return a list of hard-rule violations (empty = valid rota)."""
    problems: list[str] = []
    groups = {g.group_key: g for g in inp.groups}
    preallocated_pairs = set()
    member_to_group = {m.id: g.group_key for g in inp.groups for m in g.members}
    for spec in inp.shifts:
        if spec.preallocated_team_lead_id:
            preallocated_pairs.add(
                (member_to_group[spec.preallocated_team_lead_id], spec.index)
            )
        for vid in spec.preallocated_volunteer_ids:
            preallocated_pairs.add((member_to_group[vid], spec.index))

    allocated: dict[str, list[int]] = {key: [] for key in groups}
    for spec, shift in zip(inp.shifts, out.shifts):
        if (spec.index, spec.date, spec.size, spec.closed) != (
            shift.index,
            shift.date,
            shift.size,
            shift.closed,
        ):
            problems.append(f"shift {spec.index}: spec fields not echoed faithfully")
        if list(spec.custom_preallocations) != list(shift.custom_preallocations):
            problems.append(f"shift {spec.index}: custom preallocations not echoed")

        keys = shift.allocated_group_keys
        if len(set(keys)) != len(keys):
            problems.append(f"shift {shift.index}: duplicate group allocation")
        if spec.closed and keys:
            problems.append(f"shift {shift.index}: closed but allocated")

        ordinary = 0
        team_lead_groups = 0
        males = 0
        expected_ids: set[str] = set()
        for key in keys:
            group = groups[key]
            allocated[key].append(shift.index)
            expected_ids.update(m.id for m in group.members)
            ordinary += sum(1 for m in group.members if not m.is_team_lead)
            team_lead_groups += any(m.is_team_lead for m in group.members)
            males += sum(1 for m in group.members if m.gender == "Male")
            if (
                shift.index not in group.available_shift_indices
                and (key, shift.index) not in preallocated_pairs
            ):
                problems.append(f"shift {shift.index}: {key} not available")

        if team_lead_groups > 1:
            problems.append(f"shift {shift.index}: {team_lead_groups} team leads")
        # No male => a slot must stay open (TL slot or an ordinary seat)
        # so the rota creator can add one manually.
        if not spec.closed and males == 0 and team_lead_groups > 0:
            budget = max(0, spec.size - len(spec.custom_preallocations))
            if ordinary >= budget:
                problems.append(
                    f"shift {shift.index}: no male and no open slot to add one"
                )

        if not spec.closed and ordinary > max(0, spec.size - len(spec.custom_preallocations)):
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
                m.id == shift.team_lead_id and m.is_team_lead
                for g in inp.groups
                for m in g.members
            )
            if tl_group not in keys or not is_tl:
                problems.append(f"shift {shift.index}: bad team lead designation")
        if spec.preallocated_team_lead_id and shift.team_lead_id != spec.preallocated_team_lead_id:
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
        Member(lead_id, lead_id.capitalize(), "Test", lead_id.capitalize(), lead_gender, True),
        Member(partner_id, partner_id.capitalize(), "Test", partner_id.capitalize(), partner_gender, False),
    )
    return Group(key, members, tuple(available), 0)


def _plain_couple(key: str, a: str, b: str, *, available=()) -> Group:
    members = (make_member(a), make_member(b, gender="Male"))
    return Group(key, members, tuple(available), 0)


def _individual(name_id: str, *, available=(), gender="Female") -> Group:
    first = name_id.capitalize()
    member = Member(name_id, first, "Green", first, gender, False)
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
    num_volunteers = sum(len(g.members) for g in inp.groups)
    assert out.diagnostics.num_variables == num_volunteers * len(inp.shifts)
    assert set(out.diagnostics.constraints_applied) == {
        "no_duplicate_allocation",
        "grouping",
        "availability",
        "max_frequency",
        "shift_capacity",
        "at_most_one_team_lead",
        "male_required",
        "no_back_to_back",
        "closed_shifts",
        "preallocations",
    }


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
        historical_shifts=(),
    )
    out = solve(inp)
    assert not out.success
    assert out.solver_status == "INFEASIBLE"
    assert out.shifts == ()
    assert out.error == ""
