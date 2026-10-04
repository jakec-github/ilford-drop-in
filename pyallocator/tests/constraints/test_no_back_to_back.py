"""Solving with ONLY the no-back-to-back constraint."""

from __future__ import annotations

from conftest import (
    allocations_by_shift,
    make_group,
    make_input,
    make_member,
    make_shift,
    solve_with,
)
from pyallocator.domain import HistoricalShift
from pyallocator.constraints import no_back_to_back, preallocations

ONLY = [no_back_to_back.CONSTRAINT]


def test_two_adjacent_shifts_yield_exactly_one_allocation():
    inp = make_input(
        groups=[make_group("g1", available=[0, 1])],
        shifts=[make_shift(0), make_shift(1)],
    )
    out = solve_with(inp, ONLY)
    assert out.success
    total = sum(len(keys) for keys in allocations_by_shift(out).values())
    assert total == 1


def test_alternating_pattern_allowed():
    inp = make_input(
        groups=[make_group("g1", available=[0, 1, 2, 3, 4])],
        shifts=[make_shift(i) for i in range(5)],
    )
    out = solve_with(inp, ONLY)
    assert out.success
    allocated = sorted(i for i, keys in allocations_by_shift(out).items() if keys)
    assert allocated == [0, 2, 4]


def test_group_on_last_historical_shift_blocked_from_shift_zero():
    inp = make_input(
        groups=[make_group("g1", available=[0, 1])],
        shifts=[make_shift(0), make_shift(1)],
        historical_shifts=[
            HistoricalShift(date="2026-06-29", group_keys=("other",)),
            HistoricalShift(date="2026-07-06", group_keys=("g1",)),
        ],
    )
    out = solve_with(inp, ONLY)
    assert out.success
    by_shift = allocations_by_shift(out)
    assert by_shift[0] == ()  # boundary with previous rota
    assert by_shift[1] == ("g1",)


def test_only_last_historical_shift_matters():
    inp = make_input(
        groups=[make_group("g1", available=[0])],
        shifts=[make_shift(0)],
        historical_shifts=[
            HistoricalShift(date="2026-06-29", group_keys=("g1",)),
            HistoricalShift(date="2026-07-06", group_keys=("other",)),
        ],
    )
    out = solve_with(inp, ONLY)
    assert out.success
    assert allocations_by_shift(out)[0] == ("g1",)


# Pins are people's decisions, not the allocator's choices (ADR 0010): a
# pinned pair is exempt from the rule, but still counts against the shifts
# either side of it.
WITH_PINS = ONLY + [preallocations.CONSTRAINT]


def test_pins_on_consecutive_shifts_are_honoured():
    inp = make_input(
        groups=[make_group("g1", available=[0, 1, 2, 3])],
        shifts=[
            make_shift(0),
            make_shift(1, preallocated_volunteer_ids=["g1"]),
            make_shift(2, preallocated_volunteer_ids=["g1"]),
            make_shift(3),
        ],
    )
    out = solve_with(inp, WITH_PINS)
    assert out.success
    by_shift = allocations_by_shift(out)
    assert by_shift[1] == ("g1",)
    assert by_shift[2] == ("g1",)
    # The allocator adds nothing next to the pins.
    assert by_shift[0] == ()
    assert by_shift[3] == ()


def test_allocator_stays_off_the_shifts_beside_a_pin():
    inp = make_input(
        groups=[make_group("g1", available=[0, 1, 2, 3, 4])],
        shifts=[
            make_shift(0),
            make_shift(1),
            make_shift(2, preallocated_volunteer_ids=["g1"]),
            make_shift(3),
            make_shift(4),
        ],
    )
    out = solve_with(inp, WITH_PINS)
    assert out.success
    by_shift = allocations_by_shift(out)
    assert by_shift[1] == ()
    assert by_shift[3] == ()
    assert by_shift[0] == ("g1",)
    assert by_shift[4] == ("g1",)


def test_pin_on_shift_zero_is_exempt_from_the_history_boundary():
    inp = make_input(
        groups=[make_group("g1", available=[0, 1, 2])],
        shifts=[
            make_shift(0, preallocated_volunteer_ids=["g1"]),
            make_shift(1),
            make_shift(2),
        ],
        historical_shifts=[HistoricalShift(date="2026-07-06", group_keys=("g1",))],
    )
    out = solve_with(inp, WITH_PINS)
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
        groups=[pair],
        shifts=[
            make_shift(0, preallocated_volunteer_ids=["a"]),
            make_shift(1, preallocated_volunteer_ids=["a"]),
        ],
    )
    out = solve_with(inp, WITH_PINS)
    assert out.success
    assert allocations_by_shift(out) == {0: ("pair",), 1: ("pair",)}
