package services

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// mockChangeRotaStore implements ChangeRotaStore for testing. Its WithRotaLock
// records the requested lock and hands the mock itself to the callback as the
// transaction-bound store.
type mockChangeRotaStore struct {
	shifts      []db.Shift
	allocations []db.Allocation
	alterations []db.Alteration

	lockedRotaIDs       [][]string
	insertedCover       *db.Cover
	insertedAlterations []db.Alteration
}

func (m *mockChangeRotaStore) WithRotaLock(ctx context.Context, rotaIDs []string, fn func(store db.RotaChangeStore) error) error {
	m.lockedRotaIDs = append(m.lockedRotaIDs, rotaIDs)
	return fn(m)
}

func (m *mockChangeRotaStore) GetShiftByDate(ctx context.Context, date time.Time) (*db.Shift, error) {
	dateStr := date.Format("2006-01-02")
	for i := range m.shifts {
		if m.shifts[i].Date == dateStr {
			return &m.shifts[i], nil
		}
	}
	return nil, nil
}

func (m *mockChangeRotaStore) GetAllocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.Allocation, error) {
	want := idSet(shiftIDs)
	var filtered []db.Allocation
	for _, a := range m.allocations {
		if want[a.ShiftID] {
			filtered = append(filtered, a)
		}
	}
	return filtered, nil
}

func (m *mockChangeRotaStore) GetAlterationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.Alteration, error) {
	want := idSet(shiftIDs)
	var filtered []db.Alteration
	for _, a := range m.alterations {
		if want[a.ShiftID] {
			filtered = append(filtered, a)
		}
	}
	return filtered, nil
}

// shiftDateInRange mimics the DB's inclusive shift_date bounds, with zero
// times leaving the corresponding bound open
func shiftDateInRange(dateStr string, from, to time.Time) bool {
	if !from.IsZero() && dateStr < from.Format("2006-01-02") {
		return false
	}
	if !to.IsZero() && dateStr > to.Format("2006-01-02") {
		return false
	}
	return true
}

func (m *mockChangeRotaStore) InsertCoverAndAlterations(ctx context.Context, cover *db.Cover, alterations []db.Alteration) error {
	m.insertedCover = cover
	m.insertedAlterations = alterations
	return nil
}

// mockChangeRotaVolClient implements VolunteerClient for changeRota tests
type mockChangeRotaVolClient struct {
	volunteers []model.Volunteer
}

func (m *mockChangeRotaVolClient) ListVolunteers(cfg *config.Config) ([]model.Volunteer, error) {
	return m.volunteers, nil
}

// defaultVolunteers returns a standard set of test volunteers
func defaultVolunteers() *mockChangeRotaVolClient {
	return &mockChangeRotaVolClient{
		volunteers: []model.Volunteer{
			{ID: "alice", FirstName: "Alice", LastName: "A", Role: model.RoleVolunteer},
			{ID: "bob", FirstName: "Bob", LastName: "B", Role: model.RoleVolunteer},
			{ID: "charlie", FirstName: "Charlie", LastName: "C", Role: model.RoleVolunteer},
			{ID: "dave", FirstName: "Dave", LastName: "D", Role: model.RoleVolunteer},
		},
	}
}

var testCfg = &config.Config{}

