package services

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/clients/sheetsclient"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// mockHistoricalStore implements ViewHistoricalResponsesStore
type mockHistoricalStore struct {
	testRoleStore

	rotations          []db.Rotation
	requests           []db.AvailabilityRequest
	generations        map[string]db.AvailabilityGeneration // keyed by request id
	getRotationsErr    error
	getAvailabilityErr error
}

func (m *mockHistoricalStore) GetRotations(ctx context.Context) ([]db.Rotation, error) {
	if m.getRotationsErr != nil {
		return nil, m.getRotationsErr
	}
	return m.rotations, nil
}

func (m *mockHistoricalStore) GetAvailabilityRequestsByRotaID(ctx context.Context, rotaID string) ([]db.AvailabilityRequest, error) {
	if m.getAvailabilityErr != nil {
		return nil, m.getAvailabilityErr
	}
	var filtered []db.AvailabilityRequest
	for _, r := range m.requests {
		if r.RotaID == rotaID {
			filtered = append(filtered, r)
		}
	}
	return filtered, nil
}

func (m *mockHistoricalStore) GetLatestAvailability(ctx context.Context, requestIDs []string, cutoff *time.Time) (map[string]db.AvailabilityGeneration, error) {
	latest := make(map[string]db.AvailabilityGeneration)
	for _, id := range requestIDs {
		generation, ok := m.generations[id]
		if !ok {
			continue
		}
		if cutoff != nil && generation.SubmittedAt.After(*cutoff) {
			continue
		}
		latest[id] = generation
	}
	return latest, nil
}

// answersOn builds a generation's positives from shift ids, submitted at the
// given moment.
func answersOn(submittedAt time.Time, shiftIDs ...string) db.AvailabilityGeneration {
	answers := make([]db.ShiftAnswer, 0, len(shiftIDs))
	for _, id := range shiftIDs {
		answers = append(answers, db.ShiftAnswer{ShiftID: id, Answer: db.AnswerYes})
	}
	return db.AvailabilityGeneration{SubmittedAt: submittedAt, Answers: answers}
}

// mockHistoricalVolClient implements VolunteerClient
type mockHistoricalVolClient struct {
	volunteers []model.Volunteer
	listErr    error
}

func (m *mockHistoricalVolClient) ListVolunteers(cfg *config.Config, roles model.Roles) ([]model.Volunteer, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	sheetsclient.ComputeDisplayNames(m.volunteers)
	return m.volunteers, nil
}

// historicalCutoff is the moment every rota in these tests was allocated, and
// therefore the moment answers stop counting.
var historicalCutoff = time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)

func allocatedRota(id, start string, shiftCount int) db.Rotation {
	return db.Rotation{
		ID: id, Start: start, ShiftCount: shiftCount,
		AllocatedDatetime: historicalCutoff.Format(time.RFC3339),
	}
}

func historicalRequest(rotaID, volunteerID string) db.AvailabilityRequest {
	return db.AvailabilityRequest{
		ID: rotaID + "/" + volunteerID, RotaID: rotaID, VolunteerID: volunteerID,
		Token: "tok-" + rotaID + "-" + volunteerID,
	}
}

