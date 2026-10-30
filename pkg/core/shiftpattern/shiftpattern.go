// Package shiftpattern reads an Organisation's Shift pattern and says when its
// Shifts start and how long they run (ADR 0013, ADR 0014).
//
// A pattern is a list of entries, each an iCalendar VEVENT holding exactly one
// DTSTART, DURATION and RRULE: when it first happens, how long it runs, how it
// repeats. This package is the only evaluator of one. The descriptions on the
// Rota Defaults screen, the calendar beside them and the Shifts a rota is
// minted with all come from here, so the screen cannot promise a Shift that
// defining a rota would not mint.
//
// Everything is wall-clock time. A start is a date and a time of day in the
// Organisation's own zone, carried in a time.Time whose location is UTC only
// because a time.Time must have one: 19:30 is 19:30 either side of a clock
// change, as a Shift's own times are (ADR 0007). No zone is ever applied here;
// the zone is only what a DTSTART's TZID is checked against.
package shiftpattern

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/teambition/rrule-go"
)

// Horizon is how far past where it starts looking a search for Shifts gives
// up. A pattern that generates nothing for five years is one that has ended —
// a COUNT or an UNTIL has run out — rather than one that is about to start. It
// is also how far ahead two entries are checked for making the same start.
const Horizon = 5 * 365 * 24 * time.Hour

// MaxLength bounds how long an entry's Shifts run. A day: a Shift longer than
// that would overlap the same Shift the next day on any daily pattern, and is
// not a session anybody runs.
const MaxLength = 24 * time.Hour

// Entry is one VEVENT of a pattern, read: what it says in plain English, or
// what is wrong with it.
type Entry struct {
	// Number is its place among the entries, counting from one.
	Number int
	// Line is the line its BEGIN:VEVENT is on in the text as typed, counting
	// from one and counting blank lines, so it is the line an Organiser would
	// count to.
	Line int
	// Description says when its Shifts start and how long they run, e.g.
	// "Every Sunday at 19:30 for 3 hours, from 1 Nov 2026". Empty when it has
	// problems.
	Description string
	// Problems is everything wrong with it, each a lowercase sentence. Empty
	// when it can be used.
	Problems []string
	// Warnings is everything in it that is read past — a UID, a SUMMARY —
	// so a pasted calendar event reads without anybody trimming it first.
	Warnings []string
}

// Reading is a pattern read: each of its entries, and what is wrong with or
// read past in the text around them.
type Reading struct {
	Entries  []Entry
	Problems []string
	Warnings []string
}

// usable is whether nothing in the reading stops the pattern being used.
func (r Reading) usable() bool {
	if len(r.Problems) > 0 || len(r.Entries) == 0 {
		return false
	}
	for _, e := range r.Entries {
		if len(e.Problems) > 0 {
			return false
		}
	}
	return true
}

// Start is one Shift a pattern generates: when it starts, and how long the
// entry that generated it runs.
type Start struct {
	At     time.Time
	Length time.Duration
}

// Pattern is a Shift pattern every entry of which can be used.
type Pattern struct {
	first   time.Time
	entries []entry
}

// entry is one usable entry: its rule, counted from its own DTSTART, and its
// length.
type entry struct {
	rule   *rrule.RRule
	start  time.Time
	length time.Duration
}

