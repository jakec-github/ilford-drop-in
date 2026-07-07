"""Encourages male volunteers to be spread evenly across shifts: the
first male on a shift is worth much more than the second, so the
solver distributes males one-per-shift before doubling up anywhere.

Complements the male_required CONSTRAINT (no all-female shift): the
constraint forbids the worst case, this preference shapes everything
above it. Males are counted across all group members, team leads
included, matching the Go allocator's group MaleCount.
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..constraints.base import AssignmentVars
from ..problem import Problem
from .base import ObjectiveTerm

# Weight of a shift's first male; the nth male is worth
# SPREAD_MALES_WEIGHT // n. Below even_fill's first seats, above
# fairness and the base allocation reward.
SPREAD_MALES_WEIGHT = 30


class SpreadMalesPreference:
    name = "spread_males"
    description = "male volunteers are spread evenly across shifts"

    def objective_terms(
        self, model: cp_model.CpModel, x: AssignmentVars, problem: Problem
    ) -> list[ObjectiveTerm]:
        total_males = sum(gv.male_count for gv in problem.groups)
        if total_males == 0:
            return []
        terms: list[ObjectiveTerm] = []
        for shift in problem.shifts:
            if shift.closed:
                continue
            male_sum = sum(
                gv.male_count * x[(gv.key, shift.index)] for gv in problem.groups
            )
            levels = []
            for k in range(1, total_males + 1):
                weight = SPREAD_MALES_WEIGHT // k
                if weight == 0:
                    break
                level = model.NewBoolVar(f"male_level_{shift.index}_{k}")
                levels.append(level)
                terms.append((level, weight))
            model.Add(sum(levels) <= male_sum)
        return terms


PREFERENCE = SpreadMalesPreference()
