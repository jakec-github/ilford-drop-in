"""Solving with ONLY the male-required constraint: a shift without a
male must keep a Seat open, in whatever Role, so the rota creator can
add one manually."""

from __future__ import annotations

import dataclasses

from conftest import (
    SERVICE_VOLUNTEER,
    allocations_by_shift,
    make_group,
    make_input,
    make_member,
    make_shift,
    solve_with,
)
from pyallocator.constraints import male_required, preallocations, seat_capacity
from pyallocator.domain import Seat

ONLY = [male_required.CONSTRAINT]


def test_all_female_shift_fills_while_the_team_lead_seat_is_open():
    # Nobody in the Team lead Seat, so it is one of the open ones and a
    # male team lead can still be added manually.
    inp = make_input(
        groups=[make_group("f1", available=[0]), make_group("f2", available=[0])],
        shifts=[make_shift(0, size=2)],
    )
    out = solve_with(inp, ONLY)
    assert out.success
    assert set(allocations_by_shift(out)[0]) == {"f1", "f2"}


def test_female_team_lead_leaves_one_seat_open():
    # A (preallocated) female team lead takes the Team lead Seat, so with
    # no male an ordinary Seat must stay open for a male volunteer.
    female_tl = make_group("leads", available=[0], team_lead=True)
    inp = make_input(
        groups=[
            female_tl,
            make_group("f1", available=[0]),
            make_group("f2", available=[0]),
        ],
        shifts=[make_shift(0, size=2, preallocated_team_lead_id="leads")],
    )
    out = solve_with(inp, ONLY + [preallocations.CONSTRAINT])
    assert out.success
    keys = allocations_by_shift(out)[0]
    assert "leads" in keys
    # Only one of the two ordinary females fits; a Seat stays open.
    assert len([k for k in keys if k != "leads"]) == 1


def test_escape_comes_from_the_shape_not_a_hardcoded_slot():
    # A Shape asking for one Service volunteer Seat and nothing else has
    # exactly one Seat to leave open. Filling it with a female leaves
    # nowhere to add a male, so she is not allocated at all — where the
    # same shift with a Team lead Seat in its Shape would take her.
    one_seat = dataclasses.replace(
        make_shift(0), shape=(Seat(role=SERVICE_VOLUNTEER, count=1),)
    )
    inp = make_input(groups=[make_group("f1", available=[0])], shifts=[one_seat])
    out = solve_with(inp, ONLY)
    assert out.success
    assert allocations_by_shift(out)[0] == ()

    with_lead_seat = make_input(
        groups=[make_group("f1", available=[0])], shifts=[make_shift(0, size=1)]
    )
    out = solve_with(with_lead_seat, ONLY)
    assert out.success
    assert allocations_by_shift(out)[0] == ("f1",)


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


def test_a_male_in_any_seat_counts():
    # The rule is about who is on the shift, not which Seat they sit in:
    # a male team lead satisfies it exactly as a male volunteer would.
    male_tl = make_group(
        "leads",
        members=[make_member("lead", gender="Male", is_team_lead=True)],
        available=[0],
    )
    inp = make_input(
        groups=[
            male_tl,
            make_group("f1", available=[0]),
            make_group("f2", available=[0]),
        ],
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


def test_preallocations_filling_every_seat_win_over_male_cover():
    # A female team lead AND a female volunteer preallocated onto a
    # size-1 shift: no male and no Seat to add one to. The pins were
    # decided before the solve, so they win rather than the rota being
    # INFEASIBLE.
    out = solve_with(_every_seat_female(), ONLY + [preallocations.CONSTRAINT])
    assert out.success
    assert set(allocations_by_shift(out)[0]) == {"leads", "f1"}


def test_custom_pins_filling_every_seat_win_over_male_cover():
    # A shift covered by somebody not on the rota: their custom pin takes
    # its only Seat, and the shift needs nothing else. Seat capacity is what
    # makes the shift full, so it runs too.
    inp = make_input(
        groups=[make_group("f1", available=[0])],
        shifts=[
            dataclasses.replace(
                make_shift(0, custom_preallocations=["Outside cover"]),
                shape=(Seat(role=SERVICE_VOLUNTEER, count=1),),
            )
        ],
    )
    out = solve_with(inp, ONLY + [preallocations.CONSTRAINT, seat_capacity.CONSTRAINT])
    assert out.success
    assert allocations_by_shift(out)[0] == ()


def test_a_pinned_group_mate_counts_toward_filling_every_seat():
    # Only one person is pinned, but their partner comes with them, and
    # the two of them take both Seats.
    pair = make_group(
        "pair",
        members=[make_member("ann"), make_member("bea")],
        available=[0],
    )
    inp = make_input(
        groups=[pair],
        shifts=[
            dataclasses.replace(
                make_shift(0, preallocated_volunteer_ids=["ann"]),
                shape=(Seat(role=SERVICE_VOLUNTEER, count=2),),
            )
        ],
    )
    out = solve_with(inp, ONLY + [preallocations.CONSTRAINT])
    assert out.success
    assert allocations_by_shift(out)[0] == ("pair",)


def test_one_seat_left_unpinned_still_needs_male_cover():
    # The same pins with one Seat more: the solver has a choice again, so
    # the rule applies and keeps that Seat open rather than giving it to f2.
    inp = make_input(
        groups=[
            make_group("leads", available=[0], team_lead=True),
            make_group("f1", available=[0]),
            make_group("f2", available=[0]),
        ],
        shifts=[
            make_shift(
                0,
                size=2,
                preallocated_team_lead_id="leads",
                preallocated_volunteer_ids=["f1"],
            )
        ],
    )
    out = solve_with(inp, ONLY + [preallocations.CONSTRAINT])
    assert out.success
    assert set(allocations_by_shift(out)[0]) == {"leads", "f1"}


def test_rule_off_allows_a_shift_with_no_male_and_no_open_seat():
    # The same input with male cover switched off. Off now means the
    # constraint is not in the run's list at all, rather than in the list
    # and reading a flag to decide it has nothing to do (issue #130).
    out = solve_with(_every_seat_female(), [preallocations.CONSTRAINT])
    assert out.success
    assert set(allocations_by_shift(out)[0]) == {"leads", "f1"}


def _every_seat_female():
    female_tl = make_group("leads", available=[0], team_lead=True)
    return make_input(
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
