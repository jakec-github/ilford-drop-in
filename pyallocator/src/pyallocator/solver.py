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

A pin pins one person, and their group-mates join them whenever the rules
allow — ahead of every preference (ADR 0010, #234). CP-SAT has no hard
"if possible", and forcing the mates would let a pin make the solve
infeasible, so where a pinned volunteer has a group-mate the solve runs
twice. The first pass maximises only the mates on alongside their pinned
member, which can never be infeasible: none is always an answer. The
second floors that count at the first pass's best and solves the ordinary
objective, hinted with the first pass's rota. A dominating weight would do
the same only while it outweighed every other term put together. Each pass
has the full time limit; a first pass that runs it out floors the count at
the best it found. With no group-mate to keep, there is one pass.
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
    if built.mates_joined is None:
        return _solve(built.model)

    model = built.model
    model.Maximize(built.mates_joined)
    first = _solve(model)
    if not first.success:
        return first

    model.Add(built.mates_joined >= first.objective_value)
    for var in [*built.x.attend.values(), *built.x.role.values()]:
        model.AddHint(var, first.solver.Value(var))
    if built.objective is None:
        model.ClearObjective()
    else:
        model.Maximize(built.objective)
    second = _solve(model)
    return SolveResult(
        status=second.status,
        success=second.success,
        objective_value=second.objective_value,
        solve_time_seconds=first.solve_time_seconds + second.solve_time_seconds,
        solver=second.solver,
    )


def _solve(model: cp_model.CpModel) -> SolveResult:
    solver = cp_model.CpSolver()
    solver.parameters.max_time_in_seconds = 30
    solver.parameters.random_seed = 0
    solver.parameters.num_search_workers = 1
    solver.parameters.linearization_level = 2

    status_code = solver.Solve(model)
    status = _STATUS_NAMES.get(status_code, f"UNRECOGNISED({status_code})")
    success = status in SUCCESS_STATUSES
    return SolveResult(
        status=status,
        success=success,
        objective_value=int(solver.ObjectiveValue()) if success else 0,
        solve_time_seconds=solver.WallTime(),
        solver=solver,
    )
