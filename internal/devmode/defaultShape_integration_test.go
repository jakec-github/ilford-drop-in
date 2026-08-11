package devmode_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/internal/devmode"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
	"github.com/jakechorley/ilford-drop-in/pkg/db/dbtest"
)

// The dev stack has no admin to state a Shape, and a drop-in whose Shifts ask
// for nobody allocates nobody. The seed is what makes `scripts/dev-stack.sh
// start` hand over an app that can allocate a rota.
func TestSeedDefaultShape(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	dbtest.SeedRoles(t, database)

	seeded, err := devmode.SeedDefaultShape(ctx, database)
	require.NoError(t, err)
	assert.True(t, seeded)

	roles, err := database.ListRoles(ctx)
	require.NoError(t, err)
	shape, err := database.GetDefaultShape(ctx)
	require.NoError(t, err)
	assert.Equal(t, []db.DefaultShapeSeat{
		{RoleID: roles[0].ID, Seats: 1},
		{RoleID: roles[1].ID, Seats: 4},
	}, shape)
}

// It guards on this table rather than on the Roles, which is what makes it
// arrive at all: a dev database seeded before this ticket already has Roles, so
// a Shape hung off "are there Roles yet?" would never be written.
func TestSeedDefaultShapeSeedsOverAlreadySeededRoles(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	dbtest.SeedRoles(t, database)

	_, err := devmode.SeedRoles(ctx, database)
	require.NoError(t, err)

	seeded, err := devmode.SeedDefaultShape(ctx, database)
	require.NoError(t, err)
	assert.True(t, seeded)
}

// The seed runs on every dev-stack start, so a Shape edited by hand has to
// survive a restart.
func TestSeedDefaultShapeLeavesAStatedShapeAlone(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	dbtest.SeedRoles(t, database)

	roles, err := database.ListRoles(ctx)
	require.NoError(t, err)
	stated := []db.DefaultShapeSeat{{RoleID: roles[1].ID, Seats: 9}}
	require.NoError(t, database.SaveDefaultShape(ctx, stated))

	seeded, err := devmode.SeedDefaultShape(ctx, database)
	require.NoError(t, err)
	assert.False(t, seeded)

	shape, err := database.GetDefaultShape(ctx)
	require.NoError(t, err)
	assert.Equal(t, stated, shape)
}

// Roles somebody chose for themselves are not Roles this seed has anything to
// say about, so it writes nothing rather than inventing Seats.
func TestSeedDefaultShapeSkipsRolesItDoesNotKnow(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	require.NoError(t, database.InsertRole(ctx, db.Role{
		ID: uuid.New().String(), Name: "Food collector", Priority: 1, Colour: "amber",
	}))

	seeded, err := devmode.SeedDefaultShape(ctx, database)
	require.NoError(t, err)
	assert.False(t, seeded)

	shape, err := database.GetDefaultShape(ctx)
	require.NoError(t, err)
	assert.Empty(t, shape)
}
