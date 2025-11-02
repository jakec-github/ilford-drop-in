package allocator

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsShiftValidForGroup_GroupNotAvailable(t *testing.T) {
	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0, Date: "2024-01-01"},
			{Index: 1, Date: "2024-01-08"},
		},
	}

	group := &VolunteerGroup{
		GroupKey:              "group_a",
		AvailableShiftIndices: []int{1}, // Only available for shift 1
		AllocatedShiftIndices: []int{},
	}

	shift := state.Shifts[0] // Shift 0

	// Group is not available for shift 0
	valid := IsShiftValidForGroup(state, group, shift, []Criterion{})
	assert.False(t, valid, "Should return false for unavailable shift")
}

func TestIsShiftValidForGroup_GroupAlreadyAllocated(t *testing.T) {
	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0, Date: "2024-01-01"},
			{Index: 1, Date: "2024-01-08"},
		},
	}

	group := &VolunteerGroup{
		GroupKey:              "group_a",
		AvailableShiftIndices: []int{0, 1},
		AllocatedShiftIndices: []int{0}, // Already allocated to shift 0
	}

	shift := state.Shifts[0] // Shift 0

	// Group is already allocated to shift 0
	valid := IsShiftValidForGroup(state, group, shift, []Criterion{})
	assert.False(t, valid, "Should return false for already allocated shift")
}

func TestIsShiftValidForGroup_CriterionMarksInvalid(t *testing.T) {
	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0, Date: "2024-01-01"},
		},
	}

	group := &VolunteerGroup{
		GroupKey:              "group_a",
		AvailableShiftIndices: []int{0},
		AllocatedShiftIndices: []int{},
	}

	shift := state.Shifts[0]

	// Criterion that marks shift as invalid
	customCriterion := &mockCriterionWithValidity{
		mockCriterion: mockCriterion{
			name:           "custom",
			affinityValue:  1.0,
			affinityWeight: 1.0,
		},
		isValid: false,
	}

	valid := IsShiftValidForGroup(state, group, shift, []Criterion{customCriterion})
	assert.False(t, valid, "Should return false when criterion marks shift as invalid")
}

func TestIsShiftValidForGroup_AllValid(t *testing.T) {
	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0, Date: "2024-01-01"},
		},
	}

	group := &VolunteerGroup{
		GroupKey:              "group_a",
		AvailableShiftIndices: []int{0},
		AllocatedShiftIndices: []int{},
	}

	shift := state.Shifts[0]

	criteria := []Criterion{
		&mockCriterionWithValidity{
			mockCriterion: mockCriterion{
				name:           "valid1",
				affinityValue:  0.5,
				affinityWeight: 2.0,
			},
			isValid: true,
		},
		&mockCriterionWithValidity{
			mockCriterion: mockCriterion{
				name:           "valid2",
				affinityValue:  0.5,
				affinityWeight: 2.0,
			},
			isValid: true,
		},
	}

	valid := IsShiftValidForGroup(state, group, shift, criteria)
	assert.True(t, valid, "Should return true when all checks pass")
}

func TestCalculateShiftAffinity_GroupNotAvailable(t *testing.T) {
	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0, Date: "2024-01-01"},
			{Index: 1, Date: "2024-01-08"},
		},
	}

	group := &VolunteerGroup{
		GroupKey:              "group_a",
		AvailableShiftIndices: []int{1}, // Only available for shift 1
		AllocatedShiftIndices: []int{},
	}

	shift := state.Shifts[0] // Shift 0

	// Group is not available for shift 0
	affinity := CalculateShiftAffinity(state, group, shift, []Criterion{})

	assert.Equal(t, 0.0, affinity, "Should return 0 for unavailable shift")
}

