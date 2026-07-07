"""Solving with ONLY the at-most-one-team-lead constraint."""

from __future__ import annotations

from conftest import allocations_by_shift, make_group, make_input, make_shift, solve_with
from pyallocator.constraints import at_most_one_team_lead, preallocations

ONLY = [at_most_one_team_lead.CONSTRAINT]


def test_second_team_lead_never_allocated():
    inp = make_input(
        groups=[
            make_group("tl_a", available=[0], team_lead=True),
            make_group("tl_b", available=[0], team_lead=True),
        ],
        shifts=[make_shift(0)],
    )
    out = solve_with(inp, ONLY)
    assert out.success
    assert len(allocations_by_shift(out)[0]) == 1


def test_zero_team_leads_allowed():
    inp = make_input(
        groups=[make_group("g1", available=[0]), make_group("g2", available=[0])],
        shifts=[make_shift(0)],
    )
    out = solve_with(inp, ONLY)
    assert out.success
    assert set(allocations_by_shift(out)[0]) == {"g1", "g2"}
    assert out.shifts[0].team_lead_id == ""


def test_one_team_lead_per_shift_when_spread_possible():
    inp = make_input(
        groups=[
            make_group("tl_a", available=[0, 1], team_lead=True),
            make_group("tl_b", available=[0, 1], team_lead=True),
        ],
        shifts=[make_shift(0), make_shift(1)],
    )
    out = solve_with(inp, ONLY)
    assert out.success
    by_shift = allocations_by_shift(out)
    # Maximise allocations wants all 4 pairs; the constraint caps each
    # shift at one TL group, so both shifts get exactly one.
    assert len(by_shift[0]) == 1
    assert len(by_shift[1]) == 1


def test_preallocated_double_team_lead_is_infeasible():
    # The Go allocator errors when a second TL is preallocated onto a
    # shift; here the model is INFEASIBLE — no rule-breaking rota.
    inp = make_input(
        groups=[
            make_group("tl_a", available=[0], team_lead=True),
            make_group("tl_b", available=[0], team_lead=True),
        ],
        shifts=[make_shift(0, preallocated_volunteer_ids=["tl_a", "tl_b"])],
    )
    out = solve_with(inp, ONLY + [preallocations.CONSTRAINT])
    assert not out.success
    assert out.solver_status == "INFEASIBLE"