// Read reads a pattern whose times are in zone, and returns the Pattern it
// makes when every entry can be used. The Pattern is nil when anything has
// problems or there are no entries at all; the reading says which.
func Read(text, zone string) (Reading, *Pattern) {
	var reading Reading
	var entries []entry

	var block *blockLines
	skipping := ""
	inCalendar := false
	finish := func() {
		e, compiled := readEntry(len(reading.Entries)+1, *block, zone)
		reading.Entries = append(reading.Entries, e)
		entries = append(entries, compiled)
		block = nil
	}
	warn := func(w string) {
		if block != nil {
			block.warnings = appendOnce(block.warnings, w)
		} else {
			reading.Warnings = appendOnce(reading.Warnings, w)
		}
	}

	for _, l := range contentLines(text) {
		component := strings.ToUpper(l.Value)
		if skipping != "" {
			if l.Name == "END" && component == skipping {
				skipping = ""
			}
			continue
		}
		switch {
		case l.Name == "BEGIN" && component == "VEVENT":
			if block != nil {
				block.problems = append(block.problems, "it has no END:VEVENT before the next BEGIN:VEVENT")
				finish()
			}
			block = &blockLines{line: l.Number}
		case l.Name == "BEGIN" && component == "VCALENDAR" && block == nil && !inCalendar:
			inCalendar = true
			warn("the VCALENDAR around the entries is ignored, and so is everything it says outside them")
		case l.Name == "BEGIN":
			skipping = component
			warn(component + " is ignored")
		case l.Name == "END" && component == "VEVENT" && block != nil:
			finish()
		case l.Name == "END" && component == "VCALENDAR" && block == nil && inCalendar:
			inCalendar = false
		case l.Name == "END":
			reading.Problems = append(reading.Problems, fmt.Sprintf("line %d: END:%s has no BEGIN:%s before it", l.Number, component, component))
		case block != nil:
			block.lines = append(block.lines, l)
		case inCalendar:
			// The wrapper's own properties, VERSION and PRODID, are what
			// its warning already says is ignored.
		default:
			reading.Problems = append(reading.Problems, fmt.Sprintf(
				"line %d: %q is outside a BEGIN:VEVENT ... END:VEVENT block - every entry is one", l.Number, l.Raw))
		}
	}
	if block != nil {
		block.problems = append(block.problems, "it has no END:VEVENT")
		finish()
	}
	if len(reading.Entries) == 0 && len(reading.Problems) == 0 {
		reading.Problems = append(reading.Problems, "the pattern has no entries - write at least one BEGIN:VEVENT ... END:VEVENT block")
	}

	sameStarts(reading.Entries, entries)

	if !reading.usable() {
		return reading, nil
	}
	pattern := &Pattern{entries: entries, first: entries[0].start}
	for _, e := range entries[1:] {
		if e.start.Before(pattern.first) {
			pattern.first = e.start
		}
	}
	return reading, pattern
}

// Compile reads a pattern that is about to be used — saved, or minted from —
// and refuses one that cannot be, with every problem in one error.
func Compile(text, zone string) (*Pattern, error) {
	reading, pattern := Read(text, zone)
	if pattern != nil {
		return pattern, nil
	}
	problems := append([]string{}, reading.Problems...)
	for _, e := range reading.Entries {
		if len(e.Problems) > 0 {
			problems = append(problems, fmt.Sprintf("entry %d: %s", e.Number, strings.Join(e.Problems, "; ")))
		}
	}
	return nil, errors.New(strings.Join(problems, "; "))
}

// First is the earliest start of any entry: where the first rota starts from.
func (p *Pattern) First() time.Time {
	return p.first
}

// Between is every start from from up to, but not including, to, in order.
func (p *Pattern) Between(from, to time.Time) []Start {
	var starts []Start
	for _, e := range p.entries {
		for _, at := range e.rule.Between(from, to, true) {
			if at.Before(to) {
				starts = append(starts, Start{At: at, Length: e.length})
			}
		}
	}
	sort.SliceStable(starts, func(i, j int) bool { return starts[i].At.Before(starts[j].At) })
	return starts
}

