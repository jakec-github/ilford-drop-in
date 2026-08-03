"""Ensures preallocated volunteers are always on their shift, in the Role
they were pinned to. Preallocations are group-atomic: forcing a pair forces
every member of the group (partners included), whose ordinary members count
toward capacity — explicit here rather than relying on the grouping
constraint, so the guarantee holds even solved in isolation.

The pinned person's Role is forced as well as their attendance, because a
pin says what someone will do and not merely that they will be there. Their
group-mates get no such constraint: they attend, and the solver picks their
Seat.

Resolution of volunteer ids to groups — and the error cases (unknown id,
pinned to a Role they do not hold or the shift has no Seat for) — happens in
problem.py, because other constraints (availability) also need the resolved
pairs.
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..problem import Problem
from .base import Vars


class PreallocationsConstraint:
    name = "preallocations"
    description = "preallocated volunteers are always on their shift, in their Role"

    def apply(self, model: cp_model.CpModel, x: Vars, problem: Problem) -> None:
        for group_key, shift_index in sorted(problem.preallocated_pairs):
            for member in problem.group_by_key[group_key].members:
                model.Add(x.attend[(member.id, shift_index)] == 1)

        for (vol_id, shift_index), role in sorted(problem.preallocated_roles.items()):
            model.Add(x.role[(vol_id, shift_index, role)] == 1)


CONSTRAINT = PreallocationsConstraint()
