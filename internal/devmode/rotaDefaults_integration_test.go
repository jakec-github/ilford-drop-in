package devmode_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/internal/devmode"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
	"github.com/jakechorley/ilford-drop-in/pkg/db/dbtest"
)

// The dev stack has no Organiser to set the Shift pattern and no config key to read
// them from, so the seed is what makes `scripts/dev-stack.sh start` hand over an
// app that can allocate a rota rather than one that refuses.
func TestSeedRotaDefaults(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	seeded, err := devmode.SeedRotaDefaults(ctx, database)
	require.NoError(t, err)
	assert.True(t, seeded)

	defaults, err := database.GetRotaDefaults(ctx)
	require.NoError(t, err)
	assert.Equal(t, "RRULE:FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=30", defaults.ShiftPattern)
	assert.NotEmpty(t, defaults.PatternStartFrom)
	assert.Equal(t, 120, defaults.ShiftLengthMinutes)
	assert.Equal(t, "Europe/London", defaults.ShiftTimezone)

	// And the allocator rules, for the same reason: a dev stack that could not
	// allocate would be one nobody could try the journey on. These three are
	// what the solver applied unconditionally before they became a choice, a
	// gap of one being what no_back_to_back was.
	assert.JSONEq(t, `{"rules": [
		{"id": "seed-max-frequency", "kind": "max_frequency", "value": 34, "scope": {}},
		{"id": "seed-shift-gap", "kind": "shift_gap", "value": 1, "scope": {}},
		{"id": "seed-shift-requires", "kind": "shift_requires", "option": "Male", "scope": {}}
	]}`, defaults.AllocationSettings)
}

// The seed runs on every dev-stack start, so it has to be a seed rather than a
// reset: a pattern changed by hand survives a restart.
func TestSeedRotaDefaultsLeavesSettingsAlone(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	require.NoError(t, database.SaveShiftPattern(ctx, db.RotaDefaults{
		ShiftPattern:       "RRULE:FREQ=DAILY;BYHOUR=9;BYMINUTE=0",
		PatternStartFrom:   "2026-11-01",
		ShiftLengthMinutes: 195,
		ShiftTimezone:      "UTC",
	}))

	seeded, err := devmode.SeedRotaDefaults(ctx, database)
	require.NoError(t, err)
	assert.False(t, seeded)

	defaults, err := database.GetRotaDefaults(ctx)
	require.NoError(t, err)
	assert.Equal(t, "RRULE:FREQ=DAILY;BYHOUR=9;BYMINUTE=0", defaults.ShiftPattern)
}
