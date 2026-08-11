"""The even-fill preference spreads scarce volunteers across shifts."""

from __future__ import annotations

import dataclasses

from conftest import (
    SERVICE_VOLUNTEER,
    TEAM_LEAD,
    allocations_by_shift,
    make_group,
    make_input,
    make_member,
    make_shift,
    solve_with,
    team_lead_id,
    volunteer_ids,
)
from pyallocator.constraints import max_frequency, seat_capacity
from pyallocator.domain import Seat
from pyallocator.preferences import even_fill

PREFS = [even_fill.PREFERENCE]


def test_scarce_volunteers_spread_across_shifts():
    # 3 volunteers, cap 1 each, 2 shifts: a 2-1 split scores higher than
    # 3-0 because a shift's 3rd seat is worth less than another's 1st.
    inp = make_input(
        groups=[make_group(f"g{i}", available=[0, 1]) for i in range(3)],
        shifts=[make_shift(0, size=4), make_shift(1, size=4)],
        max_allocation_count=1,
    )
    out = solve_with(inp, [max_frequency.CONSTRAINT], preferences=PREFS)
    assert out.success
    by_shift = allocations_by_shift(out)
    assert sorted(len(keys) for keys in by_shift.values()) == [1, 2]


def test_custom_preallocations_count_as_early_seats():
    # Shift 0 already has a custom entry occupying seat 1, so the single
    # available volunteer is worth more on shift 1 (seat 1) than on
    # shift 0 (seat 2).
    inp = make_input(
        groups=[make_group("g1", available=[0, 1])],
        shifts=[
            make_shift(0, size=2, custom_preallocations=["external"]),
            make_shift(1, size=2),
        ],
        max_allocation_count=1,
    )
    out = solve_with(inp, [max_frequency.CONSTRAINT], preferences=PREFS)
    assert out.success
    assert allocations_by_shift(out)[1] == ("g1",)


def test_higher_priority_role_seat_fills_before_a_lower_one():
    # A team-lead holder who is working the shift anyway belongs in the
    # Team lead Seat. Nothing forces it — the shift has room for them
    # either way — so the priority band is what decides it.
    lead = make_group(
        "lead", members=[make_member("lead", is_team_lead=True)], available=[0]
    )
    inp = make_input(groups=[lead], shifts=[make_shift(0, size=4)])
    out = solve_with(inp, [seat_capacity.CONSTRAINT], preferences=PREFS)
    assert out.success
    assert team_lead_id(out.shifts[0]) == "lead"
    assert volunteer_ids(out.shifts[0]) == ()


def test_a_priority_band_outranks_every_seat_below_it():
    # One team-lead holder, two shifts, one allocation each. Shift 1 has
    # four ordinary Seats free and shift 0 only its Team lead Seat, so the
    # band has to beat a first ordinary Seat outright rather than being
    # traded off against how empty the other shift is.
    lead = make_group(
        "lead", members=[make_member("lead", is_team_lead=True)], available=[0, 1]
    )
    inp = make_input(
        groups=[lead],
        shifts=[
            dataclasses.replace(
                make_shift(0), shape=(Seat(role=TEAM_LEAD, count=1),)
            ),
            dataclasses.replace(
                make_shift(1), shape=(Seat(role=SERVICE_VOLUNTEER, count=4),)
            ),
        ],
        max_allocation_count=1,
    )
    out = solve_with(
        inp, [seat_capacity.CONSTRAINT, max_frequency.CONSTRAINT], preferences=PREFS
    )
    assert out.success
    assert team_lead_id(out.shifts[0]) == "lead"
    assert allocations_by_shift(out)[1] == ()


def test_every_role_spreads_across_shifts_not_just_the_biggest():
    # Two shifts asking for two Team leads each, two leads, one allocation
    # each: a 1-1 split scores higher than 2-0 because a Role's second Seat
    # is worth less than another shift's first. Team lead Seats used to take
    # a flat weight, under which the two splits tied and CP-SAT chose.
    leads = [
        make_group(
            f"lead{i}",
            members=[make_member(f"lead{i}", is_team_lead=True)],
            available=[0, 1],
        )
        for i in range(2)
    ]
    two_leads = (Seat(role=TEAM_LEAD, count=2),)
    inp = make_input(
        groups=leads,
        shifts=[
            dataclasses.replace(make_shift(0, size=0), shape=two_leads),
            dataclasses.replace(make_shift(1, size=0), shape=two_leads),
        ],
        max_allocation_count=1,
    )
    out = solve_with(
        inp, [seat_capacity.CONSTRAINT, max_frequency.CONSTRAINT], preferences=PREFS
    )
    assert out.success
    by_shift = allocations_by_shift(out)
    assert [len(keys) for keys in by_shift.values()] == [1, 1]


def test_no_reward_beyond_capacity():
    # With capacity active, fill stops at size even though more groups
    # are available; the preference must not fight the constraint.
    inp = make_input(
        groups=[make_group(f"g{i}", available=[0]) for i in range(4)],
        shifts=[make_shift(0, size=2)],
    )
    out = solve_with(inp, [seat_capacity.CONSTRAINT], preferences=PREFS)
    assert out.success
    assert len(allocations_by_shift(out)[0]) == 2
