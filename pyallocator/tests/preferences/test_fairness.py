"""The fairness preference favours groups with fewer past allocations."""

from __future__ import annotations

from conftest import allocations_by_shift, make_group, make_input, make_shift, solve_with
from pyallocator.constraints import shift_capacity
from pyallocator.preferences import fairness

PREFS = [fairness.PREFERENCE]


def test_fresh_group_beats_heavily_allocated_group():
    # One seat, two candidates: the group with no history wins over the
    # one with 5 historical allocations.
    inp = make_input(
        groups=[
            make_group("veteran", available=[0], historical_count=5),
            make_group("fresh", available=[0], historical_count=0),
        ],
        shifts=[make_shift(0, size=1)],
    )
    out = solve_with(inp, [shift_capacity.CONSTRAINT], preferences=PREFS)
    assert out.success
    assert allocations_by_shift(out)[0] == ("fresh",)


def test_repeat_allocations_diminish_within_the_rota():
    # 2 shifts, 2 seats total (size 1 each), both groups available for
    # both: giving each group one shift beats giving one group both,
    # because a group's 2nd allocation is worth half its 1st.
    inp = make_input(
        groups=[
            make_group("g1", available=[0, 1]),
            make_group("g2", available=[0, 1]),
        ],
        shifts=[make_shift(0, size=1), make_shift(1, size=1)],
    )
    out = solve_with(inp, [shift_capacity.CONSTRAINT], preferences=PREFS)
    assert out.success
    by_shift = allocations_by_shift(out)
    allocated = [key for keys in by_shift.values() for key in keys]
    assert sorted(allocated) == ["g1", "g2"]
