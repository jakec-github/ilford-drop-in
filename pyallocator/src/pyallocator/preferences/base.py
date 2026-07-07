"""Preference protocol: a soft goal traded off in the objective.

Each preference module contributes weighted linear terms; the model
builder sums every (expression, weight) pair into a single Maximize.
Preferences never make a feasible rota infeasible — rules that must
never be broken belong in constraints/.
"""

from __future__ import annotations

from typing import Protocol

from ortools.sat.python import cp_model

from ..constraints.base import AssignmentVars
from ..problem import Problem

# (linear expression, integer weight): the objective gains expr * weight.
ObjectiveTerm = tuple[cp_model.LinearExpr, int]


class Preference(Protocol):
    name: str  # short id, e.g. "at_most_one_team_lead"
    description: str  # human sentence: what rota feature this encourages

    def objective_terms(
        self, model: cp_model.CpModel, x: AssignmentVars, problem: Problem
    ) -> list[ObjectiveTerm]: ...
