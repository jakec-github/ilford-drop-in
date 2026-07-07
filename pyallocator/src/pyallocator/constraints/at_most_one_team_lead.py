"""Ensures a shift never has more than one team lead.

Zero team leads is fine (missing team leads are filled in manually
later); two is never allowed. Preallocations that force two team-lead
groups onto one shift therefore make the model INFEASIBLE — matching
the Go allocator, which errors when preallocating a team lead onto a
shift that already has one.

Counting is per GROUP (via the group's first team-lead member as its
indicator), not per team-lead volunteer: only one member of a group is
ever designated team lead in extraction, so a group containing two
team leads still counts as one — mirroring the Go allocator. When
roles become solver-assigned decisions (flexible roles), this should
become a straight sum over team-lead role assignments.
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
        tl_indicators = []
        for group in problem.groups:
            for member in group.members:
                if member.is_team_lead:
                    tl_indicators.append(member.id)
                    break
        if len(tl_indicators) < 2:
            return
        for shift in problem.shifts:
            model.Add(
                sum(x[(vol_id, shift.index)] for vol_id in tl_indicators) <= 1
            )


CONSTRAINT = AtMostOneTeamLeadConstraint()
