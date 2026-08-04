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
    team_lead_id,
    volunteer_ids,
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
    assert set(volunteer_ids(shift)) == {"a", "b"}


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
    assert team_lead_id(out.shifts[0]) == "lead"


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


def test_pin_grants_a_role_the_volunteer_does_not_hold():
    """A pin is a decision someone has already taken: they have been asked
    to do this job on this shift, and the sheet not saying so yet is the
    sheet lagging. The grant is worth one Seat on one shift — the Roles the
    volunteer holds are left untouched, so nothing else changes for them.
    """
    inp = make_input(
        groups=[make_group("g1", available=[0])],  # g1 does not hold Team lead
        shifts=[make_shift(0, preallocated_team_lead_id="g1")],
    )
    out = solve_with(inp, ONLY, preferences=[])
    assert out.success
    assert team_lead_id(out.shifts[0]) == "g1"


def test_pinning_someone_to_a_role_the_shift_has_no_seat_for_errors():
    """The Shape is still the last word on what a shift needs. A pin can
    grant the Role, but it cannot conjure a Seat that is not there.
    """
    inp = make_input(
        groups=[make_group("g1", available=[0])],
        shifts=[make_shift(0, size=0, preallocated_volunteer_ids=["g1"])],
    )
    with pytest.raises(ProblemError, match="has no Seat for"):
        Problem(inp)


def test_preallocation_on_closed_shift_errors():
    inp = make_input(
        groups=[make_group("g1", available=[0])],
        shifts=[make_shift(0, closed=True, preallocated_volunteer_ids=["g1"])],
    )
    with pytest.raises(ProblemError, match="closed"):
        Problem(inp)
