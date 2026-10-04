"""CLI contract: JSON in/out, exit codes (0 = well-formed run including
INFEASIBLE; 1 = invalid input/crash)."""

from __future__ import annotations

import io
import json
from pathlib import Path

import pytest

from pyallocator.cli import main

ROLES = [
    {"name": "Team lead", "priority": 1},
    {"name": "Service volunteer", "priority": 2},
]


def shape(size: int) -> list[dict]:
    return [
        {"role": "Team lead", "count": 1},
        {"role": "Service volunteer", "count": size},
    ]


# One shift, one volunteer. The volunteer is Male so the default
# male_required constraint lets a one-volunteer shift fill. It lives in a file
# because scripts/image-smoke.sh feeds the same input to the solver inside the
# built server image: this test keeps it a valid, solvable input.
VALID_INPUT = json.loads(
    (Path(__file__).parent / "testdata" / "cli_input.json").read_text()
)


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
    assert out["shifts"][0]["assignments"] == [
        {"volunteer_id": "v1", "custom": "", "role": "Service volunteer"}
    ]
    assert out["error"] == ""


class _Contradiction:
    name = "contradiction"
    description = "no rota satisfies this"

    def apply(self, model, x, problem) -> None:
        model.Add(sum(x.attend.values()) >= len(x.attend) + 1)


def test_infeasible_exit_zero(tmp_path, monkeypatch):
    # No input can make the model INFEASIBLE any more: pins are honoured
    # past every rule, and a pin pins one person (ADR 0010, #234). The path
    # still has to report rather than crash should one ever reach it, so a
    # contradiction is patched in as the run's only rule.
    monkeypatch.setattr(
        "pyallocator.api.constraints_for", lambda enabled: [_Contradiction()]
    )
    code, out = run_cli(tmp_path, VALID_INPUT)
    assert code == 0
    assert out["success"] is False
    assert out["solver_status"] == "INFEASIBLE"


def test_no_groups_solves_with_every_seat_unfilled(tmp_path):
    # Nobody has answered yet: every rota's state until the first reply
    # (issue #188). The solve is as ordinary as any other, and the rota it
    # answers with is empty but for the pins, which need nobody to answer.
    payload = json.loads(json.dumps(VALID_INPUT))
    payload["enabled_constraints"] = [
        "max_frequency",
        "male_required",
        "no_back_to_back",
        "one_shift_per_month",
    ]
    payload["groups"] = []
    payload["shifts"].append(
        {
            "index": 1,
            "date": "2026-07-20",
            "shape": shape(2),
            "preallocations": [
                {"volunteer_id": "", "custom": "St John's", "role": "Service volunteer"}
            ],
        }
    )
    code, out = run_cli(tmp_path, payload)
    assert code == 0
    assert out["success"] is True
    assert out["solver_status"] == "OPTIMAL"
    assert out["shifts"][0]["assignments"] == []
    assert out["shifts"][1]["assignments"] == [
        {"volunteer_id": "", "custom": "St John's", "role": "Service volunteer"}
    ]


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
    payload["shifts"][0]["preallocations"] = [
        {"volunteer_id": "nobody", "custom": "", "role": "Service volunteer"}
    ]
    code, out = run_cli(tmp_path, payload)
    assert code == 1
    assert "nobody" in out["error"]


def test_stdin_stdout(monkeypatch, capsys):
    monkeypatch.setattr("sys.stdin", io.StringIO(json.dumps(VALID_INPUT)))
    code = main([])
    assert code == 0
    out = json.loads(capsys.readouterr().out)
    assert out["success"] is True
