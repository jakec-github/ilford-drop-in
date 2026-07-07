"""Encourages shifts to fill EVENLY: early seats on a shift are worth
much more than later ones, so when volunteers are scarce the solver
gets every shift to 3 before pushing any shift to 4 — rather than
filling some shifts completely and leaving others near-empty.

Mechanics: per open shift, one BoolVar per seat level, constrained so
at most `fill` of them can be on; maximisation switches on the
highest-weighted (earliest) levels first. Seat weights diminish
harmonically (EVEN_FILL_WEIGHT // seat_number). Custom (free-text)
preallocations occupy the first seat levels, so a shift that already
has one custom entry values its first solver-placed volunteer as seat
2, not seat 1.
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..constraints.base import AssignmentVars
from ..problem import Problem
from .base import ObjectiveTerm

# Weight of a shift's first seat; seat n is worth EVEN_FILL_WEIGHT // n.
# Dominant over spread_males/fairness/maximize_allocations by design.
EVEN_FILL_WEIGHT = 60


class EvenFillPreference:
    name = "even_fill"
    description = (
        "shifts fill evenly: early seats on a shift are worth more than later ones"
    )

    def objective_terms(
        self, model: cp_model.CpModel, x: AssignmentVars, problem: Problem
    ) -> list[ObjectiveTerm]:
        terms: list[ObjectiveTerm] = []
        for shift in problem.shifts:
            if shift.closed:
                continue
            budget = max(0, shift.size - len(shift.custom_preallocations))
            if budget == 0:
                continue
            fill = sum(
                v.seat_cost * x[(v.id, shift.index)] for v in problem.volunteers
            )
            levels = []
            for k in range(1, budget + 1):
                seat_number = len(shift.custom_preallocations) + k
                weight = EVEN_FILL_WEIGHT // seat_number
                if weight == 0:
                    break
                level = model.NewBoolVar(f"fill_level_{shift.index}_{seat_number}")
                levels.append(level)
                terms.append((level, weight))
            if levels:
                model.Add(sum(levels) <= fill)
        return terms


PREFERENCE = EvenFillPreference()
