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
	"github.com/jakechorley/ilford-drop-in/pkg/core/allocator"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services/utils"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// sundayShifts builds the shift rows a rota's backfill would have produced:
// one shift per consecutive Sunday from start, mirroring the old date
// arithmetic so tests can feed shift rows through the mock stores.
func sundayShifts(rotaID, start string, count int) []db.Shift {
	dates, err := utils.CalculateShiftDates(start, count)
	if err != nil {
		panic(err)
	}
	shifts := make([]db.Shift, len(dates))
	for i, d := range dates {
		shifts[i] = db.Shift{
			ID:     fmt.Sprintf("%s-shift-%d", rotaID, i),
			RotaID: rotaID,
			Date:   d.Format("2006-01-02"),
		}
	}
	return shifts
}

// mockAllocateRotaStore implements AllocateRotaStore for testing
type mockAllocateRotaStore struct {
	rotations            []db.Rotation
	shifts               []db.Shift
	availabilityRequests []db.AvailabilityRequest
	allocations          []db.Allocation
	alterations          []db.Alteration
	insertedAllocations  []db.Allocation
	getRotationsErr      error
	getAvailabilityErr   error
	getAllocationsErr    error
	getAlterationsErr    error
	insertAllocationsErr error
}

func (m *mockAllocateRotaStore) GetRotations(ctx context.Context) ([]db.Rotation, error) {
	if m.getRotationsErr != nil {
		return nil, m.getRotationsErr
	}
	return m.rotations, nil
}

func (m *mockAllocateRotaStore) GetShiftsByRotaID(ctx context.Context, rotaID string) ([]db.Shift, error) {
	var filtered []db.Shift
	for _, s := range m.shifts {
		if s.RotaID == rotaID {
			filtered = append(filtered, s)
		}
	}
	return filtered, nil
}

func (m *mockAllocateRotaStore) GetAvailabilityRequestsByRotaID(ctx context.Context, rotaID string) ([]db.AvailabilityRequest, error) {
	if m.getAvailabilityErr != nil {
		return nil, m.getAvailabilityErr
	}
	var filtered []db.AvailabilityRequest
	for _, r := range m.availabilityRequests {
		if r.RotaID == rotaID {
			filtered = append(filtered, r)
		}
	}
	return filtered, nil
}

func (m *mockAllocateRotaStore) GetAllocationsByRotaID(ctx context.Context, rotaID string) ([]db.Allocation, error) {
	if m.getAllocationsErr != nil {
		return nil, m.getAllocationsErr
	}
	var filtered []db.Allocation
	for _, a := range m.allocations {
		if a.RotaID == rotaID {
			filtered = append(filtered, a)
		}
	}
	return filtered, nil
}

func (m *mockAllocateRotaStore) GetAlterationsByRotaID(ctx context.Context, rotaID string) ([]db.Alteration, error) {
	if m.getAlterationsErr != nil {
		return nil, m.getAlterationsErr
	}
	var filtered []db.Alteration
	for _, a := range m.alterations {
		if a.RotaID == rotaID {
			filtered = append(filtered, a)
		}
	}
	return filtered, nil
}

func (m *mockAllocateRotaStore) InsertAllocationsAndSetAllocated(ctx context.Context, allocations []db.Allocation, rotaID string, datetime time.Time) error {
	if m.insertAllocationsErr != nil {
		return m.insertAllocationsErr
	}
	m.insertedAllocations = append(m.insertedAllocations, allocations...)
	return nil
}

// mockVolClient implements VolunteerClient for testing
type mockVolClient struct {
	volunteers []model.Volunteer
	listErr    error
}

func (m *mockVolClient) ListVolunteers(cfg *config.Config) ([]model.Volunteer, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	// Compute display names like the real client does
	sheetsclient.ComputeDisplayNames(m.volunteers)
	return m.volunteers, nil
}

// mockFormsClientWithResponses implements FormsClientWithResponses for testing
type mockFormsClientWithResponses struct {
	responses map[string]*formsclient.FormResponse // Map of volunteer name to response
	getErr    error
}

func (m *mockFormsClientWithResponses) GetFormResponse(formID string, volunteerName string, shiftDates []time.Time) (*formsclient.FormResponse, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	if resp, ok := m.responses[volunteerName]; ok {
		return resp, nil
	}
	// Default: not responded
	return &formsclient.FormResponse{
		HasResponded:     false,
		UnavailableDates: []string{},
	}, nil
}

func (m *mockFormsClientWithResponses) HasResponse(formID string) (bool, error) {
	return false, nil
}

