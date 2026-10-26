"""Encourages each shift's split between men and women to match the
split across the whole roster: with twice as many men as women, a shift
of three is best as two men and a woman, and a shift of six as four and
two — whichever gender is the majority.

Only Male and Female count. A volunteer of any other gender, or none,
is in neither split, so they score nothing and cost nothing wherever
they go.

Mechanics: men and women are each spread across shifts in proportion
to the shifts' sizes, and two genders each spread that way leave every
shift with the roster's split. Per open shift and gender, one BoolVar
per head, at most as many on as the shift has of that gender; the kth
is worth

    weight(shift, k) = SPREAD_GENDERS_WEIGHT * seats(shift) // (k * largest)

where largest is the most seats any open shift has. Where every shift
is the same size that is the 30 // k the male-only preference this
replaced gave men, so a shift's first woman and its first man are each
worth the most, and a bigger shift's are worth proportionally more.

A direct penalty on each shift's distance from the roster's split says
the same thing, but its linear relaxation is too loose for CP-SAT to
prove a production rota optimal in the time it has (test_prod_scale);
diminishing levels like even_fill's are not.

Complements a shift_requires rule for "Male" or "Female": the
constraint forbids the worst case, this preference shapes everything
above it. Team leads count like everyone else, matching the Go
allocator's group MaleCount.
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..constraints.base import Vars
from ..problem import Problem
from .base import ObjectiveTerm

# Weight of the first man or woman on the largest shift; the kth is worth
# SPREAD_GENDERS_WEIGHT // k there. Below even_fill's first seats, above
# fairness and the base allocation reward.
SPREAD_GENDERS_WEIGHT = 30


class SpreadGendersPreference:
    name = "spread_genders"
    description = (
        "each shift's split between men and women matches the split "
        "across the roster"
    )

    def objective_terms(
        self, model: cp_model.CpModel, x: Vars, problem: Problem
    ) -> list[ObjectiveTerm]:
        men = [v for v in problem.volunteers if v.is_male]
        women = [v for v in problem.volunteers if v.is_female]
        if not men or not women:
            return []
        open_shifts = [s for s in problem.shifts if not s.closed]
        seats = {
            s.index: sum(problem.seats_for(s, r.name) for r in problem.roles)
            for s in open_shifts
        }
        largest = max(seats.values(), default=0)
        if largest == 0:
            return []
        terms: list[ObjectiveTerm] = []
        for shift in open_shifts:
            for gender, people in (("male", men), ("female", women)):
                heads = sum(x.attend[(v.id, shift.index)] for v in people)
                levels = []
                for k in range(1, min(len(people), seats[shift.index]) + 1):
                    weight = SPREAD_GENDERS_WEIGHT * seats[shift.index] // (k * largest)
                    if weight == 0:
                        break
                    level = model.NewBoolVar(f"{gender}_level_{shift.index}_{k}")
                    levels.append(level)
                    terms.append((level, weight))
                if levels:
                    model.Add(sum(levels) <= heads)
        return terms


PREFERENCE = SpreadGendersPreference()
