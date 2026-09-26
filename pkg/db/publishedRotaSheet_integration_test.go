package db_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/db/dbtest"
)

// A deployment that has never published has no record, which is an ordinary
// answer: its first publish is a new rota.
func TestGetPublishedRotaSheetUnset(t *testing.T) {
	database, _ := dbtest.New(t)

	layout, stale, err := database.GetPublishedRotaSheet(context.Background())

	require.NoError(t, err)
	assert.Nil(t, layout)
	assert.False(t, stale)
}

// Each publish replaces the record of the one before: Latest holds one rota.
func TestSavePublishedRotaSheetReplacesTheLastOne(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	require.NoError(t, database.SavePublishedRotaSheet(ctx, uuid.New().String(), []byte(`{"rotaId":"first"}`)))
	require.NoError(t, database.SavePublishedRotaSheet(ctx, uuid.New().String(), []byte(`{"rotaId":"second"}`)))

	layout, stale, err := database.GetPublishedRotaSheet(ctx)
	require.NoError(t, err)
	assert.JSONEq(t, `{"rotaId":"second"}`, string(layout))
	assert.False(t, stale)
}

// A failed publish marks the record untrustworthy, and the next publish that
// lands makes it trustworthy again.
func TestPublishedRotaSheetGoesStaleUntilTheNextPublish(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	rotaID := uuid.New().String()
	require.NoError(t, database.SavePublishedRotaSheet(ctx, rotaID, []byte(`{"rotaId":"r"}`)))

	require.NoError(t, database.MarkPublishedRotaSheetStale(ctx))
	layout, stale, err := database.GetPublishedRotaSheet(ctx)
	require.NoError(t, err)
	assert.True(t, stale)
	assert.JSONEq(t, `{"rotaId":"r"}`, string(layout), "the layout is kept: it says which rota Latest was showing")

	require.NoError(t, database.SavePublishedRotaSheet(ctx, rotaID, []byte(`{"rotaId":"r"}`)))
	_, stale, err = database.GetPublishedRotaSheet(ctx)
	require.NoError(t, err)
	assert.False(t, stale)
}

// With no record there is nothing to mark, and marking is not an error.
func TestMarkPublishedRotaSheetStaleWithNoRecord(t *testing.T) {
	database, _ := dbtest.New(t)

	require.NoError(t, database.MarkPublishedRotaSheetStale(context.Background()))

	layout, _, err := database.GetPublishedRotaSheet(context.Background())
	require.NoError(t, err)
	assert.Nil(t, layout)
}