func TestAllocateRota_SuccessfulAllocation(t *testing.T) {
	// Test successful allocation with team leads and sufficient volunteers
	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{
			{
				ID:         "rota-1",
				Start:      "2025-01-05", // Sunday
				ShiftCount: 3,
			},
		},
		shifts: sundayShifts("rota-1", "2025-01-05", 3),
		availabilityRequests: []db.AvailabilityRequest{
			{FormID: "form-1", VolunteerID: "alice", RotaID: "rota-1", FormSent: true},
			{FormID: "form-1", VolunteerID: "bob", RotaID: "rota-1", FormSent: true},
			{FormID: "form-1", VolunteerID: "charlie", RotaID: "rota-1", FormSent: true},
			{FormID: "form-1", VolunteerID: "dave", RotaID: "rota-1", FormSent: true},
			{FormID: "form-1", VolunteerID: "eve", RotaID: "rota-1", FormSent: true},
			{FormID: "form-1", VolunteerID: "frank", RotaID: "rota-1", FormSent: true},
			{FormID: "form-1", VolunteerID: "grace", RotaID: "rota-1", FormSent: true},
			{FormID: "form-1", VolunteerID: "henry", RotaID: "rota-1", FormSent: true},
			{FormID: "form-1", VolunteerID: "iris", RotaID: "rota-1", FormSent: true},
		},
	}

	volunteerClient := &mockVolClient{
		volunteers: []model.Volunteer{
			// Team leads (3 - one per shift)
			{ID: "alice", FirstName: "Alice", LastName: "Smith", Gender: "Female", Status: "Active", Role: model.RoleTeamLead},
			{ID: "bob", FirstName: "Bob", LastName: "Jones", Gender: "Male", Status: "Active", Role: model.RoleTeamLead},
			{ID: "charlie", FirstName: "Charlie", LastName: "Brown", Gender: "Male", Status: "Active", Role: model.RoleTeamLead},
			// Regular volunteers (mix of genders for MaleBalance criterion)
			{ID: "dave", FirstName: "Dave", LastName: "Wilson", Gender: "Male", Status: "Active", Role: model.RoleVolunteer},
			{ID: "eve", FirstName: "Eve", LastName: "Davis", Gender: "Female", Status: "Active", Role: model.RoleVolunteer},
			{ID: "frank", FirstName: "Frank", LastName: "Miller", Gender: "Male", Status: "Active", Role: model.RoleVolunteer},
			{ID: "grace", FirstName: "Grace", LastName: "Taylor", Gender: "Female", Status: "Active", Role: model.RoleVolunteer},
			{ID: "henry", FirstName: "Henry", LastName: "Anderson", Gender: "Male", Status: "Active", Role: model.RoleVolunteer},
			{ID: "iris", FirstName: "Iris", LastName: "Thomas", Gender: "Female", Status: "Active", Role: model.RoleVolunteer},
		},
	}

	formsClient := &mockFormsClientWithResponses{
		responses: map[string]*formsclient.FormResponse{
			"Alice Smith":    {HasResponded: true, UnavailableDates: []string{}},
			"Bob Jones":      {HasResponded: true, UnavailableDates: []string{}},
			"Charlie Brown":  {HasResponded: true, UnavailableDates: []string{}},
			"Dave Wilson":    {HasResponded: true, UnavailableDates: []string{}},
			"Eve Davis":      {HasResponded: true, UnavailableDates: []string{}},
			"Frank Miller":   {HasResponded: true, UnavailableDates: []string{}},
			"Grace Taylor":   {HasResponded: true, UnavailableDates: []string{}},
			"Henry Anderson": {HasResponded: true, UnavailableDates: []string{}},
			"Iris Thomas":    {HasResponded: true, UnavailableDates: []string{}},
		},
	}

	cfg := &config.Config{
		MaxAllocationFrequency: 1.0, // 100% - can be allocated to all shifts
		DefaultShiftSize:       2,   // 2 volunteers per shift (plus 1 team lead = 3 total)
	}

	logger := zap.NewNop()
	ctx := context.Background()

	// Run allocation (not dry-run, no force commit)
	result, err := AllocateRota(ctx, store, volunteerClient, formsClient, cfg, logger, false, false)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Assertions on basic structure
	assert.Equal(t, "rota-1", result.RotaID)
	assert.Equal(t, 3, result.ShiftCount)
	assert.Len(t, result.ShiftDates, 3)
	assert.Len(t, result.AllocatedShifts, 3)

	// Check that allocation succeeded
	assert.True(t, result.Success, "Allocation should succeed with team leads and sufficient volunteers")
	assert.Empty(t, result.ValidationErrors, "Should have no validation errors")

	// Check that shifts are properly filled
	for i, shift := range result.AllocatedShifts {
		assert.Equal(t, 2, shift.CurrentSize(), "Shift %d should have 2 regular volunteers", i)
		assert.NotNil(t, shift.TeamLead, "Shift %d should have a team lead", i)
		assert.True(t, shift.MaleCount >= 1, "Shift %d should have at least one male volunteer", i)
	}

	// Check that allocations were saved to database
	assert.NotEmpty(t, store.insertedAllocations, "Allocations should be saved")

	// Verify we have the right number of allocations
	// 3 shifts * (2 volunteers + 1 team lead) = 9 total allocations
	assert.Equal(t, 9, len(store.insertedAllocations), "Should have 9 allocations (3 shifts * 3 people)")

	// Count team lead vs volunteer allocations
	teamLeadCount := 0
	volunteerCount := 0
	for _, alloc := range store.insertedAllocations {
		switch alloc.Role {
		case string(model.RoleTeamLead):
			teamLeadCount++
		case string(model.RoleVolunteer):
			volunteerCount++
		}
	}
	assert.Equal(t, 3, teamLeadCount, "Should have 3 team lead allocations (1 per shift)")
	assert.Equal(t, 6, volunteerCount, "Should have 6 volunteer allocations (2 per shift)")
}

