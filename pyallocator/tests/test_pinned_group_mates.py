"""A pin pins one person; their group-mates join whenever the rules allow
(ADR 0010, #234).

A group-mate is never forced — forcing them would let a pin make the solve
infeasible — but placing them alongside their pinned member outranks every
preference. That is a two-pass solve (solver.py), run only when a pinned
volunteer has a group-mate. These tests solve with every rule on and the
production objective, because "outranks every preference" is the claim.
"""

from __future__ import annotations

from conftest import (
    SERVICE_VOLUNTEER,
    make_group,
    make_input,
    make_member,
    make_shift,
    volunteer_ids,
)
from pyallocator.api import solve
from pyallocator.domain import HistoricalShift, Preallocation, Seat, ShiftSpec
from pyallocator.problem import Problem

# Every Volunteer Rule kind, at the values that were the old switchable rules:
# no cap to speak of, no back-to-back shifts, one shift a month.
EVERY_RULE = (("max_frequency", 99), ("shift_gap", 1), ("monthly_cap", 1))


def _couple(key: str, a: str, b: str, *, available, historical_count=0):
    return make_group(
        key,
        members=[make_member(a), make_member(b)],
        available=available,
        historical_count=historical_count,
    )


def _service_shift(index: int, seats: int, pinned: tuple[str, ...] = ()) -> ShiftSpec:
    # Only Service volunteer Seats, and no shift_requires to leave one open:
    # every Seat here is one somebody fills.
    return ShiftSpec(
        index=index,
        date=f"2026-0{7 + index}-13",
        closed=False,
        shape=(Seat(role=SERVICE_VOLUNTEER, count=seats),),
        preallocations=tuple(
            Preallocation(volunteer_id=v, custom="", role=SERVICE_VOLUNTEER)
            for v in pinned
        ),
    )


def _rules(*without: str) -> tuple[tuple[str, int], ...]:
    return tuple(r for r in EVERY_RULE if r[0] not in without)


def test_available_partner_joins_over_every_preference():
    # Bob's group has worked ten shifts and Carol none, so fairness would
    # give the one Seat Alice's pin leaves to Carol. Keeping the couple
    # together outranks it.
    inp = make_input(
        groups=[
            _couple("couple", "alice", "bob", available=[0], historical_count=10),
            make_group("carol", available=[0]),
        ],
        shifts=[_service_shift(0, seats=2, pinned=("alice",))],
        rules=_rules(),
    )
    out = solve(inp)
    assert out.success
    assert set(volunteer_ids(out.shifts[0])) == {"alice", "bob"}


def test_unavailable_partner_stays_home():
    inp = make_input(
        groups=[_couple("couple", "alice", "bob", available=[])],
        shifts=[_service_shift(0, seats=2, pinned=("alice",))],
        rules=_rules(),
    )
    out = solve(inp)
    assert out.success
    assert volunteer_ids(out.shifts[0]) == ("alice",)


def test_partner_with_no_seat_left_stays_home():
    # Once "the pin brings the group", and INFEASIBLE.
    inp = make_input(
        groups=[_couple("couple", "alice", "bob", available=[0])],
        shifts=[_service_shift(0, seats=1, pinned=("alice",))],
        rules=_rules(),
    )
    out = solve(inp)
    assert out.success
    assert volunteer_ids(out.shifts[0]) == ("alice",)


def test_partner_stays_home_rather_than_work_back_to_back():
    # The couple worked the previous rota's last shift. Alice's pin is
    # exempt from the spacing rule; Bob, an allocator choice, is not.
    inp = make_input(
        groups=[_couple("couple", "alice", "bob", available=[0])],
        shifts=[_service_shift(0, seats=2, pinned=("alice",))],
        historical_shifts=[HistoricalShift(date="2026-06-29", group_keys=("couple",))],
        rules=_rules("monthly_cap"),
    )
    out = solve(inp)
    assert out.success
    assert volunteer_ids(out.shifts[0]) == ("alice",)


def test_mates_competing_for_one_seat_one_joins():
    # Two pins leave one Seat between two partners: either may have it,
    # and the solver must not leave it to a stranger.
    inp = make_input(
        groups=[
            _couple("c1", "alice", "bob", available=[0], historical_count=10),
            _couple("c2", "dana", "eve", available=[0], historical_count=10),
            make_group("carol", available=[0]),
        ],
        shifts=[_service_shift(0, seats=3, pinned=("alice", "dana"))],
        rules=_rules(),
    )
    out = solve(inp)
    assert out.success
    placed = set(volunteer_ids(out.shifts[0]))
    assert {"alice", "dana"} <= placed
    assert len(placed & {"bob", "eve"}) == 1
    assert "carol" not in placed


def test_each_mate_counts_on_their_own():
    # A group of three with one Seat left after the pin: one mate joins
    # rather than the group being all-or-nothing.
    trio = make_group(
        "trio",
        members=[make_member("a"), make_member("b"), make_member("c")],
        available=[0],
        historical_count=10,
    )
    inp = make_input(
        groups=[trio, make_group("carol", available=[0])],
        shifts=[_service_shift(0, seats=2, pinned=("a",))],
        rules=_rules(),
    )
    out = solve(inp)
    assert out.success
    placed = set(volunteer_ids(out.shifts[0]))
    assert "a" in placed
    assert len(placed & {"b", "c"}) == 1


def test_group_stays_together_on_shifts_without_a_pin():
    inp = make_input(
        groups=[_couple("couple", "alice", "bob", available=[0, 2])],
        shifts=[
            _service_shift(0, seats=1, pinned=("alice",)),
            _service_shift(1, seats=4),
            _service_shift(2, seats=4),
        ],
        rules=_rules(),
    )
    out = solve(inp)
    assert out.success
    assert volunteer_ids(out.shifts[0]) == ("alice",)
    assert set(volunteer_ids(out.shifts[2])) in ({"alice", "bob"}, set())


def test_only_pinned_volunteers_with_mates_need_a_second_pass():
    # Every pin a lone volunteer or a whole group: nothing to keep
    # together, so the solve is the ordinary single pass.
    lone = make_input(
        groups=[
            make_group("g1", available=[0]),
            _couple("couple", "alice", "bob", available=[0]),
        ],
        shifts=[make_shift(0, preallocated_volunteer_ids=["g1", "alice", "bob"])],
    )
    assert Problem(lone).pinned_mates == ()

    paired = make_input(
        groups=[_couple("couple", "alice", "bob", available=[0])],
        shifts=[make_shift(0, preallocated_volunteer_ids=["alice"])],
    )
    assert Problem(paired).pinned_mates == (("bob", 0),)
