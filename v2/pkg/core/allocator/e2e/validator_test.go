package e2e

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
			VolunteerGroups:          []*VolunteerGroup{groupA, groupB},
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
			VolunteerGroups:          []*VolunteerGroup{groupA},
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
			VolunteerGroups:          []*VolunteerGroup{groupA},
			ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
		},
		Shifts: []*Shift{
			{
				Index:           0,
				Date:            "2024-01-01",
				Size:            5,                         // Underfilled
				MaleCount:       2,                         // 1 from group + 1 from team lead
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
			VolunteerGroups:          []*VolunteerGroup{groupA},
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
				MaleCount:       2,                         // 1 from group + 1 from team lead
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
