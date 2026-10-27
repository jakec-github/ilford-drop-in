"""Contract round-trip and input validation tests."""

from __future__ import annotations

import copy

import pytest

from pyallocator.domain import (
    AllocationOutput,
    Assignment,
    Diagnostics,
    OutputShift,
    Rule,
)
from pyallocator.serialization import InputError, output_to_dict, parse_input

VALID_INPUT = {
    "roles": [
        {"name": "Team lead", "priority": 1},
        {"name": "Service volunteer", "priority": 2},
    ],
    "rules": [
        {"kind": "shift_requires", "volunteer_ids": [], "value": 0, "option": "Male"},
        {"kind": "shift_gap", "volunteer_ids": ["vol-1"], "value": 2},
    ],
    "shifts": [
        {
            "index": 0,
            "id": "shift-0",
            "date": "2026-07-13",
            "closed": False,
            "shape": [
                {"role": "Team lead", "count": 1},
                {"role": "Service volunteer", "count": 4},
            ],
            "pins": [
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
            "id": "shift-1",
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
    "historical_shifts": [
        {"id": "shift-past", "date": "2026-06-29", "group_keys": ["couple_x"]}
    ],
}


def test_parse_valid_input():
    parsed = parse_input(VALID_INPUT)
    assert parsed.rules == (
        Rule(kind="shift_requires", volunteer_ids=(), value=0, option="Male"),
        Rule(kind="shift_gap", volunteer_ids=("vol-1",), value=2),
    )
    assert [r.name for r in parsed.roles] == ["Team lead", "Service volunteer"]
    assert len(parsed.shifts) == 2
    shift0 = parsed.shifts[0]
    assert shift0.id == "shift-0"
    assert [(s.role, s.count) for s in shift0.shape] == [
        ("Team lead", 1),
        ("Service volunteer", 4),
    ]
    assert [(p.volunteer_id, p.custom, p.role) for p in shift0.pins] == [
        ("", "St John's team", "Service volunteer"),
        ("vol-1", "", "Service volunteer"),
        ("vol-9", "", "Team lead"),
    ]
    assert not shift0.closed
    # Optional fields default sensibly.
    shift1 = parsed.shifts[1]
    assert shift1.pins == ()
    assert not shift1.closed
    group = parsed.groups[0]
    assert group.group_key == "couple_alice_bob"
    assert group.members[1].roles == ("Team lead", "Service volunteer")
    assert group.available_shift_indices == (0, 1)
    assert group.historical_allocation_count == 3
    assert parsed.historical_shifts[0].id == "shift-past"
    assert parsed.historical_shifts[0].group_keys == ("couple_x",)


# A rule's kind is not checked here. A kind this build does not know is
# ignored by the solve rather than refused (ADR 0006), and so is a Volunteer
# Rule naming nobody in the problem.
def test_rules_are_optional_and_any_kind_parses():
    data = copy.deepcopy(VALID_INPUT)
    data.pop("rules")
    assert parse_input(data).rules == ()

    data["rules"] = [{"kind": "phase_of_the_moon", "volunteer_ids": ["nobody"]}]
    assert parse_input(data).rules == (
        Rule(kind="phase_of_the_moon", volunteer_ids=("nobody",), value=0),
    )


@pytest.mark.parametrize(
    "mutate,fragment",
    [
        (lambda d: d["rules"][1].pop("kind"), "kind"),
        (lambda d: d["rules"][1].update(value="2"), "expected int"),
        (lambda d: d["rules"][1].update(volunteer_ids=[3]), "expected str"),
        (lambda d: d.update(rules={}), "expected list"),
        (lambda d: d.pop("shifts"), "shifts"),
        (lambda d: d["shifts"][0].pop("index"), "index"),
        (lambda d: d["shifts"][0].pop("id"), "id"),
        (lambda d: d["historical_shifts"][0].pop("id"), "id"),
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
        (lambda d: d.pop("roles"), "roles"),
        (
            lambda d: d["roles"].append({"name": "Team lead", "priority": 3}),
            "must be unique",
        ),
        (
            lambda d: d["shifts"][0]["shape"].append(
                {"role": "Food collector", "count": 1}
            ),
            "shape names unknown role",
        ),
        (
            lambda d: d["shifts"][0]["pins"][0].update(role="Hot food"),
            "pin names unknown role",
        ),
        (
            lambda d: d["shifts"][0]["pins"][1].update(
                custom="Both at once"
            ),
            "exactly one of 'volunteer_id' and 'custom'",
        ),
    ],
)
def test_parse_rejects_bad_input(mutate, fragment):
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
                id="shift-0",
                date="2026-07-13",
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
                "id": "shift-0",
                "date": "2026-07-13",
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
