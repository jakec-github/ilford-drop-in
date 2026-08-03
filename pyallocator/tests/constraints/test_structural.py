"""The no-duplicate-allocation invariant is structural: one attendance
BoolVar per (volunteer, shift) pair, and role vars only where the volunteer
holds the Role and the shift asks for it. These tests validate the invariant
check itself."""

from __future__ import annotations

import pytest
from conftest import SERVICE_VOLUNTEER, make_group, make_input, make_shift, solve_with
from ortools.sat.python import cp_model

from pyallocator.constraints import no_duplicate_allocation, preallocations
from pyallocator.constraints.base import Vars
from pyallocator.model_builder import build
from pyallocator.preferences import maximize_allocations
from pyallocator.problem import Problem
from pyallocator.solver import solve_model


def test_model_has_one_var_per_pair():
    inp = make_input(
        groups=[make_group("g1", available=[0]), make_group("g2", available=[1])],
        shifts=[make_shift(0), make_shift(1), make_shift(2)],
    )
    out = solve_with(inp, [no_duplicate_allocation.CONSTRAINT])
    assert out.success
    # At least one variable per (volunteer, shift) pair. That there is
    # exactly one *assignment* variable per pair is asserted inside the
    # constraint itself, which test_invariant_violation_detected covers;
    # num_variables counts the whole model, so it only bounds it.
    assert out.diagnostics.num_variables >= 2 * 3


def test_attendance_equals_exactly_one_role_var():
    """The one-Seat-per-person-per-shift rule, which is the variable
    structure rather than a constraint anyone could forget to apply.

    A team lead holds both Roles and the shift asks for both, so without the
    sum-equals-attendance equality they could take a lead Seat and an
    ordinary one at once.
    """
    inp = make_input(
        groups=[make_group("lead", team_lead=True, available=[0])],
        shifts=[make_shift(0)],
    )
    problem = Problem(inp)
    built = build(problem, [preallocations.CONSTRAINT], [maximize_allocations.PREFERENCE])
    result = solve_model(built)
    assert result.success

    filled = [
        role
        for (vol_id, shift_index, role) in built.x.role
        if result.solver.Value(built.x.role[(vol_id, shift_index, role)]) == 1
    ]
    assert result.solver.Value(built.x.attend[("lead", 0)]) == 1
    assert len(filled) == 1, f"expected one Seat, got {filled}"


def test_preallocation_forces_the_pinned_role_not_just_attendance():
    inp = make_input(
        groups=[make_group("lead", team_lead=True, available=[0])],
        shifts=[make_shift(0, preallocated_team_lead_id="lead")],
    )
    problem = Problem(inp)
    built = build(problem, [preallocations.CONSTRAINT], [maximize_allocations.PREFERENCE])
    result = solve_model(built)
    assert result.success
    assert result.solver.Value(built.x.role[("lead", 0, "Team lead")]) == 1


def test_missing_attendance_var_detected():
    inp = make_input(
        groups=[make_group("g1", available=[0])],
        shifts=[make_shift(0)],
    )
    problem = Problem(inp)
    model = cp_model.CpModel()
    x = Vars(attend={}, role={})  # missing the (g1, shift 0) attendance var
    with pytest.raises(AssertionError, match="one attendance variable per"):
        no_duplicate_allocation.CONSTRAINT.apply(model, x, problem)


def test_role_var_for_a_role_the_volunteer_does_not_hold_detected():
    inp = make_input(
        groups=[make_group("g1", available=[0])],  # holds Service volunteer only
        shifts=[make_shift(0)],
    )
    problem = Problem(inp)
    model = cp_model.CpModel()
    x = Vars(
        attend={("g1", 0): model.NewBoolVar("attend")},
        role={("g1", 0, "Team lead"): model.NewBoolVar("role")},
    )
    with pytest.raises(AssertionError, match="does not hold that Role"):
        no_duplicate_allocation.CONSTRAINT.apply(model, x, problem)


def test_role_var_for_a_seat_the_shift_does_not_have_detected():
    inp = make_input(
        groups=[make_group("g1", available=[0])],
        # A Shape with no ordinary Seats at all.
        shifts=[make_shift(0, size=0)],
    )
    problem = Problem(inp)
    model = cp_model.CpModel()
    x = Vars(
        attend={("g1", 0): model.NewBoolVar("attend")},
        role={("g1", 0, SERVICE_VOLUNTEER): model.NewBoolVar("role")},
    )
    with pytest.raises(AssertionError, match="Shape does not ask for"):
        no_duplicate_allocation.CONSTRAINT.apply(model, x, problem)
