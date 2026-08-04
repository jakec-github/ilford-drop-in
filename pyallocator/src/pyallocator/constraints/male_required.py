"""Ensures every shift can end up with a male volunteer: a shift with
no male allocated must leave a slot open — the team-lead slot or an
ordinary seat — so the rota creator can manually add a male after
finding a suitable volunteer.

Per open shift, at least one of:
  1. a male is allocated (team leads included);
  2. the Team lead Seat is empty (a male team lead can still be added);
  3. at least one ordinary Seat is unfilled (a male volunteer can
     still be added).

Escapes 2 and 3 are the same idea written twice — "some Seat is still
free" — which #89 commit 8 collapses into one rule over the Shape.

Custom (free-text) preallocations have unknown gender: they occupy
Seats but never satisfy the male requirement.
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..problem import Problem
from ..domain import ShiftSpec
from .base import Vars


class MaleRequiredConstraint:
    name = "male_required"
    description = (
        "a shift without a male keeps a slot open (team lead or seat) "
        "so one can be added manually"
    )

    def apply(
        self, model: cp_model.CpModel, x: Vars, problem: Problem
    ) -> None:
        lead_role = problem.lead_role
        uncapped = problem.uncapped_role
        for shift in problem.shifts:
            if shift.closed:
                continue
            male_sum = sum(
                x.attend[(v.id, shift.index)] for v in problem.volunteers if v.is_male
            )
            has_male = model.NewBoolVar(f"male_present_{shift.index}")
            model.Add(male_sum >= 1).OnlyEnforceIf(has_male)
            escapes = [has_male]

            if lead_role is not None:
                tl_slot_open = model.NewBoolVar(f"tl_slot_open_{shift.index}")
                model.Add(
                    _occupants(x, problem, shift, lead_role.name) == 0
                ).OnlyEnforceIf(tl_slot_open)
                escapes.append(tl_slot_open)

            budget = max(
                0,
                problem.seats_for(shift, uncapped.name)
                - len(problem.customs_for(shift, uncapped.name)),
            )
            seat_open = model.NewBoolVar(f"seat_open_{shift.index}")
            model.Add(
                _occupants(x, problem, shift, uncapped.name) <= budget - 1
            ).OnlyEnforceIf(seat_open)
            escapes.append(seat_open)

            model.AddBoolOr(escapes)


def _occupants(x: Vars, problem: Problem, shift: ShiftSpec, role: str):
    """How many volunteers the solver put in this shift's Seats of a Role."""
    return sum(
        x.role[(v.id, shift.index, role)]
        for v in problem.volunteers
        if (v.id, shift.index, role) in x.role
    )


CONSTRAINT = MaleRequiredConstraint()
