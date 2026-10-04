"""Ensures groups are atomic: couples/families work a shift together
or not at all.

This used to be structural (one variable per group); with per-volunteer
variables it is an explicit equality between each member and the
group's first member, per shift. Single-member groups need nothing.

A shift where a member is pinned is exempt for that group (ADR 0010). The
pin puts that one person on; the others are allocator choices held to every
rule, so the group splits there wherever the rules keep a member away. The
solve keeps them together wherever the rules allow (solver.py).
"""

from __future__ import annotations

from ortools.sat.python import cp_model

from ..problem import Problem
from .base import Vars


class GroupingConstraint:
    name = "grouping"
    description = "members of a group work each shift together or not at all"

    def apply(
        self, model: cp_model.CpModel, x: Vars, problem: Problem
    ) -> None:
        for group in problem.groups:
            first, *rest = group.members
            for member in rest:
                for shift in problem.shifts:
                    if (group.group_key, shift.index) in problem.pinned_group_shifts:
                        continue
                    model.Add(
                        x.attend[(member.id, shift.index)] == x.attend[(first.id, shift.index)]
                    )


CONSTRAINT = GroupingConstraint()
