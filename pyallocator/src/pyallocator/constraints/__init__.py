"""Hard constraints: rules that forbid impossible allocations.

DEFAULT_CONSTRAINTS is the explicit, ordered production list — add new
constraint modules here. Tests inject subsets via model_builder.build().
"""

from . import (
    at_most_one_team_lead,
    availability,
    closed_shifts,
    male_required,
    max_frequency,
    no_back_to_back,
    no_duplicate_allocation,
    preallocations,
    shift_capacity,
)
from .base import AssignmentVars, Constraint

DEFAULT_CONSTRAINTS: list[Constraint] = [
    no_duplicate_allocation.CONSTRAINT,
    availability.CONSTRAINT,
    max_frequency.CONSTRAINT,
    shift_capacity.CONSTRAINT,
    at_most_one_team_lead.CONSTRAINT,
    male_required.CONSTRAINT,
    no_back_to_back.CONSTRAINT,
    closed_shifts.CONSTRAINT,
    preallocations.CONSTRAINT,
]

__all__ = ["AssignmentVars", "Constraint", "DEFAULT_CONSTRAINTS"]
