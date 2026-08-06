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

// seedRole writes a Role for a Standing Preallocation to reference, since the
// foreign key means one cannot exist without it.
func seedRole(t *testing.T, database *db.DB, name string) string {
	t.Helper()
	id := uuid.New().String()
	require.NoError(t, database.InsertRole(context.Background(), db.Role{
		ID: id, Name: name, Priority: 1, Colour: "teal",
	}))
	return id
}

func TestStandingPreallocationInsertReadDelete(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	roleID := seedRole(t, database, "Service volunteer")

	volunteer := db.StandingPreallocation{
		ID: uuid.New().String(), RRule: "FREQ=MONTHLY;BYDAY=1SU", RoleID: roleID, VolunteerID: "alice",
	}
	custom := db.StandingPreallocation{
		ID: uuid.New().String(), RRule: "FREQ=WEEKLY;BYDAY=SU", RoleID: roleID, CustomValue: "St John's team",
	}
	require.NoError(t, database.InsertStandingPreallocation(ctx, volunteer))
	require.NoError(t, database.InsertStandingPreallocation(ctx, custom))

	standing, err := database.GetStandingPreallocations(ctx)
	require.NoError(t, err)
	require.Len(t, standing, 2)

	byID := make(map[string]db.StandingPreallocation, len(standing))
	for _, s := range standing {
		byID[s.ID] = s
	}
	// A subject that was not named reads back as empty rather than as a null
	// leaking into the struct.
	assert.Equal(t, "alice", byID[volunteer.ID].VolunteerID)
	assert.Empty(t, byID[volunteer.ID].CustomValue)
	assert.Equal(t, "St John's team", byID[custom.ID].CustomValue)
	assert.Empty(t, byID[custom.ID].VolunteerID)
	assert.Equal(t, roleID, byID[custom.ID].RoleID)

	deleted, err := database.DeleteStandingPreallocationByID(ctx, volunteer.ID)
	require.NoError(t, err)
	assert.True(t, deleted)

	deleted, err = database.DeleteStandingPreallocationByID(ctx, volunteer.ID)
	require.NoError(t, err)
	assert.False(t, deleted, "a second delete reports that nothing matched")
}

// The same subject on the same recurrence is one promise, whatever Role either
// naming of it gives them: a person fills at most one Seat on a Shift.
func TestStandingPreallocationRefusesARepeat(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	volunteerRole := seedRole(t, database, "Service volunteer")
	leadRole := seedRole(t, database, "Team lead")

	require.NoError(t, database.InsertStandingPreallocation(ctx, db.StandingPreallocation{
		ID: uuid.New().String(), RRule: "FREQ=WEEKLY;BYDAY=SU", RoleID: volunteerRole, VolunteerID: "alice",
	}))

	err := database.InsertStandingPreallocation(ctx, db.StandingPreallocation{
		ID: uuid.New().String(), RRule: "FREQ=WEEKLY;BYDAY=SU", RoleID: leadRole, VolunteerID: "alice",
	})
	assert.ErrorIs(t, err, db.ErrDuplicateStandingPreallocation)

	err = database.InsertStandingPreallocation(ctx, db.StandingPreallocation{
		ID: uuid.New().String(), RRule: "FREQ=WEEKLY;BYDAY=SU", RoleID: volunteerRole, CustomValue: "Scouts",
	})
	require.NoError(t, err, "a different subject on the same rule is a different promise")

	err = database.InsertStandingPreallocation(ctx, db.StandingPreallocation{
		ID: uuid.New().String(), RRule: "FREQ=MONTHLY;BYDAY=1SU", RoleID: volunteerRole, VolunteerID: "alice",
	})
	require.NoError(t, err, "the same subject on a different rule is a different promise")
}

// Defining a rota writes its rotation, its shifts and its seeded pins in one
// transaction, so a rota can never exist with only some of the pins an admin
// was promised.
func TestInsertDefinedRotaWritesSeededPreallocations(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	rota := &db.Rotation{ID: uuid.New().String()}
	shift := db.Shift{ID: uuid.New().String(), Date: "2026-08-02", RotaID: rota.ID}
	pin := db.Preallocation{
		ID: uuid.New().String(), ShiftID: shift.ID, Role: "Service volunteer", CustomValue: "St John's team",
	}

	require.NoError(t, database.InsertDefinedRota(ctx, rota, []db.Shift{shift}, []db.Preallocation{pin}))

	pins, err := database.GetPreallocationsByShiftIDs(ctx, []string{shift.ID})
	require.NoError(t, err)
	require.Len(t, pins, 1)
	assert.Equal(t, "St John's team", pins[0].CustomValue)
}

// A pin naming a shift that is not in the batch fails the whole definition
// rather than leaving a rota behind with pins missing from it.
func TestInsertDefinedRotaRollsBackOnABadPin(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	rota := &db.Rotation{ID: uuid.New().String()}
	shift := db.Shift{ID: uuid.New().String(), Date: "2026-08-02", RotaID: rota.ID}
	pin := db.Preallocation{
		ID: uuid.New().String(), ShiftID: uuid.New().String(), Role: "Service volunteer", VolunteerID: "alice",
	}

	require.Error(t, database.InsertDefinedRota(ctx, rota, []db.Shift{shift}, []db.Preallocation{pin}))

	rotations, err := database.GetRotations(ctx)
	require.NoError(t, err)
	assert.Empty(t, rotations, "the rotation is rolled back with the pin that failed")
}
