"""Ensures every shift can end up with a male volunteer: a shift with no
male allocated must leave a Seat open — whichever Role that Seat belongs
to — so the rota creator can manually add a male into it after finding a
suitable volunteer.

Per open shift, at least one of:
  1. a male is allocated, whatever Seat they are in;
  2. some Role's Seats are not all taken.

Before Roles that second escape was two, "no team lead is allocated" and
"an ordinary seat is unfilled", which were the same statement about two
hardcoded Roles. A Team lead Seat and a Service volunteer Seat are both
just Seats, and a Role whose Seats nobody available can fill is one of
the open ones — that is where the old "no team lead allocated" escape
comes from now.

Custom (free-text) preallocations have unknown gender: they occupy Seats
but never satisfy the male requirement, so they narrow the escape.

The rule is config (`requiresMale`). It is on everywhere today, which is
what the code used to assume; the flag exists so a shift pattern that
does not need male cover can say so rather than work around it.
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..domain import ShiftSpec
from ..problem import Problem
from .base import Vars


class MaleRequiredConstraint:
    name = "male_required"
    description = (
        "a shift without a male keeps a Seat open so one can be added manually"
    )

    def apply(self, model: cp_model.CpModel, x: Vars, problem: Problem) -> None:
        if not problem.requires_male:
            return

        for shift in problem.shifts:
            if shift.closed:
                continue

            male_sum = cp_model.LinearExpr.Sum(
                [
                    x.attend[(v.id, shift.index)]
                    for v in problem.volunteers
                    if v.is_male
                ]
            )
            has_male = model.NewBoolVar(f"male_present_{shift.index}")
            model.Add(male_sum >= 1).OnlyEnforceIf(has_male)
            escapes = [has_male]

            for role in problem.roles:
                seats = problem.seats_for(shift, role.name)
                customs = len(problem.customs_for(shift, role.name))
                if seats - customs < 1:
                    continue  # no Seat here that a male could be added to
                seat_open = model.NewBoolVar(f"seat_open_{shift.index}_{role.name}")
                model.Add(
                    _occupants(x, problem, shift, role.name) <= seats - customs - 1
                ).OnlyEnforceIf(seat_open)
                escapes.append(seat_open)

            model.AddBoolOr(escapes)


def _occupants(
    x: Vars, problem: Problem, shift: ShiftSpec, role: str
) -> cp_model.LinearExpr:
    """How many volunteers the solver put in this shift's Seats of a Role."""
    return cp_model.LinearExpr.Sum(
        [
            x.role[(v.id, shift.index, role)]
            for v in problem.volunteers
            if (v.id, shift.index, role) in x.role
        ]
    )


CONSTRAINT = MaleRequiredConstraint()
