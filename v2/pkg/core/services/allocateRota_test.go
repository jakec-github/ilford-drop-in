package services

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/clients/formsclient"
	"github.com/jakechorley/ilford-drop-in/pkg/core/allocator"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// mockAllocateRotaStore implements AllocateRotaStore for testing
type mockAllocateRotaStore struct {
	rotations            []db.Rotation
	availabilityRequests []db.AvailabilityRequest
	insertedAllocations  []db.Allocation
	getRotationsErr      error
	getAvailabilityErr   error
	insertAllocationsErr error
}

func (m *mockAllocateRotaStore) GetRotations(ctx context.Context) ([]db.Rotation, error) {
	if m.getRotationsErr != nil {
		return nil, m.getRotationsErr
	}
	return m.rotations, nil
}

func (m *mockAllocateRotaStore) GetAvailabilityRequests(ctx context.Context) ([]db.AvailabilityRequest, error) {
	if m.getAvailabilityErr != nil {
		return nil, m.getAvailabilityErr
	}
	return m.availabilityRequests, nil
}

func (m *mockAllocateRotaStore) InsertAllocations(allocations []db.Allocation) error {
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
			TeamLead:               &allocator.Volunteer{ID: "alice", IsTeamLead: true},
			PreAllocatedVolunteers: []string{"external_john"},
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
		if alloc.Preallocation == "external_john" {
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

	active := filterActiveVolunteers(volunteers)
	assert.Len(t, active, 2)
	assert.Equal(t, "alice", active[0].ID)
	assert.Equal(t, "charlie", active[1].ID)
}

func TestCalcShiftDates(t *testing.T) {
	dates, err := calculateShiftDates("2025-01-05", 4) // Start on Sunday, Jan 5
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
	_, err := calculateShiftDates("invalid-date", 4)
	assert.Error(t, err)
}
