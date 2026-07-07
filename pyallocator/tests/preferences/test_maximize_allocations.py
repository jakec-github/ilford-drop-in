"""The placeholder objective rewards every allocation equally."""

from __future__ import annotations

from conftest import allocations_by_shift, make_group, make_input, make_shift, solve_with
from pyallocator.preferences import maximize_allocations

PREFS = [maximize_allocations.PREFERENCE]


def test_everything_allocated_without_constraints():
    inp = make_input(
        groups=[make_group("g1", available=[0]), make_group("g2", available=[1])],
        shifts=[make_shift(0), make_shift(1)],
    )
    out = solve_with(inp, [], preferences=PREFS)
    assert out.success
    # No constraints: every (group, shift) pair is allocated.
    assert out.objective_value == 2 * 2
    by_shift = allocations_by_shift(out)
    assert set(by_shift[0]) == {"g1", "g2"}
    assert set(by_shift[1]) == {"g1", "g2"}


def test_no_preferences_yields_empty_but_feasible_rota():
    inp = make_input(
        groups=[make_group("g1", available=[0])],
        shifts=[make_shift(0)],
    )
    out = solve_with(inp, [], preferences=[])
    assert out.success
    assert out.objective_value == 0
