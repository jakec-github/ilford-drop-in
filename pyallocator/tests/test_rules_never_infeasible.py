"""No set of Allocation Rules can stop a rota being allocated (ADR 0012).

Every day-one kind can be satisfied by leaving Seats empty, and every rule
exempts pins (ADR 0010), so whatever rules an Organiser adds, at whatever
values and Scopes, the solve finds a rota. This tries every combination of
kinds, each at a random value and over a random Scope, against a random
roster with couples, pins (some on volunteers who did not say they were
free), a Closed Shift and a history ending on one.
"""

from __future__ import annotations

import itertools
import random

import pytest
from conftest import make_group, make_input, make_member, make_shift
from pyallocator.api import solve
from pyallocator.domain import HistoricalShift, Rule

KINDS = {
    # kind: the values Go may send for it.
    "max_frequency": (1, 6),
    "shift_gap": (1, 52),
    "monthly_cap": (1, 31),
    "shift_requires": (0, 0),
}
NUM_SHIFTS = 6


def random_input(rng: random.Random, kinds: tuple[str, ...]):
    groups = []
    for i in range(8):
        gender = rng.choice(("Male", "Female"))
        available = sorted(rng.sample(range(NUM_SHIFTS), rng.randint(0, NUM_SHIFTS)))
        if i % 3 == 0:
            members = [
                make_member(f"v{i}a", gender=gender, is_team_lead=rng.random() < 0.5),
                make_member(f"v{i}b", gender="Female"),
            ]
            groups.append(make_group(f"g{i}", members=members, available=available))
        else:
            member = make_member(f"v{i}", gender=gender, is_team_lead=rng.random() < 0.3)
            groups.append(make_group(f"g{i}", members=[member], available=available))

    ids = [m.id for g in groups for m in g.members]
    closed = rng.randrange(NUM_SHIFTS)
    shifts = []
    for i in range(NUM_SHIFTS):
        pinned = [] if i == closed else rng.sample(ids, rng.randint(0, 3))
        shifts.append(
            make_shift(
                i,
                # Two months, so a monthly cap has a boundary to cross.
                date=f"2026-0{7 + i // 3}-{1 + 7 * (i % 3):02d}",
                size=rng.randint(1, 3),
                closed=i == closed,
                pinned_volunteer_ids=pinned,
            )
        )

    keys = [g.group_key for g in groups]
    history = [
        HistoricalShift(date=f"2026-06-{1 + 7 * k:02d}", group_keys=tuple(rng.sample(keys, 3)))
        for k in range(3)
    ] + [HistoricalShift(date="2026-06-29", group_keys=())]

    rules = []
    for kind in kinds:
        low, high = KINDS[kind]
        covered = () if kind == "shift_requires" else tuple(rng.sample(ids, rng.randint(0, len(ids))))
        # Mostly the tightest values, which are the ones pins collide with.
        value = rng.choice((low, low, low + 1, rng.randint(low, high))) if high else 0
        option = rng.choice(("Male", "Female")) if kind == "shift_requires" else ""
        rules.append(Rule(kind=kind, volunteer_ids=covered, value=value, option=option))

    return make_input(groups=groups, shifts=shifts, historical_shifts=history, rules=rules)


COMBINATIONS = [
    combo
    for size in range(1, len(KINDS) + 1)
    for combo in itertools.combinations(KINDS, size)
]


@pytest.mark.parametrize("seed", range(3))
@pytest.mark.parametrize("kinds", COMBINATIONS, ids=lambda c: "+".join(c))
def test_every_combination_of_rules_solves(kinds, seed):
    rng = random.Random(f"{'+'.join(kinds)}/{seed}")
    out = solve(random_input(rng, kinds))
    assert out.success, out.solver_status
