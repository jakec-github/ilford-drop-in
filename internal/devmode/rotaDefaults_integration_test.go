package devmode_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/internal/devmode"
	"github.com/jakechorley/ilford-drop-in/pkg/core/shiftpattern"
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
	// Every Sunday at 19:30 for two hours, from the coming Sunday — and a
	// pattern the app itself would save, not merely a string.
	assert.Contains(t, defaults.ShiftPattern, "T193000\nDURATION:PT2H\nRRULE:FREQ=WEEKLY;BYDAY=SU\n")
	pattern, err := shiftpattern.Compile(defaults.ShiftPattern, defaults.ShiftTimezone)
	require.NoError(t, err)
	assert.Equal(t, time.Sunday, pattern.First().Weekday())
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

	daily := "BEGIN:VEVENT\nDTSTART:20261101T090000\nDURATION:PT3H15M\nRRULE:FREQ=DAILY\nEND:VEVENT"
	require.NoError(t, database.SaveShiftPattern(ctx, daily))

	seeded, err := devmode.SeedRotaDefaults(ctx, database)
	require.NoError(t, err)
	assert.False(t, seeded)

	defaults, err := database.GetRotaDefaults(ctx)
	require.NoError(t, err)
	assert.Equal(t, daily, defaults.ShiftPattern)
}
