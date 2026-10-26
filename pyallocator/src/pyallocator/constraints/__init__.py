"""Hard constraints: rules that forbid impossible allocations.

Add new constraint modules here.

FUNDAMENTAL_CONSTRAINTS are what makes a rota a rota. They always apply and
nothing can remove them.

RULE_KINDS are the Allocation Rules an Organiser adds from the Rule Library
(ADR 0012): each kind builds the constraint for one rule, from that rule's
value and the volunteers its Scope covers. This registry is the **authority on
what a kind does**; Go's model.RuleLibrary is what an Organiser is offered,
and a test on each side pins the two to the same ids.

Every kind here can be satisfied by leaving Seats empty, and exempts pins
(ADR 0010), so no set of rules can make a solve infeasible. A kind that
demands something of a Shift must carry an escape before it may join.

Tests inject subsets via model_builder.build().
"""

from __future__ import annotations

from typing import Callable, Iterable

from ..domain import Rule
from . import (
    availability,
    closed_shifts,
    grouping,
    male_required,
    max_frequency,
    monthly_cap,
    no_duplicate_allocation,
    preallocations,
    seat_capacity,
    shift_gap,
)
from .base import Constraint, Vars

FUNDAMENTAL_CONSTRAINTS: list[Constraint] = [
    no_duplicate_allocation.CONSTRAINT,
    grouping.CONSTRAINT,
    availability.CONSTRAINT,
    seat_capacity.CONSTRAINT,
    closed_shifts.CONSTRAINT,
    preallocations.CONSTRAINT,
]

RULE_KINDS: dict[str, Callable[[Rule], Constraint]] = {
    "max_frequency": max_frequency.MaxFrequencyConstraint,
    "shift_gap": shift_gap.ShiftGapConstraint,
    "monthly_cap": monthly_cap.MonthlyCapConstraint,
    "male_required": male_required.for_rule,
}


def rule_constraints(rules: Iterable[Rule]) -> list[Constraint]:
    """One constraint per rule, in the order the rules arrived.

    A kind this build does not have makes nothing, and is not an error.
    Stored rules outlive any one deploy, and one this build cannot apply
    must not be able to stop a rota being allocated (ADR 0006).
    """
    return [RULE_KINDS[r.kind](r) for r in rules if r.kind in RULE_KINDS]


def constraints_for(rules: Iterable[Rule]) -> list[Constraint]:
    """The constraint list for one run: the fundamentals, then the rules'."""
    return FUNDAMENTAL_CONSTRAINTS + rule_constraints(rules)


__all__ = [
    "Constraint",
    "FUNDAMENTAL_CONSTRAINTS",
    "RULE_KINDS",
    "Vars",
    "constraints_for",
    "rule_constraints",
]
