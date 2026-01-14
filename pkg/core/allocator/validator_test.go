package allocator

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateCoreInvariants_OverAllocation(t *testing.T) {
	groupA := &VolunteerGroup{
		GroupKey:              "group_a",
		AllocatedShiftIndices: []int{0, 1, 2}, // Allocated 3 times
		AvailableShiftIndices: []int{0, 1, 2},
		HasTeamLead:           false,
		MaleCount:             1,
	}

	state := &RotaState{
		MaxAllocationFrequency: 0.5, // Frequency ratio 50% with 3 shifts = floor(3 * 0.5) = 1 max allocation
		VolunteerState: &VolunteerState{
			VolunteerGroups:          []*VolunteerGroup{groupA},
			ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
		},
		Shifts: []*Shift{
			{Index: 0, AllocatedGroups: []*VolunteerGroup{groupA}, MaleCount: 1},
			{Index: 1, AllocatedGroups: []*VolunteerGroup{groupA}, MaleCount: 1},
			{Index: 2, AllocatedGroups: []*VolunteerGroup{groupA}, MaleCount: 1},
		},
	}

	errors := validateCoreInvariants(state)
	assert.NotEmpty(t, errors, "Should detect over-allocation")

	found := false
	for _, err := range errors {
		if err.CriterionName == "CoreInvariant" && err.Description != "" {
			assert.Contains(t, err.Description, "group_a")
			assert.Contains(t, err.Description, "allocated to 3 shifts but max is 1")
			found = true
		}
	}
	assert.True(t, found, "Should have over-allocation error")
}

func TestValidateCoreInvariants_DuplicateAllocation(t *testing.T) {
	groupA := &VolunteerGroup{
		GroupKey:              "group_a",
		AllocatedShiftIndices: []int{0},
		AvailableShiftIndices: []int{0},
		HasTeamLead:           false,
		MaleCount:             1,
	}

	state := &RotaState{
		MaxAllocationFrequency: 1.0, // Frequency ratio 100% with 1 shift = floor(1 * 1.0) = 1 max allocation
		VolunteerState: &VolunteerState{
			VolunteerGroups:          []*VolunteerGroup{groupA},
			ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
		},
		Shifts: []*Shift{
			{
				Index:           0,
				Date:            "2024-01-01",
				AllocatedGroups: []*VolunteerGroup{groupA, groupA}, // Duplicate!
				MaleCount:       2,
			},
		},
	}

	errors := validateCoreInvariants(state)
	assert.NotEmpty(t, errors, "Should detect duplicate allocation")

	found := false
	for _, err := range errors {
		if err.CriterionName == "CoreInvariant" && err.ShiftIndex == 0 {
			assert.Contains(t, err.Description, "group_a")
			assert.Contains(t, err.Description, "allocated multiple times to the same shift")
			found = true
		}
	}
	assert.True(t, found, "Should have duplicate allocation error")
}

func TestValidateCoreInvariants_AvailabilityViolation(t *testing.T) {
	groupA := &VolunteerGroup{
		GroupKey:              "group_a",
		AllocatedShiftIndices: []int{0},
		AvailableShiftIndices: []int{1, 2}, // NOT available for shift 0
		HasTeamLead:           false,
		MaleCount:             1,
	}

	state := &RotaState{
		MaxAllocationFrequency: 1.0, // Frequency ratio 100% with 1 shift = floor(1 * 1.0) = 1 max allocation
		VolunteerState: &VolunteerState{
			VolunteerGroups:          []*VolunteerGroup{groupA},
			ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
		},
		Shifts: []*Shift{
			{
				Index:           0,
				Date:            "2024-01-01",
				AllocatedGroups: []*VolunteerGroup{groupA}, // But allocated anyway
				MaleCount:       1,
			},
		},
	}

	errors := validateCoreInvariants(state)
	assert.NotEmpty(t, errors, "Should detect availability violation")

	found := false
	for _, err := range errors {
		if err.CriterionName == "CoreInvariant" && err.ShiftIndex == 0 {
			assert.Contains(t, err.Description, "group_a")
			assert.Contains(t, err.Description, "not available for it")
			found = true
		}
	}
	assert.True(t, found, "Should have availability violation error")
}

