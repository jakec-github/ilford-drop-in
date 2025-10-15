package rotageneration

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
