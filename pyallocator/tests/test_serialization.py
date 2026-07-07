"""Contract round-trip and input validation tests."""

from __future__ import annotations

import pytest

from pyallocator.domain import AllocationOutput, Diagnostics, OutputShift
from pyallocator.serialization import InputError, output_to_dict, parse_input

VALID_INPUT = {
    "max_allocation_count": 4,
    "shifts": [
        {
            "index": 0,
            "date": "2026-07-13",
            "size": 4,
            "closed": False,
            "custom_preallocations": ["St John's team"],
            "preallocated_volunteer_ids": ["vol-1"],
            "preallocated_team_lead_id": "vol-9",
        },
        {"index": 1, "date": "2026-07-20", "size": 3},
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
                    "is_team_lead": False,
                },
                {
                    "id": "vol-9",
                    "first_name": "Bob",
                    "last_name": "Smith",
                    "display_name": "Bob S",
                    "gender": "Male",
                    "is_team_lead": True,
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
    assert len(parsed.shifts) == 2
    shift0 = parsed.shifts[0]
    assert shift0.custom_preallocations == ("St John's team",)
    assert shift0.preallocated_volunteer_ids == ("vol-1",)
    assert shift0.preallocated_team_lead_id == "vol-9"
    assert not shift0.closed
    # Optional fields default sensibly.
    shift1 = parsed.shifts[1]
    assert shift1.custom_preallocations == ()
    assert shift1.preallocated_team_lead_id == ""
    group = parsed.groups[0]
    assert group.group_key == "couple_alice_bob"
    assert group.members[1].is_team_lead
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
                team_lead_id="vol-9",
                volunteer_ids=("vol-1", "vol-2"),
                custom_preallocations=("St John's team",),
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
                "team_lead_id": "vol-9",
                "volunteer_ids": ["vol-1", "vol-2"],
                "custom_preallocations": ["St John's team"],
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
