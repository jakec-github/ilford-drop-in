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