func TestChangeRota_SuccessWithInOut(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockChangeRotaStore{
		shifts: sundayShifts("rota-1", "2025-01-05", 3),
		allocations: []db.Allocation{
			{ID: "a1", ShiftID: "2025-01-05", Role: string(model.RoleTeamLead), VolunteerID: "alice"},
			{ID: "a2", ShiftID: "2025-01-05", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
			{ID: "a3", ShiftID: "2025-01-12", Role: string(model.RoleVolunteer), VolunteerID: "charlie"},
		},
	}

	params := ChangeRotaParams{
		Date:      "2025-01-05",
		Out:       "bob",
		In:        "dave",
		Reason:    "Holiday cover",
		UserEmail: "test@example.com",
	}

	result, err := ChangeRota(ctx, store, defaultVolunteers(), testCfg, params, logger)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.NotEmpty(t, result.CoverID)
	assert.Len(t, result.Alterations, 2)

	// Check cover was inserted
	require.NotNil(t, store.insertedCover)
	assert.Equal(t, "Holiday cover", store.insertedCover.Reason)
	assert.Equal(t, "test@example.com", store.insertedCover.UserEmail)

	// Check alterations
	require.Len(t, store.insertedAlterations, 2)

	// Find remove and add alterations
	var removeAlt, addAlt *db.Alteration
	for i := range store.insertedAlterations {
		if store.insertedAlterations[i].Direction == "remove" {
			removeAlt = &store.insertedAlterations[i]
		} else {
			addAlt = &store.insertedAlterations[i]
		}
	}

	require.NotNil(t, removeAlt)
	assert.Equal(t, "bob", removeAlt.VolunteerID)
	assert.Equal(t, "2025-01-05", removeAlt.ShiftID)
	assert.Equal(t, "2025-01-05", result.DatesByShiftID[removeAlt.ShiftID])

	require.NotNil(t, addAlt)
	assert.Equal(t, "dave", addAlt.VolunteerID)
	assert.Equal(t, "2025-01-05", addAlt.ShiftID)
}

func TestChangeRota_SwapDate(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockChangeRotaStore{
		shifts: sundayShifts("rota-1", "2025-01-05", 3),
		allocations: []db.Allocation{
			{ID: "a1", ShiftID: "2025-01-05", Role: string(model.RoleVolunteer), VolunteerID: "alice"},
			{ID: "a2", ShiftID: "2025-01-12", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
		},
	}

	params := ChangeRotaParams{
		Date:      "2025-01-05",
		Out:       "alice",
		In:        "bob",
		SwapDate:  "2025-01-12",
		Reason:    "Swap",
		UserEmail: "test@example.com",
	}

	result, err := ChangeRota(ctx, store, defaultVolunteers(), testCfg, params, logger)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Should have 4 alterations: 2 for primary date + 2 for swap date
	assert.Len(t, result.Alterations, 4)

	// Primary date: remove alice, add bob
	// Swap date: remove bob, add alice (reversed)
	var primaryRemove, primaryAdd, swapRemove, swapAdd int
	for _, alt := range store.insertedAlterations {
		if alt.ShiftID == "2025-01-05" && alt.Direction == "remove" {
			primaryRemove++
			assert.Equal(t, "alice", alt.VolunteerID)
		}
		if alt.ShiftID == "2025-01-05" && alt.Direction == "add" {
			primaryAdd++
			assert.Equal(t, "bob", alt.VolunteerID)
		}
		if alt.ShiftID == "2025-01-12" && alt.Direction == "remove" {
			swapRemove++
			assert.Equal(t, "bob", alt.VolunteerID)
		}
		if alt.ShiftID == "2025-01-12" && alt.Direction == "add" {
			swapAdd++
			assert.Equal(t, "alice", alt.VolunteerID)
		}
	}

	assert.Equal(t, 1, primaryRemove)
	assert.Equal(t, 1, primaryAdd)
	assert.Equal(t, 1, swapRemove)
	assert.Equal(t, 1, swapAdd)
}

func TestChangeRota_CustomInOut(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockChangeRotaStore{
		shifts: sundayShifts("rota-1", "2025-01-05", 1),
		allocations: []db.Allocation{
			{ID: "a1", ShiftID: "2025-01-05", Role: string(model.RoleVolunteer), CustomEntry: "External John"},
		},
	}

	params := ChangeRotaParams{
		Date:      "2025-01-05",
		OutCustom: "External John",
		InCustom:  "External Jane",
		Reason:    "Replacement",
		UserEmail: "test@example.com",
	}

	result, err := ChangeRota(ctx, store, defaultVolunteers(), testCfg, params, logger)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Len(t, result.Alterations, 2)

	var removeAlt, addAlt *db.Alteration
	for i := range store.insertedAlterations {
		if store.insertedAlterations[i].Direction == "remove" {
			removeAlt = &store.insertedAlterations[i]
		} else {
			addAlt = &store.insertedAlterations[i]
		}
	}

	require.NotNil(t, removeAlt)
	assert.Equal(t, "External John", removeAlt.CustomValue)

	require.NotNil(t, addAlt)
	assert.Equal(t, "External Jane", addAlt.CustomValue)
}

func TestChangeRota_NoInputsError(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockChangeRotaStore{}

	params := ChangeRotaParams{
		Date:      "2025-01-05",
		Reason:    "No changes",
		UserEmail: "test@example.com",
	}

	_, err := ChangeRota(ctx, store, defaultVolunteers(), testCfg, params, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "at least one of")
	assert.ErrorIs(t, err, ErrInvalidInput)
}

func TestChangeRota_DateNotInAnyRota(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockChangeRotaStore{
		shifts: sundayShifts("rota-1", "2025-01-05", 2),
	}

	params := ChangeRotaParams{
		Date:      "2025-03-01", // Not in any rota
		Out:       "bob",
		Reason:    "Test",
		UserEmail: "test@example.com",
	}

	_, err := ChangeRota(ctx, store, defaultVolunteers(), testCfg, params, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not in any rota")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestChangeRota_RemoveVolunteerNotOnShift(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockChangeRotaStore{
		shifts: sundayShifts("rota-1", "2025-01-05", 1),
		allocations: []db.Allocation{
			{ID: "a1", ShiftID: "2025-01-05", Role: string(model.RoleVolunteer), VolunteerID: "alice"},
		},
	}

	params := ChangeRotaParams{
		Date:      "2025-01-05",
		Out:       "bob", // Not on the shift
		Reason:    "Test",
		UserEmail: "test@example.com",
	}

	_, err := ChangeRota(ctx, store, defaultVolunteers(), testCfg, params, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not on the shift")
}

func TestChangeRota_AddVolunteerAlreadyOnShift(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockChangeRotaStore{
		shifts: sundayShifts("rota-1", "2025-01-05", 1),
		allocations: []db.Allocation{
			{ID: "a1", ShiftID: "2025-01-05", Role: string(model.RoleVolunteer), VolunteerID: "alice"},
		},
	}

	params := ChangeRotaParams{
		Date:      "2025-01-05",
		In:        "alice", // Already on the shift
		Reason:    "Test",
		UserEmail: "test@example.com",
	}

	_, err := ChangeRota(ctx, store, defaultVolunteers(), testCfg, params, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already on the shift")
	assert.ErrorIs(t, err, ErrConflict)
}

func TestChangeRota_RemoveCustomNotOnShift(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockChangeRotaStore{
		shifts: sundayShifts("rota-1", "2025-01-05", 1),
		allocations: []db.Allocation{
			{ID: "a1", ShiftID: "2025-01-05", Role: string(model.RoleVolunteer), VolunteerID: "alice"},
		},
	}

	params := ChangeRotaParams{
		Date:      "2025-01-05",
		OutCustom: "External John", // Not on the shift
		Reason:    "Test",
		UserEmail: "test@example.com",
	}

	_, err := ChangeRota(ctx, store, defaultVolunteers(), testCfg, params, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not on the shift")
}

func TestChangeRota_AddDuplicateCustomAllowed(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockChangeRotaStore{
		shifts: sundayShifts("rota-1", "2025-01-05", 1),
		allocations: []db.Allocation{
			{ID: "a1", ShiftID: "2025-01-05", Role: string(model.RoleVolunteer), CustomEntry: "Org X"},
		},
	}

	// Adding a duplicate custom entry should succeed (e.g. multiple people from same org)
	params := ChangeRotaParams{
		Date:      "2025-01-05",
		InCustom:  "Org X",
		Reason:    "Second person from Org X",
		UserEmail: "test@example.com",
	}

	result, err := ChangeRota(ctx, store, defaultVolunteers(), testCfg, params, logger)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Len(t, result.Alterations, 1)
	assert.Equal(t, "add", result.Alterations[0].Direction)
	assert.Equal(t, "Org X", result.Alterations[0].CustomValue)
}

func TestChangeRota_SwapDateValidation(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	// alice is on Jan 5, bob is on Jan 12
	// Swap: remove alice from Jan 5, add bob to Jan 5, remove bob from Jan 12, add alice to Jan 12
	// But if alice is also on Jan 12, the swap date validation (add alice) should fail
	store := &mockChangeRotaStore{
		shifts: sundayShifts("rota-1", "2025-01-05", 2),
		allocations: []db.Allocation{
			{ID: "a1", ShiftID: "2025-01-05", Role: string(model.RoleVolunteer), VolunteerID: "alice"},
			{ID: "a2", ShiftID: "2025-01-12", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
			{ID: "a3", ShiftID: "2025-01-12", Role: string(model.RoleVolunteer), VolunteerID: "alice"}, // alice is also on swap date
		},
	}

	params := ChangeRotaParams{
		Date:      "2025-01-05",
		Out:       "alice",
		In:        "bob",
		SwapDate:  "2025-01-12",
		Reason:    "Swap",
		UserEmail: "test@example.com",
	}

	// Swap date validation checks reversed: out=bob (ok, bob is there), in=alice (fail, alice is already there)
	_, err := ChangeRota(ctx, store, defaultVolunteers(), testCfg, params, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "swap date")
	assert.Contains(t, err.Error(), "already on the shift")
}

func TestChangeRota_MissingReason(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockChangeRotaStore{}

	params := ChangeRotaParams{
		Date:      "2025-01-05",
		Out:       "bob",
		UserEmail: "test@example.com",
	}

	_, err := ChangeRota(ctx, store, defaultVolunteers(), testCfg, params, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--reason is required")
}

func TestChangeRota_OnlyOut(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockChangeRotaStore{
		shifts: sundayShifts("rota-1", "2025-01-05", 1),
		allocations: []db.Allocation{
			{ID: "a1", ShiftID: "2025-01-05", Role: string(model.RoleVolunteer), VolunteerID: "alice"},
		},
	}

	params := ChangeRotaParams{
		Date:      "2025-01-05",
		Out:       "alice",
		Reason:    "No longer available",
		UserEmail: "test@example.com",
	}

	result, err := ChangeRota(ctx, store, defaultVolunteers(), testCfg, params, logger)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Len(t, result.Alterations, 1)
	assert.Equal(t, "remove", result.Alterations[0].Direction)
	assert.Equal(t, "alice", result.Alterations[0].VolunteerID)
}

func TestChangeRota_OnlyIn(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockChangeRotaStore{
		shifts: sundayShifts("rota-1", "2025-01-05", 1),
		allocations: []db.Allocation{
			{ID: "a1", ShiftID: "2025-01-05", Role: string(model.RoleVolunteer), VolunteerID: "alice"},
		},
	}

	params := ChangeRotaParams{
		Date:      "2025-01-05",
		In:        "bob",
		Reason:    "Extra help needed",
		UserEmail: "test@example.com",
	}

	result, err := ChangeRota(ctx, store, defaultVolunteers(), testCfg, params, logger)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Len(t, result.Alterations, 1)
	assert.Equal(t, "add", result.Alterations[0].Direction)
	assert.Equal(t, "bob", result.Alterations[0].VolunteerID)
}

func TestChangeRota_SwapDateDifferentRota(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	// Two rotas: rota-1 has Jan 5+12, rota-2 has Jan 19+26
	// alice is on Jan 12 (rota-1), bob is on Jan 19 (rota-2)
	store := &mockChangeRotaStore{
		shifts: append(
			sundayShifts("rota-1", "2025-01-05", 2),
			sundayShifts("rota-2", "2025-01-19", 2)...,
		),
		allocations: []db.Allocation{
			{ID: "a1", ShiftID: "2025-01-12", Role: string(model.RoleVolunteer), VolunteerID: "alice"},
			{ID: "a2", ShiftID: "2025-01-19", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
		},
	}

	params := ChangeRotaParams{
		Date:      "2025-01-12",
		Out:       "alice",
		In:        "bob",
		SwapDate:  "2025-01-19",
		Reason:    "Cross-rota swap",
		UserEmail: "test@example.com",
	}

	result, err := ChangeRota(ctx, store, defaultVolunteers(), testCfg, params, logger)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Len(t, result.Alterations, 4)

	// Rota identity now lives on the shift, so the cross-rota span shows up as
	// both rotas being locked together and the alterations carrying the two
	// rotas' shift ids (2025-01-12 in rota-1, 2025-01-19 in rota-2).
	require.Len(t, store.lockedRotaIDs, 1)
	assert.ElementsMatch(t, []string{"rota-1", "rota-2"}, store.lockedRotaIDs[0])

	byShift := map[string]int{}
	for _, alt := range store.insertedAlterations {
		byShift[alt.ShiftID]++
	}
	assert.Equal(t, 2, byShift["2025-01-12"], "primary date on rota-1's shift")
	assert.Equal(t, 2, byShift["2025-01-19"], "swap date on rota-2's shift")
}

func TestChangeRota_RespectsExistingAlterations(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	// alice was originally on the shift but was already removed by a previous alteration
	store := &mockChangeRotaStore{
		shifts: sundayShifts("rota-1", "2025-01-05", 1),
		allocations: []db.Allocation{
			{ID: "a1", ShiftID: "2025-01-05", Role: string(model.RoleVolunteer), VolunteerID: "alice"},
			{ID: "a2", ShiftID: "2025-01-05", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
		},
		alterations: []db.Alteration{
			{ID: "prev-alt", ShiftID: "2025-01-05", Direction: "remove", VolunteerID: "alice", SetTime: "2025-01-01T00:00:00Z"},
		},
	}

	// Try to remove alice again - should fail because she's already been removed
	params := ChangeRotaParams{
		Date:      "2025-01-05",
		Out:       "alice",
		Reason:    "Test",
		UserEmail: "test@example.com",
	}

	_, err := ChangeRota(ctx, store, defaultVolunteers(), testCfg, params, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not on the shift")
}

func TestChangeRota_InvalidInVolunteerID(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockChangeRotaStore{}

	params := ChangeRotaParams{
		Date:      "2025-01-05",
		In:        "nonexistent",
		Reason:    "Test",
		UserEmail: "test@example.com",
	}

	_, err := ChangeRota(ctx, store, defaultVolunteers(), testCfg, params, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "volunteer nonexistent not found")
}

func TestChangeRota_InvalidOutVolunteerID(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockChangeRotaStore{}

	params := ChangeRotaParams{
		Date:      "2025-01-05",
		Out:       "nonexistent",
		Reason:    "Test",
		UserEmail: "test@example.com",
	}

	_, err := ChangeRota(ctx, store, defaultVolunteers(), testCfg, params, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "volunteer nonexistent not found")
}

func TestChangeRota_TeamLeadGetsCorrectRole(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockChangeRotaStore{
		shifts: sundayShifts("rota-1", "2025-01-05", 1),
		allocations: []db.Allocation{
			{ID: "a1", ShiftID: "2025-01-05", Role: string(model.RoleVolunteer), VolunteerID: "alice"},
		},
	}

	// bob is a team lead in the volunteer list
	volClient := &mockChangeRotaVolClient{
		volunteers: []model.Volunteer{
			{ID: "alice", FirstName: "Alice", LastName: "A", Role: model.RoleVolunteer},
			{ID: "bob", FirstName: "Bob", LastName: "B", Role: model.RoleTeamLead},
		},
	}

	params := ChangeRotaParams{
		Date:      "2025-01-05",
		In:        "bob",
		Reason:    "Adding team lead",
		UserEmail: "test@example.com",
	}

	result, err := ChangeRota(ctx, store, volClient, testCfg, params, logger)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Find the add alteration
	var addAlt *db.Alteration
	for i := range store.insertedAlterations {
		if store.insertedAlterations[i].Direction == "add" {
			addAlt = &store.insertedAlterations[i]
		}
	}

	require.NotNil(t, addAlt)
	assert.Equal(t, "bob", addAlt.VolunteerID)
	assert.Equal(t, string(model.RoleTeamLead), addAlt.Role)
}

func TestChangeRota_VolunteerReplacesTeamLead_InheritsRole(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockChangeRotaStore{
		shifts: sundayShifts("rota-1", "2025-01-05", 1),
		allocations: []db.Allocation{
			{ID: "a1", ShiftID: "2025-01-05", Role: string(model.RoleTeamLead), VolunteerID: "alice"},
			{ID: "a2", ShiftID: "2025-01-05", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
		},
	}

	// dave is a regular volunteer replacing alice (team lead)
	params := ChangeRotaParams{
		Date:      "2025-01-05",
		Out:       "alice",
		In:        "dave",
		Reason:    "Replacing team lead",
		UserEmail: "test@example.com",
	}

	result, err := ChangeRota(ctx, store, defaultVolunteers(), testCfg, params, logger)
	require.NoError(t, err)
	require.NotNil(t, result)

	var addAlt *db.Alteration
	for i := range store.insertedAlterations {
		if store.insertedAlterations[i].Direction == "add" {
			addAlt = &store.insertedAlterations[i]
		}
	}

	require.NotNil(t, addAlt)
	assert.Equal(t, "dave", addAlt.VolunteerID)
	assert.Equal(t, string(model.RoleTeamLead), addAlt.Role, "volunteer replacing team lead should inherit team lead role")
}

func TestChangeRota_TeamLeadReplacesVolunteer_InheritsRole(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockChangeRotaStore{
		shifts: sundayShifts("rota-1", "2025-01-05", 1),
		allocations: []db.Allocation{
			{ID: "a1", ShiftID: "2025-01-05", Role: string(model.RoleTeamLead), VolunteerID: "alice"},
			{ID: "a2", ShiftID: "2025-01-05", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
		},
	}

	// eve is a team lead replacing bob (volunteer)
	volClient := &mockChangeRotaVolClient{
		volunteers: []model.Volunteer{
			{ID: "alice", FirstName: "Alice", LastName: "A", Role: model.RoleTeamLead},
			{ID: "bob", FirstName: "Bob", LastName: "B", Role: model.RoleVolunteer},
			{ID: "eve", FirstName: "Eve", LastName: "E", Role: model.RoleTeamLead},
		},
	}

	params := ChangeRotaParams{
		Date:      "2025-01-05",
		Out:       "bob",
		In:        "eve",
		Reason:    "Team lead filling volunteer slot",
		UserEmail: "test@example.com",
	}

	result, err := ChangeRota(ctx, store, volClient, testCfg, params, logger)
	require.NoError(t, err)
	require.NotNil(t, result)

	var addAlt *db.Alteration
	for i := range store.insertedAlterations {
		if store.insertedAlterations[i].Direction == "add" {
			addAlt = &store.insertedAlterations[i]
		}
	}

	require.NotNil(t, addAlt)
	assert.Equal(t, "eve", addAlt.VolunteerID)
	assert.Equal(t, string(model.RoleVolunteer), addAlt.Role, "team lead replacing volunteer should inherit volunteer role")
}

func TestChangeRota_TeamLeadAddedToShiftWithExistingTeamLead_Downgraded(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockChangeRotaStore{
		shifts: sundayShifts("rota-1", "2025-01-05", 1),
		allocations: []db.Allocation{
			{ID: "a1", ShiftID: "2025-01-05", Role: string(model.RoleTeamLead), VolunteerID: "alice"},
			{ID: "a2", ShiftID: "2025-01-05", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
		},
	}

	// eve is a team lead being added (no --out) to a shift that already has alice as team lead
	volClient := &mockChangeRotaVolClient{
		volunteers: []model.Volunteer{
			{ID: "alice", FirstName: "Alice", LastName: "A", Role: model.RoleTeamLead},
			{ID: "bob", FirstName: "Bob", LastName: "B", Role: model.RoleVolunteer},
			{ID: "eve", FirstName: "Eve", LastName: "E", Role: model.RoleTeamLead},
		},
	}

	params := ChangeRotaParams{
		Date:      "2025-01-05",
		In:        "eve",
		Reason:    "Extra help",
		UserEmail: "test@example.com",
	}

	result, err := ChangeRota(ctx, store, volClient, testCfg, params, logger)
	require.NoError(t, err)
	require.NotNil(t, result)

	var addAlt *db.Alteration
	for i := range store.insertedAlterations {
		if store.insertedAlterations[i].Direction == "add" {
			addAlt = &store.insertedAlterations[i]
		}
	}

	require.NotNil(t, addAlt)
	assert.Equal(t, "eve", addAlt.VolunteerID)
	assert.Equal(t, string(model.RoleVolunteer), addAlt.Role, "team lead added to shift with existing team lead should be downgraded to volunteer")
}
