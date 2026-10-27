"""Solving with ONLY a shift gap rule.

A gap of one is what no_back_to_back was, so the tests written for that rule
are kept with it, and the wider gaps follow.
"""

from __future__ import annotations

from conftest import (
    allocations_by_shift,
    make_group,
    make_input,
    make_member,
    make_shift,
    rules_of,
    solve_with,
)
from pyallocator.domain import HistoricalShift
from pyallocator.constraints import closed_shifts, pins



RULE = ("shift_gap", 1)


def test_two_adjacent_shifts_yield_exactly_one_allocation():
    inp = make_input(
        rules=[RULE],
        groups=[make_group("g1", available=[0, 1])],
        shifts=[make_shift(0), make_shift(1)],
    )
    out = solve_with(inp, rules_of(inp))
    assert out.success
    total = sum(len(keys) for keys in allocations_by_shift(out).values())
    assert total == 1


def test_alternating_pattern_allowed():
    inp = make_input(
        rules=[RULE],
        groups=[make_group("g1", available=[0, 1, 2, 3, 4])],
        shifts=[make_shift(i) for i in range(5)],
    )
    out = solve_with(inp, rules_of(inp))
    assert out.success
    allocated = sorted(i for i, keys in allocations_by_shift(out).items() if keys)
    assert allocated == [0, 2, 4]


def test_group_on_last_historical_shift_blocked_from_shift_zero():
    inp = make_input(
        rules=[RULE],
        groups=[make_group("g1", available=[0, 1])],
        shifts=[make_shift(0), make_shift(1)],
        historical_shifts=[
            HistoricalShift(date="2026-06-29", group_keys=("other",)),
            HistoricalShift(date="2026-07-06", group_keys=("g1",)),
        ],
    )
    out = solve_with(inp, rules_of(inp))
    assert out.success
    by_shift = allocations_by_shift(out)
    assert by_shift[0] == ()  # boundary with previous rota
    assert by_shift[1] == ("g1",)


def test_only_last_historical_shift_matters():
    inp = make_input(
        rules=[RULE],
        groups=[make_group("g1", available=[0])],
        shifts=[make_shift(0)],
        historical_shifts=[
            HistoricalShift(date="2026-06-29", group_keys=("g1",)),
            HistoricalShift(date="2026-07-06", group_keys=("other",)),
        ],
    )
    out = solve_with(inp, rules_of(inp))
    assert out.success
    assert allocations_by_shift(out)[0] == ("g1",)


# Pins are people's decisions, not the allocator's choices (ADR 0010): a
# pinned pair is exempt from the rule, but still counts against the shifts
# either side of it.
WITH_PINS = [pins.CONSTRAINT]


def test_pins_on_consecutive_shifts_are_honoured():
    inp = make_input(
        rules=[RULE],
        groups=[make_group("g1", available=[0, 1, 2, 3])],
        shifts=[
            make_shift(0),
            make_shift(1, pinned_volunteer_ids=["g1"]),
            make_shift(2, pinned_volunteer_ids=["g1"]),
            make_shift(3),
        ],
    )
    out = solve_with(inp, rules_of(inp) + WITH_PINS)
    assert out.success
    by_shift = allocations_by_shift(out)
    assert by_shift[1] == ("g1",)
    assert by_shift[2] == ("g1",)
    # The allocator adds nothing next to the pins.
    assert by_shift[0] == ()
    assert by_shift[3] == ()


def test_allocator_stays_off_the_shifts_beside_a_pin():
    inp = make_input(
        rules=[RULE],
        groups=[make_group("g1", available=[0, 1, 2, 3, 4])],
        shifts=[
            make_shift(0),
            make_shift(1),
            make_shift(2, pinned_volunteer_ids=["g1"]),
            make_shift(3),
            make_shift(4),
        ],
    )
    out = solve_with(inp, rules_of(inp) + WITH_PINS)
    assert out.success
    by_shift = allocations_by_shift(out)
    assert by_shift[1] == ()
    assert by_shift[3] == ()
    assert by_shift[0] == ("g1",)
    assert by_shift[4] == ("g1",)


def test_pin_on_shift_zero_is_exempt_from_the_history_boundary():
    inp = make_input(
        rules=[RULE],
        groups=[make_group("g1", available=[0, 1, 2])],
        shifts=[
            make_shift(0, pinned_volunteer_ids=["g1"]),
            make_shift(1),
            make_shift(2),
        ],
        historical_shifts=[HistoricalShift(date="2026-07-06", group_keys=("g1",))],
    )
    out = solve_with(inp, rules_of(inp) + WITH_PINS)
    assert out.success
    by_shift = allocations_by_shift(out)
    assert by_shift[0] == ("g1",)
    assert by_shift[1] == ()
    assert by_shift[2] == ("g1",)


