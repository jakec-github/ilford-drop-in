"""Solving with ONLY the one-shift-per-month constraint."""

from __future__ import annotations

from conftest import allocations_by_shift, make_group, make_input, make_shift, solve_with
from pyallocator.domain import HistoricalShift
from pyallocator.constraints import one_shift_per_month, preallocations

ONLY = [one_shift_per_month.CONSTRAINT]


def test_same_month_yields_exactly_one_allocation():
    inp = make_input(
        groups=[make_group("g1", available=[0, 1])],
        shifts=[make_shift(0, date="2026-07-05"), make_shift(1, date="2026-07-12")],
    )
    out = solve_with(inp, ONLY)
    assert out.success
    total = sum(len(keys) for keys in allocations_by_shift(out).values())
    assert total == 1


def test_different_months_allow_one_each():
    inp = make_input(
        groups=[make_group("g1", available=[0, 1])],
        shifts=[make_shift(0, date="2026-07-26"), make_shift(1, date="2026-08-02")],
    )
    out = solve_with(inp, ONLY)
    assert out.success
    by_shift = allocations_by_shift(out)
    assert by_shift[0] == ("g1",)
    assert by_shift[1] == ("g1",)


def test_group_worked_this_month_in_history_is_blocked():
    inp = make_input(
        groups=[make_group("g1", available=[0])],
        shifts=[make_shift(0, date="2026-07-26")],
        historical_shifts=[
            HistoricalShift(date="2026-07-05", group_keys=("g1",)),
        ],
    )
    out = solve_with(inp, ONLY)
    assert out.success
    assert allocations_by_shift(out)[0] == ()  # already worked in July


def test_history_in_a_different_month_does_not_block():
    inp = make_input(
        groups=[make_group("g1", available=[0])],
        shifts=[make_shift(0, date="2026-07-05")],
        historical_shifts=[
            HistoricalShift(date="2026-06-28", group_keys=("g1",)),
        ],
    )
    out = solve_with(inp, ONLY)
    assert out.success
    assert allocations_by_shift(out)[0] == ("g1",)


# Pins are people's decisions, not the allocator's choices (ADR 0010): a
# pinned pair is exempt from the cap, but still uses up the month.
WITH_PINS = ONLY + [preallocations.CONSTRAINT]


def test_two_same_month_pins_are_honoured_and_the_month_gets_no_third():
    inp = make_input(
        groups=[make_group("g1", available=[0, 1, 2, 3])],
        shifts=[
            make_shift(0, date="2026-07-05", preallocated_volunteer_ids=["g1"]),
            make_shift(1, date="2026-07-12", preallocated_volunteer_ids=["g1"]),
            make_shift(2, date="2026-07-19"),
            make_shift(3, date="2026-08-02"),
        ],
    )
    out = solve_with(inp, WITH_PINS)
    assert out.success
    by_shift = allocations_by_shift(out)
    assert by_shift[0] == ("g1",)
    assert by_shift[1] == ("g1",)
    assert by_shift[2] == ()
    # Another month is untouched by the pins.
    assert by_shift[3] == ("g1",)


def test_allocator_gives_no_second_shift_in_a_pinned_month():
    inp = make_input(
        groups=[make_group("g1", available=[0, 1])],
        shifts=[
            make_shift(0, date="2026-07-05"),
            make_shift(1, date="2026-07-12", preallocated_volunteer_ids=["g1"]),
        ],
    )
    out = solve_with(inp, WITH_PINS)
    assert out.success
    assert allocations_by_shift(out) == {0: (), 1: ("g1",)}


def test_pin_in_a_month_already_worked_in_history_is_honoured():
    inp = make_input(
        groups=[make_group("g1", available=[0, 1])],
        shifts=[
            make_shift(0, date="2026-07-19", preallocated_volunteer_ids=["g1"]),
            make_shift(1, date="2026-07-26"),
        ],
        historical_shifts=[HistoricalShift(date="2026-07-05", group_keys=("g1",))],
    )
    out = solve_with(inp, WITH_PINS)
    assert out.success
    assert allocations_by_shift(out) == {0: ("g1",), 1: ()}