func TestValidateCoreInvariants_AllocatedIndicesMismatch(t *testing.T) {
	groupA := &VolunteerGroup{
		GroupKey:              "group_a",
		AllocatedShiftIndices: []int{0, 1}, // Says it's allocated to 0 and 1
		AvailableShiftIndices: []int{0, 1, 2},
		HasTeamLead:           false,
		MaleCount:             1,
	}

	state := &RotaState{
		MaxAllocationFrequency: 1.0, // Frequency ratio 100% with 2 shifts = floor(2 * 1.0) = 2 max allocation
		VolunteerState: &VolunteerState{
			VolunteerGroups:          []*VolunteerGroup{groupA},
			ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
		},
		Shifts: []*Shift{
			{
				Index:           0,
				Date:            "2024-01-01",
				AllocatedGroups: []*VolunteerGroup{groupA}, // Only actually in shift 0
				MaleCount:       1,
			},
			{
				Index:           1,
				Date:            "2024-01-08",
				AllocatedGroups: []*VolunteerGroup{}, // NOT in shift 1
				MaleCount:       0,
			},
		},
	}

	errors := validateCoreInvariants(state)
	assert.NotEmpty(t, errors, "Should detect allocated indices mismatch")

	found := false
	for _, err := range errors {
		if err.CriterionName == "CoreInvariant" {
			assert.Contains(t, err.Description, "group_a")
			assert.Contains(t, err.Description, "AllocatedShiftIndices")
			found = true
		}
	}
	assert.True(t, found, "Should have allocated indices mismatch error")
}

func TestValidateCoreInvariants_MaleCountFieldMismatch(t *testing.T) {
	groupA := &VolunteerGroup{
		GroupKey:              "group_a",
		AllocatedShiftIndices: []int{0},
		AvailableShiftIndices: []int{0},
		HasTeamLead:           false,
		MaleCount:             2, // Group has 2 males
	}

	state := &RotaState{
		MaxAllocationFrequency: 1.0, // Frequency ratio 100% with 1 shift = floor(1 * 1.0) = 1 max allocation
		VolunteerState: &VolunteerState{
			VolunteerGroups:          []*VolunteerGroup{groupA},
			ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
		},
		Shifts: []*Shift{
			{
				Index:           0,
				Date:            "2024-01-01",
				AllocatedGroups: []*VolunteerGroup{groupA},
				MaleCount:       1, // But shift says only 1 male!
			},
		},
	}

	errors := validateCoreInvariants(state)
	assert.NotEmpty(t, errors, "Should detect male count field mismatch")

	found := false
	for _, err := range errors {
		if err.CriterionName == "CoreInvariant" && err.ShiftIndex == 0 {
			assert.Contains(t, err.Description, "MaleCount")
			assert.Contains(t, err.Description, "is 1 but actual male count from groups is 2")
			found = true
		}
	}
	assert.True(t, found, "Should have male count field mismatch error")
}

func TestValidateCoreInvariants_AllValid(t *testing.T) {
	groupA := &VolunteerGroup{
		GroupKey:              "group_a",
		AllocatedShiftIndices: []int{0},
		AvailableShiftIndices: []int{0, 1},
		HasTeamLead:           true,
		MaleCount:             1,
		Members: []Volunteer{
			{ID: "tl1", IsTeamLead: true, Gender: "Male"},
		},
	}

	state := &RotaState{
		MaxAllocationFrequency: 1.0, // Frequency ratio 100% with 1 shift = floor(1 * 1.0) = 1 max allocation
		VolunteerState: &VolunteerState{
			VolunteerGroups:          []*VolunteerGroup{groupA},
			ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
		},
		Shifts: []*Shift{
			{
				Index:           0,
				Date:            "2024-01-01",
				Size:            1,
				AllocatedGroups: []*VolunteerGroup{groupA},
				TeamLead:        &Volunteer{ID: "tl1", IsTeamLead: true, Gender: "Male"},
				MaleCount:       1,
			},
		},
	}

	errors := validateCoreInvariants(state)
	assert.Empty(t, errors, "Should have no errors for valid state")
}
