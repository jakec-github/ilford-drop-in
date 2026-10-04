"""Ensures no volunteer works consecutive shifts.

Adjacency is by shift index (i conflicts with i±1), not calendar
distance — matching the Go allocator. The boundary with the previous
rota also counts: history is recorded per group, so a volunteer whose
group was present on the last historical shift cannot take shift 0 of
this rota.

The rule binds the allocator, not people's decisions (ADR 0010). Two
pins on consecutive shifts are both honoured, and a pin on shift 0 is
honoured across the history boundary. A pin still counts as a shift
worked, so the allocator never places the same person beside one.
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..problem import Problem
from .base import Vars


class NoBackToBackConstraint:
    name = "no_back_to_back"
    description = (
        "no volunteer works consecutive shifts, including the boundary "
        "from the previous rota's last shift, unless pins put them there"
    )

    def apply(
        self, model: cp_model.CpModel, x: Vars, problem: Problem
    ) -> None:
        for v in problem.volunteers:
            for shift in problem.shifts[:-1]:
                # Only two pins together are exempt. With one, the pair still
                # holds, and the pin's attendance bars the shift beside it.
                if problem.forced_by_pin(v, shift.index) and problem.forced_by_pin(
                    v, shift.index + 1
                ):
                    continue
                model.Add(
                    x.attend[(v.id, shift.index)] + x.attend[(v.id, shift.index + 1)] <= 1
                )
            if (
                problem.shifts
                and v.group_key in problem.last_historical_group_keys
                and not problem.forced_by_pin(v, 0)
            ):
                model.Add(x.attend[(v.id, 0)] == 0)


CONSTRAINT = NoBackToBackConstraint()