def test_exemption_covers_every_group_member_the_pin_forces():
    pair = make_group(
        "pair", members=[make_member("a"), make_member("b")], available=[0, 1]
    )
    inp = make_input(
        rules=[RULE],
        groups=[pair],
        shifts=[
            make_shift(0, pinned_volunteer_ids=["a"]),
            make_shift(1, pinned_volunteer_ids=["a"]),
        ],
    )
    out = solve_with(inp, rules_of(inp) + WITH_PINS)
    assert out.success
    assert allocations_by_shift(out) == {0: ("pair",), 1: ("pair",)}


# Wider gaps. The gap counts shifts by index, so in any run of gap + 1
# consecutive shifts a volunteer works at most one.


def worked(out) -> list[int]:
    return sorted(i for i, keys in allocations_by_shift(out).items() if keys)


def test_a_gap_of_two_leaves_two_shifts_off_between_any_two_worked():
    inp = make_input(
        groups=[make_group("g1", available=list(range(7)))],
        shifts=[make_shift(i) for i in range(7)],
        rules=[("shift_gap", 2)],
    )
    out = solve_with(inp, rules_of(inp))
    assert out.success
    assert worked(out) == [0, 3, 6]


def test_a_gap_longer_than_the_rota_allows_one_shift():
    inp = make_input(
        groups=[make_group("g1", available=[0, 1, 2])],
        shifts=[make_shift(i) for i in range(3)],
        rules=[("shift_gap", 5)],
    )
    out = solve_with(inp, rules_of(inp))
    assert out.success
    assert len(worked(out)) == 1


# A Closed Shift is still a week nobody worked, so it counts towards the gap.
def test_a_closed_shift_counts_as_a_shift_off():
    inp = make_input(
        groups=[make_group("g1", available=[0, 1, 2])],
        shifts=[make_shift(0), make_shift(1, closed=True), make_shift(2)],
        rules=[RULE],
    )
    out = solve_with(inp, rules_of(inp) + [closed_shifts.CONSTRAINT])
    assert out.success
    assert worked(out) == [0, 2]


# The previous rota counts as far back as the gap reaches: a group that worked
# its second-to-last shift is barred from this rota's first under a gap of two.
def test_history_reaches_back_as_far_as_the_gap():
    inp = make_input(
        groups=[make_group("g1", available=[0, 1, 2, 3])],
        shifts=[make_shift(i) for i in range(4)],
        historical_shifts=[
            HistoricalShift(date="2026-06-29", group_keys=("g1",)),
            HistoricalShift(date="2026-07-06", group_keys=("other",)),
        ],
        rules=[("shift_gap", 2)],
    )
    out = solve_with(inp, rules_of(inp))
    assert out.success
    # Without the history g1 would work shifts 0 and 3.
    assert 0 not in worked(out)
    assert len(worked(out)) == 1


def test_history_bars_as_many_shifts_as_the_gap_leaves():
    inp = make_input(
        groups=[make_group("g1", available=[0, 1, 2])],
        shifts=[make_shift(i) for i in range(3)],
        historical_shifts=[HistoricalShift(date="2026-07-06", group_keys=("g1",))],
        rules=[("shift_gap", 2)],
    )
    out = solve_with(inp, rules_of(inp))
    assert out.success
    assert worked(out) == [2]


# History carries the previous rota's Closed Shifts too, as shifts nobody
# worked, so one at the end of it is already a shift off.
def test_a_closed_shift_ending_the_previous_rota_counts_as_a_shift_off():
    inp = make_input(
        groups=[make_group("g1", available=[0, 1, 2])],
        shifts=[make_shift(i) for i in range(3)],
        historical_shifts=[
            HistoricalShift(date="2026-06-29", group_keys=("g1",)),
            HistoricalShift(date="2026-07-06", group_keys=()),
        ],
        rules=[RULE],
    )
    out = solve_with(inp, rules_of(inp))
    assert out.success
    assert worked(out) == [0, 2]


def test_pins_inside_a_wide_gap_are_honoured_and_bar_their_neighbours():
    inp = make_input(
        groups=[make_group("g1", available=list(range(6)))],
        shifts=[
            make_shift(0),
            make_shift(1, pinned_volunteer_ids=["g1"]),
            make_shift(2),
            make_shift(3, pinned_volunteer_ids=["g1"]),
            make_shift(4),
            make_shift(5),
        ],
        rules=[("shift_gap", 2)],
    )
    out = solve_with(inp, rules_of(inp) + WITH_PINS)
    assert out.success
    assert worked(out) == [1, 3]
