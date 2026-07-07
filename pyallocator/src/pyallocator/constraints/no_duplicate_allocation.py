"""Ensures a group can never appear twice on the same shift.

This is structural rather than a CP-SAT constraint: the model has
exactly one BoolVar per (group, shift) pair, so a duplicate allocation
is unrepresentable. apply() validates that invariant holds.
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..problem import Problem
from .base import AssignmentVars


class NoDuplicateAllocationConstraint:
    name = "no_duplicate_allocation"
    description = (
        "a group appears at most once per shift (structural: one decision "
        "variable per group-shift pair)"
    )

    def apply(
        self, model: cp_model.CpModel, x: AssignmentVars, problem: Problem
    ) -> None:
        expected = {
            (gv.key, shift.index)
            for gv in problem.groups
            for shift in problem.shifts
        }
        if set(x.keys()) != expected:
            raise AssertionError(
                "model must have exactly one variable per (group, shift) pair: "
                f"expected {len(expected)} vars, got {len(x)}"
            )


CONSTRAINT = NoDuplicateAllocationConstraint()
