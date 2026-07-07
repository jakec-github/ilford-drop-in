"""Ensures groups are only allocated to shifts they said they are
available for. Preallocated (group, shift) pairs are exempt: a
preallocation applies regardless of availability, matching the Go
allocator's behaviour.
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..problem import Problem
from .base import AssignmentVars


class AvailabilityConstraint:
    name = "availability"
    description = (
        "groups are only allocated to shifts they are available for "
        "(unless preallocated)"
    )

    def apply(
        self, model: cp_model.CpModel, x: AssignmentVars, problem: Problem
    ) -> None:
        for gv in problem.groups:
            available = set(gv.group.available_shift_indices)
            for shift in problem.shifts:
                if shift.index in available:
                    continue
                if (gv.key, shift.index) in problem.preallocated_pairs:
                    continue
                model.Add(x[(gv.key, shift.index)] == 0)


CONSTRAINT = AvailabilityConstraint()
