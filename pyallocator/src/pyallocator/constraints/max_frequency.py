"""Ensures no volunteer is allocated more shifts than the rota's
allocation cap (max_allocation_count, computed in Go from
MaxAllocationFrequency). Preallocations count toward the cap. Under the
grouping constraint this equals the old per-group cap, since members of
a group always work the same shifts.
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..problem import Problem
from .base import AssignmentVars


class MaxFrequencyConstraint:
    name = "max_frequency"
    description = "no volunteer exceeds the allocation cap for the rota"

    def apply(
        self, model: cp_model.CpModel, x: AssignmentVars, problem: Problem
    ) -> None:
        for v in problem.volunteers:
            model.Add(
                sum(x[(v.id, shift.index)] for shift in problem.shifts)
                <= problem.max_allocation_count
            )


CONSTRAINT = MaxFrequencyConstraint()