// The four statuses, one of each. They are all read off the stored round now:
// what used to be a form fetch per volunteer per rota is two queries per rota.
func TestViewHistoricalResponses_BasicMatrix(t *testing.T) {
	answered := historicalCutoff.Add(-24 * time.Hour)

	store := &mockHistoricalStore{
		rotations: []db.Rotation{
			allocatedRota("rota-1", "2025-01-05", 6),
			allocatedRota("rota-2", "2025-03-02", 8),
		},
		requests: []db.AvailabilityRequest{
			historicalRequest("rota-1", "alice"),
			historicalRequest("rota-1", "bob"),
			historicalRequest("rota-2", "alice"),
			// Bob was not asked about rota-2.
		},
		generations: map[string]db.AvailabilityGeneration{
			"rota-1/alice": answersOn(answered, "d1", "d2", "d3", "d4"),
			"rota-1/bob":   answersOn(answered), // answered, available for nothing
			// Alice never answered for rota-2.
		},
	}

	volClient := &mockHistoricalVolClient{
		volunteers: []model.Volunteer{
			{ID: "alice", FirstName: "Alice", LastName: "Smith", Status: "Active"},
			{ID: "bob", FirstName: "Bob", LastName: "Jones", Status: "Active"},
		},
	}

	result, err := ViewHistoricalResponses(
		context.Background(), store, volClient, &config.Config{}, zap.NewNop(), 5, nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Len(t, result.Rotations, 2)
	assert.Equal(t, "rota-1", result.Rotations[0].ID)
	assert.Equal(t, "rota-2", result.Rotations[1].ID)
	assert.Len(t, result.Volunteers, 2)

	aliceR1 := result.Matrix["alice"]["rota-1"]
	assert.Equal(t, "available", aliceR1.Status)
	assert.Equal(t, 4, aliceR1.AvailableCount)
	assert.Equal(t, 6, aliceR1.ShiftCount)

	bobR1 := result.Matrix["bob"]["rota-1"]
	assert.Equal(t, "no_availability", bobR1.Status)
	assert.Equal(t, 6, bobR1.ShiftCount)

	assert.Equal(t, "no_response", result.Matrix["alice"]["rota-2"].Status)
	assert.Equal(t, "no_form", result.Matrix["bob"]["rota-2"].Status)
}

// The cut-off is the rota's own allocation. An answer changed after the rota
// went out never informed it, so the report must not credit it — this is the
// whole reason the read is bounded rather than "latest wins".
func TestViewHistoricalResponses_IgnoresAnswersAfterAllocation(t *testing.T) {
	store := &mockHistoricalStore{
		rotations: []db.Rotation{allocatedRota("rota-1", "2025-01-05", 4)},
		requests:  []db.AvailabilityRequest{historicalRequest("rota-1", "alice")},
		generations: map[string]db.AvailabilityGeneration{
			"rota-1/alice": answersOn(historicalCutoff.Add(time.Hour), "d1", "d2"),
		},
	}

	volClient := &mockHistoricalVolClient{
		volunteers: []model.Volunteer{
			{ID: "alice", FirstName: "Alice", LastName: "Smith", Status: "Active"},
		},
	}

	result, err := ViewHistoricalResponses(
		context.Background(), store, volClient, &config.Config{}, zap.NewNop(), 5, nil)
	require.NoError(t, err)

	assert.Equal(t, "no_response", result.Matrix["alice"]["rota-1"].Status,
		"an answer given after allocation did not count towards that rota")
}

func TestViewHistoricalResponses_CountLimitsRotations(t *testing.T) {
	answered := historicalCutoff.Add(-24 * time.Hour)

	store := &mockHistoricalStore{
		rotations: []db.Rotation{
			allocatedRota("rota-1", "2025-01-05", 4),
			allocatedRota("rota-2", "2025-03-02", 4),
			allocatedRota("rota-3", "2025-05-04", 4),
		},
		requests: []db.AvailabilityRequest{
			historicalRequest("rota-1", "alice"),
			historicalRequest("rota-2", "alice"),
			historicalRequest("rota-3", "alice"),
		},
		generations: map[string]db.AvailabilityGeneration{
			"rota-1/alice": answersOn(answered, "d1"),
			"rota-2/alice": answersOn(answered, "d1"),
			"rota-3/alice": answersOn(answered, "d1"),
		},
	}

	volClient := &mockHistoricalVolClient{
		volunteers: []model.Volunteer{
			{ID: "alice", FirstName: "Alice", LastName: "Smith", Status: "Active"},
		},
	}

	result, err := ViewHistoricalResponses(
		context.Background(), store, volClient, &config.Config{}, zap.NewNop(), 2, nil)
	require.NoError(t, err)

	assert.Len(t, result.Rotations, 2)
	assert.Equal(t, "rota-2", result.Rotations[0].ID)
	assert.Equal(t, "rota-3", result.Rotations[1].ID)
}

func TestViewHistoricalResponses_FiltersUnallocatedRotations(t *testing.T) {
	answered := historicalCutoff.Add(-24 * time.Hour)

	store := &mockHistoricalStore{
		rotations: []db.Rotation{
			allocatedRota("rota-1", "2025-01-05", 4),
			{ID: "rota-2", Start: "2025-03-02", ShiftCount: 4}, // not allocated
			allocatedRota("rota-3", "2025-05-04", 4),
		},
		requests: []db.AvailabilityRequest{
			historicalRequest("rota-1", "alice"),
			historicalRequest("rota-3", "alice"),
		},
		generations: map[string]db.AvailabilityGeneration{
			"rota-1/alice": answersOn(answered, "d1"),
			"rota-3/alice": answersOn(answered, "d1"),
		},
	}

	volClient := &mockHistoricalVolClient{
		volunteers: []model.Volunteer{
			{ID: "alice", FirstName: "Alice", LastName: "Smith", Status: "Active"},
		},
	}

	result, err := ViewHistoricalResponses(
		context.Background(), store, volClient, &config.Config{}, zap.NewNop(), 10, nil)
	require.NoError(t, err)

	assert.Len(t, result.Rotations, 2)
	assert.Equal(t, "rota-1", result.Rotations[0].ID)
	assert.Equal(t, "rota-3", result.Rotations[1].ID)
}

func TestViewHistoricalResponses_VolunteerIDFilter(t *testing.T) {
	answered := historicalCutoff.Add(-24 * time.Hour)

	store := &mockHistoricalStore{
		rotations: []db.Rotation{allocatedRota("rota-1", "2025-01-05", 4)},
		requests: []db.AvailabilityRequest{
			historicalRequest("rota-1", "alice"),
			historicalRequest("rota-1", "bob"),
			historicalRequest("rota-1", "carol"),
		},
		generations: map[string]db.AvailabilityGeneration{
			"rota-1/alice": answersOn(answered, "d1"),
			"rota-1/bob":   answersOn(answered, "d1"),
			"rota-1/carol": answersOn(answered, "d1"),
		},
	}

	volClient := &mockHistoricalVolClient{
		volunteers: []model.Volunteer{
			{ID: "alice", FirstName: "Alice", LastName: "Smith", Status: "Active"},
			{ID: "bob", FirstName: "Bob", LastName: "Jones", Status: "Active"},
			{ID: "carol", FirstName: "Carol", LastName: "Davis", Status: "Active"},
		},
	}

	result, err := ViewHistoricalResponses(
		context.Background(), store, volClient, &config.Config{}, zap.NewNop(), 5, []string{"alice", "carol"})
	require.NoError(t, err)

	require.Len(t, result.Volunteers, 2)
	volIDs := make(map[string]bool)
	for _, vol := range result.Volunteers {
		volIDs[vol.ID] = true
	}
	assert.True(t, volIDs["alice"])
	assert.True(t, volIDs["carol"])
	assert.False(t, volIDs["bob"])

	_, bobExists := result.Matrix["bob"]
	assert.False(t, bobExists)
}

func TestViewHistoricalResponses_NoAllocatedRotations(t *testing.T) {
	store := &mockHistoricalStore{
		rotations: []db.Rotation{{ID: "rota-1", Start: "2025-01-05", ShiftCount: 4}},
	}

	volClient := &mockHistoricalVolClient{
		volunteers: []model.Volunteer{
			{ID: "alice", FirstName: "Alice", LastName: "Smith", Status: "Active"},
		},
	}

	_, err := ViewHistoricalResponses(
		context.Background(), store, volClient, &config.Config{}, zap.NewNop(), 5, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no allocated rotations found")
}

// Someone who was not on the roster when a round was minted has no request for
// it, which reads as "not asked" rather than as a missed reply.
func TestViewHistoricalResponses_VolunteerAcrossRotations(t *testing.T) {
	answered := historicalCutoff.Add(-24 * time.Hour)

	store := &mockHistoricalStore{
		rotations: []db.Rotation{
			allocatedRota("rota-1", "2025-01-05", 4),
			allocatedRota("rota-2", "2025-03-02", 6),
		},
		requests: []db.AvailabilityRequest{
			historicalRequest("rota-1", "alice"),
			historicalRequest("rota-2", "alice"),
			historicalRequest("rota-1", "bob"),   // bob only in rota-1
			historicalRequest("rota-2", "carol"), // carol only in rota-2
		},
		generations: map[string]db.AvailabilityGeneration{
			"rota-1/alice": answersOn(answered, "d1", "d2", "d3", "d4"),
			"rota-2/alice": answersOn(answered, "d1", "d2", "d3", "d4", "d5", "d6"),
			"rota-1/bob":   answersOn(answered, "d1", "d2"),
			"rota-2/carol": answersOn(answered, "d1"),
		},
	}

	volClient := &mockHistoricalVolClient{
		volunteers: []model.Volunteer{
			{ID: "alice", FirstName: "Alice", LastName: "Smith", Status: "Active"},
			{ID: "bob", FirstName: "Bob", LastName: "Jones", Status: "Active"},
			{ID: "carol", FirstName: "Carol", LastName: "Davis", Status: "Active"},
		},
	}

	result, err := ViewHistoricalResponses(
		context.Background(), store, volClient, &config.Config{}, zap.NewNop(), 5, nil)
	require.NoError(t, err)

	assert.Len(t, result.Volunteers, 3)
	assert.Equal(t, "no_form", result.Matrix["bob"]["rota-2"].Status)
	assert.Equal(t, "no_form", result.Matrix["carol"]["rota-1"].Status)
	assert.Equal(t, "available", result.Matrix["alice"]["rota-1"].Status)
	assert.Equal(t, 4, result.Matrix["alice"]["rota-1"].AvailableCount)
	assert.Equal(t, "available", result.Matrix["alice"]["rota-2"].Status)
	assert.Equal(t, 6, result.Matrix["alice"]["rota-2"].AvailableCount)
	assert.Equal(t, 1, result.Matrix["carol"]["rota-2"].AvailableCount)
}

func TestViewHistoricalResponses_EmptyVolunteerIDFilterShowsAll(t *testing.T) {
	answered := historicalCutoff.Add(-24 * time.Hour)

	store := &mockHistoricalStore{
		rotations: []db.Rotation{allocatedRota("rota-1", "2025-01-05", 4)},
		requests: []db.AvailabilityRequest{
			historicalRequest("rota-1", "alice"),
			historicalRequest("rota-1", "bob"),
		},
		generations: map[string]db.AvailabilityGeneration{
			"rota-1/alice": answersOn(answered, "d1"),
			"rota-1/bob":   answersOn(answered, "d1"),
		},
	}

	volClient := &mockHistoricalVolClient{
		volunteers: []model.Volunteer{
			{ID: "alice", FirstName: "Alice", LastName: "Smith", Status: "Active"},
			{ID: "bob", FirstName: "Bob", LastName: "Jones", Status: "Active"},
		},
	}

	result, err := ViewHistoricalResponses(
		context.Background(), store, volClient, &config.Config{}, zap.NewNop(), 5, []string{})
	require.NoError(t, err)
	assert.Len(t, result.Volunteers, 2)

	result, err = ViewHistoricalResponses(
		context.Background(), store, volClient, &config.Config{}, zap.NewNop(), 5, nil)
	require.NoError(t, err)
	assert.Len(t, result.Volunteers, 2)
}
