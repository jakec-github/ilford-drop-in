"""Ensures volunteers are only allocated to shifts they said they are
available for. Availability is resolved per group in Go, so every
member inherits its group's shifts. A volunteer pinned onto a shift is
exempt there: a pin applies regardless of availability. Only the pinned
person — their group-mates are allocator choices (ADR 0010).
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..problem import Problem
from .base import Vars


class AvailabilityConstraint:
    name = "availability"
    description = (
        "volunteers are only allocated to shifts they are available for "
        "(unless preallocated)"
    )

    def apply(
        self, model: cp_model.CpModel, x: Vars, problem: Problem
    ) -> None:
        for v in problem.volunteers:
            for shift in problem.shifts:
                if shift.index in v.available_shift_indices:
                    continue
                if problem.forced_by_pin(v, shift.index):
                    continue
                model.Add(x.attend[(v.id, shift.index)] == 0)


CONSTRAINT = AvailabilityConstraint()
