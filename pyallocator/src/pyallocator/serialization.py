"""dict <-> dataclass conversion for the JSON contract, with input validation.

parse_input raises InputError (with a path-like message) for malformed
input; the CLI maps that to exit code 1. Output serialization is
best-effort faithful: field names match the contract exactly.
"""

from __future__ import annotations

from typing import Any

from .domain import (
    AllocationInput,
    AllocationOutput,
    Diagnostics,
    Group,
    HistoricalShift,
    Member,
    OutputShift,
    ShiftSpec,
)


class InputError(ValueError):
    """Raised when the input JSON does not match the contract."""


def _require(d: dict[str, Any], key: str, typ: type, where: str) -> Any:
    if key not in d:
        raise InputError(f"{where}: missing required field '{key}'")
    value = d[key]
    if typ is float and isinstance(value, int):
        value = float(value)
    if typ is int and isinstance(value, bool):
        raise InputError(f"{where}.{key}: expected {typ.__name__}, got bool")
    if not isinstance(value, typ):
        raise InputError(
            f"{where}.{key}: expected {typ.__name__}, got {type(value).__name__}"
        )
    return value


def _optional(d: dict[str, Any], key: str, typ: type, default: Any, where: str) -> Any:
    if key not in d or d[key] is None:
        return default
    return _require(d, key, typ, where)


def _str_tuple(d: dict[str, Any], key: str, where: str) -> tuple[str, ...]:
    raw = _optional(d, key, list, [], where)
    for i, item in enumerate(raw):
        if not isinstance(item, str):
            raise InputError(f"{where}.{key}[{i}]: expected str, got {type(item).__name__}")
    return tuple(raw)


def _int_tuple(d: dict[str, Any], key: str, where: str) -> tuple[int, ...]:
    raw = _optional(d, key, list, [], where)
    for i, item in enumerate(raw):
        if isinstance(item, bool) or not isinstance(item, int):
            raise InputError(f"{where}.{key}[{i}]: expected int, got {type(item).__name__}")
    return tuple(raw)


def _parse_member(d: dict[str, Any], where: str) -> Member:
    if not isinstance(d, dict):
        raise InputError(f"{where}: expected object, got {type(d).__name__}")
    return Member(
        id=_require(d, "id", str, where),
        first_name=_require(d, "first_name", str, where),
        last_name=_require(d, "last_name", str, where),
        display_name=_optional(d, "display_name", str, "", where),
        gender=_optional(d, "gender", str, "", where),
        is_team_lead=_optional(d, "is_team_lead", bool, False, where),
    )


def _parse_group(d: dict[str, Any], where: str) -> Group:
    if not isinstance(d, dict):
        raise InputError(f"{where}: expected object, got {type(d).__name__}")
    members_raw = _require(d, "members", list, where)
    if not members_raw:
        raise InputError(f"{where}.members: group must have at least one member")
    members = tuple(
        _parse_member(m, f"{where}.members[{i}]") for i, m in enumerate(members_raw)
    )
    return Group(
        group_key=_require(d, "group_key", str, where),
        members=members,
        available_shift_indices=_int_tuple(d, "available_shift_indices", where),
        historical_allocation_count=_optional(
            d, "historical_allocation_count", int, 0, where
        ),
    )


def _parse_shift(d: dict[str, Any], where: str) -> ShiftSpec:
    if not isinstance(d, dict):
        raise InputError(f"{where}: expected object, got {type(d).__name__}")
    return ShiftSpec(
        index=_require(d, "index", int, where),
        date=_require(d, "date", str, where),
        size=_require(d, "size", int, where),
        closed=_optional(d, "closed", bool, False, where),
        custom_preallocations=_str_tuple(d, "custom_preallocations", where),
        preallocated_volunteer_ids=_str_tuple(d, "preallocated_volunteer_ids", where),
        preallocated_team_lead_id=_optional(
            d, "preallocated_team_lead_id", str, "", where
        ),
    )


def _parse_historical_shift(d: dict[str, Any], where: str) -> HistoricalShift:
    if not isinstance(d, dict):
        raise InputError(f"{where}: expected object, got {type(d).__name__}")
    return HistoricalShift(
        date=_require(d, "date", str, where),
        group_keys=_str_tuple(d, "group_keys", where),
    )


def parse_input(data: Any) -> AllocationInput:
    """Convert a decoded-JSON dict into an AllocationInput, validating shape."""
    if not isinstance(data, dict):
        raise InputError(f"input: expected object, got {type(data).__name__}")

    shifts_raw = _require(data, "shifts", list, "input")
    groups_raw = _require(data, "groups", list, "input")
    historical_raw = _optional(data, "historical_shifts", list, [], "input")

    shifts = tuple(
        _parse_shift(s, f"input.shifts[{i}]") for i, s in enumerate(shifts_raw)
    )
    for i, shift in enumerate(shifts):
        if shift.index != i:
            raise InputError(
                f"input.shifts[{i}]: index {shift.index} out of order "
                "(shifts must be sorted with contiguous indices from 0)"
            )

    groups = tuple(
        _parse_group(g, f"input.groups[{i}]") for i, g in enumerate(groups_raw)
    )
    seen_keys: set[str] = set()
    for i, group in enumerate(groups):
        if group.group_key in seen_keys:
            raise InputError(f"input.groups[{i}]: duplicate group_key '{group.group_key}'")
        seen_keys.add(group.group_key)
        for idx in group.available_shift_indices:
            if idx < 0 or idx >= len(shifts):
                raise InputError(
                    f"input.groups[{i}]: available shift index {idx} out of range"
                )

    return AllocationInput(
        max_allocation_count=_require(data, "max_allocation_count", int, "input"),
        shifts=shifts,
        groups=groups,
        historical_shifts=tuple(
            _parse_historical_shift(h, f"input.historical_shifts[{i}]")
            for i, h in enumerate(historical_raw)
        ),
    )


def output_to_dict(output: AllocationOutput) -> dict[str, Any]:
    """Convert an AllocationOutput to a JSON-serializable dict."""
    result: dict[str, Any] = {
        "solver_status": output.solver_status,
        "success": output.success,
        "error": output.error,
        "objective_value": output.objective_value,
        "shifts": [
            {
                "index": s.index,
                "date": s.date,
                "size": s.size,
                "closed": s.closed,
                "team_lead_id": s.team_lead_id,
                "volunteer_ids": list(s.volunteer_ids),
                "custom_preallocations": list(s.custom_preallocations),
                "allocated_group_keys": list(s.allocated_group_keys),
            }
            for s in output.shifts
        ],
    }
    if output.diagnostics is not None:
        result["diagnostics"] = {
            "solve_time_seconds": output.diagnostics.solve_time_seconds,
            "num_groups": output.diagnostics.num_groups,
            "num_variables": output.diagnostics.num_variables,
            "constraints_applied": list(output.diagnostics.constraints_applied),
        }
    return result
