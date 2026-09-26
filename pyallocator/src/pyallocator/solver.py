"""CP-SAT solver wrapper with deterministic parameters.

Rotas must be reproducible run-to-run, so the solver is pinned to a
fixed seed and a single search worker; the time limit keeps pathological
inputs from hanging the Go CLI.

The worker linearizes the whole model (linearization_level 2) rather than
CP-SAT's default of only its plain linear constraints. It changes no rule —
the rotas allowed are the same — only how tight a bound the solver can put
on the best one. At the default, a production-sized rota's bound never meets
the best rota found, so the solve runs out the time limit and returns
FEASIBLE: a rota that depends on how fast the machine was. At level 2 it is
proved OPTIMAL in well under a second (issue #224).
"""

from __future__ import annotations

from dataclasses import dataclass

from ortools.sat.python import cp_model

from .model_builder import BuiltModel

_STATUS_NAMES = {
    cp_model.OPTIMAL: "OPTIMAL",
    cp_model.FEASIBLE: "FEASIBLE",
    cp_model.INFEASIBLE: "INFEASIBLE",
    cp_model.MODEL_INVALID: "MODEL_INVALID",
    cp_model.UNKNOWN: "UNKNOWN",
}

SUCCESS_STATUSES = frozenset({"OPTIMAL", "FEASIBLE"})


@dataclass(frozen=True)
class SolveResult:
    status: str  # one of _STATUS_NAMES values
    success: bool  # status in SUCCESS_STATUSES
    objective_value: int
    solve_time_seconds: float
    solver: cp_model.CpSolver  # for reading variable values


def solve_model(built: BuiltModel) -> SolveResult:
    solver = cp_model.CpSolver()
    solver.parameters.max_time_in_seconds = 30
    solver.parameters.random_seed = 0
    solver.parameters.num_search_workers = 1
    solver.parameters.linearization_level = 2

    status_code = solver.Solve(built.model)
    status = _STATUS_NAMES.get(status_code, f"UNRECOGNISED({status_code})")
    success = status in SUCCESS_STATUSES
    return SolveResult(
        status=status,
        success=success,
        objective_value=int(solver.ObjectiveValue()) if success else 0,
        solve_time_seconds=solver.WallTime(),
        solver=solver,
    )
