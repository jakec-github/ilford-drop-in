package services

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/clients/formsclient"
	"github.com/jakechorley/ilford-drop-in/pkg/clients/sheetsclient"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// mockHistoricalStore implements ViewHistoricalResponsesStore
type mockHistoricalStore struct {
	rotations            []db.Rotation
	availabilityRequests []db.AvailabilityRequest
	getRotationsErr      error
	getAvailabilityErr   error
}

func (m *mockHistoricalStore) GetRotations(ctx context.Context) ([]db.Rotation, error) {
	if m.getRotationsErr != nil {
		return nil, m.getRotationsErr
	}
	return m.rotations, nil
}

func (m *mockHistoricalStore) GetAvailabilityRequests(ctx context.Context) ([]db.AvailabilityRequest, error) {
	if m.getAvailabilityErr != nil {
		return nil, m.getAvailabilityErr
	}
	return m.availabilityRequests, nil
}

// mockHistoricalFormsClient implements HistoricalFormsClient
type mockHistoricalFormsClient struct {
	// responses keyed by "formID" -> FormResponse
	responses map[string]*formsclient.FormResponse
	err       error
}

func (m *mockHistoricalFormsClient) GetFormResponseBefore(formID string, volunteerName string, shiftDates []time.Time, before time.Time) (*formsclient.FormResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	if resp, ok := m.responses[formID]; ok {
		return resp, nil
	}
	return &formsclient.FormResponse{
		VolunteerName: volunteerName,
		HasResponded:  false,
	}, nil
}

// mockHistoricalVolClient implements VolunteerClient
type mockHistoricalVolClient struct {
	volunteers []model.Volunteer
	listErr    error
}

func (m *mockHistoricalVolClient) ListVolunteers(cfg *config.Config) ([]model.Volunteer, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	sheetsclient.ComputeDisplayNames(m.volunteers)
	return m.volunteers, nil
}

func TestViewHistoricalResponses_BasicMatrix(t *testing.T) {
	cutoff := time.Date(2025, 2, 1, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)

	store := &mockHistoricalStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2025-01-05", ShiftCount: 6, AllocatedDatetime: cutoff},
			{ID: "rota-2", Start: "2025-03-02", ShiftCount: 8, AllocatedDatetime: cutoff},
		},
		availabilityRequests: []db.AvailabilityRequest{
			{RotaID: "rota-1", VolunteerID: "alice", FormID: "form-a1", FormSent: true},
			{RotaID: "rota-1", VolunteerID: "bob", FormID: "form-b1", FormSent: true},
			{RotaID: "rota-2", VolunteerID: "alice", FormID: "form-a2", FormSent: true},
			// Bob has no form for rota-2
		},
	}

	volClient := &mockHistoricalVolClient{
		volunteers: []model.Volunteer{
			{ID: "alice", FirstName: "Alice", LastName: "Smith", Status: "Active"},
			{ID: "bob", FirstName: "Bob", LastName: "Jones", Status: "Active"},
		},
	}

	formsClient := &mockHistoricalFormsClient{
		responses: map[string]*formsclient.FormResponse{
			"form-a1": {HasResponded: true, AvailableDates: []string{"d1", "d2", "d3", "d4"}},
			"form-b1": {HasResponded: true, AvailableDates: []string{}}, // no availability
			"form-a2": {HasResponded: false},                            // no response
		},
	}

	ctx := context.Background()
	logger := zap.NewNop()
	cfg := &config.Config{}

	result, err := ViewHistoricalResponses(ctx, store, volClient, formsClient, cfg, logger, 5, nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Should include both rotations
	assert.Len(t, result.Rotations, 2)
	assert.Equal(t, "rota-1", result.Rotations[0].ID)
	assert.Equal(t, "rota-2", result.Rotations[1].ID)

	// Should include both volunteers
	assert.Len(t, result.Volunteers, 2)

	// Alice rota-1: available with 4 dates
	aliceR1 := result.Matrix["alice"]["rota-1"]
	assert.Equal(t, "available", aliceR1.Status)
	assert.Equal(t, 4, aliceR1.AvailableCount)
	assert.Equal(t, 6, aliceR1.ShiftCount)

	// Bob rota-1: no_availability (responded, 0 available dates)
	bobR1 := result.Matrix["bob"]["rota-1"]
	assert.Equal(t, "no_availability", bobR1.Status)
	assert.Equal(t, 6, bobR1.ShiftCount)

	// Alice rota-2: no_response
	aliceR2 := result.Matrix["alice"]["rota-2"]
	assert.Equal(t, "no_response", aliceR2.Status)

	// Bob rota-2: no_form (no availability request for this rota)
	bobR2 := result.Matrix["bob"]["rota-2"]
	assert.Equal(t, "no_form", bobR2.Status)
}

