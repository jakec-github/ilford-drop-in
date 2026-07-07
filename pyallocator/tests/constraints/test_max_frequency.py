"""Solving with ONLY the max-frequency constraint."""

from __future__ import annotations

from conftest import allocations_by_shift, make_group, make_input, make_shift, solve_with
from pyallocator.constraints import max_frequency, preallocations

ONLY = [max_frequency.CONSTRAINT]


def test_group_capped_at_max_allocation_count():
    inp = make_input(
        groups=[make_group("g1", available=[0, 1, 2, 3])],
        shifts=[make_shift(i) for i in range(4)],
        max_allocation_count=2,
    )
    out = solve_with(inp, ONLY)
    assert out.success
    total = sum(len(keys) for keys in allocations_by_shift(out).values())
    assert total == 2


def test_zero_cap_means_no_allocations():
    inp = make_input(
        groups=[make_group("g1", available=[0, 1])],
        shifts=[make_shift(0), make_shift(1)],
        max_allocation_count=0,
    )
    out = solve_with(inp, ONLY)
    assert out.success
    assert all(keys == () for keys in allocations_by_shift(out).values())


def test_preallocations_count_toward_cap():
    # Cap of 1 with a preallocation on shift 0: the preallocation uses
    # the whole budget, so nothing else can be allocated.
    inp = make_input(
        groups=[make_group("g1", available=[0, 1, 2])],
        shifts=[
            make_shift(0, preallocated_volunteer_ids=["g1"]),
            make_shift(1),
            make_shift(2),
        ],
        max_allocation_count=1,
    )
    out = solve_with(inp, ONLY + [preallocations.CONSTRAINT])
    assert out.success
    by_shift = allocations_by_shift(out)
    assert by_shift[0] == ("g1",)
    assert by_shift[1] == ()
    assert by_shift[2] == ()
