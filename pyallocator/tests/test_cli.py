"""CLI contract: JSON in/out, exit codes (0 = well-formed run including
INFEASIBLE; 1 = invalid input/crash)."""

from __future__ import annotations

import io
import json

import pytest

from pyallocator.cli import main

VALID_INPUT = {
    "max_allocation_count": 1,
    "shifts": [{"index": 0, "date": "2026-07-13", "size": 2}],
    "groups": [
        {
            "group_key": "Solo Volunteer",
            "members": [
                {
                    "id": "v1",
                    "first_name": "Solo",
                    "last_name": "Volunteer",
                    "display_name": "Solo",
                    # Male so the default male_required constraint lets a
                    # one-volunteer shift fill in this contract test.
                    "gender": "Male",
                    "is_team_lead": False,
                }
            ],
            "available_shift_indices": [0],
            "historical_allocation_count": 0,
        }
    ],
    "historical_shifts": [],
}


def run_cli(tmp_path, payload) -> tuple[int, dict]:
    input_path = tmp_path / "input.json"
    output_path = tmp_path / "output.json"
    input_path.write_text(json.dumps(payload) if isinstance(payload, dict) else payload)
    code = main(["--input", str(input_path), "--output", str(output_path)])
    return code, json.loads(output_path.read_text())


def test_valid_input_exit_zero(tmp_path):
    code, out = run_cli(tmp_path, VALID_INPUT)
    assert code == 0
    assert out["success"] is True
    assert out["solver_status"] == "OPTIMAL"
    assert out["shifts"][0]["volunteer_ids"] == ["v1"]
    assert out["shifts"][0]["team_lead_id"] == ""
    assert out["error"] == ""


def test_infeasible_exit_zero(tmp_path):
    payload = json.loads(json.dumps(VALID_INPUT))
    # Two singles preallocated onto a size-1 shift -> INFEASIBLE.
    payload["shifts"][0]["size"] = 1
    payload["shifts"][0]["preallocated_volunteer_ids"] = ["v1", "v2"]
    payload["groups"].append(
        {
            "group_key": "Other Volunteer",
            "members": [
                {
                    "id": "v2",
                    "first_name": "Other",
                    "last_name": "Volunteer",
                    "display_name": "Other",
                    "gender": "Male",
                    "is_team_lead": False,
                }
            ],
            "available_shift_indices": [0],
            "historical_allocation_count": 0,
        }
    )
    code, out = run_cli(tmp_path, payload)
    assert code == 0
    assert out["success"] is False
    assert out["solver_status"] == "INFEASIBLE"


def test_malformed_json_exit_one(tmp_path, capsys):
    code, out = run_cli(tmp_path, "{not json")
    assert code == 1
    assert out["success"] is False
    assert out["error"]
    assert capsys.readouterr().err


def test_contract_violation_exit_one(tmp_path):
    payload = json.loads(json.dumps(VALID_INPUT))
    del payload["shifts"][0]["date"]
    code, out = run_cli(tmp_path, payload)
    assert code == 1
    assert "date" in out["error"]


def test_unknown_preallocated_id_exit_one(tmp_path):
    payload = json.loads(json.dumps(VALID_INPUT))
    payload["shifts"][0]["preallocated_volunteer_ids"] = ["nobody"]
    code, out = run_cli(tmp_path, payload)
    assert code == 1
    assert "nobody" in out["error"]


def test_stdin_stdout(monkeypatch, capsys):
    monkeypatch.setattr("sys.stdin", io.StringIO(json.dumps(VALID_INPUT)))
    code = main([])
    assert code == 0
    out = json.loads(capsys.readouterr().out)
    assert out["success"] is True
