"""The even-fill preference spreads scarce volunteers across shifts."""

from __future__ import annotations

from conftest import allocations_by_shift, make_group, make_input, make_shift, solve_with
from pyallocator.constraints import max_frequency, shift_capacity
from pyallocator.preferences import even_fill

PREFS = [even_fill.PREFERENCE]


def test_scarce_volunteers_spread_across_shifts():
    # 3 volunteers, cap 1 each, 2 shifts: a 2-1 split scores higher than
    # 3-0 because a shift's 3rd seat is worth less than another's 1st.
    inp = make_input(
        groups=[make_group(f"g{i}", available=[0, 1]) for i in range(3)],
        shifts=[make_shift(0, size=4), make_shift(1, size=4)],
        max_allocation_count=1,
    )
    out = solve_with(inp, [max_frequency.CONSTRAINT], preferences=PREFS)
    assert out.success
    by_shift = allocations_by_shift(out)
    assert sorted(len(keys) for keys in by_shift.values()) == [1, 2]


def test_custom_preallocations_count_as_early_seats():
    # Shift 0 already has a custom entry occupying seat 1, so the single
    # available volunteer is worth more on shift 1 (seat 1) than on
    # shift 0 (seat 2).
    inp = make_input(
        groups=[make_group("g1", available=[0, 1])],
        shifts=[
            make_shift(0, size=2, custom_preallocations=["external"]),
            make_shift(1, size=2),
        ],
        max_allocation_count=1,
    )
    out = solve_with(inp, [max_frequency.CONSTRAINT], preferences=PREFS)
    assert out.success
    assert allocations_by_shift(out)[1] == ("g1",)


def test_no_reward_beyond_capacity():
    # With capacity active, fill stops at size even though more groups
    # are available; the preference must not fight the constraint.
    inp = make_input(
        groups=[make_group(f"g{i}", available=[0]) for i in range(4)],
        shifts=[make_shift(0, size=2)],
    )
    out = solve_with(inp, [shift_capacity.CONSTRAINT], preferences=PREFS)
    assert out.success
    assert len(allocations_by_shift(out)[0]) == 2