func TestCalculateShiftAffinity_GroupAlreadyAllocated(t *testing.T) {
	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0, Date: "2024-01-01"},
			{Index: 1, Date: "2024-01-08"},
		},
	}

	group := &VolunteerGroup{
		GroupKey:              "group_a",
		AvailableShiftIndices: []int{0, 1},
		AllocatedShiftIndices: []int{0}, // Already allocated to shift 0
	}

	shift := state.Shifts[0] // Shift 0

	// Group is already allocated to shift 0
	affinity := CalculateShiftAffinity(state, group, shift, []Criterion{})

	assert.Equal(t, 0.0, affinity, "Should return 0 for already allocated shift")
}

func TestCalculateShiftAffinity_InvalidShift(t *testing.T) {
	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0, Date: "2024-01-01"},
		},
	}

	group := &VolunteerGroup{
		GroupKey:              "group_a",
		AvailableShiftIndices: []int{0},
		AllocatedShiftIndices: []int{},
	}

	shift := state.Shifts[0]

	// Criterion that marks shift as invalid
	customCriterion := &mockCriterionWithValidity{
		mockCriterion: mockCriterion{
			name:           "custom",
			affinityValue:  1.0,
			affinityWeight: 1.0,
		},
		isValid: false,
	}

	affinity := CalculateShiftAffinity(state, group, shift, []Criterion{customCriterion})

	assert.Equal(t, 0.0, affinity, "Should return 0 when criterion marks shift as invalid")
}

func TestCalculateShiftAffinity_NoCriteria(t *testing.T) {
	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0, Date: "2024-01-01"},
		},
	}

	group := &VolunteerGroup{
		GroupKey:              "group_a",
		AvailableShiftIndices: []int{0},
		AllocatedShiftIndices: []int{},
	}

	shift := state.Shifts[0]

	// No criteria - should return 0 (sum of nothing)
	affinity := CalculateShiftAffinity(state, group, shift, []Criterion{})

	assert.Equal(t, 0.0, affinity, "Should return 0 when no criteria provided")
}

func TestCalculateShiftAffinity_WithCriteria(t *testing.T) {
	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0, Date: "2024-01-01"},
		},
	}

	group := &VolunteerGroup{
		GroupKey:              "group_a",
		AvailableShiftIndices: []int{0},
		AllocatedShiftIndices: []int{},
	}

	shift := state.Shifts[0]

	criteria := []Criterion{
		&mockCriterion{
			name:           "criterion1",
			affinityValue:  0.8,
			affinityWeight: 10.0,
		},
		&mockCriterion{
			name:           "criterion2",
			affinityValue:  0.5,
			affinityWeight: 5.0,
		},
	}

	affinity := CalculateShiftAffinity(state, group, shift, criteria)

	// Expected: (0.8 * 10.0) + (0.5 * 5.0) = 8.0 + 2.5 = 10.5
	assert.Equal(t, 10.5, affinity)
}

func TestCalculateShiftAffinity_MixedAffinityValues(t *testing.T) {
	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0, Date: "2024-01-01"},
		},
	}

	group := &VolunteerGroup{
		GroupKey:              "group_a",
		AvailableShiftIndices: []int{0},
		AllocatedShiftIndices: []int{},
	}

	shift := state.Shifts[0]

	criteria := []Criterion{
		&mockCriterion{
			name:           "high_affinity",
			affinityValue:  1.0, // Maximum affinity
			affinityWeight: 10.0,
		},
		&mockCriterion{
			name:           "low_affinity",
			affinityValue:  0.0, // Minimum affinity
			affinityWeight: 5.0,
		},
		&mockCriterion{
			name:           "medium_affinity",
			affinityValue:  0.5,
			affinityWeight: 2.0,
		},
	}

	affinity := CalculateShiftAffinity(state, group, shift, criteria)

	// Expected: (1.0 * 10.0) + (0.0 * 5.0) + (0.5 * 2.0) = 10.0 + 0.0 + 1.0 = 11.0
	assert.Equal(t, 11.0, affinity)
}

