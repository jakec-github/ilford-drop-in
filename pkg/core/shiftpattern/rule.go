package shiftpattern

import (
	"strings"
	"time"

	"github.com/teambition/rrule-go"
)

// An Override picks out some of the Shifts a pattern generates with a rule of
// its own (ADR 0013). It is read by the same parser as a pattern line, and
// described by the same words, so a rule means one thing wherever it is
// written. What differs is that it need not state a time, and what that
// decides: with BYHOUR and BYMINUTE it picks out the Shift generated at
// exactly that time, and without either every Shift on the dates it names.

// Rule is an Override's rule that can be used.
type Rule struct {
	rule  *rrule.RRule
	exact bool
}

// ReadRule reads an Override's rule counted from startFrom: what it says, or
// what is wrong with it, and the Rule when it can be used. Only startFrom's
// date is used, as for a pattern.
func ReadRule(text string, startFrom time.Time) (Line, *Rule) {
	trimmed := strings.TrimSpace(text)
	line := Line{Number: 1, Text: trimmed}
	switch {
	case trimmed == "":
		line.Problems = []string{"the rule is empty - write one RRULE, like FREQ=MONTHLY;BYDAY=1SU"}
		return line, nil
	case strings.Contains(trimmed, "\n"):
		line.Problems = []string{"an Override is one rule, on one line"}
		return line, nil
	}

	option, problems := parseLine(trimmed, false)
	if len(problems) > 0 {
		line.Problems = problems
		return line, nil
	}

	anchor := Day(startFrom)
	option.Dtstart = anchor
	rule, err := rrule.NewRRule(option)
	if err != nil {
		line.Problems = []string{err.Error()}
		return line, nil
	}
	line.Description = describe(option, anchor)
	return line, &Rule{rule: rule, exact: len(option.Byhour) > 0}
}

// Exact is whether the rule names a time, and so matches only the Shift
// generated at it rather than every Shift on its dates.
func (r *Rule) Exact() bool {
	return r.exact
}

// Picks says, for each start, whether the rule picks it out: the start itself
// for an exact rule, its date for one that names no time.
//
// The starts are the times the pattern generated, never a time anything has
// since moved a Shift to (ADR 0013).
func (r *Rule) Picks(starts []time.Time) []bool {
	if r.exact {
		hits := r.occurrences(starts, func(t time.Time) time.Time { return t })
		return lookup(starts, hits, func(t time.Time) time.Time { return t })
	}
	return r.OnDate(starts)
}

// OnDate says, for each start, whether the rule generates anything on its
// date, whatever time it names. For an exact rule that picks nothing out it is
// the clue that the times disagree: it is aimed at the right days, so the
// pattern's time has likely changed under it.
func (r *Rule) OnDate(starts []time.Time) []bool {
	hits := r.occurrences(starts, Day)
	return lookup(starts, hits, Day)
}

// occurrences is every time the rule generates on the dates the starts span,
// reduced by key and held as Unix seconds — not as time.Time, whose == compares
// its zone as well as the moment.
func (r *Rule) occurrences(starts []time.Time, key func(time.Time) time.Time) map[int64]bool {
	hits := map[int64]bool{}
	if len(starts) == 0 {
		return hits
	}
	first, last := starts[0], starts[0]
	for _, s := range starts {
		if s.Before(first) {
			first = s
		}
		if s.After(last) {
			last = s
		}
	}
	for _, t := range r.rule.Between(Day(first), Day(last).Add(24*time.Hour), true) {
		hits[key(t).Unix()] = true
	}
	return hits
}

func lookup(starts []time.Time, hits map[int64]bool, key func(time.Time) time.Time) []bool {
	picked := make([]bool, len(starts))
	for i, s := range starts {
		picked[i] = hits[key(s).Unix()]
	}
	return picked
}
