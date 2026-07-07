"""Constraint protocol: a hard rule that forbids impossible allocations.

Each constraint module ensures exactly one rota feature, stated in its
module docstring and `description`. Constraints only ever restrict the
model; anything that trades off against other goals is a preference
(see preferences/base.py).
"""

from __future__ import annotations

from typing import Protocol

from ortools.sat.python import cp_model

from ..problem import Problem

# x[(volunteer_id, shift_index)] -> BoolVar: "this volunteer works this
# shift". Group atomicity is the grouping constraint, not the variables.
AssignmentVars = dict[tuple[str, int], cp_model.IntVar]


class Constraint(Protocol):
    name: str  # short id, e.g. "availability"
    description: str  # human sentence: what rota feature this ensures

    def apply(
        self, model: cp_model.CpModel, x: AssignmentVars, problem: Problem
    ) -> None: ...