func TestViewHistoricalResponses_CountLimitsRotations(t *testing.T) {
	cutoff := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)

	store := &mockHistoricalStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2025-01-05", ShiftCount: 4, AllocatedDatetime: cutoff},
			{ID: "rota-2", Start: "2025-03-02", ShiftCount: 4, AllocatedDatetime: cutoff},
			{ID: "rota-3", Start: "2025-05-04", ShiftCount: 4, AllocatedDatetime: cutoff},
		},
		availabilityRequests: []db.AvailabilityRequest{
			{RotaID: "rota-1", VolunteerID: "alice", FormID: "f1", FormSent: true},
			{RotaID: "rota-2", VolunteerID: "alice", FormID: "f2", FormSent: true},
			{RotaID: "rota-3", VolunteerID: "alice", FormID: "f3", FormSent: true},
		},
	}

	volClient := &mockHistoricalVolClient{
		volunteers: []model.Volunteer{
			{ID: "alice", FirstName: "Alice", LastName: "Smith", Status: "Active"},
		},
	}

	formsClient := &mockHistoricalFormsClient{
		responses: map[string]*formsclient.FormResponse{
			"f1": {HasResponded: true, AvailableDates: []string{"d1"}},
			"f2": {HasResponded: true, AvailableDates: []string{"d1"}},
			"f3": {HasResponded: true, AvailableDates: []string{"d1"}},
		},
	}

	ctx := context.Background()
	logger := zap.NewNop()
	cfg := &config.Config{}

	// Request only last 2
	result, err := ViewHistoricalResponses(ctx, store, volClient, formsClient, cfg, logger, 2, nil)
	require.NoError(t, err)

	assert.Len(t, result.Rotations, 2)
	assert.Equal(t, "rota-2", result.Rotations[0].ID)
	assert.Equal(t, "rota-3", result.Rotations[1].ID)
}

func TestViewHistoricalResponses_FiltersUnallocatedRotations(t *testing.T) {
	cutoff := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)

	store := &mockHistoricalStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2025-01-05", ShiftCount: 4, AllocatedDatetime: cutoff},
			{ID: "rota-2", Start: "2025-03-02", ShiftCount: 4, AllocatedDatetime: ""}, // not allocated
			{ID: "rota-3", Start: "2025-05-04", ShiftCount: 4, AllocatedDatetime: cutoff},
		},
		availabilityRequests: []db.AvailabilityRequest{
			{RotaID: "rota-1", VolunteerID: "alice", FormID: "f1", FormSent: true},
			{RotaID: "rota-3", VolunteerID: "alice", FormID: "f3", FormSent: true},
		},
	}

	volClient := &mockHistoricalVolClient{
		volunteers: []model.Volunteer{
			{ID: "alice", FirstName: "Alice", LastName: "Smith", Status: "Active"},
		},
	}

	formsClient := &mockHistoricalFormsClient{
		responses: map[string]*formsclient.FormResponse{
			"f1": {HasResponded: true, AvailableDates: []string{"d1"}},
			"f3": {HasResponded: true, AvailableDates: []string{"d1"}},
		},
	}

	ctx := context.Background()
	logger := zap.NewNop()
	cfg := &config.Config{}

	result, err := ViewHistoricalResponses(ctx, store, volClient, formsClient, cfg, logger, 10, nil)
	require.NoError(t, err)

	// Only allocated rotations (rota-1, rota-3)
	assert.Len(t, result.Rotations, 2)
	assert.Equal(t, "rota-1", result.Rotations[0].ID)
	assert.Equal(t, "rota-3", result.Rotations[1].ID)
}

