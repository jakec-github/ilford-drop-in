package model

import (
	"fmt"
	"time"
)

// DefaultShiftTimezone is the zone a shift's times are read in when nobody has
// said otherwise. A fallback rather than a missing answer: without a zone a
// time of day cannot become a moment at all, and every deployment of this app
// is one Organisation in one place.
const DefaultShiftTimezone = "Europe/London"

// ShiftTimestampLayout is how a Shift's own start and end are spelled: a date
// and a time of day with no zone on the end of it, because a Shift's times are
// wall-clock facts about Ilford rather than instants on a global timeline
// (ADR 0007). The absent offset is the point — a timestamp carrying "Z" would
// be a different kind of thing.
const ShiftTimestampLayout = "2006-01-02T15:04:05"

// RotaDefaults is what an Organiser has decided about how the Organisation as a whole
// runs: one live, global record, edited on the Settings screen rather than set
// by an operator in the config file (ADR 0006): the Shift pattern rotas are
// minted from (ADR 0013), the zone their times are read in, and which optional
// allocator rules apply.
//
// Nothing seeds it, so every field is empty on a deployment nobody has
// configured. Empty is an ordinary state rather than a fault: without a pattern
// no rota can be defined, and that is all. Rotas already defined carry their
// own times and still render, allocate and publish.
type RotaDefaults struct {
	// ShiftPattern is the RRULE lines Shifts are generated from, as an Organiser
	// typed them. Empty means unset.
	ShiftPattern string
	// PatternStartFrom is the date the pattern counts from, "2006-01-02".
	// Empty means unset.
	PatternStartFrom string
	// ShiftLengthMinutes is how long each generated Shift runs. Zero means
	// unset.
	ShiftLengthMinutes int
	// ShiftTimezone is an IANA zone name. Empty means unset, and reads as
	// DefaultShiftTimezone.
	ShiftTimezone string
	// AllocationSettings is which optional allocator rules apply. The zero
	// value means every rule off, which is where a deployment starts.
	AllocationSettings AllocationSettings
}

// Timezone is the zone the shift times are read in: the one an Organiser chose, or
// the default when they have not chosen.
func (d RotaDefaults) Timezone() string {
	if d.ShiftTimezone == "" {
		return DefaultShiftTimezone
	}
	return d.ShiftTimezone
}

// HasShiftPattern reports whether a rota could be minted from these settings:
// a pattern, the date it counts from, and how long its Shifts run.
func (d RotaDefaults) HasShiftPattern() bool {
	return d.ShiftPattern != "" && d.PatternStartFrom != "" && d.ShiftLengthMinutes > 0
}

// ShiftInstants reads a Shift's own start and end — the local wall-clock times
// it carries, in ShiftTimestampLayout — as the moments they fall at, by
// applying the Organisation's timezone. It is the conversion every reader that needs
// an instant makes: a subscriber's calendar client and a browser both want to
// know when 19:30 in Ilford is where they are (ADR 0007).
//
// The times come from the Shift, which has carried its own since it was minted;
// the settings supply only the zone to read them in, and that always has an
// answer. So a Shift keeps the times it was minted with even after an Organiser
// changes the pattern, which is the point of storing them on the Shift.
//
// An untimed Shift is refused rather than answered with a midnight nobody
// chose. Callers render one by leaving the time out.
func (d RotaDefaults) ShiftInstants(startAt, endAt string) (start, end time.Time, err error) {
	loc, err := time.LoadLocation(d.Timezone())
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("failed to load shift timezone %q: %w", d.Timezone(), err)
	}

	start, err = time.ParseInLocation(ShiftTimestampLayout, startAt, loc)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("failed to read shift start %q: %w", startAt, err)
	}

	end, err = time.ParseInLocation(ShiftTimestampLayout, endAt, loc)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("failed to read shift end %q: %w", endAt, err)
	}

	return start, end, nil
}
