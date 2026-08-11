"""Ensures a shift never oversubscribes any Role's Seats.

A shift's Shape says how many Seats it offers in each Role, and this caps
how many volunteers the solver may place in each. Custom (free-text)
preallocations name a Role too and are not solver decisions, so they take
their Role's Seats before the solver sees them.

The Shape is the only ceiling. A Role used to carry a max of its own and
this applied that as a second, unconditional one; since a Shift's Shape
states every Role's count and an admin edits it per Shift (#137/#138), that
ceiling had nothing left to say and went with the field (#185).

This replaces the pre-Roles pair shift_capacity + at_most_one_team_lead: a
team lead is now a Role a Shape asks for one Seat of, so "at most one team
lead" is just this rule. That carries one deliberate divergence. The old rule
counted per *group*, which meant a second team-lead holder could not work
a shift at all; per-Role Seats let them work it in an ordinary Seat, which
is what the roster migration (every team lead also holds Service
volunteer) exists for.
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..problem import Problem
from .base import Vars


class SeatCapacityConstraint:
    name = "seat_capacity"
    description = (
        "a shift never has more volunteers in a Role than its Shape offers "
        "Seats for, less any custom preallocations on that Role"
    )

    def apply(self, model: cp_model.CpModel, x: Vars, problem: Problem) -> None:
        for shift in problem.shifts:
            if shift.closed:
                continue
            for role in problem.roles:
                occupants = [
                    x.role[(v.id, shift.index, role.name)]
                    for v in problem.volunteers
                    if (v.id, shift.index, role.name) in x.role
                ]
                if not occupants:
                    continue

                seats = problem.seats_for(shift, role.name)
                customs = len(problem.customs_for(shift, role.name))
                model.Add(sum(occupants) <= max(0, seats - customs))


CONSTRAINT = SeatCapacityConstraint()
