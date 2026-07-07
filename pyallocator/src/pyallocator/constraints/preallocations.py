"""Ensures preallocated volunteers and team leads are always on their
shift. Preallocations are group-atomic: forcing a pair brings the whole
group (partners included), whose ordinary members count toward capacity.

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
            model.Add(x[(group_key, shift_index)] == 1)


CONSTRAINT = PreallocationsConstraint()
