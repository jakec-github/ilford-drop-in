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
//
// The refusal is a named error, not just any error: it is the one failure the
// settings screen has to explain differently from "something went wrong", and
// telling it apart by inspecting the driver's error is the caller's job to
// avoid.
func TestInsertRoleRejectsDuplicateName(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	require.NoError(t, database.InsertRole(ctx, db.Role{
		ID: uuid.New().String(), Name: "Team lead", Priority: 1, Colour: "violet",
	}))
	err := database.InsertRole(ctx, db.Role{
		ID: uuid.New().String(), Name: "Team lead", Priority: 3, Colour: "teal",
	})
	assert.ErrorIs(t, err, db.ErrDuplicateRoleName)
}

// Every editable field moves at once, because the settings screen edits them
// together: a form saves the Role as it now stands rather than a diff of it.
func TestUpdateRole(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	id := uuid.New().String()
	require.NoError(t, database.InsertRole(ctx, db.Role{
		ID: id, Name: "Team lead", Max: intPtr(1), Priority: 1, Colour: "violet",
	}))

	updated, err := database.UpdateRole(ctx, db.Role{
		ID: id, Name: "Shift lead", Max: intPtr(2), Priority: 4, Colour: "amber",
	})
	require.NoError(t, err)
	assert.True(t, updated)

	roles, err := database.ListRoles(ctx)
	require.NoError(t, err)
	require.Len(t, roles, 1, "an edit changes the Role rather than adding one")
	assert.Equal(t, db.Role{
		ID: id, Name: "Shift lead", Max: intPtr(2), Priority: 4, Colour: "amber",
	}, roles[0])
}

// Uncapping a Role has to be expressible: the ceiling is the field most likely
// to be taken off, and a nil that quietly left the old ceiling in place would
// cap a Role nobody meant to cap.
func TestUpdateRoleClearsTheCeiling(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	id := uuid.New().String()
	require.NoError(t, database.InsertRole(ctx, db.Role{
		ID: id, Name: "Team lead", Max: intPtr(1), Priority: 1, Colour: "violet",
	}))

	_, err := database.UpdateRole(ctx, db.Role{
		ID: id, Name: "Team lead", Priority: 1, Colour: "violet",
	})
	require.NoError(t, err)

	roles, err := database.ListRoles(ctx)
	require.NoError(t, err)
	assert.Nil(t, roles[0].Max)
}

// An id nothing matches is reported as a miss rather than as an error, because
// the caller turns it into a 404 and has nothing else to distinguish it by.
// Roles are never deleted, so this is a wrong id, not a race.
func TestUpdateRoleReportsAnUnknownID(t *testing.T) {
	database, _ := dbtest.New(t)

	updated, err := database.UpdateRole(context.Background(), db.Role{
		ID: uuid.New().String(), Name: "Team lead", Priority: 1, Colour: "violet",
	})
	require.NoError(t, err)
	assert.False(t, updated)
}

// Renaming onto a name another Role already holds is the same ambiguity an
// insert is refused for, and is refused the same way.
func TestUpdateRoleRejectsDuplicateName(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	id := uuid.New().String()
	require.NoError(t, database.InsertRole(ctx, db.Role{
		ID: id, Name: "Team lead", Priority: 1, Colour: "violet",
	}))
	require.NoError(t, database.InsertRole(ctx, db.Role{
		ID: uuid.New().String(), Name: "Food collector", Priority: 2, Colour: "teal",
	}))

	_, err := database.UpdateRole(ctx, db.Role{
		ID: id, Name: "Food collector", Priority: 1, Colour: "violet",
	})
	assert.ErrorIs(t, err, db.ErrDuplicateRoleName)
}

// Saving a Role under the name it already has is an ordinary edit — the screen
// sends the whole Role whether or not the name moved — so the unique index must
// not read the row's own name as a clash.
func TestUpdateRoleKeepingItsOwnName(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	id := uuid.New().String()
	require.NoError(t, database.InsertRole(ctx, db.Role{
		ID: id, Name: "Team lead", Priority: 1, Colour: "violet",
	}))

	updated, err := database.UpdateRole(ctx, db.Role{
		ID: id, Name: "Team lead", Priority: 5, Colour: "rose",
	})
	require.NoError(t, err)
	assert.True(t, updated)
}
