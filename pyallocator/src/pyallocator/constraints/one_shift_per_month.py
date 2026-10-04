"""Caps every volunteer at one shift per calendar month.

The month is the YYYY-MM prefix of a shift's ISO date; the allocator does
no real date arithmetic (dates are opaque strings everywhere else). History
counts: a group present on a historical shift in month M is barred from
every current shift in month M, so a volunteer who already worked earlier
this month in the previous rota is not scheduled again. Matching is by
group key, mirroring no_back_to_back.

The cap binds the allocator, not people's decisions (ADR 0010). Any number
of pins in one month are honoured, history or not, but they use up the
month: the allocator adds a shift there only while pins and history leave
room under the cap.
"""

from __future__ import annotations

from collections import defaultdict

from ortools.sat.python import cp_model

from ..problem import Problem
from .base import Vars


class OneShiftPerMonthConstraint:
    name = "one_shift_per_month"
    description = (
        "no volunteer works more than one shift per calendar month, "
        "counting shifts already worked in the previous rota, unless pins "
        "put them there"
    )

    def apply(
        self, model: cp_model.CpModel, x: Vars, problem: Problem
    ) -> None:
        months_to_indices: dict[str, list[int]] = defaultdict(list)
        for shift in problem.shifts:
            months_to_indices[shift.date[:7]].append(shift.index)

        for v in problem.volunteers:
            worked = problem.historical_group_months.get(v.group_key, frozenset())
            for month, indices in months_to_indices.items():
                # A month already worked in history leaves no room for another.
                cap = 0 if month in worked else 1
                pinned = [i for i in indices if problem.forced_by_pin(v, i)]
                chosen = [i for i in indices if not problem.forced_by_pin(v, i)]
                room = max(0, cap - len(pinned))
                model.Add(sum(x.attend[(v.id, i)] for i in chosen) <= room)


CONSTRAINT = OneShiftPerMonthConstraint()
