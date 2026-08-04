"""Encourages shifts to fill EVENLY: early Seats on a shift are worth
much more than later ones, so when volunteers are scarce the solver
gets every shift to 3 before pushing any shift to 4 — rather than
filling some shifts completely and leaving others near-empty.

Mechanics: per open shift and Role, one BoolVar per Seat the Shape
offers, constrained so at most as many can be on as that Role has
occupants; maximisation switches on the highest-weighted Seats first.
Custom (free-text) preallocations occupy the first Seats of the Role
they name, so a shift that already has one custom entry values its
first solver-placed volunteer as Seat 2, not Seat 1.

Weights, and why they differ by Role:

- The uncapped Role's Seats diminish harmonically
  (EVEN_FILL_WEIGHT // seat_number, so 60, 30, 20, 15, ...). The gap
  between the first Seat and the fourth is what spreads scarce
  volunteers across shifts rather than piling them onto one.
- A capped Role's Seats are worth a flat CAPPED_SEAT_WEIGHT, above
  every uncapped Seat, ordered among themselves by Role priority. So a
  Seat only a few people can fill is filled before an ordinary one: a
  team-lead holder working the shift anyway takes the Team lead Seat
  rather than a Service volunteer Seat. Before Roles that was a tie
  CP-SAT happened to break the right way.
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..constraints.base import Vars
from ..domain import Role
from ..problem import Problem
from .base import ObjectiveTerm

# Weight of a shift's first uncapped Seat; Seat n is worth
# EVEN_FILL_WEIGHT // n. Dominant over spread_males, fairness and
# maximize_allocations by design.
EVEN_FILL_WEIGHT = 60

# A capped Role's Seat, worth more than the best uncapped one so it fills
# first. Higher-priority capped Roles get a little more again.
CAPPED_SEAT_WEIGHT = EVEN_FILL_WEIGHT + 1


class EvenFillPreference:
    name = "even_fill"
    description = (
        "shifts fill evenly: early Seats on a shift are worth more than later "
        "ones, and a capped Role's Seat more than an ordinary one"
    )

    def objective_terms(
        self, model: cp_model.CpModel, x: Vars, problem: Problem
    ) -> list[ObjectiveTerm]:
        capped = [role for role in problem.roles if role.capped]
        terms: list[ObjectiveTerm] = []
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

                customs = len(problem.customs_for(shift, role.name))
                budget = max(0, problem.seats_for(shift, role.name) - customs)
                levels = []
                for k in range(1, budget + 1):
                    seat_number = customs + k
                    weight = _seat_weight(role, seat_number, capped)
                    if weight == 0:
                        break
                    level = model.NewBoolVar(
                        f"fill_level_{shift.index}_{role.name}_{seat_number}"
                    )
                    levels.append(level)
                    terms.append((level, weight))
                if levels:
                    model.Add(sum(levels) <= sum(occupants))
        return terms


def _seat_weight(role: Role, seat_number: int, capped: list[Role]) -> int:
    if not role.capped:
        return EVEN_FILL_WEIGHT // seat_number
    # Flat, and separated so the Role that should be staffed first is:
    # `capped` is in priority order, lowest number first.
    return CAPPED_SEAT_WEIGHT + (len(capped) - 1 - capped.index(role))


PREFERENCE = EvenFillPreference()
