"""The spread-males preference distributes males one-per-shift first."""

from __future__ import annotations

from conftest import make_group, make_input, make_member, make_shift, solve_with
from pyallocator.constraints import max_frequency
from pyallocator.preferences import spread_males

PREFS = [spread_males.PREFERENCE]


def _male(key: str, available) -> object:
    return make_group(
        key, members=[make_member(key, gender="Male")], available=available
    )


def _males_per_shift(inp, out) -> dict[int, int]:
    male_keys = {
        g.group_key
        for g in inp.groups
        if any(m.gender == "Male" for m in g.members)
    }
    return {
        s.index: sum(1 for key in s.allocated_group_keys if key in male_keys)
        for s in out.shifts
    }


def test_males_spread_one_per_shift():
    # 2 males, cap 1 each, 2 shifts: one male each beats two on one
    # shift (second male on a shift is worth half the first).
    inp = make_input(
        groups=[_male("m1", [0, 1]), _male("m2", [0, 1])],
        shifts=[make_shift(0), make_shift(1)],
        max_allocation_count=1,
    )
    out = solve_with(inp, [max_frequency.CONSTRAINT], preferences=PREFS)
    assert out.success
    assert _males_per_shift(inp, out) == {0: 1, 1: 1}


def test_no_males_no_terms():
    inp = make_input(
        groups=[make_group("f1", available=[0])],
        shifts=[make_shift(0)],
    )
    out = solve_with(inp, [], preferences=PREFS)
    assert out.success
    # No males anywhere: preference contributes nothing, empty rota is
    # optimal (objective 0).
    assert out.objective_value == 0
