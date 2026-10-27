package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

const weeklySunday = "RRULE:FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=30"

func shiftPatternParams() ShiftPatternParams {
	return ShiftPatternParams{Pattern: weeklySunday, StartFrom: "2026-11-01", Timezone: "Europe/London", LengthMinutes: 120}
}

func TestSaveShiftPattern(t *testing.T) {
	store := &stubRotaDefaultsStore{}

	saved, err := SaveShiftPattern(context.Background(), store, shiftPatternParams(), zap.NewNop())
	require.NoError(t, err)

	want := db.RotaDefaults{ShiftPattern: weeklySunday, PatternStartFrom: "2026-11-01", ShiftLengthMinutes: 120, ShiftTimezone: "Europe/London"}
	require.Len(t, store.saved, 1)
	assert.Equal(t, want, store.saved[0])
	assert.True(t, saved.HasShiftPattern())
}

// Start from defaults to the day it is set, in the Organisation's own zone
// (ADR 0013), and the zone to the default, so an Organiser who fills in only
// the pattern and the length saves a pattern that can be used.
func TestShiftPatternFillsInStartFromAndTheZone(t *testing.T) {
	params := shiftPatternParams()
	params.StartFrom, params.Timezone = "", ""

	// 23:30 UTC on 31 October is already 1 November in Auckland, and still
	// 31 October in London.
	now := time.Date(2026, 10, 31, 23, 30, 0, 0, time.UTC)
	row, err := params.validate(now)
	require.NoError(t, err)
	assert.Equal(t, "2026-10-31", row.PatternStartFrom)
	assert.Equal(t, model.DefaultShiftTimezone, row.ShiftTimezone)

	params.Timezone = "Pacific/Auckland"
	row, err = params.validate(now)
	require.NoError(t, err)
	assert.Equal(t, "2026-11-01", row.PatternStartFrom)
}

// Every way of getting the section wrong is an ordinary mistake with a
// message, not a failure: an Organiser is typing into a form.
func TestSaveShiftPatternRefusesBadInput(t *testing.T) {
	cases := map[string]func(*ShiftPatternParams){
		"no pattern":       func(p *ShiftPatternParams) { p.Pattern = "  " },
		"a bad line":       func(p *ShiftPatternParams) { p.Pattern = weeklySunday + "\nFREQ=WEEKLY;BYDAY=SU" },
		"no length":        func(p *ShiftPatternParams) { p.LengthMinutes = 0 },
		"over a day":       func(p *ShiftPatternParams) { p.LengthMinutes = 24*60 + 1 },
		"not a date":       func(p *ShiftPatternParams) { p.StartFrom = "1/11/2026" },
		"unknown timezone": func(p *ShiftPatternParams) { p.Timezone = "Not/AZone" },
	}

	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			store := &stubRotaDefaultsStore{}
			params := shiftPatternParams()
			change(&params)

			_, err := SaveShiftPattern(context.Background(), store, params, zap.NewNop())
			require.ErrorIs(t, err, ErrInvalidInput)
			assert.Empty(t, store.saved)
		})
	}
}

// A refused pattern says which line is wrong and why, so the message is one
// an Organiser can act on.
func TestSaveShiftPatternNamesTheBadLine(t *testing.T) {
	params := shiftPatternParams()
	params.Pattern = weeklySunday + "\nFREQ=HOURLY;BYHOUR=10;BYMINUTE=0"

	_, err := SaveShiftPattern(context.Background(), &stubRotaDefaultsStore{}, params, zap.NewNop())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "line 2: FREQ=HOURLY is refused")
}

// A write the database refuses is a failure rather than an Organiser's
// mistake, and stays one all the way out — the API answers 500, not 400.
func TestSaveShiftPatternSurfacesAWriteFailure(t *testing.T) {
	store := &stubRotaDefaultsStore{writeErr: errors.New("connection refused")}

	_, err := SaveShiftPattern(context.Background(), store, shiftPatternParams(), zap.NewNop())
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrInvalidInput)
}

func TestPreviewShiftPattern(t *testing.T) {
	preview, err := PreviewShiftPattern(ShiftPatternPreviewParams{
		Pattern:   weeklySunday + "\nRRULE:FREQ=MONTHLY;BYDAY=1SA;BYHOUR=10;BYMINUTE=0",
		StartFrom: "2026-11-01",
		From:      "2026-11-01",
		To:        "2026-11-15",
	})
	require.NoError(t, err)

	require.Len(t, preview.Lines, 2)
	assert.Equal(t, "Every week on Sunday at 19:30", preview.Lines[0].Description)
	assert.Equal(t, "Every month on the first Saturday at 10:00", preview.Lines[1].Description)
	assert.Equal(t, []string{"2026-11-01T19:30:00", "2026-11-07T10:00:00", "2026-11-08T19:30:00"}, preview.Starts)
}

// A pattern with a problem is answered with it, line by line, rather than
// refused: it is a pattern still being written. It generates nothing, because
// it could not be saved.
func TestPreviewShiftPatternAnswersProblemsByLine(t *testing.T) {
	preview, err := PreviewShiftPattern(ShiftPatternPreviewParams{
		Pattern:   weeklySunday + "\nRDATE:20261101T100000",
		StartFrom: "2026-11-01",
		From:      "2026-11-01",
		To:        "2026-12-01",
	})
	require.NoError(t, err)

	require.Len(t, preview.Lines, 2)
	assert.Empty(t, preview.Lines[0].Problems)
	assert.NotEmpty(t, preview.Lines[1].Problems)
	assert.Empty(t, preview.Starts)
}

func TestPreviewShiftPatternRefusesARangeItCannotRead(t *testing.T) {
	cases := map[string][2]string{
		"not a date":    {"2026-11-01", "soon"},
		"backwards":     {"2026-12-01", "2026-11-01"},
		"over a year":   {"2026-01-01", "2027-06-01"},
		"an empty span": {"2026-11-01", "2026-11-01"},
	}
	for name, span := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := PreviewShiftPattern(ShiftPatternPreviewParams{Pattern: weeklySunday, StartFrom: "2026-11-01", From: span[0], To: span[1]})
			require.ErrorIs(t, err, ErrInvalidInput)
		})
	}
}
