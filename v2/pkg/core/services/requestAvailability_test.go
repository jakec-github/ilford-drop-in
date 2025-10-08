package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// mockAvailabilityRequestStore implements AvailabilityRequestStore for testing
type mockAvailabilityRequestStore struct {
	rotations           []db.Rotation
	availabilityRequests []db.AvailabilityRequest
	getRotationsErr     error
	getRequestsErr      error
}

func (m *mockAvailabilityRequestStore) GetRotations(ctx context.Context) ([]db.Rotation, error) {
	if m.getRotationsErr != nil {
		return nil, m.getRotationsErr
	}
	return m.rotations, nil
}

func (m *mockAvailabilityRequestStore) GetAvailabilityRequests(ctx context.Context) ([]db.AvailabilityRequest, error) {
	if m.getRequestsErr != nil {
		return nil, m.getRequestsErr
	}
	return m.availabilityRequests, nil
}

// mockVolunteerClient implements VolunteerClient for testing
type mockVolunteerClient struct {
	volunteers []model.Volunteer
	err        error
}

func (m *mockVolunteerClient) ListVolunteers(cfg *config.Config) ([]model.Volunteer, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.volunteers, nil
}

func TestRequestAvailability_NoRotations(t *testing.T) {
	mockStore := &mockAvailabilityRequestStore{
		rotations: []db.Rotation{},
	}
	mockClient := &mockVolunteerClient{}
	logger := zap.NewNop()
	ctx := context.Background()
	cfg := &config.Config{}

	result, err := RequestAvailability(ctx, mockStore, mockClient, cfg, logger, "2024-01-15")

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no rotations found")
}

func TestRequestAvailability_NoActiveVolunteers(t *testing.T) {
	mockStore := &mockAvailabilityRequestStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2024-01-01", ShiftCount: 10},
		},
		availabilityRequests: []db.AvailabilityRequest{},
	}
	mockClient := &mockVolunteerClient{
		volunteers: []model.Volunteer{
			{ID: "vol-1", FirstName: "John", Status: "Inactive"},
			{ID: "vol-2", FirstName: "Jane", Status: "On Leave"},
		},
	}
	logger := zap.NewNop()
	ctx := context.Background()
	cfg := &config.Config{}

	result, err := RequestAvailability(ctx, mockStore, mockClient, cfg, logger, "2024-01-15")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "rota-1", result.LatestRota.ID)
	assert.Empty(t, result.VolunteersWithoutRequest, "Should have no active volunteers")
}

func TestRequestAvailability_AllVolunteersHaveRequests(t *testing.T) {
	mockStore := &mockAvailabilityRequestStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2024-01-01", ShiftCount: 10},
		},
		availabilityRequests: []db.AvailabilityRequest{
			{ID: "req-1", RotaID: "rota-1", VolunteerID: "vol-1", FormSent: true},
			{ID: "req-2", RotaID: "rota-1", VolunteerID: "vol-2", FormSent: true},
		},
	}
	mockClient := &mockVolunteerClient{
		volunteers: []model.Volunteer{
			{ID: "vol-1", FirstName: "John", Status: "Active"},
			{ID: "vol-2", FirstName: "Jane", Status: "Active"},
		},
	}
	logger := zap.NewNop()
	ctx := context.Background()
	cfg := &config.Config{}

	result, err := RequestAvailability(ctx, mockStore, mockClient, cfg, logger, "2024-01-15")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "rota-1", result.LatestRota.ID)
	assert.Empty(t, result.VolunteersWithoutRequest, "All volunteers should have requests")
}

func TestRequestAvailability_SomeVolunteersWithoutRequests(t *testing.T) {
	mockStore := &mockAvailabilityRequestStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2024-01-01", ShiftCount: 10},
		},
		availabilityRequests: []db.AvailabilityRequest{
			{ID: "req-1", RotaID: "rota-1", VolunteerID: "vol-1", FormSent: true},
			// vol-2 and vol-3 don't have requests
		},
	}
	mockClient := &mockVolunteerClient{
		volunteers: []model.Volunteer{
			{ID: "vol-1", FirstName: "John", Status: "Active"},
			{ID: "vol-2", FirstName: "Jane", Status: "Active"},
			{ID: "vol-3", FirstName: "Bob", Status: "Active"},
		},
	}
	logger := zap.NewNop()
	ctx := context.Background()
	cfg := &config.Config{}

	result, err := RequestAvailability(ctx, mockStore, mockClient, cfg, logger, "2024-01-15")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "rota-1", result.LatestRota.ID)
	require.Len(t, result.VolunteersWithoutRequest, 2)

	// Check that vol-2 and vol-3 are in the result
	volunteerIDs := make(map[string]bool)
	for _, vol := range result.VolunteersWithoutRequest {
		volunteerIDs[vol.ID] = true
	}
	assert.True(t, volunteerIDs["vol-2"], "vol-2 should be in result")
	assert.True(t, volunteerIDs["vol-3"], "vol-3 should be in result")
	assert.False(t, volunteerIDs["vol-1"], "vol-1 should not be in result")
}

