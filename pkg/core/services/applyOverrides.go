package services

import (
	"fmt"
	"strings"
	"time"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/shiftpattern"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// Applying the Overrides to a rota's Shifts is the arithmetic both the Define
// Rota preview and the definition itself run, so the preview's tags are what
// defining the rota will do.
//
// Each Override is applied in list order to every Shift it matches: on its
// generated start for one that names a time, on its date for one that does
// not (ADR 0013). Matching is always against the start the pattern generated —
// nothing an Override sets moves it, so a later Override still finds a Shift
// an earlier one moved where the pattern put it. A later start time or length
// replaces an earlier one; a later Shape replaces an earlier one whole; a later Pin for a volunteer replaces their earlier one, since a person
// fills at most one Seat on a Shift; custom entries add up.

// OverriddenShift is what the Overrides make of one planned Shift.
type OverriddenShift struct {
	// StartTime is the time of its own day the Overrides move it to, "15:04",
	// and "" when none did.
	StartTime string
	// LengthMinutes is how long the Overrides make it run, and 0 when none
	// did and it runs the pattern's length.
	LengthMinutes int
	// Seats is the Shape the Overrides give it, and SetsShape whether any did.
	// Without one the Shift takes the default Shape.
	SetsShape bool
	Seats     []db.OverrideSeat
	// Pins are the Pins it is minted with, before it has an id to carry.
	Pins []db.OverridePin
	// Touches are the Overrides that matched it, in the order they applied.
	Touches []OverrideTouch
}

// OverrideTouch is one Override matching one Shift, and what it set there.
type OverrideTouch struct {
	Number int
	// StartTime and LengthMinutes are what it set, "" and 0 for neither.
	StartTime     string
	LengthMinutes int
	SetsShape     bool
	Pins          int
}

// OverrideReach is how far one Override reached across a rota's Shifts.
type OverrideReach struct {
	Number      int
	Description string
	Granularity string
	Matched     int
	// MissedByTime is an exact-time Override that matched nothing, though it
	// falls on a date the rota has a Shift on: it is aimed at the right days,
	// so the pattern's time has likely changed under it.
	MissedByTime bool
}

// applyOverrides applies the Overrides, in order, to the Shifts the pattern
// generated. Both results are aligned with what they describe: one
// OverriddenShift per Shift, one OverrideReach per Override.
//
// A stored rule this build cannot read is an error rather than skipped. It was
// valid when it was saved, and skipping it would mint a rota quietly missing
// what an Organiser was promised.
func applyOverrides(rows []db.Override, shifts []db.Shift) ([]OverriddenShift, []OverrideReach, error) {
	starts := make([]time.Time, len(shifts))
	for i, s := range shifts {
		start, err := time.Parse(model.ShiftTimestampLayout, s.StartAt)
		if err != nil {
			return nil, nil, fmt.Errorf("planned shift has an unreadable start %q: %w", s.StartAt, err)
		}
		starts[i] = start
	}

	out := make([]OverriddenShift, len(shifts))
	reach := make([]OverrideReach, 0, len(rows))
	for n, row := range rows {
		number := n + 1
		line, rule := shiftpattern.ReadRule(row.RRule)
		if rule == nil {
			return nil, nil, wrapf(ErrInvalidInput,
				"override %d cannot be used - %s; correct it in the rota defaults",
				number, strings.Join(line.Problems, "; "))
		}

		// The granularity on the row decides, not the rule re-read: it is the
		// fact the Override was saved with.
		exact := row.Granularity == db.OverrideMatchesTime
		picks := rule.OnDate(starts)
		if exact {
			picks = rule.Picks(starts)
		}

		r := OverrideReach{Number: number, Description: line.Description, Granularity: row.Granularity}
		for i, picked := range picks {
			if !picked {
				continue
			}
			r.Matched++
			shift := &out[i]
			if row.StartTime != "" {
				shift.StartTime = row.StartTime
			}
			if row.LengthMinutes != 0 {
				shift.LengthMinutes = row.LengthMinutes
			}
			if row.SetsShape {
				shift.SetsShape = true
				shift.Seats = row.Seats
			}
			for _, pin := range row.Pins {
				if pin.VolunteerID != "" {
					shift.Pins = withoutVolunteer(shift.Pins, pin.VolunteerID)
				}
				shift.Pins = append(shift.Pins, pin)
			}
			shift.Touches = append(shift.Touches, OverrideTouch{
				Number:        number,
				StartTime:     row.StartTime,
				LengthMinutes: row.LengthMinutes,
				SetsShape:     row.SetsShape,
				Pins:          len(row.Pins),
			})
		}
		if exact && r.Matched == 0 {
			for _, onDate := range rule.OnDate(starts) {
				if onDate {
					r.MissedByTime = true
					break
				}
			}
		}
		reach = append(reach, r)
	}
	return out, reach, nil
}

func withoutVolunteer(pins []db.OverridePin, volunteerID string) []db.OverridePin {
	kept := pins[:0:0]
	for _, p := range pins {
		if p.VolunteerID != volunteerID {
			kept = append(kept, p)
		}
	}
	return kept
}
