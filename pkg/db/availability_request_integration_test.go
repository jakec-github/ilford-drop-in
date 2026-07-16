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

// TestInsertAvailabilityRequestsRejectsDuplicateVolunteer pins the concurrency
// role of the availability_request (rota_id, volunteer_id) UNIQUE constraint
// (issue #41, hazard H3): a batch that would give a volunteer a second request
// for the same rota fails wholesale with ErrDuplicateAvailabilityRequest,
// writing none of its rows — so a losing concurrent RequestAvailability run
// records nothing and can be stopped before it emails anyone.
func TestInsertAvailabilityRequestsRejectsDuplicateVolunteer(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	rotaID := uuid.New().String()
	require.NoError(t, database.InsertRotationAndShifts(ctx, &db.Rotation{ID: rotaID}, []db.Shift{
		{ID: uuid.New().String(), Date: "2026-08-02", RotaID: rotaID},
	}))

	first := db.AvailabilityRequest{
		ID: uuid.New().String(), RotaID: rotaID, VolunteerID: "alice",
		FormID: "form-1", FormURL: "https://forms.google.com/form-1",
	}
	require.NoError(t, database.InsertAvailabilityRequests(ctx, []db.AvailabilityRequest{first}))

	// The duplicate for alice must sink the whole batch, including bob's row.
	err := database.InsertAvailabilityRequests(ctx, []db.AvailabilityRequest{
		{ID: uuid.New().String(), RotaID: rotaID, VolunteerID: "alice",
			FormID: "form-2", FormURL: "https://forms.google.com/form-2"},
		{ID: uuid.New().String(), RotaID: rotaID, VolunteerID: "bob",
			FormID: "form-3", FormURL: "https://forms.google.com/form-3"},
	})
	require.ErrorIs(t, err, db.ErrDuplicateAvailabilityRequest)

	requests, err := database.GetAvailabilityRequestsByRotaID(ctx, rotaID)
	require.NoError(t, err)
	require.Len(t, requests, 1, "losing batch must write nothing")
	assert.Equal(t, first.ID, requests[0].ID)
}

// TestAvailabilityRequestUniquePermitsDistinctRotas guards against the
// constraint being over-broad: the same volunteer may hold requests on
// different rotas.
func TestAvailabilityRequestUniquePermitsDistinctRotas(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	rota1 := uuid.New().String()
	rota2 := uuid.New().String()
	require.NoError(t, database.InsertRotationAndShifts(ctx, &db.Rotation{ID: rota1}, []db.Shift{
		{ID: uuid.New().String(), Date: "2026-08-02", RotaID: rota1},
	}))
	require.NoError(t, database.InsertRotationAndShifts(ctx, &db.Rotation{ID: rota2}, []db.Shift{
		{ID: uuid.New().String(), Date: "2026-09-06", RotaID: rota2},
	}))

	require.NoError(t, database.InsertAvailabilityRequests(ctx, []db.AvailabilityRequest{
		{ID: uuid.New().String(), RotaID: rota1, VolunteerID: "alice",
			FormID: "form-1", FormURL: "https://forms.google.com/form-1"},
	}))
	require.NoError(t, database.InsertAvailabilityRequests(ctx, []db.AvailabilityRequest{
		{ID: uuid.New().String(), RotaID: rota2, VolunteerID: "alice",
			FormID: "form-2", FormURL: "https://forms.google.com/form-2"},
	}))
}
