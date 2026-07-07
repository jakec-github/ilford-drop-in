"""Ensures no group works consecutive shifts.

Adjacency is by shift index (i conflicts with i±1), not calendar
distance — matching the Go allocator. The boundary with the previous
rota also counts: a group present on the last historical shift cannot
take shift 0 of this rota.
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..problem import Problem
from .base import AssignmentVars


class NoBackToBackConstraint:
    name = "no_back_to_back"
    description = (
        "no group works consecutive shifts, including the boundary from "
        "the previous rota's last shift"
    )

    def apply(
        self, model: cp_model.CpModel, x: AssignmentVars, problem: Problem
    ) -> None:
        for gv in problem.groups:
            for shift in problem.shifts[:-1]:
                model.Add(
                    x[(gv.key, shift.index)] + x[(gv.key, shift.index + 1)] <= 1
                )
            if problem.shifts and gv.key in problem.last_historical_group_keys:
                model.Add(x[(gv.key, 0)] == 0)


CONSTRAINT = NoBackToBackConstraint()
