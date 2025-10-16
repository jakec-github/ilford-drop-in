package rotageneration

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateRotaState_ValidRota(t *testing.T) {
	teamLead1 := &Volunteer{ID: "tl1", IsTeamLead: true, Gender: "Male"}
	teamLead2 := &Volunteer{ID: "tl2", IsTeamLead: true, Gender: "Male"}

	groupA := &VolunteerGroup{
		GroupKey:              "group_a",
		MaleCount:             1,
		HasTeamLead:           false,
		AllocatedShiftIndices: []int{0},
		AvailableShiftIndices: []int{0, 1},
		Members: []Volunteer{
			{ID: "v1", IsTeamLead: false, Gender: "F"},
		},
	}

	groupB := &VolunteerGroup{
		GroupKey:              "group_b",
		MaleCount:             1,
		HasTeamLead:           false,
		AllocatedShiftIndices: []int{1},
		AvailableShiftIndices: []int{0, 1},
		Members: []Volunteer{
			{ID: "v2", IsTeamLead: false, Gender: "F"},
		},
	}

	state := &RotaState{
		MaxAllocationFrequency: 1.0, // Frequency ratio 100% with 2 shifts = floor(2 * 1.0) = 2 max allocation
		VolunteerState: &VolunteerState{
		VolunteerGroups:        []*VolunteerGroup{groupA, groupB},
		ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
	},
		Shifts: []*Shift{
			{
				Index:           0,
				Date:            "2024-01-01",
				Size:            1, // Size is 1 (team leads don't count), group has 1 non-team-lead volunteer
				MaleCount:       2, // 1 from group + 1 from team lead
				AllocatedGroups: []*VolunteerGroup{groupA},
				TeamLead:        teamLead1,
			},
			{
				Index:           1,
				Date:            "2024-01-08",
				Size:            1, // Size is 1 (team leads don't count), group has 1 non-team-lead volunteer
				MaleCount:       2, // 1 from group + 1 from team lead
				AllocatedGroups: []*VolunteerGroup{groupB},
				TeamLead:        teamLead2,
			},
		},
	}

	criteria := []Criterion{
		NewShiftSizeCriterion(1.0, 1.0),
		NewTeamLeadCriterion(1.0, 1.0),
		NewMaleBalanceCriterion(1.0, 1.0),
		NewNoDoubleShiftsCriterion(1.0, 1.0),
		NewShiftSpreadCriterion(1.0, 1.0),
	}

	errors := ValidateRotaState(state, criteria)
	assert.Empty(t, errors, "Should have no errors for a valid rota")
}

func TestValidateRotaState_MultipleViolations(t *testing.T) {
	groupA := &VolunteerGroup{
		GroupKey:              "group_a",
		MaleCount:             0,
		HasTeamLead:           false,
		AllocatedShiftIndices: []int{0, 1},
		AvailableShiftIndices: []int{0, 1},
		Members: []Volunteer{
			{ID: "v1", IsTeamLead: false, Gender: "F"},
		},
	}

	state := &RotaState{
		MaxAllocationFrequency: 1.0, // Frequency ratio 100% with 2 shifts = floor(2 * 1.0) = 2 max allocation
		VolunteerState: &VolunteerState{
		VolunteerGroups:        []*VolunteerGroup{groupA},
		ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
	},
		Shifts: []*Shift{
			{
				Index:           0,
				Date:            "2024-01-01",
				Size:            5,
				MaleCount:       0,
				AllocatedGroups: []*VolunteerGroup{groupA}, // Underfilled, no team lead, no males
			},
			{
				Index:           1,
				Date:            "2024-01-08",
				Size:            3,
				MaleCount:       0,
				AllocatedGroups: []*VolunteerGroup{groupA}, // Underfilled, no team lead, no males, double shift
			},
		},
	}

	criteria := []Criterion{
		NewShiftSizeCriterion(1.0, 1.0),
		NewTeamLeadCriterion(1.0, 1.0),
		NewMaleBalanceCriterion(1.0, 1.0),
		NewNoDoubleShiftsCriterion(1.0, 1.0),
	}

	errors := ValidateRotaState(state, criteria)

	// Should detect multiple violations:
	// - 2 shifts underfilled (ShiftSize)
	// - 2 shifts without team leads (TeamLead)
	// - 2 shifts without males (MaleBalance)
	// - 1 double shift violation (NoDoubleShifts)
	assert.Len(t, errors, 7, "Should detect all violations across criteria")

	// Count errors by criterion
	shiftSizeErrors := 0
	teamLeadErrors := 0
	maleBalanceErrors := 0
	noDoubleShiftsErrors := 0

	for _, err := range errors {
		switch err.CriterionName {
		case "ShiftSize":
			shiftSizeErrors++
		case "TeamLead":
			teamLeadErrors++
		case "MaleBalance":
			maleBalanceErrors++
		case "NoDoubleShifts":
			noDoubleShiftsErrors++
		}
	}

	assert.Equal(t, 2, shiftSizeErrors, "Should detect 2 underfilled shifts")
	assert.Equal(t, 2, teamLeadErrors, "Should detect 2 shifts without team leads")
	assert.Equal(t, 2, maleBalanceErrors, "Should detect 2 shifts without males")
	assert.Equal(t, 1, noDoubleShiftsErrors, "Should detect 1 double shift violation")
}

