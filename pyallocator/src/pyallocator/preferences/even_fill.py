"""Encourages shifts to fill EVENLY: early Seats on a shift are worth
much more than later ones, so when volunteers are scarce the solver
gets every shift to 3 before pushing any shift to 4 — rather than
filling some shifts completely and leaving others near-empty.

Mechanics: per open shift and Role, one BoolVar per Seat the Shape
offers, constrained so at most as many can be on as that Role has
occupants; maximisation switches on the highest-weighted Seats first.
Custom (free-text) pins occupy the first Seats of the Role
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

Counting per Role alone is blind to the shift as a whole: a lead's Seat
is worth the same whether the shift has one ordinary volunteer or four,
so where each Role splits as evenly as it can, the remainders may still
pile onto one shift — 3-2-1 scoring the same as 2-2-2 (#242). A second,
lighter harmonic over each shift's headcount, every Role and custom entry
together, breaks those ties towards even totals:

    weight(total_n) = TOTAL_FILL_WEIGHT // total_n

It is a third of the per-Role weight: light enough to settle what
counting per Role leaves even rather than overturn how a Role spreads.
That is a matter of weights, not a strict lexicographic order, so it was
checked rather than assumed — on the end-to-end scenario and on
production-sized rosters, plentiful and scarce, every rota it chose was
already optimal counting per Role alone.
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..constraints.base import Vars
from ..domain import Role, ShiftSpec
from ..problem import Problem
from .base import ObjectiveTerm

# Weight of the first Seat of the lowest-priority Role; its Seat n is worth
# EVEN_FILL_WEIGHT // n. Dominant over spread_genders, fairness and
# maximize_allocations by design.
EVEN_FILL_WEIGHT = 60

# One step up the priority order. Wider than the best harmonic term, so
# every Seat of a higher-priority Role outranks the first Seat of a
# lower-priority one — being short of a Team lead is worse than being short
# of an ordinary volunteer however many Seats deep the comparison is.
PRIORITY_BAND = EVEN_FILL_WEIGHT + 1

# Weight of a shift's first head, of any Role; its nth is worth
# TOTAL_FILL_WEIGHT // n. A tie-break on headcount beneath the per-Role
# Seats, and the smallest round weight whose levels stay distinct up to the
# six heads a production shift asks for (20, 10, 6, 5, 4, 3).
TOTAL_FILL_WEIGHT = 20


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
            terms.extend(_total_terms(model, x, problem, shift))
        return terms


def _total_terms(
    model: cp_model.CpModel, x: Vars, problem: Problem, shift: ShiftSpec
) -> list[ObjectiveTerm]:
    """One level per head the shift's Shape has room for, every Role
    together; custom entries are its first heads, as they are a Role's
    first Seats."""
    customs = sum(len(problem.customs_for(shift, role.name)) for role in problem.roles)
    seats = sum(problem.seats_for(shift, role.name) for role in problem.roles)
    heads = [x.attend[(v.id, shift.index)] for v in problem.volunteers]
    terms: list[ObjectiveTerm] = []
    levels = []
    for k in range(1, max(0, seats - customs) + 1):
        head_number = customs + k
        weight = TOTAL_FILL_WEIGHT // head_number
        if weight == 0:
            break
        level = model.NewBoolVar(f"total_level_{shift.index}_{head_number}")
        levels.append(level)
        terms.append((level, weight))
    if levels:
        model.Add(sum(levels) <= sum(heads))
    return terms


def _priority_bands(roles: tuple[Role, ...]) -> dict[int, int]:
    """{priority: band}, the highest-priority Roles in the highest band.

    Banded by the priority itself rather than by position, so Roles an
    Organiser gave the same priority are peers — which is what saying so meant,
    and priorities are deliberately not unique (013_role.sql).
    """
    distinct = sorted({role.priority for role in roles})
    return {
        priority: (len(distinct) - 1 - rank) * PRIORITY_BAND
        for rank, priority in enumerate(distinct)
    }


PREFERENCE = EvenFillPreference()
