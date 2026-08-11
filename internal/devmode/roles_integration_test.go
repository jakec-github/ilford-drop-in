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

// The dev stack has no admin to create Roles and no config key to read them
// from, so the seed is what makes `scripts/dev-stack.sh start` hand over an app
// anybody holds a Role in. The names have to be the ones
// test_data/volunteers.csv spells, or the roster parses to nobody holding
// anything.
func TestSeedRoles(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	seeded, err := devmode.SeedRoles(ctx, database)
	require.NoError(t, err)
	assert.Equal(t, 2, seeded)

	roles, err := database.ListRoles(ctx)
	require.NoError(t, err)
	require.Len(t, roles, 2)

	assert.Equal(t, "Team lead", roles[0].Name)
	assert.Equal(t, 1, roles[0].Priority)
	assert.Equal(t, "violet", roles[0].Colour)

	assert.Equal(t, "Service volunteer", roles[1].Name)
	assert.Equal(t, 2, roles[1].Priority)
}

// The seed runs on every dev-stack start, so it has to be a seed rather than a
// reset: a Role added or edited by hand survives a restart, and nothing is
// inserted twice.
func TestSeedRolesLeavesAnAlreadySeededDatabaseAlone(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	require.NoError(t, database.InsertRole(ctx, db.Role{
		ID: uuid.New().String(), Name: "Food collector", Priority: 1, Colour: "amber",
	}))

	seeded, err := devmode.SeedRoles(ctx, database)
	require.NoError(t, err)
	assert.Equal(t, 0, seeded)

	roles, err := database.ListRoles(ctx)
	require.NoError(t, err)
	require.Len(t, roles, 1)
	assert.Equal(t, "Food collector", roles[0].Name)
}

func intPtr(n int) *int { return &n }
