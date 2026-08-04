"""Hard constraints: rules that forbid impossible allocations.

Add new constraint modules here.

FUNDAMENTAL CONSTRAINTS are required for allocation.
ADDITIONAL_CONSTRAINTS improve allocation.
STRICT_CONSTRAINTS are regularly too difficult to satisfy.

Tests inject subsets via model_builder.build().
"""

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

ADDITIONAL_CONSTRAINTS: list[Constraint] = [
    max_frequency.CONSTRAINT,
    male_required.CONSTRAINT,
    no_back_to_back.CONSTRAINT,
]

STRICT_CONSTRAINTS: list[Constraint] = [
    one_shift_per_month.CONSTRAINT,
]

DEFAULT_CONSTRAINTS = FUNDAMENTAL_CONSTRAINTS + ADDITIONAL_CONSTRAINTS # + STRICT_CONSTRAINTS

__all__ = ["Constraint", "DEFAULT_CONSTRAINTS", "Vars"]
