package rotageneration

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateRotaState_ValidRota(t *testing.T) {
	groupA := &VolunteerGroup{
		GroupKey:  "group_a",
		MaleCount: 1,
		HasTeamLead: true,
		Members: []Volunteer{
			{ID: "tl1", IsTeamLead: true, Gender: "M"},
			{ID: "v1", IsTeamLead: false, Gender: "F"},
		},
	}

	groupB := &VolunteerGroup{
		GroupKey:  "group_b",
		MaleCount: 1,
		HasTeamLead: true,
		Members: []Volunteer{
			{ID: "tl2", IsTeamLead: true, Gender: "M"},
			{ID: "v2", IsTeamLead: false, Gender: "F"},
		},
	}

	state := &RotaState{
		Shifts: []*Shift{
			{
				Index:     0,
				Date:      "2024-01-01",
				Size:      1, // Size is 1 (team leads don't count), group has 1 non-team-lead volunteer
				MaleCount: 1,
				AllocatedGroups: []*VolunteerGroup{groupA},
			},
			{
				Index:     1,
				Date:      "2024-01-08",
				Size:      1, // Size is 1 (team leads don't count), group has 1 non-team-lead volunteer
				MaleCount: 1,
				AllocatedGroups: []*VolunteerGroup{groupB},
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
		GroupKey:  "group_a",
		MaleCount: 0,
		HasTeamLead: false,
		Members: []Volunteer{
			{ID: "v1", IsTeamLead: false, Gender: "F"},
		},
	}

	state := &RotaState{
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
	groupA := &VolunteerGroup{
		GroupKey:  "group_a",
		MaleCount: 1,
		HasTeamLead: true,
		Members: []Volunteer{
			{ID: "tl1", IsTeamLead: true, Gender: "M"},
			{ID: "v1", IsTeamLead: false, Gender: "F"},
		},
	}

	state := &RotaState{
		Shifts: []*Shift{
			{
				Index:           0,
				Date:            "2024-01-01",
				Size:            5, // Underfilled
				MaleCount:       1, // Has male
				AllocatedGroups: []*VolunteerGroup{groupA}, // Has team lead
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
	groupA := &VolunteerGroup{
		GroupKey:  "group_a",
		MaleCount: 1,
		HasTeamLead: true,
		Members: []Volunteer{
			{ID: "tl1", IsTeamLead: true, Gender: "M"},
		},
	}

	state := &RotaState{
		Shifts: []*Shift{
			{
				Index:           0,
				Date:            "2024-01-01",
				Size:            1,
				MaleCount:       1,
				AllocatedGroups: []*VolunteerGroup{groupA},
			},
			{
				Index:           1,
				Date:            "2024-01-08",
				Size:            1,
				MaleCount:       1,
				AllocatedGroups: []*VolunteerGroup{groupA}, // Even with double shift
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
