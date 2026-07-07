"""Solving with ONLY the shift-capacity constraint."""

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

ONLY = [shift_capacity.CONSTRAINT]


def test_shift_never_overfilled():
    inp = make_input(
        groups=[make_group(f"g{i}", available=[0]) for i in range(5)],
        shifts=[make_shift(0, size=3)],
    )
    out = solve_with(inp, ONLY)
    assert out.success
    assert len(allocations_by_shift(out)[0]) == 3


def test_couple_occupies_two_seats():
    couple = make_group(
        "couple",
        members=[make_member("a"), make_member("b")],
        available=[0],
    )
    single = make_group("single", available=[0])
    inp = make_input(groups=[couple, single], shifts=[make_shift(0, size=2)])
    out = solve_with(inp, ONLY)
    assert out.success
    # Size 2 fits the couple OR the single, never both (3 seats). Which
    # one wins is an objective tie, so only assert the capacity bound.
    assert allocations_by_shift(out)[0] in (("couple",), ("single",))


def test_team_lead_does_not_count_toward_size():
    tl_couple = make_group(
        "tl_couple",
        members=[make_member("lead", is_team_lead=True), make_member("partner")],
        available=[0],
    )
    single = make_group("single", available=[0])
    inp = make_input(groups=[tl_couple, single], shifts=[make_shift(0, size=2)])
    out = solve_with(inp, ONLY)
    assert out.success
    # tl_couple costs only 1 seat (partner), so both groups fit in size 2.
    assert set(allocations_by_shift(out)[0]) == {"single", "tl_couple"}


def test_custom_preallocations_consume_seats():
    inp = make_input(
        groups=[make_group(f"g{i}", available=[0]) for i in range(3)],
        shifts=[make_shift(0, size=3, custom_preallocations=["ext1", "ext2"])],
    )
    out = solve_with(inp, ONLY)
    assert out.success
    # Two of three seats taken by custom entries: one group fits.
    assert len(allocations_by_shift(out)[0]) == 1


def test_more_custom_preallocations_than_size_floors_at_zero():
    inp = make_input(
        groups=[make_group("g1", available=[0])],
        shifts=[make_shift(0, size=1, custom_preallocations=["e1", "e2", "e3"])],
    )
    out = solve_with(inp, ONLY)
    assert out.success
    assert allocations_by_shift(out)[0] == ()
