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

// The dev stack has no admin to set the shift times and no config key to read
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
	assert.Equal(t, db.RotaDefaults{
		ShiftStartTime: "19:30",
		ShiftEndTime:   "21:30",
		ShiftTimezone:  "Europe/London",
	}, defaults)
}

// The seed runs on every dev-stack start, so it has to be a seed rather than a
// reset: a time changed by hand survives a restart.
func TestSeedRotaDefaultsLeavesSettingsAlone(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	require.NoError(t, database.SaveRotaDefaults(ctx, db.RotaDefaults{
		ShiftStartTime: "09:00", ShiftEndTime: "12:15", ShiftTimezone: "UTC",
	}))

	seeded, err := devmode.SeedRotaDefaults(ctx, database)
	require.NoError(t, err)
	assert.False(t, seeded)

	defaults, err := database.GetRotaDefaults(ctx)
	require.NoError(t, err)
	assert.Equal(t, "09:00", defaults.ShiftStartTime)
}
