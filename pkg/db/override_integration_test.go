package db_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
	"github.com/jakechorley/ilford-drop-in/pkg/db/dbtest"
)

// seedRole writes a Role for an Override to reference, since the foreign keys
// mean one cannot exist without it.
func seedRole(t *testing.T, database *db.DB, name string, priority int) string {
	t.Helper()
	id := uuid.New().String()
	require.NoError(t, database.InsertRole(context.Background(), db.Role{
		ID: id, Name: name, Priority: priority, Colour: "teal",
	}))
	return id
}

func overrideIDs(overrides []db.Override) []string {
	ids := make([]string, len(overrides))
	for i, o := range overrides {
		ids[i] = o.ID
	}
	return ids
}

func TestOverrideInsertReadUpdateDelete(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	lead := seedRole(t, database, "Team lead", 1)
	service := seedRole(t, database, "Service volunteer", 2)

	first := db.Override{
		ID: uuid.New().String(), RRule: "DTSTART;VALUE=DATE:20261101\nRRULE:FREQ=MONTHLY;BYDAY=1SU",
		Granularity: db.OverrideMatchesDate,
		Pins: []db.OverridePin{
			{ID: uuid.New().String(), RoleID: lead, VolunteerID: "alice"},
			{ID: uuid.New().String(), RoleID: service, CustomValue: "St John's team"},
		},
	}
	// A Shape that asks for nobody: set, with no Seats.
	second := db.Override{
		ID: uuid.New().String(), RRule: "DTSTART;VALUE=DATE:20261108\nRRULE:FREQ=WEEKLY;BYDAY=SU;BYHOUR=10;BYMINUTE=0",
		Granularity: db.OverrideMatchesTime, SetsShape: true,
	}
	require.NoError(t, database.InsertOverride(ctx, first))
	require.NoError(t, database.InsertOverride(ctx, second))

	overrides, err := database.GetOverrides(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{first.ID, second.ID}, overrideIDs(overrides), "a new Override goes last")

	got := overrides[0]
	assert.Equal(t, "DTSTART;VALUE=DATE:20261101\nRRULE:FREQ=MONTHLY;BYDAY=1SU", got.RRule)
	assert.Equal(t, db.OverrideMatchesDate, got.Granularity)
	assert.False(t, got.SetsShape)
	assert.Empty(t, got.Seats)
	assert.ElementsMatch(t, first.Pins, got.Pins)
	assert.True(t, overrides[1].SetsShape)
	assert.Empty(t, overrides[1].Seats)
	assert.Equal(t, "", got.StartTime, "no start time leaves the pattern's")
	assert.Equal(t, 0, got.LengthMinutes, "no length leaves the pattern's")

	// An update replaces the Shape and the Pins whole, and keeps the place.
	first.SetsShape = true
	first.Seats = []db.OverrideSeat{{RoleID: service, Seats: 4}, {RoleID: lead, Seats: 1}}
	first.Pins = []db.OverridePin{{ID: uuid.New().String(), RoleID: service, VolunteerID: "bob"}}
	first.Granularity = db.OverrideMatchesTime
	first.RRule = "FREQ=MONTHLY;BYDAY=1SU;BYHOUR=19;BYMINUTE=30"
	first.StartTime = "18:45"
	first.LengthMinutes = 150
	require.NoError(t, database.UpdateOverride(ctx, first))

	overrides, err = database.GetOverrides(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{first.ID, second.ID}, overrideIDs(overrides))
	got = overrides[0]
	assert.Equal(t, first.RRule, got.RRule)
	assert.Equal(t, db.OverrideMatchesTime, got.Granularity)
	assert.Equal(t, []db.OverrideSeat{{RoleID: lead, Seats: 1}, {RoleID: service, Seats: 4}}, got.Seats,
		"Seats come back in the order they are filled")
	assert.Equal(t, first.Pins, got.Pins)
	assert.Equal(t, "18:45", got.StartTime)
	assert.Equal(t, 150, got.LengthMinutes)

	// And clearing them leaves the pattern's again.
	first.StartTime, first.LengthMinutes = "", 0
	require.NoError(t, database.UpdateOverride(ctx, first))
	overrides, err = database.GetOverrides(ctx)
	require.NoError(t, err)
	assert.Equal(t, "", overrides[0].StartTime)
	assert.Equal(t, 0, overrides[0].LengthMinutes)

	deleted, err := database.DeleteOverride(ctx, first.ID)
	require.NoError(t, err)
	assert.True(t, deleted)
	deleted, err = database.DeleteOverride(ctx, first.ID)
	require.NoError(t, err)
	assert.False(t, deleted, "a second delete reports that nothing matched")

	assert.ErrorIs(t, database.UpdateOverride(ctx, first), db.ErrOverrideNotFound)
}

