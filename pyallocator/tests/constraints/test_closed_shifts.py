"""Solving with ONLY the closed-shifts constraint."""

from __future__ import annotations

from conftest import allocations_by_shift, make_group, make_input, make_shift, solve_with
from pyallocator.constraints import closed_shifts

ONLY = [closed_shifts.CONSTRAINT]


def test_closed_shift_gets_no_allocations():
    inp = make_input(
        groups=[make_group("g1", available=[0, 1]), make_group("g2", available=[0, 1])],
        shifts=[make_shift(0, closed=True), make_shift(1)],
    )
    out = solve_with(inp, ONLY)
    assert out.success
    by_shift = allocations_by_shift(out)
    assert by_shift[0] == ()
    assert set(by_shift[1]) == {"g1", "g2"}
    assert out.shifts[0].closed
