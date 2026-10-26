"""Caps every volunteer a monthly_cap rule covers at `value` shifts in any
one calendar month. A cap of one is one shift a month.

The month is the YYYY-MM prefix of a shift's ISO date; the allocator does
no real date arithmetic (dates are opaque strings everywhere else). History
counts shift by shift: each shift a group worked in month M in the previous
rota uses one of that month's places. Matching is by group key, mirroring
shift_gap.

The cap binds the allocator, not people's decisions (ADR 0010). Any number
of pins in one month are honoured, history or not, but they use up the
month: the allocator adds a shift there only while pins and history leave
room under the cap.
"""

from __future__ import annotations

from collections import defaultdict

from ortools.sat.python import cp_model

from ..domain import Rule
from ..problem import Problem
from .base import Vars


class MonthlyCapConstraint:
    name = "monthly_cap"
    description = (
        "no covered volunteer works more than the rule's cap of shifts in a "
        "calendar month, counting shifts already worked in the previous rota, "
        "unless pins put them there"
    )

    def __init__(self, rule: Rule) -> None:
        self.volunteer_ids = frozenset(rule.volunteer_ids)
        self.cap = rule.value

    def apply(
        self, model: cp_model.CpModel, x: Vars, problem: Problem
    ) -> None:
        months_to_indices: dict[str, list[int]] = defaultdict(list)
        for shift in problem.shifts:
            months_to_indices[shift.date[:7]].append(shift.index)

        for v in problem.volunteers:
            if v.id not in self.volunteer_ids:
                continue
            worked = problem.historical_month_counts.get(v.group_key, {})
            for month, indices in months_to_indices.items():
                pinned = [i for i in indices if problem.forced_by_pin(v, i)]
                chosen = [i for i in indices if not problem.forced_by_pin(v, i)]
                room = max(0, self.cap - worked.get(month, 0) - len(pinned))
                model.Add(sum(x.attend[(v.id, i)] for i in chosen) <= room)
