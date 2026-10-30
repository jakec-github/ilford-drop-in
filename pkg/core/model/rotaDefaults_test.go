package model_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
)

// A zone an Organiser has chosen is used; no zone falls back rather than failing,
// because without one a time of day cannot become a moment at all.
func TestShiftTimezone(t *testing.T) {
	var defaults model.RotaDefaults
	assert.Equal(t, model.DefaultShiftTimezone, defaults.Timezone())

	defaults.ShiftTimezone = "UTC"
	assert.Equal(t, "UTC", defaults.Timezone())
}

// Each entry of a pattern carries its own start and length (ADR 0014), so a
// pattern is all a rota is minted from.
func TestHasShiftPattern(t *testing.T) {
	assert.False(t, model.RotaDefaults{}.HasShiftPattern())
	assert.True(t, model.RotaDefaults{
		ShiftPattern: "BEGIN:VEVENT\nDTSTART:20261101T193000\nDURATION:PT2H\nRRULE:FREQ=WEEKLY\nEND:VEVENT",
	}.HasShiftPattern())
}

// A Shift's own times are read in the Organisation's zone, so the same stored 19:30
// is a different moment in winter and in summer — the conversion the calendar
// feed and the shift listing both need (issue #134, ADR 0007).
func TestShiftInstantsReadTheStoredTimesInTheZone(t *testing.T) {
	var defaults model.RotaDefaults

	// GMT: London is UTC+0.
	start, end, err := defaults.ShiftInstants("2026-01-11T19:30:00", "2026-01-11T21:30:00")
	require.NoError(t, err)
	assert.Equal(t, "2026-01-11T19:30:00Z", start.UTC().Format(time.RFC3339))
	assert.Equal(t, "2026-01-11T21:30:00Z", end.UTC().Format(time.RFC3339))

	// BST: London is UTC+1.
	start, end, err = defaults.ShiftInstants("2026-07-12T19:30:00", "2026-07-12T21:30:00")
	require.NoError(t, err)
	assert.Equal(t, "2026-07-12T18:30:00Z", start.UTC().Format(time.RFC3339))
	assert.Equal(t, "2026-07-12T20:30:00Z", end.UTC().Format(time.RFC3339))
}

// The zone an Organiser chose is what the stored times are read in. Unlike
// ShiftTimes this needs no shift-time settings at all: the times come from the
// Shift, and the settings supply only the zone to read them in.
func TestShiftInstantsUseTheChosenZone(t *testing.T) {
	defaults := model.RotaDefaults{ShiftTimezone: "Pacific/Auckland"}

	start, _, err := defaults.ShiftInstants("2026-07-12T19:30:00", "2026-07-12T21:30:00")
	require.NoError(t, err)
	assert.Equal(t, "2026-07-12T07:30:00Z", start.UTC().Format(time.RFC3339))
}

// A Shift with no times has no moments, and says so rather than answering with
// a midnight nobody chose. Callers render such a Shift by leaving the time out.
func TestShiftInstantsRefuseAnUntimedShift(t *testing.T) {
	var defaults model.RotaDefaults

	_, _, err := defaults.ShiftInstants("", "")
	assert.Error(t, err)

	_, _, err = defaults.ShiftInstants("2026-07-12 19:30", "2026-07-12T21:30:00")
	assert.Error(t, err, "a timestamp that is not in ShiftTimestampLayout is unreadable")
}
