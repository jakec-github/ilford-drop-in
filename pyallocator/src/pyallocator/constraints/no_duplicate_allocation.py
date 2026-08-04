"""Ensures a volunteer can never appear twice on the same shift.

This is structural rather than a CP-SAT constraint. One attendance var per
(volunteer, shift) pair makes appearing twice unrepresentable, and equating
it with the sum of that pair's role vars — done in model_builder — makes
filling two Seats on one shift unrepresentable too. apply() validates both
halves of that invariant, so a model built without them fails loudly rather
than quietly allowing a double allocation.
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..problem import Problem
from .base import Vars


class NoDuplicateAllocationConstraint:
    name = "no_duplicate_allocation"
    description = (
        "a volunteer appears at most once per shift, in one Role they may fill "
        "there (structural: one attendance variable per volunteer-shift pair, "
        "equal to the sum of their role variables for it)"
    )

    def apply(self, model: cp_model.CpModel, x: Vars, problem: Problem) -> None:
        expected = {
            (v.id, shift.index) for v in problem.volunteers for shift in problem.shifts
        }
        if set(x.attend.keys()) != expected:
            raise AssertionError(
                "model must have exactly one attendance variable per "
                f"(volunteer, shift) pair: expected {len(expected)} vars, got "
                f"{len(x.attend)}"
            )

        # A role var may only exist where the volunteer may fill the Role on
        # that shift (Problem.may_fill: they hold it, or a pin grants it there)
        # and the shift asks for it; anything else is a Seat that could be
        # filled by someone ineligible.
        volunteers_by_id = {v.id: v for v in problem.volunteers}
        shapes = {
            shift.index: {seat.role for seat in shift.shape if seat.count > 0}
            for shift in problem.shifts
        }
        for vol_id, shift_index, role in x.role:
            volunteer = volunteers_by_id.get(vol_id)
            if volunteer is None or not problem.may_fill(volunteer, shift_index, role):
                raise AssertionError(
                    f"role variable ({vol_id}, {shift_index}, {role}) exists for "
                    "a volunteer who may not fill that Role on that shift"
                )
            if role not in shapes.get(shift_index, set()):
                raise AssertionError(
                    f"role variable ({vol_id}, {shift_index}, {role}) exists for "
                    "a Role that shift's Shape does not ask for"
                )


CONSTRAINT = NoDuplicateAllocationConstraint()
