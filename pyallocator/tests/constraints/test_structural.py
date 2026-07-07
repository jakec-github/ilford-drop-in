"""The no-duplicate-allocation invariant is structural: one BoolVar per
(volunteer, shift) pair. These tests validate the invariant check itself."""

from __future__ import annotations

import pytest
from conftest import make_group, make_input, make_shift, solve_with
from ortools.sat.python import cp_model

from pyallocator.constraints import no_duplicate_allocation
from pyallocator.problem import Problem


def test_model_has_one_var_per_pair():
    inp = make_input(
        groups=[make_group("g1", available=[0]), make_group("g2", available=[1])],
        shifts=[make_shift(0), make_shift(1), make_shift(2)],
    )
    out = solve_with(inp, [no_duplicate_allocation.CONSTRAINT])
    assert out.success
    assert out.diagnostics.num_variables == 2 * 3


def test_invariant_violation_detected():
    inp = make_input(
        groups=[make_group("g1", available=[0])],
        shifts=[make_shift(0)],
    )
    problem = Problem(inp)
    model = cp_model.CpModel()
    x = {}  # missing the (volunteer g1, shift 0) variable
    with pytest.raises(AssertionError, match="one variable per"):
        no_duplicate_allocation.CONSTRAINT.apply(model, x, problem)
