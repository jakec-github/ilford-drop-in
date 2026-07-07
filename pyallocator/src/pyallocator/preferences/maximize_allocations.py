"""PLACEHOLDER preference: maximise the total number of allocations, so
that constraint-only solves return non-empty rotas instead of the
trivially-feasible empty one.

Replace/extend when real preferences land: fill-to-size, team-lead
coverage, male coverage per shift, fair distribution between volunteers
— each as its own weighted module here.
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..constraints.base import AssignmentVars
from ..problem import Problem
from .base import ObjectiveTerm

# Reward per (group, shift) allocation. Other preference weights are
# calibrated against this baseline.
ALLOCATION_REWARD = 1


class MaximizeAllocationsPreference:
    name = "maximize_allocations"
    description = "as many group-shift allocations as possible (placeholder objective)"

    def objective_terms(
        self, model: cp_model.CpModel, x: AssignmentVars, problem: Problem
    ) -> list[ObjectiveTerm]:
        return [(sum(x.values()), ALLOCATION_REWARD)]


PREFERENCE = MaximizeAllocationsPreference()
