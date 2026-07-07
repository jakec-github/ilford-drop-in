"""Ensures a shift never has more ordinary volunteers than its size.

Custom preallocations (free-text entries like "St John's team") occupy
seats, so the budget for solver-placed volunteers is
size - len(custom_preallocations), floored at zero. Team leads never
count toward size, so each volunteer costs their seat_cost (0 for a
team lead, 1 otherwise).
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..problem import Problem
from .base import AssignmentVars


class ShiftCapacityConstraint:
    name = "shift_capacity"
    description = (
        "ordinary volunteers on a shift never exceed size minus custom "
        "preallocations; team leads don't count"
    )

    def apply(
        self, model: cp_model.CpModel, x: AssignmentVars, problem: Problem
    ) -> None:
        for shift in problem.shifts:
            if shift.closed:
                continue
            budget = max(0, shift.size - len(shift.custom_preallocations))
            model.Add(
                sum(
                    v.seat_cost * x[(v.id, shift.index)]
                    for v in problem.volunteers
                )
                <= budget
            )


CONSTRAINT = ShiftCapacityConstraint()
