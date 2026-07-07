"""Solving with ONLY the availability constraint."""

from __future__ import annotations

from conftest import allocations_by_shift, make_group, make_input, make_shift, solve_with
from pyallocator.constraints import availability

ONLY = [availability.CONSTRAINT]


def test_group_never_allocated_to_unavailable_shift():
    inp = make_input(
        groups=[make_group("g1", available=[0, 2])],
        shifts=[make_shift(0), make_shift(1), make_shift(2)],
    )
    out = solve_with(inp, ONLY)
    assert out.success
    by_shift = allocations_by_shift(out)
    assert by_shift[0] == ("g1",)
    assert by_shift[1] == ()  # unavailable
    assert by_shift[2] == ("g1",)


def test_fully_unavailable_group_gets_nothing():
    inp = make_input(
        groups=[make_group("g1", available=[])],
        shifts=[make_shift(0), make_shift(1)],
    )
    out = solve_with(inp, ONLY)
    assert out.success
    assert all(keys == () for keys in allocations_by_shift(out).values())


def test_preallocation_overrides_availability():
    # g1 is NOT available for shift 1, but is preallocated there: the
    # availability constraint must exempt the preallocated pair.
    inp = make_input(
        groups=[make_group("g1", available=[0])],
        shifts=[make_shift(0), make_shift(1, preallocated_volunteer_ids=["g1"])],
    )
    out = solve_with(inp, ONLY)
    assert out.success
    # availability alone doesn't force the preallocation, but it must
    # ALLOW it — with maximise-allocations, the solver takes it.
    assert allocations_by_shift(out)[1] == ("g1",)
