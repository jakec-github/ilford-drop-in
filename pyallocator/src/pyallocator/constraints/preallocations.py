"""Ensures preallocated volunteers are always on their shift, in the Role
they were pinned to. Forcing the Role forces attendance with it: the model
equates the two. A pin says what someone will do and not merely that they
will be there.

A pin pins the one person it names (ADR 0010). Their group-mates are
allocator choices, held to every rule; grouping lets the group split on
that shift, and the solve keeps the mates with them wherever the rules
allow (solver.py). Forcing the mates instead would let a pin make the solve
infeasible.

A pin to a Role the volunteer does not hold is honoured rather than
rejected: it grants them that Seat on that shift alone (Problem.may_fill),
because a pin records a decision already taken and the roster not yet
saying so is the roster lagging. A pin past the Shape — more people in a
Role than its Seats, or a Role the Shape has none of — is honoured too
(Problem.seat_roles, ADR 0010): the Shape bounds the allocator only.

Resolution of volunteer ids to groups — and the error case, an unknown
id — happens in problem.py, because other constraints (availability,
grouping) also need the resolved pins.
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..problem import Problem
from .base import Vars


class PreallocationsConstraint:
    name = "preallocations"
    description = "preallocated volunteers are always on their shift, in their Role"

    def apply(self, model: cp_model.CpModel, x: Vars, problem: Problem) -> None:
        for (vol_id, shift_index), role in sorted(problem.preallocated_roles.items()):
            model.Add(x.role[(vol_id, shift_index, role)] == 1)


CONSTRAINT = PreallocationsConstraint()
