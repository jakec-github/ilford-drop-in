package db_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
	"github.com/jakechorley/ilford-drop-in/pkg/db/dbtest"
)

// A database nobody has configured yet has no settings row at all, and that
// reads as empty settings rather than as an error — the migration seeds nothing
// (ADR 0006), so every fresh deployment starts here.
func TestGetRotaDefaultsUnset(t *testing.T) {
	database, _ := dbtest.New(t)

	defaults, err := database.GetRotaDefaults(context.Background())
	require.NoError(t, err)
	assert.Equal(t, db.RotaDefaults{}, defaults)
}

// The first save writes the row; a later one rewrites it. Times come back in
// the "15:04" the app states them in, not in Postgres's own rendering.
func TestSaveRotaDefaults(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	require.NoError(t, database.SaveRotaDefaults(ctx, db.RotaDefaults{
		ShiftStartTime: "19:30",
		ShiftEndTime:   "21:30",
		ShiftTimezone:  "Europe/London",
	}))

	defaults, err := database.GetRotaDefaults(ctx)
	require.NoError(t, err)
	assert.Equal(t, db.RotaDefaults{
		ShiftStartTime: "19:30",
		ShiftEndTime:   "21:30",
		ShiftTimezone:  "Europe/London",
	}, defaults)

	require.NoError(t, database.SaveRotaDefaults(ctx, db.RotaDefaults{
		ShiftStartTime: "09:00",
		ShiftEndTime:   "12:15",
		ShiftTimezone:  "UTC",
	}))

	defaults, err = database.GetRotaDefaults(ctx)
	require.NoError(t, err)
	assert.Equal(t, db.RotaDefaults{
		ShiftStartTime: "09:00",
		ShiftEndTime:   "12:15",
		ShiftTimezone:  "UTC",
	}, defaults)
}

// An empty field is stored as null and reads back empty, so "not set" survives
// the round trip rather than coming back as midnight.
func TestSaveRotaDefaultsClearsFields(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	require.NoError(t, database.SaveRotaDefaults(ctx, db.RotaDefaults{
		ShiftStartTime: "19:30",
		ShiftEndTime:   "21:30",
		ShiftTimezone:  "Europe/London",
	}))
	require.NoError(t, database.SaveRotaDefaults(ctx, db.RotaDefaults{}))

	defaults, err := database.GetRotaDefaults(ctx)
	require.NoError(t, err)
	assert.Equal(t, db.RotaDefaults{}, defaults)
}

// The database is the backstop for a shift that ends before it starts. The
// service refuses it first with a message an admin can act on; this is what
// stops anything else writing one.
func TestSaveRotaDefaultsRefusesEndBeforeStart(t *testing.T) {
	database, _ := dbtest.New(t)

	err := database.SaveRotaDefaults(context.Background(), db.RotaDefaults{
		ShiftStartTime: "21:30",
		ShiftEndTime:   "19:30",
	})
	require.Error(t, err)
}

// The allocation settings are one section of the record, saved on their own.
// The JSON is stored and returned verbatim: what the answers mean is the
// domain's business, and the database only has to keep them (issue #130).
func TestSaveAllocationSettings(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	require.NoError(t, database.SaveAllocationSettings(ctx,
		`{"enabled":{"no_back_to_back":true},"maxFrequency":0.34}`))

	defaults, err := database.GetRotaDefaults(ctx)
	require.NoError(t, err)
	assert.JSONEq(t, `{"enabled":{"no_back_to_back":true},"maxFrequency":0.34}`,
		defaults.AllocationSettings)
}

// Each section of the settings is saved without touching the others, so an
// admin editing the toggles cannot blank the shift times and the other way
// round.
func TestSettingsSectionsDoNotOverwriteEachOther(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	require.NoError(t, database.SaveAllocationSettings(ctx, `{"enabled":{"male_required":true}}`))
	require.NoError(t, database.SaveRotaDefaults(ctx, db.RotaDefaults{
		ShiftStartTime: "19:30",
		ShiftEndTime:   "21:30",
		ShiftTimezone:  "Europe/London",
	}))

	defaults, err := database.GetRotaDefaults(ctx)
	require.NoError(t, err)
	assert.Equal(t, "19:30", defaults.ShiftStartTime)
	assert.JSONEq(t, `{"enabled":{"male_required":true}}`, defaults.AllocationSettings)

	// And the other way round: saving the toggles leaves the times alone.
	require.NoError(t, database.SaveAllocationSettings(ctx, `{"enabled":{}}`))

	defaults, err = database.GetRotaDefaults(ctx)
	require.NoError(t, err)
	assert.Equal(t, "21:30", defaults.ShiftEndTime)
}

// A deployment nobody has configured reads as no settings at all rather than
// as an empty document, so "unset" is one state on this side of the boundary.
func TestGetAllocationSettingsUnset(t *testing.T) {
	database, _ := dbtest.New(t)

	defaults, err := database.GetRotaDefaults(context.Background())
	require.NoError(t, err)
	assert.Empty(t, defaults.AllocationSettings)
}

// The column holds a mapping of answers. Anything else is a bug in whatever
// wrote it, and the database is what stops it being stored.
func TestSaveAllocationSettingsRefusesANonObject(t *testing.T) {
	database, _ := dbtest.New(t)

	err := database.SaveAllocationSettings(context.Background(), `["no_back_to_back"]`)
	require.Error(t, err)
}
