"""The switchable-constraint registry: which rules an allocation runs with.

The fundamentals always apply. The switchable ones apply only when the
caller names them, which is how an admin's Allocation Settings reach the
solver — Go sends the enabled list, and this module is the authority on
what those names may mean.
"""

from __future__ import annotations

from conftest import make_group, make_input, make_shift
from pyallocator.api import solve
from pyallocator.constraints import (
    FUNDAMENTAL_CONSTRAINTS,
    SWITCHABLE_CONSTRAINTS,
    constraints_for,
)


def names(constraints) -> list[str]:
    return [c.name for c in constraints]


# The four switchable rules, pinned by name. These strings are the contract:
# they are what the settings record stores, what Go's registry offers an admin
# and what arrives in enabled_constraints. Renaming one here silently turns it
# off on every deployment that had it on, so it takes a data migration.
def test_the_switchable_registry_is_the_four_optional_rules():
    assert names(SWITCHABLE_CONSTRAINTS) == [
        "max_frequency",
        "male_required",
        "no_back_to_back",
        "one_shift_per_month",
    ]


# A rota without the fundamentals is not a rota, so nothing can switch them
# off — not naming them is not a way to ask for that.
def test_the_fundamentals_apply_whatever_is_enabled():
    assert names(constraints_for(())) == names(FUNDAMENTAL_CONSTRAINTS)


def test_enabled_constraints_are_added_in_registry_order():
    applied = constraints_for(("no_back_to_back", "max_frequency"))

    assert names(applied) == names(FUNDAMENTAL_CONSTRAINTS) + [
        "max_frequency",
        "no_back_to_back",
    ]


# A name this build does not know selects nothing rather than raising. The
# stored answers outlive any one deploy, and a constraint that has been
# withdrawn must not be able to stop a rota being allocated.
def test_an_unknown_name_is_ignored():
    applied = constraints_for(("max_frequency", "invented_rule"))

    assert names(applied) == names(FUNDAMENTAL_CONSTRAINTS) + ["max_frequency"]


# solve() takes its constraint list from the input when the caller does not
# pass one. This is what "Python no longer owns a default list" means: with no
# enabled constraints in the input, only the fundamentals run.
def test_solve_applies_exactly_the_inputs_enabled_constraints():
    inp = make_input(
        groups=[make_group("a", available=[0])],
        shifts=[make_shift(0, size=1)],
        enabled_constraints=("no_back_to_back",),
    )

    out = solve(inp)

    assert out.diagnostics is not None
    assert names(FUNDAMENTAL_CONSTRAINTS) + ["no_back_to_back"] == list(
        out.diagnostics.constraints_applied
    )


def test_solve_with_nothing_enabled_applies_only_the_fundamentals():
    inp = make_input(
        groups=[make_group("a", available=[0])],
        shifts=[make_shift(0, size=1)],
    )

    out = solve(inp)

    assert out.diagnostics is not None
    assert list(out.diagnostics.constraints_applied) == names(FUNDAMENTAL_CONSTRAINTS)
