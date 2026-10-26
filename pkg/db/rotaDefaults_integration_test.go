package db_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
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
// service refuses it first with a message an Organiser can act on; this is what
// stops anything else writing one.
func TestSaveRotaDefaultsRefusesEndBeforeStart(t *testing.T) {
	database, _ := dbtest.New(t)

	err := database.SaveRotaDefaults(context.Background(), db.RotaDefaults{
		ShiftStartTime: "21:30",
		ShiftEndTime:   "19:30",
	})
	require.Error(t, err)
}

// The allocation settings are one section of the record, edited on their own.
// The document is stored and returned verbatim: what the rules in it mean is
// the domain's business, and the database only has to keep them (ADR 0012).
func TestUpdateAllocationSettings(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	var seen []string
	edit := func(next string) func(string) (string, error) {
		return func(current string) (string, error) {
			seen = append(seen, current)
			return next, nil
		}
	}

	require.NoError(t, database.UpdateAllocationSettings(ctx, edit(`{"rules":[{"id":"1"}]}`)))
	require.NoError(t, database.UpdateAllocationSettings(ctx, edit(`{"rules":[{"id":"2"}]}`)))

	assert.Equal(t, "", seen[0], "the first edit starts from nothing")
	assert.JSONEq(t, `{"rules":[{"id":"1"}]}`, seen[1], "each edit starts from the last one's document")

	defaults, err := database.GetRotaDefaults(ctx)
	require.NoError(t, err)
	assert.JSONEq(t, `{"rules":[{"id":"2"}]}`, defaults.AllocationSettings)
}

// An edit that refuses writes nothing, so a refused rule leaves the others as
// they were.
func TestUpdateAllocationSettingsWritesNothingWhenTheEditRefuses(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	require.NoError(t, database.UpdateAllocationSettings(ctx, func(string) (string, error) {
		return `{"rules":[{"id":"1"}]}`, nil
	}))

	refused := errors.New("refused")
	err := database.UpdateAllocationSettings(ctx, func(string) (string, error) {
		return `{"rules":[]}`, refused
	})
	require.ErrorIs(t, err, refused)

	defaults, err := database.GetRotaDefaults(ctx)
	require.NoError(t, err)
	assert.JSONEq(t, `{"rules":[{"id":"1"}]}`, defaults.AllocationSettings)
}

// Two edits at once each see the other's result rather than overwriting it:
// editing one rule must never lose a rule somebody else added meanwhile.
func TestConcurrentAllocationSettingsEditsBothLand(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			assert.NoError(t, database.UpdateAllocationSettings(ctx, func(current string) (string, error) {
				count := 0
				if current != "" {
					count = len(strings.Split(current, "x")) - 1
				}
				return fmt.Sprintf(`{"rules":"%s"}`, strings.Repeat("x", count+1)), nil
			}))
		}()
	}
	wg.Wait()

	defaults, err := database.GetRotaDefaults(ctx)
	require.NoError(t, err)
	assert.JSONEq(t, `{"rules":"xxxxxxxx"}`, defaults.AllocationSettings)
}

// Each section of the settings is saved without touching the others, so an
// Organiser editing the rules cannot blank the shift times and the other way
// round.
func TestSettingsSectionsDoNotOverwriteEachOther(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	write := func(document string) func(string) (string, error) {
		return func(string) (string, error) { return document, nil }
	}

	require.NoError(t, database.UpdateAllocationSettings(ctx, write(`{"rules":[{"id":"1"}]}`)))
	require.NoError(t, database.SaveRotaDefaults(ctx, db.RotaDefaults{
		ShiftStartTime: "19:30",
		ShiftEndTime:   "21:30",
		ShiftTimezone:  "Europe/London",
	}))

	defaults, err := database.GetRotaDefaults(ctx)
	require.NoError(t, err)
	assert.Equal(t, "19:30", defaults.ShiftStartTime)
	assert.JSONEq(t, `{"rules":[{"id":"1"}]}`, defaults.AllocationSettings)

	// And the other way round: editing the rules leaves the times alone.
	require.NoError(t, database.UpdateAllocationSettings(ctx, write(`{"rules":[]}`)))

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

// The column holds a document. Anything else is a bug in whatever wrote it,
// and the database is what stops it being stored.
func TestUpdateAllocationSettingsRefusesANonObject(t *testing.T) {
	database, _ := dbtest.New(t)

	err := database.UpdateAllocationSettings(context.Background(), func(string) (string, error) {
		return `["no_back_to_back"]`, nil
	})
	require.Error(t, err)
}
