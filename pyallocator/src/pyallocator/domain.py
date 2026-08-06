"""Frozen dataclasses mirroring the Go <-> Python JSON contract.

Input types describe the allocation problem as sent by the Go CLI
(resolved groups, shift specs, preallocations). Output types describe
the solved rota returned on stdout. Field names match the snake_case
JSON contract exactly; see serialization.py for the dict conversion.
"""

from __future__ import annotations

from dataclasses import dataclass, field

# The only gender string with semantics (mirrors allocator.GenderMale in Go).
GENDER_MALE = "Male"


@dataclass(frozen=True)
class Role:
    """A job on a Shift. Volunteers hold Roles; Seats ask for them.

    max is the ceiling on this Role's Seats per Shift — None means uncapped,
    and there is exactly one uncapped Role. priority orders Seat filling,
    lowest first.
    """

    name: str
    max: int | None
    priority: int

    @property
    def capped(self) -> bool:
        return self.max is not None


@dataclass(frozen=True)
class Seat:
    """One entry in a Shift's Shape: count Seats asking for this Role."""

    role: str
    count: int


@dataclass(frozen=True)
class Preallocation:
    """A volunteer, or a custom entry, pinned to a Role on a Shift.

    Exactly one of volunteer_id and custom is non-empty.
    """

    volunteer_id: str
    custom: str
    role: str


@dataclass(frozen=True)
class Member:
    """One volunteer inside a group, with the Roles they hold."""

    id: str
    first_name: str
    last_name: str
    display_name: str
    gender: str
    roles: tuple[str, ...] = ()


@dataclass(frozen=True)
class Group:
    """Allocation unit: couples/families are allocated together.

    Groups are built in Go (allocator.InitVolunteerGroups); availability
    is already resolved to shift indices.
    """

    group_key: str
    members: tuple[Member, ...]
    available_shift_indices: tuple[int, ...]
    historical_allocation_count: int


@dataclass(frozen=True)
class ShiftSpec:
    """One shift in the rota being allocated.

    shape is the Shift's Seats, override-resolved in Go. It replaces a bare
    size, which could only describe a rota with one Role.
    """

    index: int
    date: str
    closed: bool
    shape: tuple[Seat, ...] = ()
    preallocations: tuple[Preallocation, ...] = ()


@dataclass(frozen=True)
class HistoricalShift:
    """A past shift; group_keys are Go-derived. Sorted ascending by date."""

    date: str
    group_keys: tuple[str, ...]


@dataclass(frozen=True)
class AllocationInput:
    """The full problem sent by Go on stdin.

    roles is the configured Role set every Seat, tick and pin is matched
    against.

    enabled_constraints names the optional rules this run applies, from the
    admin's Allocation Settings. Which rules those names may be is
    constraints.SWITCHABLE_CONSTRAINTS' business; an unrecognised one is
    ignored. Empty means the fundamentals only — a rule nobody has switched
    on is off, and there is no default list on this side (ADR 0006).
    """

    max_allocation_count: int
    shifts: tuple[ShiftSpec, ...]
    groups: tuple[Group, ...]
    roles: tuple[Role, ...] = ()
    enabled_constraints: tuple[str, ...] = ()
    historical_shifts: tuple[HistoricalShift, ...] = ()


@dataclass(frozen=True)
class Assignment:
    """One filled Seat: who is in it, and what Role it is.

    Exactly one of volunteer_id and custom is set, as on Preallocation
    coming the other way — a custom entry is free text the rota creator
    typed ("St John's team"), not a volunteer the system knows.
    """

    volunteer_id: str
    custom: str
    role: str


@dataclass(frozen=True)
class OutputShift:
    """One solved shift.

    assignments are the Seats that ended up filled: the solver's own, in
    canonical volunteer order, then the custom preallocations echoed back
    in input order. A Seat nobody filled is simply absent — which is how
    "this shift has no team lead" is said, and it is common.
    """

    index: int
    date: str
    size: int
    closed: bool
    assignments: tuple[Assignment, ...]
    allocated_group_keys: tuple[str, ...]


@dataclass(frozen=True)
class Diagnostics:
    """Solve statistics, for logs and sanity checks — never for control flow.

    num_variables counts every decision variable in the model, whatever
    the model is made of. It is a size indicator, so callers should treat
    an exact count as an implementation detail and assert lower bounds.
    """

    solve_time_seconds: float
    num_groups: int
    num_variables: int
    constraints_applied: tuple[str, ...]


@dataclass(frozen=True)
class AllocationOutput:
    """The solved rota returned to Go on stdout.

    success is true iff solver_status is OPTIMAL or FEASIBLE. INFEASIBLE
    is a well-formed result (success=false, empty shifts), not a crash.
    """

    solver_status: str
    success: bool
    error: str
    objective_value: int
    shifts: tuple[OutputShift, ...] = ()
    diagnostics: Diagnostics | None = None
