"""Solving with ONLY a monthly cap rule.

A cap of one is what one_shift_per_month was, so the tests written for that
rule are kept with it, and the higher caps follow.
"""

from __future__ import annotations

from conftest import allocations_by_shift, make_group, make_input, make_shift, rules_of, solve_with
from pyallocator.domain import HistoricalShift
from pyallocator.constraints import preallocations



RULE = ("monthly_cap", 1)


def test_same_month_yields_exactly_one_allocation():
    inp = make_input(
        rules=[RULE],
        groups=[make_group("g1", available=[0, 1])],
        shifts=[make_shift(0, date="2026-07-05"), make_shift(1, date="2026-07-12")],
    )
    out = solve_with(inp, rules_of(inp))
    assert out.success
    total = sum(len(keys) for keys in allocations_by_shift(out).values())
    assert total == 1


def test_different_months_allow_one_each():
    inp = make_input(
        rules=[RULE],
        groups=[make_group("g1", available=[0, 1])],
        shifts=[make_shift(0, date="2026-07-26"), make_shift(1, date="2026-08-02")],
    )
    out = solve_with(inp, rules_of(inp))
    assert out.success
    by_shift = allocations_by_shift(out)
    assert by_shift[0] == ("g1",)
    assert by_shift[1] == ("g1",)


def test_group_worked_this_month_in_history_is_blocked():
    inp = make_input(
        rules=[RULE],
        groups=[make_group("g1", available=[0])],
        shifts=[make_shift(0, date="2026-07-26")],
        historical_shifts=[
            HistoricalShift(date="2026-07-05", group_keys=("g1",)),
        ],
    )
    out = solve_with(inp, rules_of(inp))
    assert out.success
    assert allocations_by_shift(out)[0] == ()  # already worked in July


def test_history_in_a_different_month_does_not_block():
    inp = make_input(
        rules=[RULE],
        groups=[make_group("g1", available=[0])],
        shifts=[make_shift(0, date="2026-07-05")],
        historical_shifts=[
            HistoricalShift(date="2026-06-28", group_keys=("g1",)),
        ],
    )
    out = solve_with(inp, rules_of(inp))
    assert out.success
    assert allocations_by_shift(out)[0] == ("g1",)


# Pins are people's decisions, not the allocator's choices (ADR 0010): a
# pinned pair is exempt from the cap, but still uses up the month.
WITH_PINS = [preallocations.CONSTRAINT]


def test_two_same_month_pins_are_honoured_and_the_month_gets_no_third():
    inp = make_input(
        rules=[RULE],
        groups=[make_group("g1", available=[0, 1, 2, 3])],
        shifts=[
            make_shift(0, date="2026-07-05", preallocated_volunteer_ids=["g1"]),
            make_shift(1, date="2026-07-12", preallocated_volunteer_ids=["g1"]),
            make_shift(2, date="2026-07-19"),
            make_shift(3, date="2026-08-02"),
        ],
    )
    out = solve_with(inp, rules_of(inp) + WITH_PINS)
    assert out.success
    by_shift = allocations_by_shift(out)
    assert by_shift[0] == ("g1",)
    assert by_shift[1] == ("g1",)
    assert by_shift[2] == ()
    # Another month is untouched by the pins.
    assert by_shift[3] == ("g1",)


def test_allocator_gives_no_second_shift_in_a_pinned_month():
    inp = make_input(
        rules=[RULE],
        groups=[make_group("g1", available=[0, 1])],
        shifts=[
            make_shift(0, date="2026-07-05"),
            make_shift(1, date="2026-07-12", preallocated_volunteer_ids=["g1"]),
        ],
    )
    out = solve_with(inp, rules_of(inp) + WITH_PINS)
    assert out.success
    assert allocations_by_shift(out) == {0: (), 1: ("g1",)}


def test_pin_in_a_month_already_worked_in_history_is_honoured():
    inp = make_input(
        rules=[RULE],
        groups=[make_group("g1", available=[0, 1])],
        shifts=[
            make_shift(0, date="2026-07-19", preallocated_volunteer_ids=["g1"]),
            make_shift(1, date="2026-07-26"),
        ],
        historical_shifts=[HistoricalShift(date="2026-07-05", group_keys=("g1",))],
    )
    out = solve_with(inp, rules_of(inp) + WITH_PINS)
    assert out.success
    assert allocations_by_shift(out) == {0: ("g1",), 1: ()}


# Higher caps.


def july_shifts(n: int) -> list:
    return [make_shift(i, date=f"2026-07-{5 + 7 * i:02d}") for i in range(n)]


def test_a_cap_of_two_allows_two_shifts_in_a_month():
    inp = make_input(
        groups=[make_group("g1", available=[0, 1, 2, 3])],
        shifts=july_shifts(4),
        rules=[("monthly_cap", 2)],
    )
    out = solve_with(inp, rules_of(inp))
    assert out.success
    assert sum(len(keys) for keys in allocations_by_shift(out).values()) == 2


# History counts shift by shift, not just as a month touched: one shift worked
# in July leaves one under a cap of two, and two leave none.
def test_history_counts_every_shift_already_worked_that_month():
    for history, expected in ((1, 1), (2, 0)):
        inp = make_input(
            groups=[make_group("g1", available=[0, 1, 2])],
            shifts=[make_shift(i, date=f"2026-07-{19 + 3 * i:02d}") for i in range(3)],
            historical_shifts=[
                HistoricalShift(date=f"2026-07-{1 + 7 * k:02d}", group_keys=("g1",))
                for k in range(history)
            ],
            rules=[("monthly_cap", 2)],
        )
        out = solve_with(inp, rules_of(inp))
        assert out.success
        assert sum(len(keys) for keys in allocations_by_shift(out).values()) == expected
