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

// mockHistoricalStore holds the rotas and the stored rounds: the part of
// VolunteerHistoryStore that says who was asked and what they answered.
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

func (m *mockHistoricalVolClient) ListVolunteers(organisation *config.Organisation, roles model.Roles) ([]model.Volunteer, error) {
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

// mockVolunteerHistoryStore adds a rota's shifts, allocations and alterations
// to the rotas and rounds mockHistoricalStore holds.
type mockVolunteerHistoryStore struct {
	mockHistoricalStore

	shifts      []db.Shift
	allocations []db.Allocation
	alterations []db.Alteration
}

func (m *mockVolunteerHistoryStore) GetShiftsByRotaID(ctx context.Context, rotaID string) ([]db.Shift, error) {
	var out []db.Shift
	for _, s := range m.shifts {
		if s.RotaID == rotaID {
			out = append(out, s)
		}
	}
	return out, nil
}

func (m *mockVolunteerHistoryStore) GetAllocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.Allocation, error) {
	ids := toSet(shiftIDs)
	var out []db.Allocation
	for _, a := range m.allocations {
		if ids[a.ShiftID] {
			out = append(out, a)
		}
	}
	return out, nil
}

func (m *mockVolunteerHistoryStore) GetAlterationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.Alteration, error) {
	ids := toSet(shiftIDs)
	var out []db.Alteration
	for _, a := range m.alterations {
		if ids[a.ShiftID] {
			out = append(out, a)
		}
	}
	return out, nil
}

func toSet(ids []string) map[string]bool {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}

func TestAvailabilityBand(t *testing.T) {
	cases := []struct {
		name   string
		status VolunteerRotaStatus
		open   int
		want   AvailabilityBand
	}{
		{"not asked", VolunteerRotaStatus{Status: "no_form"}, 8, BandNotSent},
		{"silent", VolunteerRotaStatus{Status: "no_response"}, 8, BandNoResponse},
		{"answered no to everything", VolunteerRotaStatus{Status: "no_availability"}, 8, BandUnavailable},
		{"under a quarter", VolunteerRotaStatus{Status: "available", AvailableCount: 1}, 8, BandLow},
		{"exactly a quarter", VolunteerRotaStatus{Status: "available", AvailableCount: 2}, 8, BandOK},
		{"exactly half", VolunteerRotaStatus{Status: "available", AvailableCount: 4}, 8, BandOK},
		{"over half", VolunteerRotaStatus{Status: "available", AvailableCount: 5}, 8, BandGood},
		// A rota with no open shifts cannot be a fraction of anything; an answer
		// at all is the most it can be.
		{"no open shifts", VolunteerRotaStatus{Status: "available", AvailableCount: 1}, 0, BandGood},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, BandFor(tc.status, tc.open))
		})
	}
}

func TestVolunteerHistory(t *testing.T) {
	answered := historicalCutoff.Add(-24 * time.Hour)
	altered := historicalCutoff.Add(24 * time.Hour).Format(time.RFC3339)

	store := &mockVolunteerHistoryStore{
		mockHistoricalStore: mockHistoricalStore{
			rotations: []db.Rotation{
				allocatedRota("rota-1", "2025-01-05", 4),
				// Not allocated: still in flight, so not history.
				{ID: "rota-2", Start: "2025-03-02", ShiftCount: 4},
			},
			requests: []db.AvailabilityRequest{
				historicalRequest("rota-1", "alice"),
				historicalRequest("rota-1", "bob"),
			},
			generations: map[string]db.AvailabilityGeneration{
				"rota-1/alice": answersOn(answered, "s1", "s2", "s3"),
			},
		},
		shifts: []db.Shift{
			{ID: "s1", RotaID: "rota-1"},
			{ID: "s2", RotaID: "rota-1"},
			{ID: "s3", RotaID: "rota-1"},
			{ID: "s4", RotaID: "rota-1", Closed: true},
		},
		allocations: []db.Allocation{
			{ShiftID: "s1", VolunteerID: "alice"},
			{ShiftID: "s2", VolunteerID: "alice"},
			{ShiftID: "s3", VolunteerID: "bob"},
			// A Closed Shift is nobody's shift, whatever is stored against it.
			{ShiftID: "s4", VolunteerID: "carol"},
		},
		alterations: []db.Alteration{
			// Alice swapped out of s2; Carol, never asked, covered it.
			{ShiftID: "s2", VolunteerID: "alice", Direction: "remove", SetTime: altered},
			{ShiftID: "s2", VolunteerID: "carol", Direction: "add", SetTime: altered},
		},
	}
	volClient := &mockHistoricalVolClient{volunteers: []model.Volunteer{
		{ID: "alice", FirstName: "Alice", LastName: "A"},
		{ID: "bob", FirstName: "Bob", LastName: "B"},
		{ID: "carol", FirstName: "Carol", LastName: "C"},
		{ID: "dave", FirstName: "Dave", LastName: "D"}, // in no rota at all
	}}

	result, err := VolunteerHistory(context.Background(), store, volClient, &config.Organisation{}, zap.NewNop(), 6)
	require.NoError(t, err)

	require.Len(t, result.Rotations, 1)
	assert.Equal(t, "rota-1", result.Rotations[0].ID)

	ids := make([]string, 0, len(result.Volunteers))
	for _, v := range result.Volunteers {
		ids = append(ids, v.ID)
	}
	assert.Equal(t, []string{"alice", "bob", "carol"}, ids)

	assert.Equal(t, VolunteerHistoryEntry{Band: BandGood, Available: 3, OpenShifts: 3, Given: 2, Attended: 1}, result.Entries["alice"]["rota-1"])
	assert.Equal(t, VolunteerHistoryEntry{Band: BandNoResponse, OpenShifts: 3, Given: 1, Attended: 1}, result.Entries["bob"]["rota-1"])
	assert.Equal(t, VolunteerHistoryEntry{Band: BandNotSent, OpenShifts: 3, Given: 0, Attended: 1}, result.Entries["carol"]["rota-1"])
}

