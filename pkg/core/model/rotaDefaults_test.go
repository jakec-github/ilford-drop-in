package model_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
)

// A zone an admin has chosen is used; no zone falls back rather than failing,
// because without one a time of day cannot become a moment at all.
func TestShiftTimezone(t *testing.T) {
	defaults := model.RotaDefaults{ShiftStartTime: "19:30", ShiftEndTime: "21:30"}
	assert.Equal(t, model.DefaultShiftTimezone, defaults.Timezone())

	defaults.ShiftTimezone = "UTC"
	assert.Equal(t, "UTC", defaults.Timezone())
}

// Settings nobody has filled in are the ordinary first state of a deployment,
// named a section at a time so the refusal an allocation gives can be acted on.
func TestShiftTimesUnset(t *testing.T) {
	var defaults model.RotaDefaults

	assert.False(t, defaults.HasShiftTimes())
	assert.Equal(t, []string{
		"the default shift start time",
		"the default shift end time",
	}, defaults.MissingShiftTimes())
}

// Half-filled settings name only the half that is missing — the message an
// allocation refuses with has to say what to go and do.
func TestMissingShiftTimesNamesOnlyWhatIsMissing(t *testing.T) {
	defaults := model.RotaDefaults{ShiftStartTime: "19:30"}

	assert.Equal(t, []string{"the default shift end time"}, defaults.MissingShiftTimes())
	assert.False(t, defaults.HasShiftTimes())
}

func TestHasShiftTimes(t *testing.T) {
	defaults := model.RotaDefaults{ShiftStartTime: "19:30", ShiftEndTime: "21:30"}

	assert.True(t, defaults.HasShiftTimes())
	assert.Empty(t, defaults.MissingShiftTimes())
}

// A Shift's own start and end are wall-clock, so they carry no zone at all: the
// same 19:30 on a winter date and a summer date spell the same timestamp, and
// only ShiftInstants below turns either into a moment (ADR 0007).
func TestShiftTimestampsAreWallClock(t *testing.T) {
	start, end, err := model.ShiftTimestamps("2026-01-12", "19:30", "21:30")
	require.NoError(t, err)
	assert.Equal(t, "2026-01-12T19:30:00", start)
	assert.Equal(t, "2026-01-12T21:30:00", end)

	start, end, err = model.ShiftTimestamps("2026-07-13", "19:30", "21:30")
	require.NoError(t, err)
	assert.Equal(t, "2026-07-13T19:30:00", start)
	assert.Equal(t, "2026-07-13T21:30:00", end)
}

// A caller with no readable times to write must not write half of them, and is
// told which half it got wrong.
func TestShiftTimestampsRefuses(t *testing.T) {
	_, _, err := model.ShiftTimestamps("2026-07-13", "", "21:30")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "start")

	_, _, err = model.ShiftTimestamps("2026-07-13", "19:30", "half nine")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "end")

	_, _, err = model.ShiftTimestamps("13/07/2026", "19:30", "21:30")
	assert.Error(t, err)
}

// A Shift's own times are read in the drop-in's zone, so the same stored 19:30
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

// The zone an admin chose is what the stored times are read in. Unlike
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