// Next is the first count starts strictly after after, none earlier than
// notBefore. A zero after or notBefore does not bound the search, so with
// both zero the starts begin where the earliest entry does.
//
// It returns fewer than count when the pattern runs out within Horizon of
// where the search begins; the caller decides what that means.
func (p *Pattern) Next(after, notBefore time.Time, count int) []Start {
	from := p.first
	if after.After(from) {
		from = after
	}
	if notBefore.After(from) {
		from = notBefore
	}
	to := from.Add(Horizon)

	var starts []Start
	for _, start := range p.Between(from, to) {
		if !after.IsZero() && !start.At.After(after) {
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

// blockLines is one VEVENT's lines as they are gathered, and what was found
// wrong with or read past in the block around them.
type blockLines struct {
	line     int
	lines    []contentLine
	problems []string
	warnings []string
}

// readEntry reads one VEVENT. The entry it compiles is the zero entry when
// the Entry has problems.
func readEntry(number int, block blockLines, zone string) (Entry, entry) {
	e := Entry{Number: number, Line: block.line, Problems: block.problems, Warnings: block.warnings}

	var dtstart, duration, rule *contentLine
	once := func(slot **contentLine, l contentLine) {
		if *slot != nil {
			e.Problems = append(e.Problems, l.Name+" appears twice - an entry has exactly one")
			return
		}
		*slot = &l
	}
	for _, l := range block.lines {
		switch {
		case l.Name == "DTSTART":
			once(&dtstart, l)
		case l.Name == "DURATION":
			once(&duration, l)
		case l.isRuleLine():
			l.Name = "RRULE"
			once(&rule, l)
		case l.Name == "DTEND":
			e.Problems = append(e.Problems, "DTEND is not used - give the length as DURATION, like DURATION:PT3H")
		case refusedProperty(l.Name) != "":
			e.Problems = append(e.Problems, refusedProperty(l.Name))
		case !l.HasColon:
			e.Problems = append(e.Problems, fmt.Sprintf("line %d: %q is not a property like NAME:VALUE", l.Number, l.Raw))
		default:
			e.Warnings = appendOnce(e.Warnings, l.Name+" is ignored")
		}
	}

	var start time.Time
	if dtstart == nil {
		e.Problems = append(e.Problems, "DTSTART is missing - write when its first shift starts, like DTSTART:20261101T193000")
	} else if at, problem := entryStart(*dtstart, zone); problem != "" {
		e.Problems = append(e.Problems, problem)
	} else {
		start = at
	}

	var length time.Duration
	if duration == nil {
		e.Problems = append(e.Problems, "DURATION is missing - write how long each shift runs, like DURATION:PT3H")
	} else if d, problem := entryLength(duration.Value); problem != "" {
		e.Problems = append(e.Problems, problem)
	} else {
		length = d
	}

	var option rrule.ROption
	if rule == nil {
		e.Problems = append(e.Problems, "RRULE is missing - an entry says how its shifts repeat, and a one-off shift is not part of the pattern")
	} else {
		var problems []string
		option, problems = parseRule(rule.Value)
		e.Problems = append(e.Problems, problems...)
	}

	if len(e.Problems) > 0 {
		return e, entry{}
	}

	option.Dtstart = start
	compiled, err := rrule.NewRRule(option)
	if err != nil {
		// parseRule checks every bound rrule-go does, so this is a rule
		// that library refuses for a reason this one has not learnt.
		e.Problems = []string{err.Error()}
		return e, entry{}
	}

	// RFC 5545 counts DTSTART as the first occurrence whether the rule makes
	// it or not, which would mint a Shift the rule never describes. Refused
	// rather than dropped, so what is written is what happens.
	first := compiled.After(start.Add(-time.Second), true)
	switch {
	case first.IsZero():
		e.Problems = []string{"its RRULE generates no shifts at all from its DTSTART"}
		return e, entry{}
	case !first.Equal(start):
		e.Problems = []string{fmt.Sprintf(
			"DTSTART is %s, which its RRULE does not generate - start it on a shift the rule makes, such as %s",
			readableMoment(start), readableMoment(first))}
		return e, entry{}
	}

	e.Description = describe(option, start, length, true)
	return e, entry{rule: compiled, start: start, length: length}
}

var dateTimePattern = regexp.MustCompile(`^\d{8}T\d{6}$`)

// entryStart reads an entry's DTSTART as a floating wall-clock time. A TZID
// naming the zone the Organisation's times are read in already is accepted and
// dropped; any other zone, or UTC, would make the time mean something else.
func entryStart(l contentLine, zone string) (time.Time, string) {
	value := strings.ToUpper(l.Value)
	if strings.EqualFold(l.Params["VALUE"], "DATE") || len(value) == 8 {
		return time.Time{}, "DTSTART needs a time, like DTSTART:20261101T193000 - an entry's shifts start at a time of day"
	}
	if tzid, ok := l.Params["TZID"]; ok && tzid != zone {
		return time.Time{}, fmt.Sprintf(
			"DTSTART is in %s, but the organisation's times are read in %s - drop the TZID, or change the time zone", tzid, zone)
	}
	if strings.HasSuffix(value, "Z") {
		return time.Time{}, fmt.Sprintf(
			"DTSTART ends in Z, which makes it UTC - write the local time without it; times are read in %s", zone)
	}
	if !dateTimePattern.MatchString(value) {
		return time.Time{}, fmt.Sprintf("DTSTART %q is not a date and time like 20261101T193000", l.Value)
	}
	at, err := time.Parse("20060102T150405", value)
	if err != nil {
		return time.Time{}, fmt.Sprintf("DTSTART %q is not a date and time like 20261101T193000", l.Value)
	}
	return at, ""
}

var durationPattern = regexp.MustCompile(`^([+-])?P(?:(\d+)W)?(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?)?$`)

// entryLength reads an entry's DURATION, bounded as a Shift's length is.
func entryLength(value string) (time.Duration, string) {
	v := strings.ToUpper(strings.TrimSpace(value))
	match := durationPattern.FindStringSubmatch(v)
	if match == nil || v == "P" || strings.HasSuffix(v, "T") || v == "+P" || v == "-P" {
		return 0, fmt.Sprintf("DURATION %q is not a length like PT3H or PT1H30M", value)
	}
	units := []time.Duration{7 * 24 * time.Hour, 24 * time.Hour, time.Hour, time.Minute, time.Second}
	var d time.Duration
	for i, unit := range units {
		if match[i+2] != "" {
			n, _ := strconv.Atoi(match[i+2])
			d += time.Duration(n) * unit
		}
	}
	if match[1] == "-" {
		d = -d
	}
	if d%time.Minute != 0 {
		return 0, "DURATION must be whole minutes"
	}
	if d < time.Minute || d > MaxLength {
		return 0, "a shift lasts from 1 minute to 24 hours, so DURATION is from PT1M to PT24H"
	}
	return d, ""
}

// sameStarts refuses two entries that generate the same start, naming both on
// each. One Shift cannot run for two lengths; and two entries meaning one
// Shift is a slip worth saying so about, where it used to pass in silence.
func sameStarts(read []Entry, entries []entry) {
	var first time.Time
	for i, e := range entries {
		if len(read[i].Problems) == 0 && (first.IsZero() || e.start.Before(first)) {
			first = e.start
		}
	}
	if first.IsZero() {
		return
	}

	owner := map[int64]int{}
	reported := map[[2]int]bool{}
	for i, e := range entries {
		if len(read[i].Problems) > 0 {
			continue
		}
		for _, at := range e.rule.Between(first, first.Add(Horizon), true) {
			j, taken := owner[at.Unix()]
			if !taken {
				owner[at.Unix()] = i
				continue
			}
			if reported[[2]int{j, i}] {
				continue
			}
			reported[[2]int{j, i}] = true
			when := readableMoment(at)
			read[j].Problems = append(read[j].Problems, fmt.Sprintf(
				"entry %d also starts a shift on %s - two entries cannot make the same shift", i+1, when))
			read[i].Problems = append(read[i].Problems, fmt.Sprintf(
				"entry %d also starts a shift on %s - two entries cannot make the same shift", j+1, when))
		}
	}
}

func readableMoment(t time.Time) string {
	return t.Format("Mon 2 Jan 2006 at 15:04")
}

func appendOnce(list []string, item string) []string {
	for _, have := range list {
		if have == item {
			return list
		}
	}
	return append(list, item)
}
