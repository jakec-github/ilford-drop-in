"""Constraint protocol: a hard rule that forbids impossible allocations.

Each constraint module ensures exactly one rota feature, stated in its
module docstring and `description`. Constraints only ever restrict the
model; anything that trades off against other goals is a preference
(see preferences/base.py).
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Protocol

from ortools.sat.python import cp_model

from ..problem import Problem


@dataclass(frozen=True)
class Vars:
    """The model's decision variables.

    `role[(volunteer_id, shift_index, role_name)]` is "this volunteer fills a
    Seat of this Role on this shift". It exists only where the volunteer holds
    the Role and the shift's Shape asks for it.

    `attend[(volunteer_id, shift_index)]` is "this volunteer works this shift"
    at all, and equals the sum of their role vars for it. Equating that sum
    with a boolean is what holds a person to one Seat per shift: there is no
    separate exclusion constraint, and so none to forget.

    Rules about *whether* someone works — availability, grouping, frequency —
    read attend; rules about *what they do* read role. Group atomicity is the
    grouping constraint, not the variable structure.
    """

    attend: dict[tuple[str, int], cp_model.IntVar]
    role: dict[tuple[str, int, str], cp_model.IntVar]

    def __len__(self) -> int:
        return len(self.attend) + len(self.role)


class Constraint(Protocol):
    name: str  # short id, e.g. "availability"
    description: str  # human sentence: what rota feature this ensures

    def apply(self, model: cp_model.CpModel, x: Vars, problem: Problem) -> None: ...
