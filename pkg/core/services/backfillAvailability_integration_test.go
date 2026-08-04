package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/clients/formsclient"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
	"github.com/jakechorley/ilford-drop-in/pkg/db/dbtest"
)

// stubFormsResponses stands in for the Forms API, returning what the real client
// would have parsed out of each volunteer's form.
type stubFormsResponses struct {
	byFormID map[string][]formsclient.SubmittedFormResponse
}

func (s *stubFormsResponses) ListFormResponses(formID string, _ string, _ []time.Time) ([]formsclient.SubmittedFormResponse, error) {
	return s.byFormID[formID], nil
}

func formResponse(at time.Time, dates ...string) formsclient.SubmittedFormResponse {
	return formsclient.SubmittedFormResponse{
		SubmittedAt: at,
		Response:    &formsclient.FormResponse{HasResponded: true, AvailableDates: dates},
	}
}

// TestBackfillReproducesTheHistoricalMatrix is the ticket's fidelity criterion,
// run against a real database: after the backfill, ViewHistoricalResponses
// returns from the store exactly what it used to return from Forms.
//
// The four volunteers cover every status the report can show. Ada also answered
// twice — once in time and once after allocation — because the report reads each
// rota as at its own allocated_datetime, and a backfill that collapsed her two
// submissions into one would quietly hand the late answer the authority the
// timely one had.
func TestBackfillReproducesTheHistoricalMatrix(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	allocatedAt := time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC)
	rotaID := uuid.New().String()
	shifts := []db.Shift{
		{ID: uuid.New().String(), RotaID: rotaID, Date: "2025-03-03"},
		{ID: uuid.New().String(), RotaID: rotaID, Date: "2025-03-10"},
		{ID: uuid.New().String(), RotaID: rotaID, Date: "2025-03-17"},
	}
	require.NoError(t, database.InsertRotationAndShifts(ctx, &db.Rotation{ID: rotaID}, shifts))
	// Allocation is what closes a round, and the report only looks at closed
	// ones. Going through the real writer keeps allocated_datetime the shape
	// every read of it expects.
	require.NoError(t, database.InsertAllocationsAndSetAllocated(ctx, nil, rotaID, allocatedAt))

	legacy := []db.AvailabilityRequest{
		{ID: uuid.New().String(), RotaID: rotaID, VolunteerID: "ada", FormID: "form-ada", FormURL: "https://forms/ada", FormSent: true},
		{ID: uuid.New().String(), RotaID: rotaID, VolunteerID: "grace", FormID: "form-grace", FormURL: "https://forms/grace", FormSent: true},
		{ID: uuid.New().String(), RotaID: rotaID, VolunteerID: "alan", FormID: "form-alan", FormURL: "https://forms/alan", FormSent: true},
		// Never emailed: the report showed this as "not asked", and it must stay
		// that way.
		{ID: uuid.New().String(), RotaID: rotaID, VolunteerID: "edsger", FormID: "form-edsger", FormURL: "https://forms/edsger", FormSent: false},
	}
	require.NoError(t, database.InsertAvailabilityRequests(ctx, legacy))

	inTime := allocatedAt.Add(-48 * time.Hour)
	tooLate := allocatedAt.Add(24 * time.Hour)
	forms := &stubFormsResponses{byFormID: map[string][]formsclient.SubmittedFormResponse{
		"form-ada": {
			formResponse(inTime, "Mon Mar 3 2025", "Mon Mar 10 2025"),
			formResponse(tooLate, "Mon Mar 3 2025", "Mon Mar 10 2025", "Mon Mar 17 2025"),
		},
		// Answered, but available for nothing.
		"form-grace": {formResponse(inTime)},
		// Emailed and never answered.
		"form-alan": nil,
		"form-edsger": {
			formResponse(inTime, "Mon Mar 3 2025"),
		},
	}}

	result, err := BackfillAvailabilityFromForms(ctx, database, forms, zap.NewNop(), false)
	require.NoError(t, err)
	assert.Equal(t, 3, result.RequestsMinted, "only the volunteers who were emailed are minted")
	assert.Equal(t, 3, result.GenerationsWritten)
	assert.Empty(t, result.Warnings)

	volunteers := &mockVolunteerClient{volunteers: []model.Volunteer{
		{ID: "ada", DisplayName: "Ada", Status: "Active"},
		{ID: "grace", DisplayName: "Grace", Status: "Active"},
		{ID: "alan", DisplayName: "Alan", Status: "Active"},
		{ID: "edsger", DisplayName: "Edsger", Status: "Active"},
	}}

	report, err := ViewHistoricalResponses(ctx, database, volunteers, testCfg, zap.NewNop(), 1, nil)
	require.NoError(t, err)
	require.Len(t, report.Rotations, 1)

	assert.Equal(t, VolunteerRotaStatus{Status: "available", AvailableCount: 2, ShiftCount: 3},
		report.Matrix["ada"][rotaID], "the answer that was in at allocation is the one that counts")
	assert.Equal(t, VolunteerRotaStatus{Status: "no_availability", ShiftCount: 3},
		report.Matrix["grace"][rotaID])
	assert.Equal(t, VolunteerRotaStatus{Status: "no_response", ShiftCount: 3},
		report.Matrix["alan"][rotaID])

	// A form that was never emailed was never asked. The Forms path dropped such
	// a volunteer from the report altogether — no row, which the CLI renders as
	// "Not asked" — and the backfill must not promote them into one.
	assert.NotContains(t, report.Matrix, "edsger")
	for _, vol := range report.Volunteers {
		assert.NotEqual(t, "edsger", vol.ID)
	}

	// Re-running changes nothing: the same forms produce the same rows, and the
	// report is identical.
	second, err := BackfillAvailabilityFromForms(ctx, database, forms, zap.NewNop(), false)
	require.NoError(t, err)
	assert.Equal(t, 0, second.RequestsMinted)
	assert.Equal(t, 0, second.GenerationsWritten)
	assert.Equal(t, 3, second.GenerationsSkipped)

	rerun, err := ViewHistoricalResponses(ctx, database, volunteers, testCfg, zap.NewNop(), 1, nil)
	require.NoError(t, err)
	assert.Equal(t, report.Matrix, rerun.Matrix)
}