func TestAllocateRota_DryRun(t *testing.T) {
	// Test that dry-run mode prevents saving allocations to database
	// This test should succeed (with team leads) but not save to DB
	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2025-01-05", ShiftCount: 2},
		},
		shifts: sundayShifts("rota-1", "2025-01-05", 2),
		availabilityRequests: []db.AvailabilityRequest{
			{FormID: "form-1", VolunteerID: "alice", RotaID: "rota-1", FormSent: true},
			{FormID: "form-1", VolunteerID: "bob", RotaID: "rota-1", FormSent: true},
			{FormID: "form-1", VolunteerID: "charlie", RotaID: "rota-1", FormSent: true},
			{FormID: "form-1", VolunteerID: "dave", RotaID: "rota-1", FormSent: true},
			{FormID: "form-1", VolunteerID: "eve", RotaID: "rota-1", FormSent: true},
			{FormID: "form-1", VolunteerID: "frank", RotaID: "rota-1", FormSent: true},
		},
	}

	volunteerClient := &mockVolClient{
		volunteers: []model.Volunteer{
			// Team leads
			{ID: "alice", FirstName: "Alice", LastName: "A", Gender: "Female", Status: "Active", Role: model.RoleTeamLead},
			{ID: "bob", FirstName: "Bob", LastName: "B", Gender: "Male", Status: "Active", Role: model.RoleTeamLead},
			// Regular volunteers
			{ID: "charlie", FirstName: "Charlie", LastName: "C", Gender: "Male", Status: "Active", Role: model.RoleVolunteer},
			{ID: "dave", FirstName: "Dave", LastName: "D", Gender: "Male", Status: "Active", Role: model.RoleVolunteer},
			{ID: "eve", FirstName: "Eve", LastName: "E", Gender: "Female", Status: "Active", Role: model.RoleVolunteer},
			{ID: "frank", FirstName: "Frank", LastName: "F", Gender: "Male", Status: "Active", Role: model.RoleVolunteer},
		},
	}

	formsClient := &mockFormsClientWithResponses{
		responses: map[string]*formsclient.FormResponse{
			"Alice A":   {HasResponded: true, UnavailableDates: []string{}},
			"Bob B":     {HasResponded: true, UnavailableDates: []string{}},
			"Charlie C": {HasResponded: true, UnavailableDates: []string{}},
			"Dave D":    {HasResponded: true, UnavailableDates: []string{}},
			"Eve E":     {HasResponded: true, UnavailableDates: []string{}},
			"Frank F":   {HasResponded: true, UnavailableDates: []string{}},
		},
	}

	cfg := &config.Config{
		MaxAllocationFrequency: 1.0,
		DefaultShiftSize:       2,
	}

	logger := zap.NewNop()
	ctx := context.Background()

	// Run allocation in dry-run mode (forceCommit is ignored in dry-run)
	result, err := AllocateRota(ctx, store, volunteerClient, formsClient, cfg, logger, true, false)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Assertions
	assert.Len(t, result.AllocatedShifts, 2)
	assert.Equal(t, "rota-1", result.RotaID)
	assert.True(t, result.Success, "Allocation should succeed in dry-run mode")

	// Check shifts are filled
	for i, shift := range result.AllocatedShifts {
		assert.Equal(t, 2, shift.CurrentSize(), "Shift %d should have 2 volunteers", i)
		assert.NotNil(t, shift.TeamLead, "Shift %d should have a team lead", i)
	}

	// Allocations should NOT be saved in dry-run mode (regardless of success/failure)
	assert.Empty(t, store.insertedAllocations, "Allocations should not be saved in dry-run mode")
}

func TestAllocateRota_NoRotations(t *testing.T) {
	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{}, // No rotations
	}

	cfg := &config.Config{
		MaxAllocationFrequency: 0.5,
		DefaultShiftSize:       2,
	}

	logger := zap.NewNop()
	ctx := context.Background()

	result, err := AllocateRota(ctx, store, nil, nil, cfg, logger, false, false)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no rotations found")
}

