"""Ensures a shift never has more ordinary volunteers than its size.

Custom preallocations (free-text entries like "St John's team") occupy
seats, so the budget for solver-placed volunteers is
size - len(custom_preallocations), floored at zero. Team leads never
count toward size, so each group costs its ordinary (non-team-lead)
member count.
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
                    gv.ordinary_size * x[(gv.key, shift.index)]
                    for gv in problem.groups
                )
                <= budget
            )


CONSTRAINT = ShiftCapacityConstraint()