func TestValidateRotaState_NoCriteria(t *testing.T) {
	state := &RotaState{
		VolunteerState: &VolunteerState{
			VolunteerGroups:          []*VolunteerGroup{},
			ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
		},
		Shifts: []*Shift{
			{
				Index:           0,
				Date:            "2024-01-01",
				Size:            5,
				AllocatedGroups: []*VolunteerGroup{},
			},
		},
	}

	errors := ValidateRotaState(state, []Criterion{})
	assert.Empty(t, errors, "Should have no errors when no criteria are provided")
}

func TestValidateRotaState_OnlySomeCriteriaViolated(t *testing.T) {
	teamLead1 := &Volunteer{ID: "tl1", IsTeamLead: true, Gender: "Male"}

	groupA := &VolunteerGroup{
		GroupKey:              "group_a",
		MaleCount:             1,
		HasTeamLead:           false,
		AllocatedShiftIndices: []int{0},
		AvailableShiftIndices: []int{0},
		Members: []Volunteer{
			{ID: "v1", IsTeamLead: false, Gender: "F"},
		},
	}

	state := &RotaState{
		MaxAllocationFrequency: 1.0, // Frequency ratio 100% with 1 shift = floor(1 * 1.0) = 1 max allocation
		VolunteerState: &VolunteerState{
		VolunteerGroups:        []*VolunteerGroup{groupA},
		ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
	},
		Shifts: []*Shift{
			{
				Index:           0,
				Date:            "2024-01-01",
				Size:            5, // Underfilled
				MaleCount:       2, // 1 from group + 1 from team lead
				AllocatedGroups: []*VolunteerGroup{groupA}, // Has team lead
				TeamLead:        teamLead1,
			},
		},
	}

	criteria := []Criterion{
		NewShiftSizeCriterion(1.0, 1.0),      // Will fail
		NewTeamLeadCriterion(1.0, 1.0),       // Will pass
		NewMaleBalanceCriterion(1.0, 1.0),    // Will pass
		NewNoDoubleShiftsCriterion(1.0, 1.0), // Will pass
	}

	errors := ValidateRotaState(state, criteria)
	assert.Len(t, errors, 1, "Should only detect violations from criteria that fail")
	assert.Equal(t, "ShiftSize", errors[0].CriterionName)
}

func TestValidateRotaState_ShiftSpreadNeverFails(t *testing.T) {
	// ShiftSpread is optimization-only, so it should never produce errors
	teamLead1 := &Volunteer{ID: "tl1", IsTeamLead: true, Gender: "Male"}
	teamLead2 := &Volunteer{ID: "tl2", IsTeamLead: true, Gender: "Male"}

	groupA := &VolunteerGroup{
		GroupKey:              "group_a",
		MaleCount:             1,
		HasTeamLead:           false,
		AllocatedShiftIndices: []int{0, 1},
		AvailableShiftIndices: []int{0, 1},
		Members:               []Volunteer{},
	}

	state := &RotaState{
		MaxAllocationFrequency: 1.0, // Frequency ratio 100% with 2 shifts = floor(2 * 1.0) = 2 max allocation
		VolunteerState: &VolunteerState{
		VolunteerGroups:        []*VolunteerGroup{groupA},
		ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
	},
		Shifts: []*Shift{
			{
				Index:           0,
				Date:            "2024-01-01",
				Size:            0,
				MaleCount:       2, // 1 from group + 1 from team lead
				AllocatedGroups: []*VolunteerGroup{groupA},
				TeamLead:        teamLead1,
			},
			{
				Index:           1,
				Date:            "2024-01-08",
				Size:            0,
				MaleCount:       2, // 1 from group + 1 from team lead
				AllocatedGroups: []*VolunteerGroup{groupA}, // Even with double shift
				TeamLead:        teamLead2,
			},
		},
	}

	// Only test ShiftSpread criterion
	criteria := []Criterion{
		NewShiftSpreadCriterion(1.0, 1.0),
	}

	errors := ValidateRotaState(state, criteria)
	assert.Empty(t, errors, "ShiftSpread should never produce validation errors")
}

// Core Invariant Tests

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
		VolunteerGroups:        []*VolunteerGroup{groupA},
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
		VolunteerGroups:        []*VolunteerGroup{groupA},
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
		VolunteerGroups:        []*VolunteerGroup{groupA},
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
		VolunteerGroups:        []*VolunteerGroup{groupA},
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
		VolunteerGroups:        []*VolunteerGroup{groupA},
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
		VolunteerGroups:        []*VolunteerGroup{groupA},
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
