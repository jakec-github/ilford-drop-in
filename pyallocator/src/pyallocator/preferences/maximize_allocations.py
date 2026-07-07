"""Base reward: every (group, shift) allocation is worth a little, so
shifts fill where they can and ties between otherwise-equal solutions
break toward more volunteers on the rota.

Counting is per group (first member as its stand-in), not per
volunteer, so a couple earns the same base reward as an individual —
preserving the original group-level objective exactly.

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
        total = sum(
            x[(group.members[0].id, shift.index)]
            for group in problem.groups
            for shift in problem.shifts
        )
        return [(total, ALLOCATION_REWARD)]


PREFERENCE = MaximizeAllocationsPreference()
