"""Encourages fair distribution of shifts BETWEEN volunteer groups:
allocations to a group get progressively less valuable the more that
group has already worked, counting both its historical allocations
(previous rota) and its allocations in this rota.

A fresh group's first allocation is worth FAIRNESS_WEIGHT; a group
with 5 past allocations earns only FAIRNESS_WEIGHT // 6 for its next
one — so the solver reaches for under-used volunteers first. This
replaces the fairness that "emerged from the mechanism" in the greedy
allocator with an explicit, tunable weight (see allocator_issues.md).
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..constraints.base import AssignmentVars
from ..problem import Problem
from .base import ObjectiveTerm

# Weight of a never-allocated group's first shift; a group's nth
# lifetime allocation is worth FAIRNESS_WEIGHT // n. Below even_fill
# and spread_males, above the base allocation reward.
FAIRNESS_WEIGHT = 20


class FairnessPreference:
    name = "fairness"
    description = "shifts are distributed fairly between volunteer groups over time"

    def objective_terms(
        self, model: cp_model.CpModel, x: AssignmentVars, problem: Problem
    ) -> list[ObjectiveTerm]:
        terms: list[ObjectiveTerm] = []
        for gv in problem.groups:
            allocations = sum(
                x[(gv.key, shift.index)] for shift in problem.shifts
            )
            history = gv.group.historical_allocation_count
            levels = []
            for k in range(1, len(problem.shifts) + 1):
                weight = FAIRNESS_WEIGHT // (history + k)
                if weight == 0:
                    break
                level = model.NewBoolVar(f"fair_level_{gv.key}_{k}")
                levels.append(level)
                terms.append((level, weight))
            if levels:
                model.Add(sum(levels) <= allocations)
        return terms


PREFERENCE = FairnessPreference()