func TestCalculateShiftAffinity_MultipleValidityChecks(t *testing.T) {
	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0, Date: "2024-01-01"},
		},
	}

	group := &VolunteerGroup{
		GroupKey:              "group_a",
		AvailableShiftIndices: []int{0},
		AllocatedShiftIndices: []int{},
	}

	shift := state.Shifts[0]

	// All valid - should calculate affinity
	criteria1 := []Criterion{
		&mockCriterionWithValidity{
			mockCriterion: mockCriterion{
				name:           "valid1",
				affinityValue:  0.5,
				affinityWeight: 2.0,
			},
			isValid: true,
		},
		&mockCriterionWithValidity{
			mockCriterion: mockCriterion{
				name:           "valid2",
				affinityValue:  0.5,
				affinityWeight: 2.0,
			},
			isValid: true,
		},
	}

	affinity1 := CalculateShiftAffinity(state, group, shift, criteria1)
	assert.Equal(t, 2.0, affinity1, "Should sum affinity when all criteria are valid")

	// One invalid - should return 0
	criteria2 := []Criterion{
		&mockCriterionWithValidity{
			mockCriterion: mockCriterion{
				name:           "valid",
				affinityValue:  1.0,
				affinityWeight: 10.0,
			},
			isValid: true,
		},
		&mockCriterionWithValidity{
			mockCriterion: mockCriterion{
				name:           "invalid",
				affinityValue:  1.0,
				affinityWeight: 10.0,
			},
			isValid: false, // This makes the shift invalid
		},
	}

	affinity2 := CalculateShiftAffinity(state, group, shift, criteria2)
	assert.Equal(t, 0.0, affinity2, "Should return 0 when any criterion marks shift as invalid")
}

// mockCriterionWithValidity extends mockCriterion to allow controlling IsShiftValid
type mockCriterionWithValidity struct {
	mockCriterion
	isValid bool
}

func (m *mockCriterionWithValidity) IsShiftValid(state *RotaState, group *VolunteerGroup, shift *Shift) bool {
	return m.isValid
}

func (m *mockCriterionWithValidity) ValidateRotaState(state *RotaState) []ShiftValidationError {
	return nil
}

func TestAllocate_ClosedShifts(t *testing.T) {
	// Test that closed shifts are skipped during allocation and treated as "full"
	config := AllocationConfig{
		Volunteers: []Volunteer{
			{ID: "v1", FirstName: "Alice", LastName: "Smith", Gender: "Female", IsTeamLead: true, GroupKey: ""},
			{ID: "v2", FirstName: "Bob", LastName: "Jones", Gender: "Male", IsTeamLead: false, GroupKey: ""},
			{ID: "v3", FirstName: "Charlie", LastName: "Brown", Gender: "Male", IsTeamLead: false, GroupKey: ""},
			{ID: "v4", FirstName: "Diana", LastName: "Green", Gender: "Female", IsTeamLead: false, GroupKey: ""},
		},
		Availability: []VolunteerAvailability{
			{VolunteerID: "v1", HasResponded: true, UnavailableShiftIndices: []int{}},
			{VolunteerID: "v2", HasResponded: true, UnavailableShiftIndices: []int{}},
			{VolunteerID: "v3", HasResponded: true, UnavailableShiftIndices: []int{}},
			{VolunteerID: "v4", HasResponded: true, UnavailableShiftIndices: []int{}},
		},
		ShiftDates: []string{"2025-01-05", "2025-01-12", "2025-01-19"},
		Overrides: []ShiftOverride{
			{
				AppliesTo: func(date string) bool {
					return date == "2025-01-12" // Middle shift is closed
				},
				Closed: true,
			},
		},
		DefaultShiftSize:               2,
		MaxAllocationFrequency:         1.0,
		HistoricalShifts:               []*Shift{},
		Criteria: []Criterion{
			&mockCriterion{
				name:           "test",
				affinityValue:  1.0,
				affinityWeight: 1.0,
			},
		},
		WeightCurrentRotaUrgency:       1.0,
		WeightOverallFrequencyFairness: 1.0,
		WeightPromoteGroup:             1.0,
	}

	outcome, err := Allocate(config)
	assert.NoError(t, err)
	assert.NotNil(t, outcome)

	// Verify the middle shift is marked as closed
	assert.True(t, outcome.State.Shifts[1].Closed, "Middle shift should be closed")

	// Verify closed shift has no allocations
	assert.Empty(t, outcome.State.Shifts[1].AllocatedGroups, "Closed shift should have no allocated groups")
	assert.Nil(t, outcome.State.Shifts[1].TeamLead, "Closed shift should have no team lead")

	// Verify open shifts have allocations
	assert.NotEmpty(t, outcome.State.Shifts[0].AllocatedGroups, "First shift should have allocations")
	assert.NotEmpty(t, outcome.State.Shifts[2].AllocatedGroups, "Third shift should have allocations")

	// Allocation should be considered successful despite closed shift being empty
	assert.True(t, outcome.Success, "Allocation should succeed with closed shifts")
}

