"""Solving with ONLY the preallocations constraint, plus the resolution
error cases (which live in Problem construction)."""

from __future__ import annotations

import pytest
from conftest import (
    allocations_by_shift,
    make_group,
    make_input,
    make_member,
    make_shift,
    solve_with,
)
from pyallocator.constraints import preallocations
from pyallocator.problem import Problem, ProblemError

ONLY = [preallocations.CONSTRAINT]


def test_preallocated_volunteer_always_on_shift():
    # No preference at all: without the constraint the solver would
    # return the empty rota; the preallocation must force the pair.
    inp = make_input(
        groups=[make_group("g1", available=[0])],
        shifts=[make_shift(0, preallocated_volunteer_ids=["g1"])],
    )
    out = solve_with(inp, ONLY, preferences=[])
    assert out.success
    assert allocations_by_shift(out)[0] == ("g1",)


def test_partner_comes_along():
    couple = make_group(
        "couple", members=[make_member("a"), make_member("b")], available=[0]
    )
    inp = make_input(
        groups=[couple],
        shifts=[make_shift(0, preallocated_volunteer_ids=["a"])],
    )
    out = solve_with(inp, ONLY, preferences=[])
    assert out.success
    shift = out.shifts[0]
    assert set(shift.volunteer_ids) == {"a", "b"}


def test_preallocated_team_lead_designated():
    tl_group = make_group(
        "leads", members=[make_member("lead", is_team_lead=True)], available=[0]
    )
    inp = make_input(
        groups=[tl_group],
        shifts=[make_shift(0, preallocated_team_lead_id="lead")],
    )
    out = solve_with(inp, ONLY, preferences=[])
    assert out.success
    assert out.shifts[0].team_lead_id == "lead"


def test_multiple_ids_same_group_dedupe():
    couple = make_group(
        "couple", members=[make_member("a"), make_member("b")], available=[0]
    )
    inp = make_input(
        groups=[couple],
        shifts=[make_shift(0, preallocated_volunteer_ids=["a", "b"])],
    )
    out = solve_with(inp, ONLY, preferences=[])
    assert out.success
    assert allocations_by_shift(out)[0] == ("couple",)


def test_unknown_preallocated_volunteer_errors():
    inp = make_input(
        groups=[make_group("g1", available=[0])],
        shifts=[make_shift(0, preallocated_volunteer_ids=["nobody"])],
    )
    with pytest.raises(ProblemError, match="does not match any volunteer"):
        Problem(inp)


def test_unknown_preallocated_team_lead_errors():
    inp = make_input(
        groups=[make_group("g1", available=[0])],
        shifts=[make_shift(0, preallocated_team_lead_id="nobody")],
    )
    with pytest.raises(ProblemError, match="does not match any volunteer"):
        Problem(inp)


def test_non_team_lead_designated_as_team_lead_errors():
    inp = make_input(
        groups=[make_group("g1", available=[0])],  # g1 is not a team lead
        shifts=[make_shift(0, preallocated_team_lead_id="g1")],
    )
    with pytest.raises(ProblemError, match="is not a team lead"):
        Problem(inp)


def test_preallocation_on_closed_shift_errors():
    inp = make_input(
        groups=[make_group("g1", available=[0])],
        shifts=[make_shift(0, closed=True, preallocated_volunteer_ids=["g1"])],
    )
    with pytest.raises(ProblemError, match="closed"):
        Problem(inp)
