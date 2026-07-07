"""Ensures no volunteer works consecutive shifts.

Adjacency is by shift index (i conflicts with i±1), not calendar
distance — matching the Go allocator. The boundary with the previous
rota also counts: history is recorded per group, so a volunteer whose
group was present on the last historical shift cannot take shift 0 of
this rota.
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..problem import Problem
from .base import AssignmentVars


class NoBackToBackConstraint:
    name = "no_back_to_back"
    description = (
        "no volunteer works consecutive shifts, including the boundary "
        "from the previous rota's last shift"
    )

    def apply(
        self, model: cp_model.CpModel, x: AssignmentVars, problem: Problem
    ) -> None:
        for v in problem.volunteers:
            for shift in problem.shifts[:-1]:
                model.Add(
                    x[(v.id, shift.index)] + x[(v.id, shift.index + 1)] <= 1
                )
            if problem.shifts and v.group_key in problem.last_historical_group_keys:
                model.Add(x[(v.id, 0)] == 0)


CONSTRAINT = NoBackToBackConstraint()
