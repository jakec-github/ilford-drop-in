"""A solve at the size production asks for: 12 weekly shifts, each a Team
lead and five Service volunteers, from about forty volunteers, with every
day-one Rule Kind applied to everyone.

The e2e scenario is too small to notice how the solver is tuned: it is
proved optimal in a moment whatever the parameters. This one is not. At
CP-SAT's default linearization the bound on it never meets the best rota
found, so the solve runs out its time limit and returns FEASIBLE — a rota
that is merely the best seen so far, and one that depends on how fast the
machine was (issue #224). This pins the solver to proving its answer.

The roster is random but seeded, so it is the same roster every run. Seed 3
is one the default setting failed to prove optimal inside ten minutes.
"""

from __future__ import annotations

import datetime
import random

from conftest import DEFAULT_ROLES, SERVICE_VOLUNTEER, TEAM_LEAD, with_rules
from pyallocator.api import solve
from pyallocator.domain import (
    AllocationInput,
    Group,
    HistoricalShift,
    Member,
    Seat,
    ShiftSpec,
)

NUM_SHIFTS = 12

# Every day-one kind but max_frequency, whose cap the input sets: a gap of one
# and a monthly cap of one are what no_back_to_back and one_shift_per_month
# were.
EVERY_RULE = (("male_required", 0), ("shift_gap", 1), ("monthly_cap", 1))
NUM_VOLUNTEERS = 40


def make_prod_scale_input(seed: int) -> AllocationInput:
    rng = random.Random(seed)
    start = datetime.date(2026, 10, 4)
    shape = (Seat(role=TEAM_LEAD, count=1), Seat(role=SERVICE_VOLUNTEER, count=5))
    shifts = tuple(
        ShiftSpec(
            index=i,
            date=str(start + datetime.timedelta(weeks=i)),
            closed=False,
            shape=shape,
        )
        for i in range(NUM_SHIFTS)
    )

    # Mostly individuals with the odd couple; about a fifth hold Team lead,
    # and some groups never answered, so are absent altogether.
    groups: list[Group] = []
    volunteer = 0
    while volunteer < NUM_VOLUNTEERS:
        size = 2 if rng.random() < 0.1 else 1
        members = []
        for _ in range(size):
            roles = (SERVICE_VOLUNTEER,) + ((TEAM_LEAD,) if rng.random() < 0.2 else ())
            gender = "Male" if rng.random() < 0.4 else "Female"
            members.append(
                Member(f"v{volunteer}", f"V{volunteer}", "Test", "", gender, roles)
            )
            volunteer += 1
        if rng.random() > 0.85:
            continue
        groups.append(
            Group(
                group_key=f"g{len(groups)}",
                members=tuple(members),
                available_shift_indices=tuple(
                    i for i in range(NUM_SHIFTS) if rng.random() < 0.6
                ),
                historical_allocation_count=rng.randint(0, 4),
            )
        )

    history_start = start - datetime.timedelta(weeks=NUM_SHIFTS)
    keys = [g.group_key for g in groups]
    historical = tuple(
        HistoricalShift(
            date=str(history_start + datetime.timedelta(weeks=i)),
            group_keys=tuple(rng.sample(keys, 5)),
        )
        for i in range(NUM_SHIFTS)
    )

    return with_rules(
        AllocationInput(
            shifts=shifts,
            groups=tuple(groups),
            roles=DEFAULT_ROLES,
            historical_shifts=historical,
        ),
        ("max_frequency", 3),
        *EVERY_RULE,
    )


def test_prod_scale_rota_is_proved_optimal():
    out = solve(make_prod_scale_input(seed=3))
    # OPTIMAL, not merely success: FEASIBLE is what running out of time
    # looks like, and it passes as success everywhere downstream.
    assert out.solver_status == "OPTIMAL"
