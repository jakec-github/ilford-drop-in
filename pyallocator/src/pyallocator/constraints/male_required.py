"""Ensures every shift can end up with a male volunteer: a shift with
no male allocated must leave a slot open — the team-lead slot or an
ordinary seat — so the rota creator can manually add a male after
finding a suitable volunteer.

Per open shift, at least one of:
  1. a male is allocated (counted across all members of allocated
     groups, team leads included, matching the Go group MaleCount);
  2. no team lead is allocated (a male team lead can still be added);
  3. at least one ordinary seat is unfilled (a male volunteer can
     still be added).

Custom (free-text) preallocations have unknown gender: they occupy
seats but never satisfy the male requirement.
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..problem import Problem
from .base import AssignmentVars


class MaleRequiredConstraint:
    name = "male_required"
    description = (
        "a shift without a male keeps a slot open (team lead or seat) "
        "so one can be added manually"
    )

    def apply(
        self, model: cp_model.CpModel, x: AssignmentVars, problem: Problem
    ) -> None:
        for shift in problem.shifts:
            if shift.closed:
                continue
            male_sum = sum(
                gv.male_count * x[(gv.key, shift.index)] for gv in problem.groups
            )
            team_lead_sum = sum(
                x[(gv.key, shift.index)]
                for gv in problem.groups
                if gv.has_team_lead
            )
            fill = sum(
                gv.ordinary_size * x[(gv.key, shift.index)] for gv in problem.groups
            )
            budget = max(0, shift.size - len(shift.custom_preallocations))

            has_male = model.NewBoolVar(f"male_present_{shift.index}")
            tl_slot_open = model.NewBoolVar(f"tl_slot_open_{shift.index}")
            seat_open = model.NewBoolVar(f"seat_open_{shift.index}")
            model.Add(male_sum >= 1).OnlyEnforceIf(has_male)
            model.Add(team_lead_sum == 0).OnlyEnforceIf(tl_slot_open)
            model.Add(fill <= budget - 1).OnlyEnforceIf(seat_open)
            model.AddBoolOr([has_male, tl_slot_open, seat_open])


CONSTRAINT = MaleRequiredConstraint()
