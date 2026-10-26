"""Ensures every shift can end up with a volunteer of the gender a
shift_requires rule names: a shift without one allocated must leave a Seat
open — whichever Role that Seat belongs to — so the rota creator can add
one manually after finding a suitable volunteer.

Per open shift, at least one of:
  1. a volunteer of the gender is allocated, whatever Seat they are in;
  2. some Role's Seats are not all taken.

The gender is matched against the roster's text exactly. "Male" is the
rule as it began (male_required, issue #281 generalised it).

Custom (free-text) preallocations have unknown gender: they occupy Seats
but never satisfy the requirement, so they narrow the escape. Pinned
volunteers narrow it too, and a Role its pins have overfilled offers no
escape — but frees none in another Role either (ADR 0010).

A shift whose pins take every Seat is exempt. Its people were all decided
before the solve, so there is no choice for the rule to steer, only a rota
to refuse — and a pin is a decision an Organiser has already made, so the
pin wins. That is the one case the rule could make infeasible: with any
Seat not pinned, the solver can always leave it open.

The rule is a Shift Rule (ADR 0012): it applies to every shift when an
Organiser has added it, and carries no Scope and no value. Two rules naming
different genders ask for both.
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..domain import Rule, ShiftSpec
from ..problem import Problem
from .base import Vars


class ShiftRequiresConstraint:
    name = "shift_requires"

    def __init__(self, gender: str) -> None:
        self.gender = gender
        self.description = (
            f"a shift without a {gender.lower()} volunteer keeps a Seat open "
            "so one can be added manually"
        )

    def apply(self, model: cp_model.CpModel, x: Vars, problem: Problem) -> None:
        for shift in problem.shifts:
            if shift.closed or problem.pins_fill_every_seat(shift):
                continue

            present = cp_model.LinearExpr.Sum(
                [
                    x.attend[(v.id, shift.index)]
                    for v in problem.volunteers
                    if v.member.gender == self.gender
                ]
            )
            has_one = model.NewBoolVar(f"{self.gender}_present_{shift.index}")
            model.Add(present >= 1).OnlyEnforceIf(has_one)
            escapes = [has_one]

            for role in problem.roles:
                room = problem.room_for(shift, role.name)
                if room < 1:
                    continue  # no Seat here that anybody could be added to
                seat_open = model.NewBoolVar(
                    f"{self.gender}_seat_open_{shift.index}_{role.name}"
                )
                model.Add(
                    _chosen(x, problem, shift, role.name) <= room - 1
                ).OnlyEnforceIf(seat_open)
                escapes.append(seat_open)

            model.AddBoolOr(escapes)


def _chosen(
    x: Vars, problem: Problem, shift: ShiftSpec, role: str
) -> cp_model.LinearExpr:
    """How many volunteers the solver itself put in this shift's Seats of a
    Role — the pinned are already counted out of its room."""
    return cp_model.LinearExpr.Sum(
        [
            x.role[(v.id, shift.index, role)]
            for v in problem.volunteers
            if (v.id, shift.index, role) in x.role
            and not problem.is_pinned_to(v.id, shift.index, role)
        ]
    )


def for_rule(rule: Rule) -> ShiftRequiresConstraint:
    """The constraint a shift_requires rule makes, for the gender it names."""
    return ShiftRequiresConstraint(rule.option)
