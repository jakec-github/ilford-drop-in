package model_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
)

// The stored times are wall-clock in the drop-in's zone, so the same 19:30
// shift is a different moment in winter and in summer. This is the whole reason
// the zone is stored rather than an offset.
func TestShiftTimesFollowsTheZone(t *testing.T) {
	defaults := model.RotaDefaults{ShiftStartTime: "19:30", ShiftEndTime: "21:30"}

	// GMT date: London is UTC+0
	start, end, err := defaults.ShiftTimes("2026-01-12")
	require.NoError(t, err)
	assert.Equal(t, "2026-01-12T19:30:00Z", start.UTC().Format(time.RFC3339))
	assert.Equal(t, "2026-01-12T21:30:00Z", end.UTC().Format(time.RFC3339))

	// BST date: London is UTC+1
	start, end, err = defaults.ShiftTimes("2026-07-13")
	require.NoError(t, err)
	assert.Equal(t, "2026-07-13T18:30:00Z", start.UTC().Format(time.RFC3339))
	assert.Equal(t, "2026-07-13T20:30:00Z", end.UTC().Format(time.RFC3339))
}

// A zone an admin has chosen is used; no zone falls back rather than failing,
// because without one a time of day cannot become a moment at all.
func TestShiftTimesTimezone(t *testing.T) {
	defaults := model.RotaDefaults{ShiftStartTime: "19:30", ShiftEndTime: "21:30"}
	assert.Equal(t, model.DefaultShiftTimezone, defaults.Timezone())

	defaults.ShiftTimezone = "UTC"
	assert.Equal(t, "UTC", defaults.Timezone())

	start, _, err := defaults.ShiftTimes("2026-07-13")
	require.NoError(t, err)
	assert.Equal(t, "2026-07-13T19:30:00Z", start.UTC().Format(time.RFC3339))
}

func TestShiftTimesRejectsAMalformedDate(t *testing.T) {
	defaults := model.RotaDefaults{ShiftStartTime: "19:30", ShiftEndTime: "21:30"}

	_, _, err := defaults.ShiftTimes("13/07/2026")
	assert.Error(t, err)
}

// Settings nobody has filled in are the ordinary first state of a deployment.
// Asking for a time is answered with a refusal, and the caller is expected to
// have asked HasShiftTimes first.
func TestShiftTimesUnset(t *testing.T) {
	var defaults model.RotaDefaults

	assert.False(t, defaults.HasShiftTimes())
	assert.Equal(t, []string{
		"the default shift start time",
		"the default shift end time",
	}, defaults.MissingShiftTimes())

	_, _, err := defaults.ShiftTimes("2026-07-13")
	assert.Error(t, err)
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

// A Shift's own start and end are wall-clock, so they carry no zone at all —
// the same 19:30 on a winter date and a summer date spell the same timestamp,
// where ShiftTimes gives two different moments (ADR 0007).
func TestShiftTimestampsAreWallClock(t *testing.T) {
	defaults := model.RotaDefaults{ShiftStartTime: "19:30", ShiftEndTime: "21:30"}

	start, end, err := defaults.ShiftTimestamps("2026-01-12")
	require.NoError(t, err)
	assert.Equal(t, "2026-01-12T19:30:00", start)
	assert.Equal(t, "2026-01-12T21:30:00", end)

	start, end, err = defaults.ShiftTimestamps("2026-07-13")
	require.NoError(t, err)
	assert.Equal(t, "2026-07-13T19:30:00", start)
	assert.Equal(t, "2026-07-13T21:30:00", end)
}

// The zone setting cannot move a Shift's stated times, because they are not a
// moment: it is the calendar feed's job to turn them into one.
func TestShiftTimestampsIgnoreTheZone(t *testing.T) {
	defaults := model.RotaDefaults{ShiftStartTime: "19:30", ShiftEndTime: "21:30", ShiftTimezone: "Pacific/Auckland"}

	start, _, err := defaults.ShiftTimestamps("2026-07-13")
	require.NoError(t, err)
	assert.Equal(t, "2026-07-13T19:30:00", start)
}

// Unset settings and an unreadable date are refused the same way ShiftTimes
// refuses them: a caller with no times to write must not write half of them.
func TestShiftTimestampsRefuses(t *testing.T) {
	var unset model.RotaDefaults
	_, _, err := unset.ShiftTimestamps("2026-07-13")
	assert.Error(t, err)

	defaults := model.RotaDefaults{ShiftStartTime: "19:30", ShiftEndTime: "21:30"}
	_, _, err = defaults.ShiftTimestamps("13/07/2026")
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
