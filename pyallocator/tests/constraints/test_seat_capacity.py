"""Solving with ONLY the seat-capacity constraint.

This replaces the old shift_capacity and at_most_one_team_lead suites:
"at most one team lead" is now just the Team lead Role having one Seat.
The one behaviour that genuinely changed is
test_second_team_lead_takes_an_ordinary_seat.
"""

from __future__ import annotations

import dataclasses

from conftest import (
    TEAM_LEAD,
    SERVICE_VOLUNTEER,
    allocations_by_shift,
    make_group,
    make_input,
    make_member,
    make_shift,
    solve_with,
)
from pyallocator.constraints import preallocations, seat_capacity
from pyallocator.domain import Preallocation, Seat, ShiftSpec

ONLY = [seat_capacity.CONSTRAINT]


def pin_team_lead(shift: ShiftSpec, *volunteer_ids: str) -> ShiftSpec:
    """make_shift takes a single team-lead pin; these tests need two."""
    pins = tuple(
        Preallocation(volunteer_id=v, custom="", role=TEAM_LEAD)
        for v in volunteer_ids
    )
    return dataclasses.replace(shift, preallocations=shift.preallocations + pins)


def test_shift_never_overfilled():
    inp = make_input(
        groups=[make_group(f"g{i}", available=[0]) for i in range(5)],
        shifts=[make_shift(0, size=3)],
    )
    out = solve_with(inp, ONLY)
    assert out.success
    assert len(allocations_by_shift(out)[0]) == 3


def test_couple_occupies_two_seats():
    couple = make_group(
        "couple",
        members=[make_member("a"), make_member("b")],
        available=[0],
    )
    single = make_group("single", available=[0])
    inp = make_input(groups=[couple, single], shifts=[make_shift(0, size=2)])
    out = solve_with(inp, ONLY)
    assert out.success
    # Size 2 fits the couple OR the single, never both (3 seats). Which
    # one wins is an objective tie, so only assert the capacity bound.
    assert allocations_by_shift(out)[0] in (("couple",), ("single",))


def test_team_lead_seat_is_not_an_ordinary_seat():
    tl_couple = make_group(
        "tl_couple",
        members=[make_member("lead", is_team_lead=True), make_member("partner")],
        available=[0],
    )
    single = make_group("single", available=[0])
    inp = make_input(groups=[tl_couple, single], shifts=[make_shift(0, size=2)])
    out = solve_with(inp, ONLY)
    assert out.success
    # Three people, two ordinary Seats: both groups only fit if the lead
    # takes the Team lead Seat. That used to be seat_cost = 0.
    assert set(allocations_by_shift(out)[0]) == {"single", "tl_couple"}
    assert out.shifts[0].team_lead_id == "lead"


def test_second_team_lead_takes_an_ordinary_seat():
    # The accepted divergence from pre-Roles behaviour (#89). The old
    # at_most_one_team_lead counted per group, so a second team-lead
    # holder could not work the shift at all; now only the Seat is
    # capped, and they work it as a Service volunteer.
    inp = make_input(
        groups=[
            make_group("tl_a", available=[0], team_lead=True),
            make_group("tl_b", available=[0], team_lead=True),
        ],
        shifts=[make_shift(0, size=4)],
    )
    out = solve_with(inp, ONLY)
    assert out.success
    assert set(allocations_by_shift(out)[0]) == {"tl_a", "tl_b"}
    # Which Seat each takes is even_fill's business, not capacity's; all
    # this rule says is that both of them fit.


def test_two_volunteers_pinned_to_one_team_lead_seat_is_infeasible():
    inp = make_input(
        groups=[
            make_group("tl_a", available=[0], team_lead=True),
            make_group("tl_b", available=[0], team_lead=True),
        ],
        shifts=[pin_team_lead(make_shift(0), "tl_a", "tl_b")],
    )
    out = solve_with(inp, ONLY + [preallocations.CONSTRAINT])
    assert not out.success
    assert out.solver_status == "INFEASIBLE"


def test_role_max_caps_a_shape_that_asks_for_more():
    # The Shape is Go-computed from the Role's max today, so the two agree;
    # the unconditional ceiling is what keeps the Role's own limit true
    # once Shapes become editable per shift (S2). Here the Shape offers
    # three Team lead Seats and the Role's max of 1 still wins, so pinning
    # two people to it cannot be satisfied.
    overfull = dataclasses.replace(
        make_shift(0),
        shape=(Seat(role=TEAM_LEAD, count=3), Seat(role=SERVICE_VOLUNTEER, count=4)),
    )
    inp = make_input(
        groups=[
            make_group("tl_a", available=[0], team_lead=True),
            make_group("tl_b", available=[0], team_lead=True),
        ],
        shifts=[pin_team_lead(overfull, "tl_a", "tl_b")],
    )
    out = solve_with(inp, ONLY + [preallocations.CONSTRAINT])
    assert not out.success
    assert out.solver_status == "INFEASIBLE"


def test_custom_preallocations_consume_seats():
    inp = make_input(
        groups=[make_group(f"g{i}", available=[0]) for i in range(3)],
        shifts=[make_shift(0, size=3, custom_preallocations=["ext1", "ext2"])],
    )
    out = solve_with(inp, ONLY)
    assert out.success
    # Two of three seats taken by custom entries: one group fits.
    assert len(allocations_by_shift(out)[0]) == 1


def test_more_custom_preallocations_than_size_floors_at_zero():
    inp = make_input(
        groups=[make_group("g1", available=[0])],
        shifts=[make_shift(0, size=1, custom_preallocations=["e1", "e2", "e3"])],
    )
    out = solve_with(inp, ONLY)
    assert out.success
    assert allocations_by_shift(out)[0] == ()
