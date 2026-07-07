"""Ensures no group is allocated more shifts than the rota's allocation
cap (max_allocation_count, computed in Go from MaxAllocationFrequency).
Preallocations count toward the cap.
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..problem import Problem
from .base import AssignmentVars


class MaxFrequencyConstraint:
    name = "max_frequency"
    description = "no group exceeds its allocation cap for the rota"

    def apply(
        self, model: cp_model.CpModel, x: AssignmentVars, problem: Problem
    ) -> None:
        for gv in problem.groups:
            model.Add(
                sum(x[(gv.key, shift.index)] for shift in problem.shifts)
                <= problem.max_allocation_count
            )


CONSTRAINT = MaxFrequencyConstraint()
