"""Hard constraints: rules that forbid impossible allocations.

Add new constraint modules here.

FUNDAMENTAL CONSTRAINTS are required for allocation.
ADDITIONAL_CONSTRAINTS improve allocation.
STRICT_CONSTRAINTS are regularly too difficult to satisfy.

Tests inject subsets via model_builder.build().
"""

from . import (
    at_most_one_team_lead,
    availability,
    closed_shifts,
    grouping,
    male_required,
    max_frequency,
    no_back_to_back,
    no_duplicate_allocation,
    one_shift_per_month,
    preallocations,
    shift_capacity,
)
from .base import AssignmentVars, Constraint

FUNDAMENTAL_CONSTRAINTS: list[Constraint] = [
    no_duplicate_allocation.CONSTRAINT,
    grouping.CONSTRAINT,
    availability.CONSTRAINT,
    shift_capacity.CONSTRAINT,
    at_most_one_team_lead.CONSTRAINT,
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

__all__ = ["AssignmentVars", "Constraint", "DEFAULT_CONSTRAINTS"]