func TestAllocate_MultipleClosedShifts(t *testing.T) {
	// Test that multiple closed shifts work correctly
	config := AllocationConfig{
		Volunteers: []Volunteer{
			{ID: "v1", FirstName: "Alice", LastName: "Smith", Gender: "Female", IsTeamLead: true, GroupKey: ""},
			{ID: "v2", FirstName: "Bob", LastName: "Jones", Gender: "Male", IsTeamLead: false, GroupKey: ""},
			{ID: "v3", FirstName: "Charlie", LastName: "Brown", Gender: "Male", IsTeamLead: false, GroupKey: ""},
			{ID: "v4", FirstName: "Diana", LastName: "Green", Gender: "Female", IsTeamLead: false, GroupKey: ""},
		},
		Availability: []VolunteerAvailability{
			{VolunteerID: "v1", HasResponded: true, UnavailableShiftIndices: []int{}},
			{VolunteerID: "v2", HasResponded: true, UnavailableShiftIndices: []int{}},
			{VolunteerID: "v3", HasResponded: true, UnavailableShiftIndices: []int{}},
			{VolunteerID: "v4", HasResponded: true, UnavailableShiftIndices: []int{}},
		},
		ShiftDates: []string{"2025-01-05", "2025-01-12", "2025-01-19", "2025-01-26"},
		Overrides: []ShiftOverride{
			{
				AppliesTo: func(date string) bool {
					return date == "2025-01-12" || date == "2025-01-26"
				},
				Closed: true,
			},
		},
		DefaultShiftSize:               2,
		MaxAllocationFrequency:         1.0,
		HistoricalShifts:               []*Shift{},
		Criteria: []Criterion{
			&mockCriterion{
				name:           "test",
				affinityValue:  1.0,
				affinityWeight: 1.0,
			},
		},
		WeightCurrentRotaUrgency:       1.0,
		WeightOverallFrequencyFairness: 1.0,
		WeightPromoteGroup:             1.0,
	}

	outcome, err := Allocate(config)
	assert.NoError(t, err)
	assert.NotNil(t, outcome)

	// Verify both closed shifts are empty
	assert.True(t, outcome.State.Shifts[1].Closed)
	assert.Empty(t, outcome.State.Shifts[1].AllocatedGroups)

	assert.True(t, outcome.State.Shifts[3].Closed)
	assert.Empty(t, outcome.State.Shifts[3].AllocatedGroups)

	// Verify open shifts have allocations
	assert.NotEmpty(t, outcome.State.Shifts[0].AllocatedGroups, "First shift should have allocations")
	assert.NotEmpty(t, outcome.State.Shifts[2].AllocatedGroups, "Third shift should have allocations")

	assert.True(t, outcome.Success, "Allocation should succeed with multiple closed shifts")
}