func TestRequestAvailability_MultipleRotas_UsesLatest(t *testing.T) {
	mockStore := &mockAvailabilityRequestStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2024-01-01", ShiftCount: 10},
			{ID: "rota-2", Start: "2024-04-01", ShiftCount: 12}, // Latest
			{ID: "rota-3", Start: "2024-02-01", ShiftCount: 8},
		},
		availabilityRequests: []db.AvailabilityRequest{
			// Request for old rota should be ignored
			{ID: "req-1", RotaID: "rota-1", VolunteerID: "vol-1", FormSent: true},
			// Request for latest rota
			{ID: "req-2", RotaID: "rota-2", VolunteerID: "vol-2", FormSent: true},
		},
	}
	mockClient := &mockVolunteerClient{
		volunteers: []model.Volunteer{
			{ID: "vol-1", FirstName: "John", Status: "Active"},
			{ID: "vol-2", FirstName: "Jane", Status: "Active"},
		},
	}
	logger := zap.NewNop()
	ctx := context.Background()
	cfg := &config.Config{}

	result, err := RequestAvailability(ctx, mockStore, mockClient, cfg, logger, "2024-01-15")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "rota-2", result.LatestRota.ID, "Should use latest rota")

	// vol-1 should need a request (only has request for old rota)
	require.Len(t, result.VolunteersWithoutRequest, 1)
	assert.Equal(t, "vol-1", result.VolunteersWithoutRequest[0].ID)
}

func TestRequestAvailability_CaseInsensitiveActiveStatus(t *testing.T) {
	mockStore := &mockAvailabilityRequestStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2024-01-01", ShiftCount: 10},
		},
		availabilityRequests: []db.AvailabilityRequest{},
	}
	mockClient := &mockVolunteerClient{
		volunteers: []model.Volunteer{
			{ID: "vol-1", FirstName: "John", Status: "Active"},
			{ID: "vol-2", FirstName: "Jane", Status: "active"},  // lowercase
			{ID: "vol-3", FirstName: "Bob", Status: "ACTIVE"},   // uppercase
			{ID: "vol-4", FirstName: "Alice", Status: "Inactive"}, // not active
		},
	}
	logger := zap.NewNop()
	ctx := context.Background()
	cfg := &config.Config{}

	result, err := RequestAvailability(ctx, mockStore, mockClient, cfg, logger, "2024-01-15")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Len(t, result.VolunteersWithoutRequest, 3, "Should include all case variations of Active")

	volunteerIDs := make(map[string]bool)
	for _, vol := range result.VolunteersWithoutRequest {
		volunteerIDs[vol.ID] = true
	}
	assert.True(t, volunteerIDs["vol-1"])
	assert.True(t, volunteerIDs["vol-2"])
	assert.True(t, volunteerIDs["vol-3"])
	assert.False(t, volunteerIDs["vol-4"], "Inactive volunteer should not be included")
}

func TestRequestAvailability_FormSentFalse_StillCountsAsHavingRequest(t *testing.T) {
	// A volunteer with form_sent=false should NOT appear in the result
	// because they already have a request record (even if form not sent yet)
	mockStore := &mockAvailabilityRequestStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2024-01-01", ShiftCount: 10},
		},
		availabilityRequests: []db.AvailabilityRequest{
			{ID: "req-1", RotaID: "rota-1", VolunteerID: "vol-1", FormSent: false},
		},
	}
	mockClient := &mockVolunteerClient{
		volunteers: []model.Volunteer{
			{ID: "vol-1", FirstName: "John", Status: "Active"},
		},
	}
	logger := zap.NewNop()
	ctx := context.Background()
	cfg := &config.Config{}

	result, err := RequestAvailability(ctx, mockStore, mockClient, cfg, logger, "2024-01-15")

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.VolunteersWithoutRequest, "Volunteer with form_sent=false should still count as having request")
}

func TestFilterVolunteersWithoutRequests(t *testing.T) {
	volunteers := []model.Volunteer{
		{ID: "vol-1"},
		{ID: "vol-2"},
		{ID: "vol-3"},
	}
	withRequests := map[string]bool{
		"vol-1": true,
		"vol-3": true,
	}

	without := filterVolunteersWithoutRequests(volunteers, withRequests)

	require.Len(t, without, 1)
	assert.Equal(t, "vol-2", without[0].ID)
}
