"""Solving with ONLY the grouping constraint (which solve_with always
includes): couples/families work a shift together or not at all."""

from __future__ import annotations

from conftest import (
    allocations_by_shift,
    make_group,
    make_input,
    make_member,
    make_shift,
    solve_with,
)
from pyallocator.constraints import shift_capacity


def test_members_always_allocated_together():
    couple = make_group(
        "couple", members=[make_member("a"), make_member("b")], available=[0, 1]
    )
    inp = make_input(groups=[couple], shifts=[make_shift(0), make_shift(1)])
    out = solve_with(inp)
    assert out.success
    for shift in out.shifts:
        # Either the whole couple is on the shift or nobody is.
        assert set(shift.volunteer_ids) in ({"a", "b"}, set())


def test_group_too_big_for_shift_stays_off():
    # Without grouping, capacity alone would happily place one member
    # of the couple on the size-1 shift.
    couple = make_group(
        "couple", members=[make_member("a"), make_member("b")], available=[0]
    )
    inp = make_input(groups=[couple], shifts=[make_shift(0, size=1)])
    out = solve_with(inp, [shift_capacity.CONSTRAINT])
    assert out.success
    assert allocations_by_shift(out)[0] == ()
    assert out.shifts[0].volunteer_ids == ()
