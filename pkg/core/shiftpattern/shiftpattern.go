// Package shiftpattern reads an Organisation's Shift pattern and says when its
// Shifts start (ADR 0013).
//
// A pattern is a set of RRULEs, one per line, counted from a Start from date.
// This package is the only evaluator of one. The descriptions on the Rota
// Defaults screen, the calendar beside them and the Shifts a rota is minted
// with all come from here, so the screen cannot promise a Shift that defining a
// rota would not mint.
//
// Everything is wall-clock time. A start is a date and a time of day in the
// Organisation's own zone, carried in a time.Time whose location is UTC only
// because a time.Time must have one: 19:30 is 19:30 either side of a clock
// change, as a Shift's own times are (ADR 0007). No zone is ever applied here.
package shiftpattern

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/teambition/rrule-go"
)

// Horizon is how far past where it starts looking a search for Shifts gives
// up. A pattern that generates nothing for five years is one that has ended —
// a COUNT or an UNTIL has run out — rather than one that is about to start.
const Horizon = 5 * 365 * 24 * time.Hour

// Line is one line of a pattern, read: what it says in plain English, or what
// is wrong with it.
type Line struct {
	// Number is the line's position in the text as typed, counting from one
	// and counting blank lines, so it is the line an Organiser would count to.
	Number int
	Text   string
	// Description says when the line's Shifts start, e.g. "Every week on
	// Sunday at 19:30". Empty when the line has problems.
	Description string
	// Problems is everything wrong with the line, each a lowercase sentence.
	// Empty when the line can be used.
	Problems []string
}

// Pattern is a Shift pattern every line of which can be used.
type Pattern struct {
	startFrom time.Time
	rules     []*rrule.RRule
}

// Read reads every non-blank line of a pattern counted from startFrom, and
// returns the Pattern they make when every one of them can be used. The
// Pattern is nil when any line has problems or there are no lines at all;
// the lines say which.
//
// Only startFrom's date is used. It is where the pattern counts from — the
// first week of "every other week", the weekday of a bare FREQ=WEEKLY — as
// DTSTART at 00:00.
func Read(text string, startFrom time.Time) ([]Line, *Pattern) {
	anchor := Day(startFrom)

	var lines []Line
	var rules []*rrule.RRule
	usable := true
	for i, raw := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		line := Line{Number: i + 1, Text: trimmed}
		option, problems := parseLine(trimmed, true)
		if len(problems) > 0 {
			line.Problems = problems
			usable = false
			lines = append(lines, line)
			continue
		}

		option.Dtstart = anchor
		rule, err := rrule.NewRRule(option)
		if err != nil {
			// parseLine checks every bound rrule-go does, so this is a rule
			// that library refuses for a reason this one has not learnt.
			line.Problems = []string{err.Error()}
			usable = false
			lines = append(lines, line)
			continue
		}
		line.Description = describe(option, anchor)
		lines = append(lines, line)
		rules = append(rules, rule)
	}

	if !usable || len(rules) == 0 {
		return lines, nil
	}
	return lines, &Pattern{startFrom: anchor, rules: rules}
}

// Compile reads a pattern that is about to be used — saved, or minted from —
// and refuses one that cannot be, with every problem on every line in one
// error.
func Compile(text string, startFrom time.Time) (*Pattern, error) {
	lines, pattern := Read(text, startFrom)
	if pattern != nil {
		return pattern, nil
	}
	if len(lines) == 0 {
		return nil, errors.New("the shift pattern has no lines - write at least one RRULE")
	}
	var problems []string
	for _, line := range lines {
		if len(line.Problems) > 0 {
			problems = append(problems, fmt.Sprintf("line %d: %s", line.Number, strings.Join(line.Problems, "; ")))
		}
	}
	return nil, errors.New(strings.Join(problems, "; "))
}

// Between is every start from from up to, but not including, to, in order.
// Two lines generating the same start make one Shift, not two.
func (p *Pattern) Between(from, to time.Time) []time.Time {
	var starts []time.Time
	for _, rule := range p.rules {
		for _, start := range rule.Between(from, to, true) {
			if start.Before(to) {
				starts = append(starts, start)
			}
		}
	}
	return ordered(starts)
}