func TestViewHistoricalResponses_VolunteerIDFilter(t *testing.T) {
	cutoff := time.Date(2025, 2, 1, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)

	store := &mockHistoricalStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2025-01-05", ShiftCount: 4, AllocatedDatetime: cutoff},
		},
		availabilityRequests: []db.AvailabilityRequest{
			{RotaID: "rota-1", VolunteerID: "alice", FormID: "form-a", FormSent: true},
			{RotaID: "rota-1", VolunteerID: "bob", FormID: "form-b", FormSent: true},
			{RotaID: "rota-1", VolunteerID: "carol", FormID: "form-c", FormSent: true},
		},
	}

	volClient := &mockHistoricalVolClient{
		volunteers: []model.Volunteer{
			{ID: "alice", FirstName: "Alice", LastName: "Smith", Status: "Active"},
			{ID: "bob", FirstName: "Bob", LastName: "Jones", Status: "Active"},
			{ID: "carol", FirstName: "Carol", LastName: "Davis", Status: "Active"},
		},
	}

	formsClient := &mockHistoricalFormsClient{
		responses: map[string]*formsclient.FormResponse{
			"form-a": {HasResponded: true, AvailableDates: []string{"d1"}},
			"form-b": {HasResponded: true, AvailableDates: []string{"d1"}},
			"form-c": {HasResponded: true, AvailableDates: []string{"d1"}},
		},
	}

	ctx := context.Background()
	logger := zap.NewNop()
	cfg := &config.Config{}

	// Filter to only alice and carol
	result, err := ViewHistoricalResponses(ctx, store, volClient, formsClient, cfg, logger, 5, []string{"alice", "carol"})
	require.NoError(t, err)

	assert.Len(t, result.Volunteers, 2)

	volIDs := make(map[string]bool)
	for _, vol := range result.Volunteers {
		volIDs[vol.ID] = true
	}
	assert.True(t, volIDs["alice"])
	assert.True(t, volIDs["carol"])
	assert.False(t, volIDs["bob"])

	// Matrix should only have alice and carol
	_, aliceExists := result.Matrix["alice"]
	_, bobExists := result.Matrix["bob"]
	_, carolExists := result.Matrix["carol"]
	assert.True(t, aliceExists)
	assert.False(t, bobExists)
	assert.True(t, carolExists)
}

func TestViewHistoricalResponses_FormErrorHandledGracefully(t *testing.T) {
	cutoff := time.Date(2025, 2, 1, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)

	store := &mockHistoricalStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2025-01-05", ShiftCount: 4, AllocatedDatetime: cutoff},
		},
		availabilityRequests: []db.AvailabilityRequest{
			{RotaID: "rota-1", VolunteerID: "alice", FormID: "form-deleted", FormSent: true},
		},
	}

	volClient := &mockHistoricalVolClient{
		volunteers: []model.Volunteer{
			{ID: "alice", FirstName: "Alice", LastName: "Smith", Status: "Active"},
		},
	}

	// All form requests return an error (simulating deleted form)
	formsClient := &mockHistoricalFormsClient{
		err: fmt.Errorf("googleapi: Error 404: Requested entity was not found"),
	}

	ctx := context.Background()
	logger := zap.NewNop()
	cfg := &config.Config{}

	result, err := ViewHistoricalResponses(ctx, store, volClient, formsClient, cfg, logger, 5, nil)
	require.NoError(t, err)

	// Should have form_error status, not a fatal error
	aliceR1 := result.Matrix["alice"]["rota-1"]
	assert.Equal(t, "form_error", aliceR1.Status)
	assert.Equal(t, 4, aliceR1.ShiftCount)
}

func TestViewHistoricalResponses_NoAllocatedRotations(t *testing.T) {
	store := &mockHistoricalStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2025-01-05", ShiftCount: 4, AllocatedDatetime: ""},
		},
	}

	volClient := &mockHistoricalVolClient{
		volunteers: []model.Volunteer{
			{ID: "alice", FirstName: "Alice", LastName: "Smith", Status: "Active"},
		},
	}

	formsClient := &mockHistoricalFormsClient{}

	ctx := context.Background()
	logger := zap.NewNop()
	cfg := &config.Config{}

	_, err := ViewHistoricalResponses(ctx, store, volClient, formsClient, cfg, logger, 5, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no allocated rotations found")
}