func TestAllocateRota_NoAvailabilityRequests(t *testing.T) {
	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2025-01-05", ShiftCount: 2},
		},
		shifts:               sundayShifts("rota-1", "2025-01-05", 2),
		availabilityRequests: []db.AvailabilityRequest{}, // No requests
	}

	cfg := &config.Config{
		MaxAllocationFrequency: 0.5,
		DefaultShiftSize:       2,
	}

	logger := zap.NewNop()
	ctx := context.Background()

	result, err := AllocateRota(ctx, store, nil, nil, cfg, logger, false, false)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "no availability requests found")
}

func TestAllocateRota_InsufficientVolunteers(t *testing.T) {
	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2025-01-05", ShiftCount: 3},
		},
		shifts: sundayShifts("rota-1", "2025-01-05", 3),
		availabilityRequests: []db.AvailabilityRequest{
			{FormID: "form-1", VolunteerID: "alice", RotaID: "rota-1", FormSent: true},
		},
	}

	volunteerClient := &mockVolClient{
		volunteers: []model.Volunteer{
			// Only 1 volunteer, but we need to fill 3 shifts with 2 volunteers each
			{ID: "alice", FirstName: "Alice", LastName: "Smith", Gender: "Female", Status: "Active"},
		},
	}

	formsClient := &mockFormsClientWithResponses{
		responses: map[string]*formsclient.FormResponse{
			"Alice Smith": {HasResponded: true, UnavailableDates: []string{}},
		},
	}

	cfg := &config.Config{
		MaxAllocationFrequency: 1.0,
		DefaultShiftSize:       2,
	}

	logger := zap.NewNop()
	ctx := context.Background()

	result, err := AllocateRota(ctx, store, volunteerClient, formsClient, cfg, logger, false, false)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Allocation should fail due to insufficient volunteers
	assert.False(t, result.Success, "Allocation should fail with insufficient volunteers")
	assert.NotEmpty(t, result.ValidationErrors, "Should have validation errors")

	// Allocations should not be saved when allocation is unsuccessful
	assert.Empty(t, store.insertedAllocations, "Allocations should not be saved when unsuccessful")
}

func TestAllocateRota_UnsuccessfulAllocation_NotSaved(t *testing.T) {
	// Test that unsuccessful allocations (with validation errors) are not saved
	// This test has no team leads, which will cause TeamLead validation to fail
	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2025-01-05", ShiftCount: 2},
		},
		shifts: sundayShifts("rota-1", "2025-01-05", 2),
		availabilityRequests: []db.AvailabilityRequest{
			{FormID: "form-1", VolunteerID: "alice", RotaID: "rota-1", FormSent: true},
			{FormID: "form-1", VolunteerID: "bob", RotaID: "rota-1", FormSent: true},
			{FormID: "form-1", VolunteerID: "charlie", RotaID: "rota-1", FormSent: true},
			{FormID: "form-1", VolunteerID: "dave", RotaID: "rota-1", FormSent: true},
		},
	}

	volunteerClient := &mockVolClient{
		volunteers: []model.Volunteer{
			// No team leads - all regular volunteers
			// This will cause TeamLead validation to fail
			{ID: "alice", FirstName: "Alice", LastName: "A", Gender: "Female", Status: "Active", Role: model.RoleVolunteer},
			{ID: "bob", FirstName: "Bob", LastName: "B", Gender: "Male", Status: "Active", Role: model.RoleVolunteer},
			{ID: "charlie", FirstName: "Charlie", LastName: "C", Gender: "Male", Status: "Active", Role: model.RoleVolunteer},
			{ID: "dave", FirstName: "Dave", LastName: "D", Gender: "Male", Status: "Active", Role: model.RoleVolunteer},
		},
	}

	formsClient := &mockFormsClientWithResponses{
		responses: map[string]*formsclient.FormResponse{
			"Alice A":   {HasResponded: true, UnavailableDates: []string{}},
			"Bob B":     {HasResponded: true, UnavailableDates: []string{}},
			"Charlie C": {HasResponded: true, UnavailableDates: []string{}},
			"Dave D":    {HasResponded: true, UnavailableDates: []string{}},
		},
	}

	cfg := &config.Config{
		MaxAllocationFrequency: 1.0,
		DefaultShiftSize:       2,
	}

	logger := zap.NewNop()
	ctx := context.Background()

	result, err := AllocateRota(ctx, store, volunteerClient, formsClient, cfg, logger, false, false)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Should have validation errors (no team leads)
	assert.False(t, result.Success, "Allocation should fail without team leads")
	assert.NotEmpty(t, result.ValidationErrors, "Should have validation errors")

	// Verify we have TeamLead validation errors
	hasTeamLeadError := false
	for _, verr := range result.ValidationErrors {
		if verr.CriterionName == "TeamLead" {
			hasTeamLeadError = true
			break
		}
	}
	assert.True(t, hasTeamLeadError, "Should have TeamLead validation error")

	// Allocations should NOT be saved when validation fails (without forceCommit)
	assert.Empty(t, store.insertedAllocations, "Unsuccessful allocations should not be saved")
}