// Next is the first count starts strictly after after, none earlier than
// notBefore. A zero after or notBefore does not bound the search, so with
// both zero the starts begin where the pattern does.
//
// It returns fewer than count when the pattern runs out within Horizon of
// where the search begins; the caller decides what that means.
func (p *Pattern) Next(after, notBefore time.Time, count int) []time.Time {
	from := p.startFrom
	if after.After(from) {
		from = after
	}
	if notBefore.After(from) {
		from = notBefore
	}
	to := from.Add(Horizon)

	var starts []time.Time
	for _, start := range p.Between(from, to) {
		if !after.IsZero() && !start.After(after) {
			continue
		}
		starts = append(starts, start)
		if len(starts) == count {
			break
		}
	}
	return starts
}

// Day is the date of t, as the midnight this package counts days from.
func Day(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// ordered sorts starts and drops repeats.
func ordered(starts []time.Time) []time.Time {
	sort.Slice(starts, func(i, j int) bool { return starts[i].Before(starts[j]) })
	return slices.CompactFunc(starts, func(a, b time.Time) bool { return a.Equal(b) })
}

var (
	frequencies = map[string]rrule.Frequency{
		"DAILY":   rrule.DAILY,
		"WEEKLY":  rrule.WEEKLY,
		"MONTHLY": rrule.MONTHLY,
		"YEARLY":  rrule.YEARLY,
	}
	weekdays = map[string]rrule.Weekday{
		"MO": rrule.MO, "TU": rrule.TU, "WE": rrule.WE, "TH": rrule.TH,
		"FR": rrule.FR, "SA": rrule.SA, "SU": rrule.SU,
	}
	bydayPattern = regexp.MustCompile(`^([+-]?\d{1,2})?(MO|TU|WE|TH|FR|SA|SU)$`)
	untilPattern = regexp.MustCompile(`^(\d{8})(?:T(\d{6})Z?)?$`)
)

// parseLine reads one RRULE line into the options rrule-go evaluates, or
// lists everything wrong with it.
//
// It is its own reader rather than rrule-go's StrToROption, for two reasons.
// That one stops at the first problem and words it for a programmer, where an
// Organiser fixing a line wants every problem in words they can act on. And
// the ADR narrows RRULE: some parts are refused outright, and two that RRULE
// leaves optional are required here.
//
// needsTime is whether the line must state a time. A pattern line must, since
// its time is when the Shift starts. An Override's rule may state one or not,
// but not half of one: with BYHOUR and BYMINUTE it picks out the Shift
// generated at that time, and without either it picks out every Shift on the
// dates it names.
func parseLine(text string, needsTime bool) (rrule.ROption, []string) {
	s := strings.ToUpper(strings.TrimSpace(text))
	switch {
	case strings.HasPrefix(s, "DTSTART"):
		return rrule.ROption{}, []string{"DTSTART is not written in the rule - Start from says where it counts from"}
	case strings.HasPrefix(s, "RDATE"):
		return rrule.ROption{}, []string{"RDATE adds a one-off shift, and every shift comes from the pattern"}
	case strings.HasPrefix(s, "EXDATE"), strings.HasPrefix(s, "EXRULE"):
		return rrule.ROption{}, []string{"EXDATE and EXRULE are not supported - close a shift by hand instead"}
	}
	s = strings.TrimPrefix(s, "RRULE:")

	option := rrule.ROption{Interval: 1, Wkst: rrule.MO}
	var problems []string
	freqSet, freqProblem := false, false
	untilSet, positional := false, false
	seen := map[string]bool{}

	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		pair := strings.Split(part, "=")
		if len(pair) != 2 || pair[0] == "" || pair[1] == "" {
			problems = append(problems, fmt.Sprintf("%q is not KEY=VALUE", part))
			continue
		}
		key, value := pair[0], pair[1]
		if seen[key] {
			problems = append(problems, key+" appears twice")
			continue
		}
		seen[key] = true

		switch key {
		case "FREQ":
			if freq, ok := frequencies[value]; ok {
				option.Freq = freq
				freqSet = true
			} else if value == "HOURLY" || value == "MINUTELY" || value == "SECONDLY" {
				problems = append(problems, fmt.Sprintf("FREQ=%s is refused - the finest is DAILY, with the time set by BYHOUR and BYMINUTE", value))
				freqProblem = true
			} else {
				problems = append(problems, fmt.Sprintf("FREQ=%s is not a frequency", value))
				freqProblem = true
			}
		case "INTERVAL":
			if n, err := strconv.Atoi(value); err == nil && n >= 1 {
				option.Interval = n
			} else {
				problems = append(problems, "INTERVAL must be a whole number of at least 1")
			}
		case "COUNT":
			if n, err := strconv.Atoi(value); err == nil && n >= 1 {
				option.Count = n
			} else {
				problems = append(problems, "COUNT must be a whole number of at least 1")
			}
		case "UNTIL":
			until, ok := parseUntil(value)
			if !ok {
				problems = append(problems, "UNTIL must look like 20270301 or 20270301T193000")
				continue
			}
			option.Until = until
			untilSet = true
		case "WKST":
			if day, ok := weekdays[value]; ok {
				option.Wkst = day
			} else {
				problems = append(problems, fmt.Sprintf("WKST=%s is not a weekday", value))
			}
		case "BYDAY":
			for _, item := range strings.Split(value, ",") {
				match := bydayPattern.FindStringSubmatch(item)
				if match == nil {
					problems = append(problems, fmt.Sprintf("BYDAY: %q is not a weekday like SU, 1SU or -1SU", item))
					continue
				}
				day := weekdays[match[2]]
				if match[1] != "" {
					n, _ := strconv.Atoi(match[1])
					if n == 0 || n > 53 || n < -53 {
						problems = append(problems, fmt.Sprintf("BYDAY: %q has an impossible position", item))
						continue
					}
					day = day.Nth(n)
					positional = true
				}
				option.Byweekday = append(option.Byweekday, day)
			}
		case "BYMONTHDAY":
			option.Bymonthday = intList(value, -31, 31, true, key, &problems)
		case "BYMONTH":
			option.Bymonth = intList(value, 1, 12, false, key, &problems)
		case "BYHOUR":
			option.Byhour = intList(value, 0, 23, false, key, &problems)
		case "BYMINUTE":
			option.Byminute = intList(value, 0, 59, false, key, &problems)
		case "BYSETPOS", "BYWEEKNO", "BYYEARDAY", "BYSECOND":
			problems = append(problems, key+" is not supported")
		case "DTSTART":
			problems = append(problems, "DTSTART is not written in the rule - Start from says where it counts from")
		default:
			problems = append(problems, key+" is not an RRULE part")
		}
	}

	if !freqSet && !freqProblem {
		problems = append(problems, "FREQ is missing")
	}
	if option.Count > 0 && untilSet {
		problems = append(problems, "COUNT and UNTIL cannot both be given")
	}
	if positional && (option.Freq == rrule.DAILY || option.Freq == rrule.WEEKLY) {
		problems = append(problems, "a position like 1SU only means something with FREQ=MONTHLY or YEARLY")
	}
	hasHour, hasMinute := len(option.Byhour) > 0, len(option.Byminute) > 0
	switch {
	case needsTime && (!hasHour || !hasMinute):
		problems = append(problems, "every line needs BYHOUR and BYMINUTE - they are the shift's start time")
	case hasHour != hasMinute:
		problems = append(problems, "give BYHOUR and BYMINUTE together for shifts starting at that time, or neither for every shift on those days")
	}
	return option, problems
}

// parseUntil reads UNTIL as a wall-clock moment. A bare date means the end of
// that day, so a line running until a date includes that date's Shifts: read
// as its midnight, "until 1 March" would stop before a Shift on 1 March.
func parseUntil(value string) (time.Time, bool) {
	match := untilPattern.FindStringSubmatch(value)
	if match == nil {
		return time.Time{}, false
	}
	if match[2] == "" {
		day, err := time.Parse("20060102", match[1])
		if err != nil {
			return time.Time{}, false
		}
		return day.Add(24*time.Hour - time.Second), true
	}
	moment, err := time.Parse("20060102150405", match[1]+match[2])
	if err != nil {
		return time.Time{}, false
	}
	return moment, true
}

// intList reads a comma-separated list of whole numbers within [lo, hi],
// adding a problem for each that is not one. With nonZero, zero is refused
// too: the 0th day of a month is not a day.
func intList(value string, lo, hi int, nonZero bool, key string, problems *[]string) []int {
	var out []int
	for _, item := range strings.Split(value, ",") {
		n, err := strconv.Atoi(item)
		if err != nil || n < lo || n > hi || (nonZero && n == 0) {
			*problems = append(*problems, fmt.Sprintf("%s: %q is out of range", key, item))
			continue
		}
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out
}
