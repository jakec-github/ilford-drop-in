"""Solving with ONLY a max-frequency rule.

The rule's value arrives as a count of shifts: Go turns the Organiser's
percentage into one against the rota's length (model.MaxAllocationCount).
"""

from __future__ import annotations

from conftest import allocations_by_shift, make_group, make_input, make_shift, rules_of, solve_with
from pyallocator.constraints import preallocations

def test_group_capped_at_the_rules_cap():
    inp = make_input(
        groups=[make_group("g1", available=[0, 1, 2, 3])],
        shifts=[make_shift(i) for i in range(4)],
        rules=[("max_frequency", 2)],
    )
    out = solve_with(inp, rules_of(inp))
    assert out.success
    total = sum(len(keys) for keys in allocations_by_shift(out).values())
    assert total == 2


def test_zero_cap_means_no_allocations():
    inp = make_input(
        groups=[make_group("g1", available=[0, 1])],
        shifts=[make_shift(0), make_shift(1)],
        rules=[("max_frequency", 0)],
    )
    out = solve_with(inp, rules_of(inp))
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
        rules=[("max_frequency", 1)],
    )
    out = solve_with(inp, rules_of(inp) + [preallocations.CONSTRAINT])
    assert out.success
    by_shift = allocations_by_shift(out)
    assert by_shift[0] == ("g1",)
    assert by_shift[1] == ()
    assert by_shift[2] == ()


def test_pins_past_the_cap_are_honoured_and_nothing_more_is_added():
    inp = make_input(
        groups=[make_group("g1", available=[0, 1, 2, 3])],
        shifts=[
            make_shift(0, preallocated_volunteer_ids=["g1"]),
            make_shift(1),
            make_shift(2, preallocated_volunteer_ids=["g1"]),
            make_shift(3),
        ],
        rules=[("max_frequency", 1)],
    )
    out = solve_with(inp, rules_of(inp) + [preallocations.CONSTRAINT])
    assert out.success
    assert allocations_by_shift(out) == {0: ("g1",), 1: (), 2: ("g1",), 3: ()}


def test_pins_past_a_zero_cap_are_honoured():
    inp = make_input(
        groups=[make_group("g1", available=[0, 1])],
        shifts=[make_shift(0, preallocated_volunteer_ids=["g1"]), make_shift(1)],
        rules=[("max_frequency", 0)],
    )
    out = solve_with(inp, rules_of(inp) + [preallocations.CONSTRAINT])
    assert out.success
    assert allocations_by_shift(out) == {0: ("g1",), 1: ()}