func TestAllocateRota_ForceCommit_SavesUnsuccessfulAllocation(t *testing.T) {
	// Test that forceCommit allows saving unsuccessful allocations
	// This is the same scenario as UnsuccessfulAllocation_NotSaved, but with forceCommit=true
	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2025-01-05", ShiftCount: 2},
		},
		shifts: sundayShifts("rota-1", "2025-01-05", 2),
		availabilityRequests: []db.AvailabilityRequest{
			{FormID: "form-1", VolunteerID: "alice", RotaID: "rota-1", FormSent: true},
			{FormID: "form-1", VolunteerID: "bob", RotaID: "rota-1", FormSent: true},
			{FormID: "form-1", VolunteerID: "charlie", RotaID: "rota-1", FormSent: true},
			{FormID: "form-1", VolunteerID: "dave", RotaID: "rota-1", FormSent: true},
		},
	}

	volunteerClient := &mockVolClient{
		volunteers: []model.Volunteer{
			// No team leads - all regular volunteers (will cause TeamLead validation to fail)
			{ID: "alice", FirstName: "Alice", LastName: "A", Gender: "Female", Status: "Active", Role: model.RoleVolunteer},
			{ID: "bob", FirstName: "Bob", LastName: "B", Gender: "Male", Status: "Active", Role: model.RoleVolunteer},
			{ID: "charlie", FirstName: "Charlie", LastName: "C", Gender: "Male", Status: "Active", Role: model.RoleVolunteer},
			{ID: "dave", FirstName: "Dave", LastName: "D", Gender: "Male", Status: "Active", Role: model.RoleVolunteer},
		},
	}

	formsClient := &mockFormsClientWithResponses{
		responses: map[string]*formsclient.FormResponse{
			"Alice A":   {HasResponded: true, UnavailableDates: []string{}},
			"Bob B":     {HasResponded: true, UnavailableDates: []string{}},
			"Charlie C": {HasResponded: true, UnavailableDates: []string{}},
			"Dave D":    {HasResponded: true, UnavailableDates: []string{}},
		},
	}

	cfg := &config.Config{
		MaxAllocationFrequency: 1.0,
		DefaultShiftSize:       2,
	}

	logger := zap.NewNop()
	ctx := context.Background()

	// Run allocation with forceCommit=true
	result, err := AllocateRota(ctx, store, volunteerClient, formsClient, cfg, logger, false, true)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Should still have validation errors
	assert.False(t, result.Success, "Allocation should fail without team leads")
	assert.NotEmpty(t, result.ValidationErrors, "Should have validation errors")

	// But allocations SHOULD be saved because of forceCommit
	assert.NotEmpty(t, store.insertedAllocations, "Allocations should be saved with forceCommit=true")

	// Verify we got the expected number of allocations (2 shifts * 2 volunteers each = 4)
	assert.Equal(t, 4, len(store.insertedAllocations), "Should have 4 volunteer allocations")
}

func TestConvertToDBAllocations(t *testing.T) {
	// Test the conversion of allocator shifts to database allocations
	shifts := []*allocator.Shift{
		{
			Date:  "2025-01-05",
			Index: 0,
			Size:  2,
			AllocatedGroups: []*allocator.VolunteerGroup{
				{
					GroupKey: "group_a",
					Members: []allocator.Volunteer{
						{ID: "alice", IsTeamLead: true},
						{ID: "bob", IsTeamLead: false},
					},
				},
			},
			TeamLead:             &allocator.Volunteer{ID: "alice", IsTeamLead: true},
			CustomPreallocations: []string{"external_john"},
		},
	}

	allocations := convertToDBAllocations("rota-1", shifts)

	// Should have 1 regular allocation (Bob) + 1 team lead (Alice) + 1 pre-allocated (John) = 3 total
	// Alice is in the group AND the team lead, so she should only appear once as team lead
	require.Len(t, allocations, 3)

	// Check that we have the right roles
	roles := make(map[string]int)
	for _, alloc := range allocations {
		roles[alloc.Role]++
		assert.Equal(t, "rota-1", alloc.RotaID)
		assert.Equal(t, "2025-01-05", alloc.ShiftDate)
	}

	assert.Equal(t, 1, roles[string(model.RoleTeamLead)], "Should have 1 team lead")
	assert.Equal(t, 2, roles[string(model.RoleVolunteer)], "Should have 2 volunteers (Bob + external)")

	// Check pre-allocated volunteer has correct field
	found := false
	for _, alloc := range allocations {
		if alloc.CustomEntry == "external_john" {
			found = true
			assert.Equal(t, "", alloc.VolunteerID, "Pre-allocated should have empty VolunteerID")
		}
	}
	assert.True(t, found, "Should have pre-allocated volunteer")
}

