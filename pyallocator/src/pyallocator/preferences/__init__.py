"""Soft preferences: weighted objective terms traded off by the solver.

DEFAULT_PREFERENCES is the explicit, ordered production list — add new
preference modules here. Tests inject subsets via model_builder.build().

Weight hierarchy (per unit, all harmonic-diminishing):
    even_fill (60 // seat) > spread_males (30 // male)
    > fairness (20 // lifetime allocation) > maximize_allocations (1).
"""

from . import even_fill, fairness, maximize_allocations, spread_males
from .base import ObjectiveTerm, Preference

DEFAULT_PREFERENCES: list[Preference] = [
    even_fill.PREFERENCE,
    spread_males.PREFERENCE,
    fairness.PREFERENCE,
    maximize_allocations.PREFERENCE,
]

__all__ = ["ObjectiveTerm", "Preference", "DEFAULT_PREFERENCES"]
