package db_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
	"github.com/jakechorley/ilford-drop-in/pkg/db/dbtest"
)

// unboundDB is a second, unbound handle on the database dbtest.New made — the
// kind the server's startup holds before any Organisation is chosen.
func unboundDB(t *testing.T, url string) *db.DB {
	t.Helper()
	database, err := db.NewDB(context.Background(), url)
	require.NoError(t, err)
	t.Cleanup(database.Close)
	return database
}

// withPreOrganisationRota puts the database in the state migration 030 leaves
// one that already held a rota: the placeholder Organisation, owning a Role.
// It returns the Role's id.
func withPreOrganisationRota(t *testing.T, url string) string {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	require.NoError(t, err)
	defer conn.Close(ctx)

	roleID := uuid.New().String()
	_, err = conn.Exec(ctx, `INSERT INTO organisation (id) VALUES ('unassigned')`)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, `INSERT INTO role (id, name, priority, colour, organisation_id) VALUES ($1, 'Team lead', 1, 'violet', 'unassigned')`, roleID)
	require.NoError(t, err)
	return roleID
}

func organisationIDs(t *testing.T, url string) []string {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	require.NoError(t, err)
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx, `SELECT id FROM organisation ORDER BY id`)
	require.NoError(t, err)
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	require.NoError(t, err)
	return ids
}

// The first start after the migration hands the rota that was already there to
// the one Organisation config names that the database has not seen, and every
// row follows.
func TestSyncOrganisations_AdoptsTheExistingRota(t *testing.T) {
	_, url := dbtest.New(t)
	roleID := withPreOrganisationRota(t, url)
	database := unboundDB(t, url)
	ctx := context.Background()

	adopted, err := database.SyncOrganisations(ctx, []string{dbtest.Organisation, dbtest.OtherOrganisation, "east"})
	require.NoError(t, err)
	assert.Equal(t, "east", adopted)

	roles, err := database.ForOrganisation("east").ListRoles(ctx)
	require.NoError(t, err)
	require.Len(t, roles, 1)
	assert.Equal(t, roleID, roles[0].ID)
	assert.Equal(t, []string{"east", dbtest.OtherOrganisation, dbtest.Organisation}, organisationIDs(t, url))
}

// With two new Organisations there is no telling whose the old rota was, so
// nothing is guessed — and nothing is added either.
func TestSyncOrganisations_RefusesToGuessWhoseTheRotaWas(t *testing.T) {
	_, url := dbtest.New(t)
	withPreOrganisationRota(t, url)
	database := unboundDB(t, url)

	_, err := database.SyncOrganisations(context.Background(), []string{dbtest.Organisation, "east", "west"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "east")
	assert.Contains(t, err.Error(), "west")
	assert.Equal(t, []string{dbtest.OtherOrganisation, dbtest.Organisation, "unassigned"}, organisationIDs(t, url))
}

// Every start runs it, so a start with nothing new changes nothing; and an
// Organisation dropped from config keeps its rows.
func TestSyncOrganisations_IsIdempotentAndDeletesNothing(t *testing.T) {
	bound, url := dbtest.New(t)
	dbtest.SeedRoles(t, bound)
	database := unboundDB(t, url)
	ctx := context.Background()

	adopted, err := database.SyncOrganisations(ctx, []string{dbtest.OtherOrganisation})
	require.NoError(t, err)
	assert.Empty(t, adopted)
	assert.Equal(t, []string{dbtest.OtherOrganisation, dbtest.Organisation}, organisationIDs(t, url))

	roles, err := bound.ListRoles(ctx)
	require.NoError(t, err)
	assert.Len(t, roles, 2)
}

// One Organisation's rows are invisible to another's queries, and what is
// unique is unique within an Organisation: two drop-ins may each have a Team
// lead.
func TestOrganisationsShareNothing(t *testing.T) {
	east, _ := dbtest.New(t)
	west := east.ForOrganisation(dbtest.OtherOrganisation)
	ctx := context.Background()

	dbtest.SeedRoles(t, east)
	dbtest.SeedRotaDefaults(t, east)

	roles, err := west.ListRoles(ctx)
	require.NoError(t, err)
	assert.Empty(t, roles, "another Organisation's Roles")

	defaults, err := west.GetRotaDefaults(ctx)
	require.NoError(t, err)
	assert.Equal(t, db.RotaDefaults{}, defaults, "another Organisation's Rota Defaults")

	dbtest.SeedRoles(t, west)
	roles, err = west.ListRoles(ctx)
	require.NoError(t, err)
	assert.Len(t, roles, 2)

	// Renaming one Organisation's Role through another's handle touches nothing.
	eastRoles, err := east.ListRoles(ctx)
	require.NoError(t, err)
	renamed := eastRoles[0]
	renamed.Name = "Renamed"
	updated, err := west.UpdateRole(ctx, renamed)
	require.NoError(t, err)
	assert.False(t, updated)
}

// A query on a DB bound to no Organisation would read every drop-in's rows, so
// it is a panic in a test rather than a leak in production.
func TestUnboundDBRefusesScopedQueries(t *testing.T) {
	_, url := dbtest.New(t)
	database := unboundDB(t, url)

	assert.Panics(t, func() { _, _ = database.ListRoles(context.Background()) })
}
