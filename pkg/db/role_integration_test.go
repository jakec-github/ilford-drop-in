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

func intPtr(n int) *int { return &n }

// The Roles come back in priority order — the order their Seats are filled —
// whatever order they were written in, and an uncapped Role's NULL max reads
// back as no ceiling rather than as zero.
func TestListRoles(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	require.NoError(t, database.InsertRole(ctx, db.Role{
		ID: uuid.New().String(), Name: "Service volunteer", Priority: 2, Colour: "teal",
	}))
	require.NoError(t, database.InsertRole(ctx, db.Role{
		ID: uuid.New().String(), Name: "Team lead", Max: intPtr(1), Priority: 1, Colour: "violet",
	}))

	roles, err := database.ListRoles(ctx)
	require.NoError(t, err)
	require.Len(t, roles, 2)

	assert.Equal(t, "Team lead", roles[0].Name)
	require.NotNil(t, roles[0].Max)
	assert.Equal(t, 1, *roles[0].Max)
	assert.Equal(t, "violet", roles[0].Colour)

	assert.Equal(t, "Service volunteer", roles[1].Name)
	assert.Nil(t, roles[1].Max)
}

// A database nobody has put Roles in yet is an ordinary state, not an error:
// the migration seeds nothing (ADR 0006), so the first read of a fresh
// deployment lands here.
func TestListRolesEmpty(t *testing.T) {
	database, _ := dbtest.New(t)

	roles, err := database.ListRoles(context.Background())
	require.NoError(t, err)
	assert.Empty(t, roles)
}

// Names identify a Role to the volunteer roster, which is a Google Sheet naming
// them by string. Two Roles sharing a name would make a held Role ambiguous, so
// the database refuses rather than leaving the ambiguity to be discovered at
// allocation time.
func TestInsertRoleRejectsDuplicateName(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	require.NoError(t, database.InsertRole(ctx, db.Role{
		ID: uuid.New().String(), Name: "Team lead", Priority: 1, Colour: "violet",
	}))
	err := database.InsertRole(ctx, db.Role{
		ID: uuid.New().String(), Name: "Team lead", Priority: 3, Colour: "teal",
	})
	require.Error(t, err)
}
