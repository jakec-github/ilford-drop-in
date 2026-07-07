"""Solving with ONLY the male-required constraint: a shift without a
male must keep a slot open (team lead or ordinary seat) so the rota
creator can add one manually."""

from __future__ import annotations

from conftest import (
    allocations_by_shift,
    make_group,
    make_input,
    make_member,
    make_shift,
    solve_with,
)
from pyallocator.constraints import male_required, preallocations

ONLY = [male_required.CONSTRAINT]


def test_all_female_shift_fills_when_team_lead_slot_open():
    # No team lead allocated: females may fill every seat, because a
    # male team lead can still be added manually.
    inp = make_input(
        groups=[make_group("f1", available=[0]), make_group("f2", available=[0])],
        shifts=[make_shift(0, size=2)],
    )
    out = solve_with(inp, ONLY)
    assert out.success
    assert set(allocations_by_shift(out)[0]) == {"f1", "f2"}


def test_female_team_lead_leaves_one_seat_open():
    # A (preallocated) female team lead takes the TL slot, so with no
    # male one ordinary seat must stay open for a male volunteer.
    female_tl = make_group("leads", available=[0], team_lead=True)
    inp = make_input(
        groups=[female_tl, make_group("f1", available=[0]), make_group("f2", available=[0])],
        shifts=[make_shift(0, size=2, preallocated_team_lead_id="leads")],
    )
    out = solve_with(inp, ONLY + [preallocations.CONSTRAINT])
    assert out.success
    keys = allocations_by_shift(out)[0]
    assert "leads" in keys
    # Only one of the two ordinary females fits; a seat stays open.
    assert len([k for k in keys if k != "leads"]) == 1


def test_male_allows_completely_full_shift():
    female_tl = make_group("leads", available=[0], team_lead=True)
    male = make_group("m1", members=[make_member("m1", gender="Male")], available=[0])
    inp = make_input(
        groups=[female_tl, male, make_group("f1", available=[0])],
        shifts=[make_shift(0, size=2)],
    )
    out = solve_with(inp, ONLY)
    assert out.success
    assert set(allocations_by_shift(out)[0]) == {"leads", "m1", "f1"}


def test_male_team_lead_counts():
    male_tl = make_group(
        "leads",
        members=[make_member("lead", gender="Male", is_team_lead=True)],
        available=[0],
    )
    inp = make_input(
        groups=[male_tl, make_group("f1", available=[0]), make_group("f2", available=[0])],
        shifts=[make_shift(0, size=2)],
    )
    out = solve_with(inp, ONLY)
    assert out.success
    assert set(allocations_by_shift(out)[0]) == {"leads", "f1", "f2"}


def test_closed_shift_exempt():
    inp = make_input(
        groups=[make_group("f1", available=[0])],
        shifts=[make_shift(0, closed=True)],
    )
    out = solve_with(inp, ONLY)
    assert out.success


def test_preallocations_filling_every_slot_with_females_is_infeasible():
    # A female team lead AND a female volunteer preallocated onto a
    # size-1 shift: no male and nowhere to add one -> INFEASIBLE.
    female_tl = make_group("leads", available=[0], team_lead=True)
    inp = make_input(
        groups=[female_tl, make_group("f1", available=[0])],
        shifts=[
            make_shift(
                0,
                size=1,
                preallocated_team_lead_id="leads",
                preallocated_volunteer_ids=["f1"],
            )
        ],
    )
    out = solve_with(inp, ONLY + [preallocations.CONSTRAINT])
    assert not out.success
    assert out.solver_status == "INFEASIBLE"
