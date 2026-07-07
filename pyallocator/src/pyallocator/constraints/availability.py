"""Ensures volunteers are only allocated to shifts they said they are
available for. Availability is resolved per group in Go, so every
member inherits its group's shifts. Volunteers whose group is
preallocated onto a shift are exempt: a preallocation applies
regardless of availability, matching the Go allocator's behaviour.
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..problem import Problem
from .base import AssignmentVars


class AvailabilityConstraint:
    name = "availability"
    description = (
        "volunteers are only allocated to shifts they are available for "
        "(unless preallocated)"
    )

    def apply(
        self, model: cp_model.CpModel, x: AssignmentVars, problem: Problem
    ) -> None:
        for v in problem.volunteers:
            for shift in problem.shifts:
                if shift.index in v.available_shift_indices:
                    continue
                if (v.group_key, shift.index) in problem.preallocated_pairs:
                    continue
                model.Add(x[(v.id, shift.index)] == 0)


CONSTRAINT = AvailabilityConstraint()