func TestFilterActiveVols(t *testing.T) {
	volunteers := []model.Volunteer{
		{ID: "alice", Status: "Active"},
		{ID: "bob", Status: "Inactive"},
		{ID: "charlie", Status: "Active"},
		{ID: "diana", Status: ""},
	}

	active := utils.FilterActiveVolunteers(volunteers)
	assert.Len(t, active, 2)
	assert.Equal(t, "alice", active[0].ID)
	assert.Equal(t, "charlie", active[1].ID)
}

func TestCalcShiftDates(t *testing.T) {
	dates, err := utils.CalculateShiftDates("2025-01-05", 4) // Start on Sunday, Jan 5
	require.NoError(t, err)
	require.Len(t, dates, 4)

	// All dates should be Sundays
	for i, date := range dates {
		assert.Equal(t, time.Sunday, date.Weekday(), "Shift %d should be on Sunday", i)
	}

	// Check specific dates
	assert.Equal(t, "2025-01-05", dates[0].Format("2006-01-02"))
	assert.Equal(t, "2025-01-12", dates[1].Format("2006-01-02"))
	assert.Equal(t, "2025-01-19", dates[2].Format("2006-01-02"))
	assert.Equal(t, "2025-01-26", dates[3].Format("2006-01-02"))
}

func TestCalcShiftDates_Invalid(t *testing.T) {
	_, err := utils.CalculateShiftDates("invalid-date", 4)
	assert.Error(t, err)
}

func TestBuildHistoricalShifts_SkipsUnknownVolunteers(t *testing.T) {
	// Allocations whose volunteer id no longer exists in the sheet are
	// skipped, but the shift itself is still emitted. (Inactive volunteers
	// are NOT filtered here — callers pass the full volunteer list.)
	ctx := context.Background()
	logger := zap.NewNop()

	// Setup: Two rotas - old (rota-0) and current (rota-1)
	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{
			{ID: "rota-0", Start: "2024-12-01", ShiftCount: 2}, // Old rota
			{ID: "rota-1", Start: "2025-01-05", ShiftCount: 2}, // Current rota
		},
		// Allocations from the old rota (rota-0)
		// Alice and Bob are a couple (group_alice_bob)
		// Charlie has been deleted from the volunteer sheet (unknown id)
		// Dave is individual
		allocations: []db.Allocation{
			// Shift 1 - Dec 1: Alice (group), Bob (group), Charlie (individual)
			{ID: "alloc-1", RotaID: "rota-0", ShiftDate: "2024-12-01", VolunteerID: "alice", Role: string(model.RoleVolunteer)},
			{ID: "alloc-2", RotaID: "rota-0", ShiftDate: "2024-12-01", VolunteerID: "bob", Role: string(model.RoleTeamLead)},
			{ID: "alloc-3", RotaID: "rota-0", ShiftDate: "2024-12-01", VolunteerID: "charlie", Role: string(model.RoleVolunteer)}, // Unknown
			// Shift 2 - Dec 8: Dave (individual), Charlie (individual)
			{ID: "alloc-4", RotaID: "rota-0", ShiftDate: "2024-12-08", VolunteerID: "dave", Role: string(model.RoleVolunteer)},
			{ID: "alloc-5", RotaID: "rota-0", ShiftDate: "2024-12-08", VolunteerID: "charlie", Role: string(model.RoleVolunteer)}, // Unknown
			// Allocations from current rota (should be ignored)
			{ID: "alloc-6", RotaID: "rota-1", ShiftDate: "2025-01-05", VolunteerID: "alice", Role: string(model.RoleVolunteer)},
		},
	}

	// Known volunteers (Charlie has been deleted from the sheet)
	volunteers := []allocator.Volunteer{
		{ID: "alice", FirstName: "Alice", LastName: "A", Gender: "Female", GroupKey: "group_alice_bob", IsTeamLead: false},
		{ID: "bob", FirstName: "Bob", LastName: "B", Gender: "Male", GroupKey: "group_alice_bob", IsTeamLead: true},
		{ID: "dave", FirstName: "Dave", LastName: "D", Gender: "Male", GroupKey: "", IsTeamLead: false}, // Individual
		// Charlie is NOT in the list (deleted)
	}

	targetRota := &db.Rotation{ID: "rota-1", Start: "2025-01-05", ShiftCount: 2}

	// Call buildHistoricalShifts
	historicalShifts, err := buildHistoricalShifts(ctx, store, store.rotations, targetRota, volunteers, logger)
	require.NoError(t, err)

	// Assertions
	require.Len(t, historicalShifts, 2, "Should have 2 historical shifts")

	// Check first shift (Dec 1) - should only have Alice and Bob (Charlie skipped)
	shift1 := historicalShifts[0]
	assert.Equal(t, "2024-12-01", shift1.Date)
	assert.Len(t, shift1.AllocatedGroups, 1, "Should have 1 group (Alice+Bob)")

	// Verify the group
	group1 := shift1.AllocatedGroups[0]
	assert.Equal(t, "group_alice_bob", group1.GroupKey)
	assert.Len(t, group1.Members, 2, "Group should have 2 members")
	assert.True(t, group1.HasTeamLead, "Group should have a team lead")
	assert.Equal(t, 1, group1.MaleCount, "Group should have 1 male (Bob)")

	// Verify members
	memberIDs := make([]string, len(group1.Members))
	for i, member := range group1.Members {
		memberIDs[i] = member.ID
	}
	assert.Contains(t, memberIDs, "alice")
	assert.Contains(t, memberIDs, "bob")

	// Check second shift (Dec 8) - should only have Dave (Charlie skipped)
	shift2 := historicalShifts[1]
	assert.Equal(t, "2024-12-08", shift2.Date)
	assert.Len(t, shift2.AllocatedGroups, 1, "Should have 1 group (Dave)")

	// Verify Dave's individual group (should have individual_ prefix)
	group2 := shift2.AllocatedGroups[0]
	assert.Len(t, group2.Members, 1, "Group should have 1 member")
	assert.Equal(t, "dave", group2.Members[0].ID)
	assert.False(t, group2.HasTeamLead, "Dave is not a team lead")
	assert.Equal(t, 1, group2.MaleCount, "Dave is male")
}

