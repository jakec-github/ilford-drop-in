"""Discourages putting more than one team lead on the same shift.

This is a preference, not a constraint: rotas with NO team lead on a
shift are normal (they get filled in later), and stacking two team
leads must remain possible when preallocations force it — it's just
heavily penalised so the solver never chooses it voluntarily.

Weight: each excess team-lead group on a shift costs EXCESS_TL_PENALTY,
much larger than maximize_allocations.ALLOCATION_REWARD (+1 per
allocation), so a second TL is never worth the allocations it adds.
Revisit this weight when real preferences land — it must stay dominant
over per-allocation rewards.
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..constraints.base import AssignmentVars
from ..problem import Problem
from .base import ObjectiveTerm

EXCESS_TL_PENALTY = 10


class AtMostOneTeamLeadPreference:
    name = "at_most_one_team_lead"
    description = "shifts should not have more than one team lead"

    def objective_terms(
        self, model: cp_model.CpModel, x: AssignmentVars, problem: Problem
    ) -> list[ObjectiveTerm]:
        tl_groups = [gv for gv in problem.groups if gv.has_team_lead]
        if len(tl_groups) < 2:
            return []
        terms: list[ObjectiveTerm] = []
        for shift in problem.shifts:
            if shift.closed:
                continue
            tl_count = sum(x[(gv.key, shift.index)] for gv in tl_groups)
            excess = model.NewIntVar(0, len(tl_groups), f"excess_tl_{shift.index}")
            # excess >= tl_count - 1; zero or one TL costs nothing.
            model.Add(excess >= tl_count - 1)
            terms.append((excess, -EXCESS_TL_PENALTY))
        return terms


PREFERENCE = AtMostOneTeamLeadPreference()
