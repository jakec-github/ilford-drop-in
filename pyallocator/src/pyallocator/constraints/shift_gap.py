"""Ensures at least `value` shifts off between any two shifts a volunteer
covered by a shift_gap rule works. A gap of one is no back-to-back shifts.

The gap is counted by shift index, not calendar distance, so in any run of
gap + 1 consecutive shifts a covered volunteer works at most one. A Closed
Shift is still a shift in that count — a week the drop-in did not open is a
week off.

The boundary with the previous rota counts as far back as the gap reaches.
History arrives as every shift of the previous rota, Closed ones included,
so a group present k shifts before this rota's first is barred from this
rota's first gap - k + 1 shifts. History is recorded per group, and under
the grouping constraint the rule binds a covered volunteer's whole group
anyway.

The rule binds the allocator, not people's decisions (ADR 0010). Pins inside
one window are all honoured, and a pin is honoured across the history
boundary. A pin still counts as a shift worked, so the allocator never
places the same person within the gap of one.
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..domain import Rule
from ..problem import Problem
from .base import Vars


class ShiftGapConstraint:
    name = "shift_gap"
    description = (
        "a covered volunteer has at least the rule's gap of shifts between "
        "any two they work, counting from the previous rota, unless pins put "
        "them closer"
    )

    def __init__(self, rule: Rule) -> None:
        self.volunteer_ids = frozenset(rule.volunteer_ids)
        self.gap = rule.value

    def apply(
        self, model: cp_model.CpModel, x: Vars, problem: Problem
    ) -> None:
        if self.gap < 1 or not problem.shifts:
            return
        last = len(problem.shifts) - 1
        for v in problem.volunteers:
            if v.id not in self.volunteer_ids:
                continue

            # Every run of gap + 1 shifts — or the whole rota, if it is
            # shorter — holds at most one. A pin in the run takes that one;
            # the allocator then adds nothing there.
            for start in range(max(1, last - self.gap + 1)):
                window = range(start, min(last, start + self.gap) + 1)
                pinned = [i for i in window if problem.forced_by_pin(v, i)]
                chosen = [i for i in window if not problem.forced_by_pin(v, i)]
                room = max(0, 1 - len(pinned))
                model.Add(sum(x.attend[(v.id, i)] for i in chosen) <= room)

            # A shift worked k shifts before this rota bars its first
            # gap - k + 1.
            for k, keys in enumerate(problem.historical_group_keys[: self.gap], start=1):
                if v.group_key not in keys:
                    continue
                for i in range(min(last, self.gap - k) + 1):
                    if not problem.forced_by_pin(v, i):
                        model.Add(x.attend[(v.id, i)] == 0)