func TestBuildHistoricalShifts_KeepsShiftsWithNoKnownVolunteers(t *testing.T) {
	// A date whose workers are all unknown (deleted from the sheet) must
	// still appear — with empty groups — so the last historical shift is
	// the true last shift, and the result is sorted ascending by date.
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{
			{ID: "rota-0", Start: "2024-12-01", ShiftCount: 3},
			{ID: "rota-1", Start: "2025-01-05", ShiftCount: 3},
		},
		allocations: []db.Allocation{
			{ID: "alloc-1", RotaID: "rota-0", ShiftDate: "2024-12-01", VolunteerID: "alice", Role: string(model.RoleVolunteer)},
			{ID: "alloc-2", RotaID: "rota-0", ShiftDate: "2024-12-08", VolunteerID: "alice", Role: string(model.RoleVolunteer)},
			// Dec 15 (the true last shift) was worked only by a deleted volunteer.
			{ID: "alloc-3", RotaID: "rota-0", ShiftDate: "2024-12-15", VolunteerID: "ghost", Role: string(model.RoleVolunteer)},
		},
	}

	volunteers := []allocator.Volunteer{
		{ID: "alice", FirstName: "Alice", LastName: "A", Gender: "Female", GroupKey: "Alice A"},
	}

	targetRota := &db.Rotation{ID: "rota-1", Start: "2025-01-05", ShiftCount: 3}

	historicalShifts, err := buildHistoricalShifts(ctx, store, store.rotations, targetRota, volunteers, logger)
	require.NoError(t, err)
	require.Len(t, historicalShifts, 3)

	// Sorted ascending by date, no manual reordering needed.
	assert.Equal(t, "2024-12-01", historicalShifts[0].Date)
	assert.Equal(t, "2024-12-08", historicalShifts[1].Date)
	assert.Equal(t, "2024-12-15", historicalShifts[2].Date)

	// The last shift survives with no groups: Alice (on Dec 8) is NOT on
	// the boundary, and the boundary doesn't fall back to Dec 8.
	assert.Empty(t, historicalShifts[2].AllocatedGroups)
	assert.Len(t, historicalShifts[1].AllocatedGroups, 1)
}

