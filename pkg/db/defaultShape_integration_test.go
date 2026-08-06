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

// roleIDsByName reads back what dbtest.SeedRoles inserted, since a Seat is
// written against a Role's id and the seed does not hand them out.
func roleIDsByName(t *testing.T, database *db.DB) map[string]string {
	t.Helper()
	roles, err := database.ListRoles(context.Background())
	require.NoError(t, err)
	ids := make(map[string]string, len(roles))
	for _, role := range roles {
		ids[role.Name] = role.ID
	}
	return ids
}

// A database nobody has configured yet asks for nothing, which is an ordinary
// answer rather than an error: no migration seeds a Shape (ADR 0006).
func TestGetDefaultShapeUnset(t *testing.T) {
	database, _ := dbtest.New(t)

	shape, err := database.GetDefaultShape(context.Background())
	require.NoError(t, err)
	assert.Empty(t, shape)
}

// The Seats come back in the order they are filled — by the Role's priority —
// so nothing downstream has to re-sort a Shape it reads.
func TestSaveDefaultShape(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	dbtest.SeedRoles(t, database)
	ids := roleIDsByName(t, database)

	// Deliberately stated lowest-priority first: the read orders it, the write
	// does not have to.
	require.NoError(t, database.SaveDefaultShape(ctx, []db.DefaultShapeSeat{
		{RoleID: ids["Service volunteer"], Seats: 4},
		{RoleID: ids["Team lead"], Seats: 1},
	}))

	shape, err := database.GetDefaultShape(ctx)
	require.NoError(t, err)
	assert.Equal(t, []db.DefaultShapeSeat{
		{RoleID: ids["Team lead"], Seats: 1},
		{RoleID: ids["Service volunteer"], Seats: 4},
	}, shape)
}

// A save states the whole Shape, so a Role left out of the second save is a
// Role the Shape no longer asks for.
func TestSaveDefaultShapeReplacesWhatWasThere(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	dbtest.SeedRoles(t, database)
	ids := roleIDsByName(t, database)

	require.NoError(t, database.SaveDefaultShape(ctx, []db.DefaultShapeSeat{
		{RoleID: ids["Team lead"], Seats: 1},
		{RoleID: ids["Service volunteer"], Seats: 4},
	}))
	require.NoError(t, database.SaveDefaultShape(ctx, []db.DefaultShapeSeat{
		{RoleID: ids["Service volunteer"], Seats: 6},
	}))

	shape, err := database.GetDefaultShape(ctx)
	require.NoError(t, err)
	assert.Equal(t, []db.DefaultShapeSeat{
		{RoleID: ids["Service volunteer"], Seats: 6},
	}, shape)
}

// Saving nothing is how a Shape is emptied, and it leaves the table as a fresh
// deployment's — not a row of zeroes.
func TestSaveDefaultShapeEmpties(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	dbtest.SeedRoles(t, database)
	ids := roleIDsByName(t, database)

	require.NoError(t, database.SaveDefaultShape(ctx, []db.DefaultShapeSeat{
		{RoleID: ids["Team lead"], Seats: 1},
	}))
	require.NoError(t, database.SaveDefaultShape(ctx, nil))

	shape, err := database.GetDefaultShape(ctx)
	require.NoError(t, err)
	assert.Empty(t, shape)
}

// The foreign key is the whole reason this is rows rather than JSON: a Shape
// can never name a Role that does not exist.
func TestSaveDefaultShapeRefusesUnknownRole(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	dbtest.SeedRoles(t, database)
	ids := roleIDsByName(t, database)

	err := database.SaveDefaultShape(ctx, []db.DefaultShapeSeat{
		{RoleID: ids["Team lead"], Seats: 1},
		{RoleID: uuid.New().String(), Seats: 2},
	})
	require.Error(t, err)

	// The write is one transaction, so the Seat that would have been fine is
	// not left behind either.
	shape, err := database.GetDefaultShape(ctx)
	require.NoError(t, err)
	assert.Empty(t, shape)
}

// Zero Seats of a Role is not a smaller Shape, it is a Role the Shape does not
// name. The service says so in words; this is what stops anything else writing
// one.
func TestSaveDefaultShapeRefusesEmptySeat(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	dbtest.SeedRoles(t, database)
	ids := roleIDsByName(t, database)

	err := database.SaveDefaultShape(ctx, []db.DefaultShapeSeat{
		{RoleID: ids["Team lead"], Seats: 0},
	})
	require.Error(t, err)
}
