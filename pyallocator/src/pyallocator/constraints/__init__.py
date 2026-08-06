"""Hard constraints: rules that forbid impossible allocations.

Add new constraint modules here.

FUNDAMENTAL_CONSTRAINTS are what makes a rota a rota. They always apply and
nothing can switch them off.

SWITCHABLE_CONSTRAINTS are the optional rules an admin chooses in the
Allocation Settings. This list is the **authority on which toggles exist**
(ADR 0006): the settings record stores answers keyed by the names here, and
Go offers an admin exactly these. A rule arriving or leaving is an edit to
this list and nothing else — no migration, and no default list anywhere but
here.

Tests inject subsets via model_builder.build().
"""

from __future__ import annotations

from typing import Iterable

from . import (
    availability,
    closed_shifts,
    grouping,
    male_required,
    max_frequency,
    no_back_to_back,
    no_duplicate_allocation,
    one_shift_per_month,
    preallocations,
    seat_capacity,
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

SWITCHABLE_CONSTRAINTS: list[Constraint] = [
    max_frequency.CONSTRAINT,
    male_required.CONSTRAINT,
    no_back_to_back.CONSTRAINT,
    one_shift_per_month.CONSTRAINT,
]


def constraints_for(enabled: Iterable[str]) -> list[Constraint]:
    """The constraint list for one run: the fundamentals, plus the switchable
    rules named in `enabled`, in registry order.

    A name this build does not know selects nothing, and is not an error.
    Stored answers outlive any one deploy, and a rule that has since been
    withdrawn must not be able to stop a rota being allocated (ADR 0006).

    Registry order rather than the caller's, so the model is built the same
    way whatever order the answers arrive in.
    """
    wanted = set(enabled)
    return FUNDAMENTAL_CONSTRAINTS + [
        c for c in SWITCHABLE_CONSTRAINTS if c.name in wanted
    ]


__all__ = [
    "Constraint",
    "FUNDAMENTAL_CONSTRAINTS",
    "SWITCHABLE_CONSTRAINTS",
    "Vars",
    "constraints_for",
]
