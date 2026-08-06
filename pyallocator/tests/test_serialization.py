"""Contract round-trip and input validation tests."""

from __future__ import annotations

import pytest

from pyallocator.domain import (
    AllocationOutput,
    Assignment,
    Diagnostics,
    OutputShift,
)
from pyallocator.serialization import InputError, output_to_dict, parse_input

VALID_INPUT = {
    "max_allocation_count": 4,
    "roles": [
        {"name": "Team lead", "max": 1, "priority": 1},
        {"name": "Service volunteer", "max": None, "priority": 2},
    ],
    "enabled_constraints": ["male_required"],
    "shifts": [
        {
            "index": 0,
            "date": "2026-07-13",
            "closed": False,
            "shape": [
                {"role": "Team lead", "count": 1},
                {"role": "Service volunteer", "count": 4},
            ],
            "preallocations": [
                {
                    "volunteer_id": "",
                    "custom": "St John's team",
                    "role": "Service volunteer",
                },
                {"volunteer_id": "vol-1", "custom": "", "role": "Service volunteer"},
                {"volunteer_id": "vol-9", "custom": "", "role": "Team lead"},
            ],
        },
        {
            "index": 1,
            "date": "2026-07-20",
            "shape": [{"role": "Service volunteer", "count": 3}],
        },
    ],
    "groups": [
        {
            "group_key": "couple_alice_bob",
            "members": [
                {
                    "id": "vol-1",
                    "first_name": "Alice",
                    "last_name": "Smith",
                    "display_name": "Alice S",
                    "gender": "Female",
                    "roles": ["Service volunteer"],
                },
                {
                    "id": "vol-9",
                    "first_name": "Bob",
                    "last_name": "Smith",
                    "display_name": "Bob S",
                    "gender": "Male",
                    "roles": ["Team lead", "Service volunteer"],
                },
            ],
            "available_shift_indices": [0, 1],
            "historical_allocation_count": 3,
        }
    ],
    "historical_shifts": [{"date": "2026-06-29", "group_keys": ["couple_x"]}],
}


def test_parse_valid_input():
    parsed = parse_input(VALID_INPUT)
    assert parsed.max_allocation_count == 4
    assert parsed.enabled_constraints == ("male_required",)
    assert [r.name for r in parsed.roles] == ["Team lead", "Service volunteer"]
    assert parsed.roles[0].max == 1
    assert parsed.roles[1].max is None
    assert len(parsed.shifts) == 2
    shift0 = parsed.shifts[0]
    assert [(s.role, s.count) for s in shift0.shape] == [
        ("Team lead", 1),
        ("Service volunteer", 4),
    ]
    assert [(p.volunteer_id, p.custom, p.role) for p in shift0.preallocations] == [
        ("", "St John's team", "Service volunteer"),
        ("vol-1", "", "Service volunteer"),
        ("vol-9", "", "Team lead"),
    ]
    assert not shift0.closed
    # Optional fields default sensibly.
    shift1 = parsed.shifts[1]
    assert shift1.preallocations == ()
    assert not shift1.closed
    group = parsed.groups[0]
    assert group.group_key == "couple_alice_bob"
    assert group.members[1].roles == ("Team lead", "Service volunteer")
    assert group.available_shift_indices == (0, 1)
    assert group.historical_allocation_count == 3
    assert parsed.historical_shifts[0].group_keys == ("couple_x",)


@pytest.mark.parametrize(
    "mutate,fragment",
    [
        (lambda d: d.pop("max_allocation_count"), "max_allocation_count"),
        (lambda d: d.pop("shifts"), "shifts"),
        (lambda d: d["shifts"][0].pop("index"), "index"),
        (lambda d: d["shifts"][0].update(index=5), "out of order"),
        (lambda d: d["groups"][0].pop("group_key"), "group_key"),
        (lambda d: d["groups"][0].update(members=[]), "at least one member"),
        (lambda d: d["groups"][0]["members"][0].pop("id"), "id"),
        (
            lambda d: d["groups"][0].update(available_shift_indices=[7]),
            "out of range",
        ),
        (
            lambda d: d["groups"].append(dict(d["groups"][0])),
            "duplicate group_key",
        ),
        (lambda d: d.update(max_allocation_count="4"), "expected int"),
        (lambda d: d.pop("roles"), "roles"),
        (
            lambda d: d["roles"].append({"name": "Hot food", "priority": 3}),
            "exactly one uncapped role",
        ),
        (
            lambda d: d["roles"].append({"name": "Team lead", "max": 2, "priority": 3}),
            "must be unique",
        ),
        (
            lambda d: d["shifts"][0]["shape"].append(
                {"role": "Food collector", "count": 1}
            ),
            "shape names unknown role",
        ),
        (
            lambda d: d["shifts"][0]["preallocations"][0].update(role="Hot food"),
            "preallocation names unknown role",
        ),
        (
            lambda d: d["shifts"][0]["preallocations"][1].update(
                custom="Both at once"
            ),
            "exactly one of 'volunteer_id' and 'custom'",
        ),
    ],
)
def test_parse_rejects_bad_input(mutate, fragment):
    import copy

    data = copy.deepcopy(VALID_INPUT)
    mutate(data)
    with pytest.raises(InputError, match=fragment):
        parse_input(data)


def test_parse_rejects_non_dict():
    with pytest.raises(InputError):
        parse_input([1, 2, 3])


def test_output_to_dict_shape():
    output = AllocationOutput(
        solver_status="OPTIMAL",
        success=True,
        error="",
        objective_value=23,
        shifts=(
            OutputShift(
                index=0,
                date="2026-07-13",
                size=4,
                closed=False,
                assignments=(
                    Assignment("vol-9", "", "Team lead"),
                    Assignment("vol-1", "", "Service volunteer"),
                    Assignment("vol-2", "", "Service volunteer"),
                    Assignment("", "St John's team", "Service volunteer"),
                ),
                allocated_group_keys=("couple_alice_bob", "Diana Green"),
            ),
        ),
        diagnostics=Diagnostics(
            solve_time_seconds=0.12,
            num_groups=18,
            num_variables=126,
            constraints_applied=("availability",),
        ),
    )
    d = output_to_dict(output)
    assert d == {
        "solver_status": "OPTIMAL",
        "success": True,
        "error": "",
        "objective_value": 23,
        "shifts": [
            {
                "index": 0,
                "date": "2026-07-13",
                "size": 4,
                "closed": False,
                "assignments": [
                    {"volunteer_id": "vol-9", "custom": "", "role": "Team lead"},
                    {
                        "volunteer_id": "vol-1",
                        "custom": "",
                        "role": "Service volunteer",
                    },
                    {
                        "volunteer_id": "vol-2",
                        "custom": "",
                        "role": "Service volunteer",
                    },
                    {
                        "volunteer_id": "",
                        "custom": "St John's team",
                        "role": "Service volunteer",
                    },
                ],
                "allocated_group_keys": ["couple_alice_bob", "Diana Green"],
            }
        ],
        "diagnostics": {
            "solve_time_seconds": 0.12,
            "num_groups": 18,
            "num_variables": 126,
            "constraints_applied": ["availability"],
        },
    }