func TestBuildHistoricalShifts_AppliesAlterations(t *testing.T) {
	// History must reflect who actually worked: a volunteer who dropped
	// out via an alteration disappears, and their cover appears.
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{
			{ID: "rota-0", Start: "2024-12-01", ShiftCount: 2},
			{ID: "rota-1", Start: "2025-01-05", ShiftCount: 2},
		},
		allocations: []db.Allocation{
			// Dec 1: Alice worked as published.
			{ID: "alloc-1", RotaID: "rota-0", ShiftDate: "2024-12-01", VolunteerID: "alice", Role: string(model.RoleVolunteer)},
			// Dec 8: Alice dropped out and Dave covered (see alterations).
			{ID: "alloc-2", RotaID: "rota-0", ShiftDate: "2024-12-08", VolunteerID: "alice", Role: string(model.RoleVolunteer)},
		},
		alterations: []db.Alteration{
			{ID: "alt-1", RotaID: "rota-0", ShiftDate: "2024-12-08", Direction: "remove", VolunteerID: "alice", SetTime: "2024-12-05T10:00:00Z"},
			{ID: "alt-2", RotaID: "rota-0", ShiftDate: "2024-12-08", Direction: "add", VolunteerID: "dave", Role: string(model.RoleVolunteer), SetTime: "2024-12-05T10:00:01Z"},
			// Alteration on a different rota: must be ignored.
			{ID: "alt-3", RotaID: "rota-1", ShiftDate: "2024-12-01", Direction: "remove", VolunteerID: "alice", SetTime: "2024-12-05T10:00:02Z"},
		},
	}

	activeVolunteers := []allocator.Volunteer{
		{ID: "alice", FirstName: "Alice", LastName: "A", Gender: "Female", GroupKey: "Alice A"},
		{ID: "dave", FirstName: "Dave", LastName: "D", Gender: "Male", GroupKey: "Dave D"},
	}

	targetRota := &db.Rotation{ID: "rota-1", Start: "2025-01-05", ShiftCount: 2}

	historicalShifts, err := buildHistoricalShifts(ctx, store, store.rotations, targetRota, activeVolunteers, logger)
	require.NoError(t, err)
	require.Len(t, historicalShifts, 2)

	// Dec 1 untouched: Alice worked it (the rota-1 alteration is ignored).
	require.Len(t, historicalShifts[0].AllocatedGroups, 1)
	assert.Equal(t, "alice", historicalShifts[0].AllocatedGroups[0].Members[0].ID)

	// Dec 8 altered: Dave worked it, not Alice.
	require.Len(t, historicalShifts[1].AllocatedGroups, 1)
	assert.Equal(t, "dave", historicalShifts[1].AllocatedGroups[0].Members[0].ID)
}

func TestBuildHistoricalShifts_NoPreviousRota(t *testing.T) {
	// Test that buildHistoricalShifts returns empty array when there's no previous rota
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2025-01-05", ShiftCount: 2}, // Only one rota (current)
		},
	}

	targetRota := &db.Rotation{ID: "rota-1", Start: "2025-01-05", ShiftCount: 2}
	activeVolunteers := []allocator.Volunteer{
		{ID: "alice", FirstName: "Alice", LastName: "A", Gender: "Female"},
	}

	historicalShifts, err := buildHistoricalShifts(ctx, store, store.rotations, targetRota, activeVolunteers, logger)
	require.NoError(t, err)
	assert.Empty(t, historicalShifts, "Should have no historical shifts when there's no previous rota")
}

func TestBuildHistoricalShifts_NoPreviousAllocations(t *testing.T) {
	// Test that buildHistoricalShifts returns empty array when previous rota has no allocations
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{
			{ID: "rota-0", Start: "2024-12-01", ShiftCount: 2}, // Old rota
			{ID: "rota-1", Start: "2025-01-05", ShiftCount: 2}, // Current rota
		},
		allocations: []db.Allocation{}, // No allocations
	}

	targetRota := &db.Rotation{ID: "rota-1", Start: "2025-01-05", ShiftCount: 2}
	activeVolunteers := []allocator.Volunteer{
		{ID: "alice", FirstName: "Alice", LastName: "A", Gender: "Female"},
	}

	historicalShifts, err := buildHistoricalShifts(ctx, store, store.rotations, targetRota, activeVolunteers, logger)
	require.NoError(t, err)
	assert.Empty(t, historicalShifts, "Should have no historical shifts when previous rota has no allocations")
}

func TestBuildHistoricalShifts_CustomEntriesIgnored(t *testing.T) {
	// Test that custom entries (allocations with empty VolunteerID) are ignored
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{
			{ID: "rota-0", Start: "2024-12-01", ShiftCount: 1},
			{ID: "rota-1", Start: "2025-01-05", ShiftCount: 1},
		},
		allocations: []db.Allocation{
			// Regular allocation
			{ID: "alloc-1", RotaID: "rota-0", ShiftDate: "2024-12-01", VolunteerID: "alice", Role: string(model.RoleVolunteer)},
			// Custom entry (should be ignored)
			{ID: "alloc-2", RotaID: "rota-0", ShiftDate: "2024-12-01", VolunteerID: "", CustomEntry: "External John", Role: string(model.RoleVolunteer)},
		},
	}

	activeVolunteers := []allocator.Volunteer{
		{ID: "alice", FirstName: "Alice", LastName: "A", Gender: "Female", GroupKey: ""},
	}

	targetRota := &db.Rotation{ID: "rota-1", Start: "2025-01-05", ShiftCount: 1}

	historicalShifts, err := buildHistoricalShifts(ctx, store, store.rotations, targetRota, activeVolunteers, logger)
	require.NoError(t, err)
	require.Len(t, historicalShifts, 1)

	// Should only have Alice's individual group (custom entry ignored)
	assert.Len(t, historicalShifts[0].AllocatedGroups, 1)
	assert.Equal(t, "Alice A", historicalShifts[0].AllocatedGroups[0].GroupKey)
}
