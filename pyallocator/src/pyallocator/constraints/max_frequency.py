"""Ensures no volunteer is allocated more shifts than the rota's
allocation cap (max_allocation_count, computed in Go from
MaxAllocationFrequency). Under the grouping constraint this equals the
old per-group cap, since members of a group always work the same shifts.

The cap binds the allocator, not people's decisions (ADR 0010). Pins
count toward it but are never limited by it: a volunteer pinned past the
cap keeps every pin, and the allocator gives them nothing more.
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..problem import Problem
from .base import Vars


class MaxFrequencyConstraint:
    name = "max_frequency"
    description = (
        "no volunteer exceeds the allocation cap for the rota, "
        "unless pins put them past it"
    )

    def apply(
        self, model: cp_model.CpModel, x: Vars, problem: Problem
    ) -> None:
        for v in problem.volunteers:
            pinned = [s.index for s in problem.shifts if problem.forced_by_pin(v, s.index)]
            chosen = [
                s.index for s in problem.shifts if not problem.forced_by_pin(v, s.index)
            ]
            room = max(0, problem.max_allocation_count - len(pinned))
            model.Add(sum(x.attend[(v.id, i)] for i in chosen) <= room)


CONSTRAINT = MaxFrequencyConstraint()
