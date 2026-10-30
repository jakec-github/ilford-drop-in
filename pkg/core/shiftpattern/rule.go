package shiftpattern

import (
	"fmt"
	"strings"
	"time"

	"github.com/teambition/rrule-go"
)

// An Override picks out some of the Shifts a pattern generates with a rule of
// its own (ADR 0013): a DTSTART date it counts from and an RRULE, with no
// VEVENT around them, so what an Override matches stays apart from what it
// sets (ADR 0014). The RRULE is read by the same parser as a pattern entry's,
// and described by the same words, so a rule means one thing wherever it is
// written. What differs is what its time decides: with BYHOUR and BYMINUTE it
// picks out the Shift generated at exactly that time, and without either every
// Shift on the dates it names.

// Line is an Override's rule, read: what it says in plain English, or what is
// wrong with it.
type Line struct {
	Text string
	// Description says what the rule picks out, e.g. "Every month on the
	// first Sunday, from 1 Nov 2026". Empty when it has problems.
	Description string
	// Problems is everything wrong with it, each a lowercase sentence.
	Problems []string
}

// Rule is an Override's rule that can be used.
type Rule struct {
	rule  *rrule.RRule
	exact bool
}

// ReadRule reads an Override's rule: what it says, or what is wrong with it,
// and the Rule when it can be used.
func ReadRule(text string) (Line, *Rule) {
	line := Line{Text: strings.TrimSpace(text)}
	lines := contentLines(text)
	if len(lines) == 0 {
		line.Problems = []string{"the rule is empty - write a DTSTART and an RRULE, like DTSTART;VALUE=DATE:20261101 and RRULE:FREQ=MONTHLY;BYDAY=1SU"}
		return line, nil
	}

	var dtstart, rule *contentLine
	for _, l := range lines {
		l := l
		switch {
		case l.Name == "BEGIN" || l.Name == "END":
			line.Problems = appendOnce(line.Problems, "an Override is a DTSTART and an RRULE, not a VEVENT - write the two lines on their own")
		case l.Name == "DTSTART":
			if dtstart != nil {
				line.Problems = append(line.Problems, "DTSTART appears twice")
				continue
			}
			dtstart = &l
		case l.isRuleLine():
			if rule != nil {
				line.Problems = append(line.Problems, "RRULE appears twice - an Override is one rule")
				continue
			}
			rule = &l
		case refusedProperty(l.Name) != "":
			line.Problems = append(line.Problems, refusedProperty(l.Name))
		default:
			line.Problems = append(line.Problems, fmt.Sprintf("%q is not part of an Override's rule - it is a DTSTART and an RRULE", l.Raw))
		}
	}

	var anchor time.Time
	if dtstart == nil {
		line.Problems = append(line.Problems, "DTSTART is missing - write the date it counts from, like DTSTART;VALUE=DATE:20261101")
	} else if at, problem := ruleStart(*dtstart); problem != "" {
		line.Problems = append(line.Problems, problem)
	} else {
		anchor = at
	}

	var option rrule.ROption
	if rule == nil {
		line.Problems = append(line.Problems, "RRULE is missing - write which shifts it matches, like RRULE:FREQ=MONTHLY;BYDAY=1SU")
	} else {
		var problems []string
		option, problems = parseRule(rule.Value)
		line.Problems = append(line.Problems, problems...)
		if (len(option.Byhour) > 0) != (len(option.Byminute) > 0) {
			line.Problems = append(line.Problems, "give BYHOUR and BYMINUTE together for shifts starting at that time, or neither for every shift on those days")
		}
	}
	if len(line.Problems) > 0 {
		return line, nil
	}

	option.Dtstart = anchor
	compiled, err := rrule.NewRRule(option)
	if err != nil {
		line.Problems = []string{err.Error()}
		return line, nil
	}
	line.Description = describe(option, anchor, 0, false)
	return line, &Rule{rule: compiled, exact: len(option.Byhour) > 0}
}

// ruleStart reads an Override's DTSTART, which is a date: the midnight it
// counts from. A time belongs in BYHOUR and BYMINUTE, where it decides what
// the Override matches.
func ruleStart(l contentLine) (time.Time, string) {
	value := l.Value
	if len(value) != 8 || strings.ContainsAny(value, "Tt") {
		if strings.ContainsAny(value, "Tt") {
			return time.Time{}, "an Override's DTSTART is a date, like DTSTART;VALUE=DATE:20261101 - give a time with BYHOUR and BYMINUTE"
		}
		return time.Time{}, fmt.Sprintf("DTSTART %q is not a date like 20261101", value)
	}
	day, err := time.Parse("20060102", value)
	if err != nil {
		return time.Time{}, fmt.Sprintf("DTSTART %q is not a date like 20261101", value)
	}
	return day, ""
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
