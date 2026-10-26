"""The Rule Kind registry: which constraints an allocation runs with.

The fundamentals always apply. On top of them goes one constraint for each
Allocation Rule the input carries, built by the kind it names. This module is
the authority on what a kind does; Go's Rule Library is what an Organiser is
offered (ADR 0012).
"""

from __future__ import annotations

from conftest import allocations_by_shift, make_group, make_input, make_shift, rules_of, solve_with
from pyallocator.api import solve
from pyallocator.constraints import FUNDAMENTAL_CONSTRAINTS, RULE_KINDS, constraints_for
from pyallocator.domain import Rule


def names(constraints) -> list[str]:
    return [c.name for c in constraints]


# The four kinds, pinned by id. These strings are the contract: they are what a
# stored rule names, what Go's Rule Library offers (pinned from that side in
# allocationSettings_test.go) and what arrives in rules. Renaming one quietly
# stops every stored rule of that kind applying.
def test_the_registry_is_the_four_day_one_kinds():
    assert list(RULE_KINDS) == [
        "max_frequency",
        "shift_gap",
        "monthly_cap",
        "male_required",
    ]


# A rota without the fundamentals is not a rota, so no set of rules removes
# them.
def test_the_fundamentals_apply_whatever_the_rules():
    assert names(constraints_for(())) == names(FUNDAMENTAL_CONSTRAINTS)


# Rules apply in the order they arrive — the order they were added — and a kind
# can be added more than once, each with its own value and Scope.
def test_each_rule_adds_its_own_constraint():
    applied = constraints_for(
        (
            Rule(kind="shift_gap", volunteer_ids=("a",), value=1),
            Rule(kind="max_frequency", volunteer_ids=("a",), value=2),
            Rule(kind="shift_gap", volunteer_ids=("b",), value=3),
        )
    )

    assert names(applied) == names(FUNDAMENTAL_CONSTRAINTS) + [
        "shift_gap",
        "max_frequency",
        "shift_gap",
    ]


# A kind this build does not know adds nothing rather than raising. Stored rules
# outlive any one deploy, and one this build cannot apply must not stop a rota
# being allocated (ADR 0006).
def test_an_unknown_kind_is_ignored():
    applied = constraints_for((Rule(kind="phase_of_the_moon", value=3),))

    assert names(applied) == names(FUNDAMENTAL_CONSTRAINTS)


# solve() takes its constraint list from the input when the caller passes none.
def test_solve_applies_exactly_the_inputs_rules():
    inp = make_input(
        groups=[make_group("a", available=[0])],
        shifts=[make_shift(0, size=1)],
        rules=[("shift_gap", 1)],
    )

    out = solve(inp)

    assert out.diagnostics is not None
    assert list(out.diagnostics.constraints_applied) == names(FUNDAMENTAL_CONSTRAINTS) + [
        "shift_gap"
    ]


# A Volunteer Rule binds only the volunteers it names. Here a cap of one binds
# a, and b — whom the rule leaves out — works every shift.
def test_a_rule_binds_only_the_volunteers_it_names():
    inp = make_input(
        groups=[make_group("a", available=[0, 1, 2]), make_group("b", available=[0, 1, 2])],
        shifts=[make_shift(i) for i in range(3)],
        rules=[Rule(kind="max_frequency", volunteer_ids=("a",), value=1)],
    )

    out = solve_with(inp, rules_of(inp))

    assert out.success
    by_shift = allocations_by_shift(out)
    assert sum("a" in keys for keys in by_shift.values()) == 1
    assert all("b" in keys for keys in by_shift.values())


# A rule naming nobody in the problem binds nobody: the volunteers it was
# written for have left the roster.
def test_a_rule_naming_nobody_here_binds_nobody():
    inp = make_input(
        groups=[make_group("a", available=[0, 1])],
        shifts=[make_shift(0), make_shift(1)],
        rules=[Rule(kind="shift_gap", volunteer_ids=("left-the-roster",), value=1)],
    )

    out = solve_with(inp, rules_of(inp))

    assert out.success
    assert allocations_by_shift(out) == {0: ("a",), 1: ("a",)}
