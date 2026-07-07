"""The team-lead preference discourages stacking without forbidding it."""

from __future__ import annotations

from conftest import allocations_by_shift, make_group, make_input, make_shift, solve_with
from pyallocator.constraints import max_frequency, preallocations
from pyallocator.preferences import at_most_one_team_lead, maximize_allocations

PREFS = [maximize_allocations.PREFERENCE, at_most_one_team_lead.PREFERENCE]


def test_second_team_lead_not_stacked():
    # Two TL groups, one shift, no capacity limits: allocating both would
    # add +1 reward but cost the excess penalty, so only one is placed.
    inp = make_input(
        groups=[
            make_group("tl_a", available=[0], team_lead=True),
            make_group("tl_b", available=[0], team_lead=True),
        ],
        shifts=[make_shift(0)],
    )
    out = solve_with(inp, [], preferences=PREFS)
    assert out.success
    assert len(allocations_by_shift(out)[0]) == 1


def test_zero_team_leads_allowed():
    # No TL group is available: the shift still fills with ordinary
    # volunteers, and team_lead_id is empty — not an error.
    inp = make_input(
        groups=[make_group("g1", available=[0]), make_group("g2", available=[0])],
        shifts=[make_shift(0)],
    )
    out = solve_with(inp, [], preferences=PREFS)
    assert out.success
    assert set(allocations_by_shift(out)[0]) == {"g1", "g2"}
    assert out.shifts[0].team_lead_id == ""


def test_one_team_lead_per_shift_across_shifts():
    # Two TL groups, two shifts: better to spread them than stack.
    inp = make_input(
        groups=[
            make_group("tl_a", available=[0, 1], team_lead=True),
            make_group("tl_b", available=[0, 1], team_lead=True),
        ],
        shifts=[make_shift(0), make_shift(1)],
        max_allocation_count=1,
    )
    out = solve_with(inp, [max_frequency.CONSTRAINT], preferences=PREFS)
    assert out.success
    by_shift = allocations_by_shift(out)
    assert len(by_shift[0]) == 1
    assert len(by_shift[1]) == 1
    assert by_shift[0] != by_shift[1]


def test_preallocated_double_team_lead_still_solves():
    # Preallocations force two TL groups onto the same shift: the
    # preference must not make this infeasible.
    inp = make_input(
        groups=[
            make_group("tl_a", available=[0], team_lead=True),
            make_group("tl_b", available=[0], team_lead=True),
        ],
        shifts=[make_shift(0, preallocated_volunteer_ids=["tl_a", "tl_b"])],
    )
    out = solve_with(inp, [preallocations.CONSTRAINT], preferences=PREFS)
    assert out.success
    assert set(allocations_by_shift(out)[0]) == {"tl_a", "tl_b"}
    # One TL designated, the other reported as an ordinary volunteer.
    shift = out.shifts[0]
    assert shift.team_lead_id in {"tl_a", "tl_b"}
    assert len(shift.volunteer_ids) == 1
