"""Soft preferences: weighted objective terms traded off by the solver.

DEFAULT_PREFERENCES is the explicit, ordered production list — add new
preference modules here. Tests inject subsets via model_builder.build().
"""

from . import at_most_one_team_lead, maximize_allocations
from .base import ObjectiveTerm, Preference

DEFAULT_PREFERENCES: list[Preference] = [
    maximize_allocations.PREFERENCE,
    at_most_one_team_lead.PREFERENCE,
]

__all__ = ["ObjectiveTerm", "Preference", "DEFAULT_PREFERENCES"]