func TestOverrideRefusesOneVolunteerPinnedTwice(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	lead := seedRole(t, database, "Team lead", 1)
	service := seedRole(t, database, "Service volunteer", 2)

	err := database.InsertOverride(ctx, db.Override{
		ID: uuid.New().String(), RRule: "DTSTART;VALUE=DATE:20261101\nRRULE:FREQ=WEEKLY;BYDAY=SU",
		Granularity: db.OverrideMatchesDate,
		Pins: []db.OverridePin{
			{ID: uuid.New().String(), RoleID: lead, VolunteerID: "alice"},
			{ID: uuid.New().String(), RoleID: service, VolunteerID: "alice"},
		},
	})
	assert.ErrorIs(t, err, db.ErrDuplicateOverridePin)

	overrides, err := database.GetOverrides(ctx)
	require.NoError(t, err)
	assert.Empty(t, overrides, "a refused Override leaves nothing half-written")

	// A custom entry may repeat, as it may on a Shift.
	require.NoError(t, database.InsertOverride(ctx, db.Override{
		ID: uuid.New().String(), RRule: "DTSTART;VALUE=DATE:20261101\nRRULE:FREQ=WEEKLY;BYDAY=SU",
		Granularity: db.OverrideMatchesDate,
		Pins: []db.OverridePin{
			{ID: uuid.New().String(), RoleID: service, CustomValue: "Youth group"},
			{ID: uuid.New().String(), RoleID: service, CustomValue: "Youth group"},
		},
	}))
}

func TestOverrideReorder(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	var ids []string
	for range 3 {
		o := db.Override{ID: uuid.New().String(), RRule: "DTSTART;VALUE=DATE:20261101\nRRULE:FREQ=WEEKLY;BYDAY=SU", Granularity: db.OverrideMatchesDate}
		require.NoError(t, database.InsertOverride(ctx, o))
		ids = append(ids, o.ID)
	}

	// Swapping two neighbours passes each through the other's position, which
	// the deferred constraint allows within the one transaction.
	reordered := []string{ids[2], ids[0], ids[1]}
	require.NoError(t, database.ReorderOverrides(ctx, reordered))
	overrides, err := database.GetOverrides(ctx)
	require.NoError(t, err)
	assert.Equal(t, reordered, overrideIDs(overrides))

	assert.ErrorIs(t, database.ReorderOverrides(ctx, []string{uuid.New().String()}), db.ErrOverrideNotFound)
	overrides, err = database.GetOverrides(ctx)
	require.NoError(t, err)
	assert.Equal(t, reordered, overrideIDs(overrides), "a refused reorder moves nothing")

	// A new one still goes last after a reorder, and after a delete.
	_, err = database.DeleteOverride(ctx, ids[1])
	require.NoError(t, err)
	last := db.Override{ID: uuid.New().String(), RRule: "DTSTART;VALUE=DATE:20261101\nRRULE:FREQ=DAILY", Granularity: db.OverrideMatchesDate}
	require.NoError(t, database.InsertOverride(ctx, last))
	overrides, err = database.GetOverrides(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{ids[2], ids[0], last.ID}, overrideIDs(overrides))
}

func TestOverridesBelongToTheirOrganisation(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	other := database.ForOrganisation(dbtest.OtherOrganisation)
	otherRole := seedRole(t, other, "Elsewhere", 1)

	o := db.Override{ID: uuid.New().String(), RRule: "DTSTART;VALUE=DATE:20261101\nRRULE:FREQ=WEEKLY;BYDAY=SU", Granularity: db.OverrideMatchesDate}
	require.NoError(t, database.InsertOverride(ctx, o))

	overrides, err := other.GetOverrides(ctx)
	require.NoError(t, err)
	assert.Empty(t, overrides)
	deleted, err := other.DeleteOverride(ctx, o.ID)
	require.NoError(t, err)
	assert.False(t, deleted)

	// Another Organisation's Role cannot be pinned or asked for.
	err = database.InsertOverride(ctx, db.Override{
		ID: uuid.New().String(), RRule: "DTSTART;VALUE=DATE:20261101\nRRULE:FREQ=WEEKLY;BYDAY=SU", Granularity: db.OverrideMatchesDate,
		Pins: []db.OverridePin{{ID: uuid.New().String(), RoleID: otherRole, VolunteerID: "alice"}},
	})
	assert.Error(t, err)
}
