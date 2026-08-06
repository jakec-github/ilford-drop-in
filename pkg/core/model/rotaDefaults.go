package model

import (
	"fmt"
	"time"
)

// DefaultShiftTimezone is the zone a shift's times are read in when nobody has
// said otherwise. A fallback rather than a missing answer: without a zone a
// time of day cannot become a moment at all, and every deployment of this app
// is one drop-in in one place.
const DefaultShiftTimezone = "Europe/London"

// ShiftTimeLayout is how a time of day is spelled everywhere it crosses a
// boundary — the settings API, the database, an admin's form field. Hours and
// minutes only: the drop-in does not start at half past seven and eleven
// seconds.
const ShiftTimeLayout = "15:04"

// ShiftTimestampLayout is how a Shift's own start and end are spelled: a date
// and a time of day with no zone on the end of it, because a Shift's times are
// wall-clock facts about Ilford rather than instants on a global timeline
// (ADR 0007). The absent offset is the point — a timestamp carrying "Z" would
// be a different kind of thing.
const ShiftTimestampLayout = "2006-01-02T15:04:05"

// RotaDefaults is what an admin has decided about how the drop-in as a whole
// runs: one live, global record, edited on the Settings screen rather than set
// by an operator in the config file (ADR 0006). It holds the default shift
// times today; the default Shape, the allocation toggles and the Standing
// Preallocations join it in later tickets.
//
// Nothing seeds it, so every field is empty on a deployment nobody has
// configured. Empty is an ordinary state rather than a fault: it blocks
// allocation, with a message naming what is missing, and nothing else. The rota
// still renders, availability still works, and the parts that cannot be drawn
// without a time say so by leaving the time out rather than by failing.
type RotaDefaults struct {
	// ShiftStartTime and ShiftEndTime are wall-clock times of day in the
	// drop-in's own timezone, spelled in ShiftTimeLayout. Empty means unset.
	ShiftStartTime string
	ShiftEndTime   string
	// ShiftTimezone is an IANA zone name. Empty means unset, and reads as
	// DefaultShiftTimezone.
	ShiftTimezone string
	// AllocationSettings is which optional allocator rules apply. The zero
	// value means every rule off, which is where a deployment starts.
	AllocationSettings AllocationSettings
}

// Timezone is the zone the shift times are read in: the one an admin chose, or
// the default when they have not chosen.
func (d RotaDefaults) Timezone() string {
	if d.ShiftTimezone == "" {
		return DefaultShiftTimezone
	}
	return d.ShiftTimezone
}

// MissingShiftTimes names the shift-time settings an admin has yet to fill in,
// worded as they read on the Settings screen so the message an allocation
// refuses with can be acted on without translation. Empty means a time can be
// computed.
//
// The timezone is deliberately not here. It falls back rather than blocking:
// the only way to reach this app with no zone is to have set nothing at all,
// where the two times already say so.
func (d RotaDefaults) MissingShiftTimes() []string {
	var missing []string
	if d.ShiftStartTime == "" {
		missing = append(missing, "the default shift start time")
	}
	if d.ShiftEndTime == "" {
		missing = append(missing, "the default shift end time")
	}
	return missing
}

// HasShiftTimes reports whether ShiftTimes can answer.
func (d RotaDefaults) HasShiftTimes() bool {
	return len(d.MissingShiftTimes()) == 0
}

// ShiftTimes turns a Shift's date ("2006-01-02") into the moment it starts and
// the moment it ends, by reading the stored times of day in the drop-in's
// timezone. It is the successor to config.ShiftTimes — the times moved out of
// the config file and into the settings in ticket #128.
//
// Callers that merely render should ask HasShiftTimes first and leave the time
// out when the answer is no; an error here means the settings are unusable, not
// that this particular date is.
func (d RotaDefaults) ShiftTimes(dateStr string) (start, end time.Time, err error) {
	if !d.HasShiftTimes() {
		return time.Time{}, time.Time{}, fmt.Errorf("the drop-in's shift times have not been set")
	}

	loc, err := time.LoadLocation(d.Timezone())
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("failed to load shift timezone %q: %w", d.Timezone(), err)
	}

	start, err = time.ParseInLocation("2006-01-02 "+ShiftTimeLayout, dateStr+" "+d.ShiftStartTime, loc)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("failed to parse shift start for %q: %w", dateStr, err)
	}

	end, err = time.ParseInLocation("2006-01-02 "+ShiftTimeLayout, dateStr+" "+d.ShiftEndTime, loc)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("failed to parse shift end for %q: %w", dateStr, err)
	}

	return start, end, nil
}

// ShiftInstants reads a Shift's own start and end — the local wall-clock times
// it carries, in ShiftTimestampLayout — as the moments they fall at, by
// applying the drop-in's timezone. It is the conversion every reader that needs
// an instant makes: a subscriber's calendar client and a browser both want to
// know when 19:30 in Ilford is where they are (ADR 0007).
//
// Unlike ShiftTimes it does not consult the shift-time settings at all. The
// times come from the Shift, which has carried its own since it was minted; the
// settings supply only the zone to read them in, and that always has an answer.
// So a Shift keeps the times it was minted with even after an admin changes the
// defaults, which is the point of storing them on the Shift.
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

// ShiftTimestamps turns a Shift's date ("2006-01-02") into the local times the
// Shift itself carries: the default times of day written onto that date, in
// ShiftTimestampLayout. This is what a Shift stores (ADR 0007), and it is a
// different answer from ShiftTimes — no zone is applied, because none has been
// resolved. The stored zone says how to read these later; it does not move them.
//
// It refuses when the settings are unset rather than writing half a Shift's
// times, so a caller with times to write should ask HasShiftTimes first and
// leave both columns unset when the answer is no.
func (d RotaDefaults) ShiftTimestamps(dateStr string) (start, end string, err error) {
	startTime, endTime, err := d.ShiftTimes(dateStr)
	if err != nil {
		return "", "", err
	}
	return startTime.Format(ShiftTimestampLayout), endTime.Format(ShiftTimestampLayout), nil
}
