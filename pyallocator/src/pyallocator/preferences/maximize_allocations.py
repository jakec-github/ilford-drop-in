"""Base reward: every (group, shift) allocation is worth a little, so
shifts fill where they can and ties between otherwise-equal solutions
break toward more volunteers on the rota.

The shaping preferences (even_fill, spread_males, fairness) decide
WHERE and WHO; this keeps the overall pressure to allocate at all.
Other preference weights are calibrated against this baseline.
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..constraints.base import AssignmentVars
from ..problem import Problem
from .base import ObjectiveTerm

# Reward per (group, shift) allocation — the unit other preference
# weights are measured against.
ALLOCATION_REWARD = 1


class MaximizeAllocationsPreference:
    name = "maximize_allocations"
    description = "as many group-shift allocations as possible"

    def objective_terms(
        self, model: cp_model.CpModel, x: AssignmentVars, problem: Problem
    ) -> list[ObjectiveTerm]:
        return [(sum(x.values()), ALLOCATION_REWARD)]


PREFERENCE = MaximizeAllocationsPreference()
