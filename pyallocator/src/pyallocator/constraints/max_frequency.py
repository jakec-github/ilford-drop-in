"""Ensures no volunteer a max_frequency rule covers is allocated more of the
rota's shifts than its cap. The cap arrives as a count of shifts, computed in
Go from the Organiser's percentage (model.MaxAllocationCount). Under the
grouping constraint it binds the rest of a covered volunteer's group too,
since members of a group always work the same shifts.

The cap binds the allocator, not people's decisions (ADR 0010). Pins
count toward it but are never limited by it: a volunteer pinned past the
cap keeps every pin, and the allocator gives them nothing more.
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..domain import Rule
from ..problem import Problem
from .base import Vars


class MaxFrequencyConstraint:
    name = "max_frequency"
    description = (
        "no volunteer covered exceeds the rule's cap for the rota, "
        "unless pins put them past it"
    )

    def __init__(self, rule: Rule) -> None:
        self.volunteer_ids = frozenset(rule.volunteer_ids)
        self.cap = rule.value

    def apply(
        self, model: cp_model.CpModel, x: Vars, problem: Problem
    ) -> None:
        for v in problem.volunteers:
            if v.id not in self.volunteer_ids:
                continue
            pinned = [s.index for s in problem.shifts if problem.forced_by_pin(v, s.index)]
            chosen = [
                s.index for s in problem.shifts if not problem.forced_by_pin(v, s.index)
            ]
            room = max(0, self.cap - len(pinned))
            model.Add(sum(x.attend[(v.id, i)] for i in chosen) <= room)
