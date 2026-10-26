"""The spread-genders preference matches each shift's split between men
and women to the split across the whole roster; other genders are left
out of both."""

from __future__ import annotations

from conftest import make_group, make_input, make_member, make_shift, rules_of, solve_with
from pyallocator.preferences import even_fill, maximize_allocations, spread_genders

# maximize_allocations and even_fill fill seats as production does; on
# their own, every split of men and women scores the same.
PREFS = [maximize_allocations.PREFERENCE, even_fill.PREFERENCE, spread_genders.PREFERENCE]


def _person(key: str, gender: str, available) -> object:
    return make_group(
        key, members=[make_member(key, gender=gender)], available=available
    )


def _people(prefix: str, gender: str, n: int, available) -> list:
    return [_person(f"{prefix}{i}", gender, available) for i in range(n)]


def _split(inp, out) -> dict[int, dict[str, int]]:
    gender_of = {g.group_key: g.members[0].gender for g in inp.groups}
    return {
        s.index: {
            gender: sum(1 for key in s.allocated_group_keys if gender_of[key] == gender)
            for gender in sorted(set(gender_of.values()))
        }
        for s in out.shifts
    }


def test_even_roster_puts_one_of_each_on_every_shift():
    available = [0, 1]
    inp = make_input(
        groups=_people("m", "Male", 2, available) + _people("f", "Female", 2, available),
        shifts=[make_shift(0, size=2), make_shift(1, size=2)],
        rules=[("max_frequency", 1)],
    )
    out = solve_with(inp, rules_of(inp), preferences=PREFS)
    assert out.success
    assert _split(inp, out) == {0: {"Female": 1, "Male": 1}, 1: {"Female": 1, "Male": 1}}


def test_women_in_the_minority_are_spread_one_per_shift():
    available = [0, 1]
    inp = make_input(
        groups=_people("m", "Male", 4, available) + _people("f", "Female", 2, available),
        shifts=[make_shift(0, size=3), make_shift(1, size=3)],
        rules=[("max_frequency", 1)],
    )
    out = solve_with(inp, rules_of(inp), preferences=PREFS)
    assert out.success
    assert _split(inp, out) == {0: {"Female": 1, "Male": 2}, 1: {"Female": 1, "Male": 2}}


def test_shifts_of_different_sizes_each_take_the_roster_split():
    available = [0, 1]
    inp = make_input(
        groups=_people("m", "Male", 3, available) + _people("f", "Female", 3, available),
        shifts=[make_shift(0, size=2), make_shift(1, size=4)],
        rules=[("max_frequency", 1)],
    )
    out = solve_with(inp, rules_of(inp), preferences=PREFS)
    assert out.success
    assert _split(inp, out) == {0: {"Female": 1, "Male": 1}, 1: {"Female": 2, "Male": 2}}


def test_other_genders_do_not_count_towards_the_split():
    # Counted as a third share, two Others would make two men and two
    # women look like a minority each; left out, the roster is half and
    # half, and each shift is one man and one woman beside an Other.
    available = [0, 1]
    inp = make_input(
        groups=_people("m", "Male", 2, available)
        + _people("f", "Female", 2, available)
        + _people("o", "Other", 2, available),
        shifts=[make_shift(0, size=3), make_shift(1, size=3)],
        rules=[("max_frequency", 1)],
    )
    out = solve_with(inp, rules_of(inp), preferences=PREFS)
    assert out.success
    assert _split(inp, out) == {
        0: {"Female": 1, "Male": 1, "Other": 1},
        1: {"Female": 1, "Male": 1, "Other": 1},
    }


def test_others_neither_score_nor_cost():
    # The same man and woman, with and without an Other beside them: the
    # preference scores the shift the same.
    man, woman = _person("m", "Male", [0]), _person("f", "Female", [0])
    scores = []
    for groups in ([man, woman], [man, woman, _person("o", "Other", [0])]):
        inp = make_input(groups=groups, shifts=[make_shift(0, size=3)])
        out = solve_with(inp, [], preferences=[spread_genders.PREFERENCE])
        assert out.success
        scores.append(out.objective_value)
    assert scores[0] == scores[1] > 0


def test_one_gender_only_no_terms():
    inp = make_input(
        groups=[make_group("f1", available=[0])],
        shifts=[make_shift(0)],
    )
    out = solve_with(inp, [], preferences=[spread_genders.PREFERENCE])
    assert out.success
    assert out.objective_value == 0
