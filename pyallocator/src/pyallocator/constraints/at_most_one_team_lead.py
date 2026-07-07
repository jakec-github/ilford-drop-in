"""Ensures a shift never has more than one team lead.

Zero team leads is fine (missing team leads are filled in manually
later); two is never allowed. Preallocations that force two team-lead
groups onto one shift therefore make the model INFEASIBLE — matching
the Go allocator, which errors when preallocating a team lead onto a
shift that already has one.
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..problem import Problem
from .base import AssignmentVars


class AtMostOneTeamLeadConstraint:
    name = "at_most_one_team_lead"
    description = "shifts never have more than one team lead"

    def apply(
        self, model: cp_model.CpModel, x: AssignmentVars, problem: Problem
    ) -> None:
        tl_groups = [gv for gv in problem.groups if gv.has_team_lead]
        if len(tl_groups) < 2:
            return
        for shift in problem.shifts:
            model.Add(
                sum(x[(gv.key, shift.index)] for gv in tl_groups) <= 1
            )


CONSTRAINT = AtMostOneTeamLeadConstraint()
