"""Caps every volunteer at one shift per calendar month.

The month is the YYYY-MM prefix of a shift's ISO date; the allocator does
no real date arithmetic (dates are opaque strings everywhere else). History
counts: a group present on a historical shift in month M is barred from
every current shift in month M, so a volunteer who already worked earlier
this month in the previous rota is not scheduled again. Matching is by
group key, mirroring no_back_to_back.
"""

from __future__ import annotations

from collections import defaultdict

from ortools.sat.python import cp_model

from ..problem import Problem
from .base import AssignmentVars


class OneShiftPerMonthConstraint:
    name = "one_shift_per_month"
    description = (
        "no volunteer works more than one shift per calendar month, "
        "counting shifts already worked in the previous rota"
    )

    def apply(
        self, model: cp_model.CpModel, x: AssignmentVars, problem: Problem
    ) -> None:
        months_to_indices: dict[str, list[int]] = defaultdict(list)
        for shift in problem.shifts:
            months_to_indices[shift.date[:7]].append(shift.index)

        for v in problem.volunteers:
            worked = problem.historical_group_months.get(v.group_key, frozenset())
            for month, indices in months_to_indices.items():
                # A month already worked in history leaves no room for another.
                cap = 0 if month in worked else 1
                model.Add(sum(x[(v.id, i)] for i in indices) <= cap)


CONSTRAINT = OneShiftPerMonthConstraint()
