"""Soft preferences: weighted objective terms traded off by the solver.

DEFAULT_PREFERENCES is the explicit, ordered production list — add new
preference modules here. Tests inject subsets via model_builder.build().

Weight hierarchy (per unit, harmonic-diminishing unless flat):
    even_fill, capped Role Seat (61+, flat)
    > even_fill, uncapped Role Seat (60 // Seat)
    > spread_males (30 // male)
    > fairness (20 // lifetime allocation) > maximize_allocations (1).
"""

from . import even_fill, fairness, maximize_allocations, spread_males
from .base import ObjectiveTerm, Preference

FUNDAMENTAL_PREFERENCES: list[Preference] = [
    maximize_allocations.PREFERENCE,
    fairness.PREFERENCE,
    even_fill.PREFERENCE,
]

ADDITIONAL_PREFERENCES: list[Preference] = [
    spread_males.PREFERENCE,
]

DEFAULT_PREFERENCES = FUNDAMENTAL_PREFERENCES + ADDITIONAL_PREFERENCES

__all__ = ["ObjectiveTerm", "Preference", "DEFAULT_PREFERENCES"]
