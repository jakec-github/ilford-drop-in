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

// The first save writes the row; a later one rewrites it. The pattern comes
// back exactly as written.
func TestSaveShiftPattern(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	weekly := "BEGIN:VEVENT\nDTSTART:20261101T193000\nDURATION:PT2H\nRRULE:FREQ=WEEKLY;BYDAY=SU\nEND:VEVENT"
	require.NoError(t, database.SaveShiftPattern(ctx, weekly))

	defaults, err := database.GetRotaDefaults(ctx)
	require.NoError(t, err)
	assert.Equal(t, db.RotaDefaults{ShiftPattern: weekly}, defaults)

	daily := "BEGIN:VEVENT\r\nDTSTART:20270104T100000\r\nDURATION:PT3H15M\r\nRRULE:FREQ=DAILY\r\nEND:VEVENT"
	require.NoError(t, database.SaveShiftPattern(ctx, daily))

	defaults, err = database.GetRotaDefaults(ctx)
	require.NoError(t, err)
	assert.Equal(t, daily, defaults.ShiftPattern)
}

// An empty pattern is stored as null and reads back empty, so "not set"
// survives the round trip.
func TestSaveShiftPatternClearsIt(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	dbtest.SeedRotaDefaults(t, database)
	require.NoError(t, database.SaveShiftPattern(ctx, ""))

	defaults, err := database.GetRotaDefaults(ctx)
	require.NoError(t, err)
	assert.Equal(t, db.RotaDefaults{ShiftTimezone: "Europe/London"}, defaults)
}

// The time zone is a section of its own: saving it leaves the pattern alone,
// and saving the pattern leaves it alone.
func TestSaveTimezone(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	require.NoError(t, database.SaveTimezone(ctx, "America/New_York"))
	defaults, err := database.GetRotaDefaults(ctx)
	require.NoError(t, err)
	assert.Equal(t, db.RotaDefaults{ShiftTimezone: "America/New_York"}, defaults)

	require.NoError(t, database.SaveShiftPattern(ctx, dbtest.SeedPattern))
	require.NoError(t, database.SaveTimezone(ctx, "Europe/London"))
	defaults, err = database.GetRotaDefaults(ctx)
	require.NoError(t, err)
	assert.Equal(t, db.RotaDefaults{ShiftPattern: dbtest.SeedPattern, ShiftTimezone: "Europe/London"}, defaults)
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
// Organiser editing the rules cannot blank the Shift pattern and the other way
// round.
func TestSettingsSectionsDoNotOverwriteEachOther(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	write := func(document string) func(string) (string, error) {
		return func(string) (string, error) { return document, nil }
	}

	require.NoError(t, database.UpdateAllocationSettings(ctx, write(`{"rules":[{"id":"1"}]}`)))
	dbtest.SeedRotaDefaults(t, database)

	defaults, err := database.GetRotaDefaults(ctx)
	require.NoError(t, err)
	assert.Equal(t, dbtest.SeedPattern, defaults.ShiftPattern)
	assert.JSONEq(t, `{"rules":[{"id":"1"}]}`, defaults.AllocationSettings)

	// And the other way round: editing the rules leaves the pattern alone.
	require.NoError(t, database.UpdateAllocationSettings(ctx, write(`{"rules":[]}`)))

	defaults, err = database.GetRotaDefaults(ctx)
	require.NoError(t, err)
	assert.Equal(t, dbtest.SeedPattern, defaults.ShiftPattern)
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