// No rota has been allocated yet: an empty history, not an error — the tab is
// reachable from the first day.
func TestVolunteerHistory_NothingAllocated(t *testing.T) {
	store := &mockVolunteerHistoryStore{}
	result, err := VolunteerHistory(context.Background(), store, &mockHistoricalVolClient{}, &config.Organisation{}, zap.NewNop(), 6)
	require.NoError(t, err)
	assert.Empty(t, result.Rotations)
	assert.Empty(t, result.Volunteers)
}

// The cut-off is the rota's own allocation. An answer changed after the rota
// went out never informed it, so the history must not credit it — this is the
// whole reason the read is bounded rather than "latest wins".
func TestVolunteerHistory_IgnoresAnswersAfterAllocation(t *testing.T) {
	store := &mockVolunteerHistoryStore{
		mockHistoricalStore: mockHistoricalStore{
			rotations: []db.Rotation{allocatedRota("rota-1", "2025-01-05", 2)},
			requests:  []db.AvailabilityRequest{historicalRequest("rota-1", "alice")},
			generations: map[string]db.AvailabilityGeneration{
				"rota-1/alice": answersOn(historicalCutoff.Add(time.Hour), "s1", "s2"),
			},
		},
		shifts: []db.Shift{{ID: "s1", RotaID: "rota-1"}, {ID: "s2", RotaID: "rota-1"}},
	}
	volClient := &mockHistoricalVolClient{volunteers: []model.Volunteer{{ID: "alice", FirstName: "Alice", LastName: "A"}}}

	result, err := VolunteerHistory(context.Background(), store, volClient, &config.Organisation{}, zap.NewNop(), 6)
	require.NoError(t, err)

	assert.Equal(t, BandNoResponse, result.Entries["alice"]["rota-1"].Band,
		"an answer given after allocation did not count towards that rota")
}

// An answer that ticks nothing is a real reply, and reads as unavailable
// rather than as silence.
func TestVolunteerHistory_AnsweredWithNothing(t *testing.T) {
	store := &mockVolunteerHistoryStore{
		mockHistoricalStore: mockHistoricalStore{
			rotations: []db.Rotation{allocatedRota("rota-1", "2025-01-05", 1)},
			requests:  []db.AvailabilityRequest{historicalRequest("rota-1", "bob")},
			generations: map[string]db.AvailabilityGeneration{
				"rota-1/bob": answersOn(historicalCutoff.Add(-time.Hour)),
			},
		},
		shifts: []db.Shift{{ID: "s1", RotaID: "rota-1"}},
	}
	volClient := &mockHistoricalVolClient{volunteers: []model.Volunteer{{ID: "bob", FirstName: "Bob", LastName: "B"}}}

	result, err := VolunteerHistory(context.Background(), store, volClient, &config.Organisation{}, zap.NewNop(), 6)
	require.NoError(t, err)

	assert.Equal(t, BandUnavailable, result.Entries["bob"]["rota-1"].Band)
}

// count keeps the most recent allocated rotas, in date order.
func TestVolunteerHistory_CountKeepsTheLatest(t *testing.T) {
	store := &mockVolunteerHistoryStore{
		mockHistoricalStore: mockHistoricalStore{
			rotations: []db.Rotation{
				allocatedRota("rota-3", "2025-05-04", 4),
				allocatedRota("rota-1", "2025-01-05", 4),
				allocatedRota("rota-2", "2025-03-02", 4),
			},
		},
	}

	result, err := VolunteerHistory(context.Background(), store, &mockHistoricalVolClient{}, &config.Organisation{}, zap.NewNop(), 2)
	require.NoError(t, err)

	require.Len(t, result.Rotations, 2)
	assert.Equal(t, "rota-2", result.Rotations[0].ID)
	assert.Equal(t, "rota-3", result.Rotations[1].ID)
}
