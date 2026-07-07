"""Ensures preallocated volunteers and team leads are always on their
shift. Preallocations are group-atomic: forcing a pair forces every
member of the group (partners included), whose ordinary members count
toward capacity — explicit here rather than relying on the grouping
constraint, so the guarantee holds even solved in isolation.

Resolution of volunteer ids to groups — and the error cases (unknown
id, non-team-lead designated as TL) — happens in problem.py, because
other constraints (availability) also need the resolved pairs.
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..problem import Problem
from .base import AssignmentVars


class PreallocationsConstraint:
    name = "preallocations"
    description = "preallocated volunteers and team leads are always on their shift"

    def apply(
        self, model: cp_model.CpModel, x: AssignmentVars, problem: Problem
    ) -> None:
        for group_key, shift_index in sorted(problem.preallocated_pairs):
            for member in problem.group_by_key[group_key].members:
                model.Add(x[(member.id, shift_index)] == 1)


CONSTRAINT = PreallocationsConstraint()