func TestViewHistoricalResponses_VolunteerAcrossRotations(t *testing.T) {
	// A volunteer with forms in rota-1 but not rota-2 should show "no_form" for rota-2
	cutoff := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)

	store := &mockHistoricalStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2025-01-05", ShiftCount: 4, AllocatedDatetime: cutoff},
			{ID: "rota-2", Start: "2025-03-02", ShiftCount: 6, AllocatedDatetime: cutoff},
		},
		availabilityRequests: []db.AvailabilityRequest{
			// Alice has forms for both
			{RotaID: "rota-1", VolunteerID: "alice", FormID: "f-a1", FormSent: true},
			{RotaID: "rota-2", VolunteerID: "alice", FormID: "f-a2", FormSent: true},
			// Bob only has form for rota-1
			{RotaID: "rota-1", VolunteerID: "bob", FormID: "f-b1", FormSent: true},
			// Carol only has form for rota-2
			{RotaID: "rota-2", VolunteerID: "carol", FormID: "f-c2", FormSent: true},
		},
	}

	volClient := &mockHistoricalVolClient{
		volunteers: []model.Volunteer{
			{ID: "alice", FirstName: "Alice", LastName: "Smith", Status: "Active"},
			{ID: "bob", FirstName: "Bob", LastName: "Jones", Status: "Active"},
			{ID: "carol", FirstName: "Carol", LastName: "Davis", Status: "Active"},
		},
	}

	formsClient := &mockHistoricalFormsClient{
		responses: map[string]*formsclient.FormResponse{
			"f-a1": {HasResponded: true, AvailableDates: []string{"d1", "d2", "d3", "d4"}},
			"f-a2": {HasResponded: true, AvailableDates: []string{"d1", "d2", "d3", "d4", "d5", "d6"}},
			"f-b1": {HasResponded: true, AvailableDates: []string{"d1", "d2"}},
			"f-c2": {HasResponded: true, AvailableDates: []string{"d1"}},
		},
	}

	ctx := context.Background()
	logger := zap.NewNop()
	cfg := &config.Config{}

	result, err := ViewHistoricalResponses(ctx, store, volClient, formsClient, cfg, logger, 5, nil)
	require.NoError(t, err)

	// All three volunteers should appear
	assert.Len(t, result.Volunteers, 3)

	// Bob rota-2: no_form
	assert.Equal(t, "no_form", result.Matrix["bob"]["rota-2"].Status)

	// Carol rota-1: no_form
	assert.Equal(t, "no_form", result.Matrix["carol"]["rota-1"].Status)

	// Alice: available in both
	assert.Equal(t, "available", result.Matrix["alice"]["rota-1"].Status)
	assert.Equal(t, 4, result.Matrix["alice"]["rota-1"].AvailableCount)
	assert.Equal(t, "available", result.Matrix["alice"]["rota-2"].Status)
	assert.Equal(t, 6, result.Matrix["alice"]["rota-2"].AvailableCount)

	// Carol rota-2: available with 1
	assert.Equal(t, "available", result.Matrix["carol"]["rota-2"].Status)
	assert.Equal(t, 1, result.Matrix["carol"]["rota-2"].AvailableCount)
}

func TestViewHistoricalResponses_EmptyVolunteerIDFilterShowsAll(t *testing.T) {
	cutoff := time.Date(2025, 2, 1, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)

	store := &mockHistoricalStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2025-01-05", ShiftCount: 4, AllocatedDatetime: cutoff},
		},
		availabilityRequests: []db.AvailabilityRequest{
			{RotaID: "rota-1", VolunteerID: "alice", FormID: "form-a", FormSent: true},
			{RotaID: "rota-1", VolunteerID: "bob", FormID: "form-b", FormSent: true},
		},
	}

	volClient := &mockHistoricalVolClient{
		volunteers: []model.Volunteer{
			{ID: "alice", FirstName: "Alice", LastName: "Smith", Status: "Active"},
			{ID: "bob", FirstName: "Bob", LastName: "Jones", Status: "Active"},
		},
	}

	formsClient := &mockHistoricalFormsClient{
		responses: map[string]*formsclient.FormResponse{
			"form-a": {HasResponded: true, AvailableDates: []string{"d1"}},
			"form-b": {HasResponded: true, AvailableDates: []string{"d1"}},
		},
	}

	ctx := context.Background()
	logger := zap.NewNop()
	cfg := &config.Config{}

	// Empty slice should show all volunteers
	result, err := ViewHistoricalResponses(ctx, store, volClient, formsClient, cfg, logger, 5, []string{})
	require.NoError(t, err)
	assert.Len(t, result.Volunteers, 2)

	// nil should also show all
	result, err = ViewHistoricalResponses(ctx, store, volClient, formsClient, cfg, logger, 5, nil)
	require.NoError(t, err)
	assert.Len(t, result.Volunteers, 2)
}
