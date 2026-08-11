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

The weight of one Seat is its Role's priority band plus a harmonic term:

    weight(role, seat_n) = band(role.priority) + EVEN_FILL_WEIGHT // seat_n

Every Role's Seats diminish, which is what spreads scarce volunteers
across shifts rather than piling them onto one — the gap between a
Role's first Seat and its fourth is the whole mechanism. The band puts
a scarce Role's Seats above an ordinary Role's, so a Seat only a few
people can fill is filled first: a team-lead holder working the shift
anyway takes the Team lead Seat rather than a Service volunteer Seat.
Before Roles that was a tie CP-SAT happened to break the right way.

One rule for every Role. Until #185 there were two — capped Roles took a
flat weight above every uncapped Seat — and the split rested on a `max`
that no longer exists. Priority is what says which Role is filled first
now, which is what priority always meant.
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..constraints.base import Vars
from ..domain import Role
from ..problem import Problem
from .base import ObjectiveTerm

# Weight of the first Seat of the lowest-priority Role; its Seat n is worth
# EVEN_FILL_WEIGHT // n. Dominant over spread_males, fairness and
# maximize_allocations by design.
EVEN_FILL_WEIGHT = 60

# One step up the priority order. Wider than the best harmonic term, so
# every Seat of a higher-priority Role outranks the first Seat of a
# lower-priority one — being short of a Team lead is worse than being short
# of an ordinary volunteer however many Seats deep the comparison is.
PRIORITY_BAND = EVEN_FILL_WEIGHT + 1


class EvenFillPreference:
    name = "even_fill"
    description = (
        "shifts fill evenly: early Seats on a shift are worth more than later "
        "ones, and a higher-priority Role's Seat more than a lower one's"
    )

    def objective_terms(
        self, model: cp_model.CpModel, x: Vars, problem: Problem
    ) -> list[ObjectiveTerm]:
        bands = _priority_bands(problem.roles)
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
                    weight = bands[role.priority] + EVEN_FILL_WEIGHT // seat_number
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


def _priority_bands(roles: tuple[Role, ...]) -> dict[int, int]:
    """{priority: band}, the highest-priority Roles in the highest band.

    Banded by the priority itself rather than by position, so Roles an
    admin gave the same priority are peers — which is what saying so meant,
    and priorities are deliberately not unique (013_role.sql).
    """
    distinct = sorted({role.priority for role in roles})
    return {
        priority: (len(distinct) - 1 - rank) * PRIORITY_BAND
        for rank, priority in enumerate(distinct)
    }


PREFERENCE = EvenFillPreference()
