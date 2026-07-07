"""Solving with ONLY the no-back-to-back constraint."""

from __future__ import annotations

from conftest import allocations_by_shift, make_group, make_input, make_shift, solve_with
from pyallocator.domain import HistoricalShift
from pyallocator.constraints import no_back_to_back

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
