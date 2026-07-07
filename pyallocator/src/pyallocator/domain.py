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
class Member:
    """One volunteer inside a group."""

    id: str
    first_name: str
    last_name: str
    display_name: str
    gender: str
    is_team_lead: bool


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
    """One shift in the rota being allocated. Size is override-resolved."""

    index: int
    date: str
    size: int
    closed: bool
    custom_preallocations: tuple[str, ...] = ()
    preallocated_volunteer_ids: tuple[str, ...] = ()
    preallocated_team_lead_id: str = ""


@dataclass(frozen=True)
class HistoricalShift:
    """A past shift; group_keys are Go-derived. Sorted ascending by date."""

    date: str
    group_keys: tuple[str, ...]


@dataclass(frozen=True)
class AllocationInput:
    """The full problem sent by Go on stdin."""

    max_allocation_count: int
    shifts: tuple[ShiftSpec, ...]
    groups: tuple[Group, ...]
    historical_shifts: tuple[HistoricalShift, ...] = ()


@dataclass(frozen=True)
class OutputShift:
    """One solved shift. team_lead_id is "" when no team lead (common)."""

    index: int
    date: str
    size: int
    closed: bool
    team_lead_id: str
    volunteer_ids: tuple[str, ...]
    custom_preallocations: tuple[str, ...]
    allocated_group_keys: tuple[str, ...]


@dataclass(frozen=True)
class Diagnostics:
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
