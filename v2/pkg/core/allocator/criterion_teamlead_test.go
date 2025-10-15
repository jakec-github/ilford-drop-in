package rotageneration

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTeamLeadCriterion_Name(t *testing.T) {
	criterion := NewTeamLeadCriterion(1.0, 1.0)
	assert.Equal(t, "TeamLead", criterion.Name())
}

func TestTeamLeadCriterion_Weights(t *testing.T) {
	criterion := NewTeamLeadCriterion(5.0, 10.0)
	assert.Equal(t, 5.0, criterion.GroupWeight())
	assert.Equal(t, 10.0, criterion.AffinityWeight())
}

func TestTeamLeadCriterion_PromoteVolunteerGroup_WithTeamLead(t *testing.T) {
	criterion := NewTeamLeadCriterion(1.0, 1.0)
	state := &RotaState{}

	group := &VolunteerGroup{
		HasTeamLead: true,
	}

	promotion := criterion.PromoteVolunteerGroup(state, group)
	assert.Equal(t, 1.0, promotion)
}

func TestTeamLeadCriterion_PromoteVolunteerGroup_WithoutTeamLead(t *testing.T) {
	criterion := NewTeamLeadCriterion(1.0, 1.0)
	state := &RotaState{}

	group := &VolunteerGroup{
		HasTeamLead: false,
	}

	promotion := criterion.PromoteVolunteerGroup(state, group)
	assert.Equal(t, 0.0, promotion)
}

func TestTeamLeadCriterion_IsShiftValid_NoTeamLeadYet(t *testing.T) {
	criterion := NewTeamLeadCriterion(1.0, 1.0)
	state := &RotaState{}

	shift := &Shift{
		TeamLead: nil, // No team lead yet
	}

	groupWithTeamLead := &VolunteerGroup{
		HasTeamLead: true,
	}

	groupWithoutTeamLead := &VolunteerGroup{
		HasTeamLead: false,
	}

	// Both should be valid
	assert.True(t, criterion.IsShiftValid(state, groupWithTeamLead, shift))
	assert.True(t, criterion.IsShiftValid(state, groupWithoutTeamLead, shift))
}

func TestTeamLeadCriterion_IsShiftValid_TeamLeadAlreadyAssigned(t *testing.T) {
	criterion := NewTeamLeadCriterion(1.0, 1.0)
	state := &RotaState{}

	shift := &Shift{
		TeamLead: &Volunteer{ID: "tl1", IsTeamLead: true}, // Team lead already assigned
	}

	groupWithTeamLead := &VolunteerGroup{
		HasTeamLead: true,
	}

	groupWithoutTeamLead := &VolunteerGroup{
		HasTeamLead: false,
	}

	// Group with team lead should be invalid
	assert.False(t, criterion.IsShiftValid(state, groupWithTeamLead, shift))

	// Group without team lead should still be valid
	assert.True(t, criterion.IsShiftValid(state, groupWithoutTeamLead, shift))
}

func TestTeamLeadCriterion_CalculateShiftAffinity_GroupWithoutTeamLead(t *testing.T) {
	criterion := NewTeamLeadCriterion(1.0, 1.0)

	group := &VolunteerGroup{
		HasTeamLead: false,
	}

	state := &RotaState{
		VolunteerGroups: []*VolunteerGroup{group},
	}

	shift := &Shift{
		Index:                  0,
		TeamLead:               nil,
		AvailableGroupIndices:  []int{0},
	}

	// Should return 0 for groups without team leads
	affinity := criterion.CalculateShiftAffinity(state, group, shift)
	assert.Equal(t, 0.0, affinity)
}

func TestTeamLeadCriterion_CalculateShiftAffinity_ShiftAlreadyHasTeamLead(t *testing.T) {
	criterion := NewTeamLeadCriterion(1.0, 1.0)

	group := &VolunteerGroup{
		GroupKey:    "group_a",
		HasTeamLead: true,
	}

	state := &RotaState{
		VolunteerGroups: []*VolunteerGroup{group},
	}

	shift := &Shift{
		Index:                  0,
		TeamLead:               &Volunteer{ID: "tl1", IsTeamLead: true}, // Already has team lead
		AvailableGroupIndices:  []int{0},
	}

	// Should return 0 as a safety check
	affinity := criterion.CalculateShiftAffinity(state, group, shift)
	assert.Equal(t, 0.0, affinity)
}

func TestTeamLeadCriterion_CalculateShiftAffinity_ManyTeamLeadsAvailable(t *testing.T) {
	criterion := NewTeamLeadCriterion(1.0, 1.0)

	// Create 10 groups with team leads
	groups := make([]*VolunteerGroup, 10)
	for i := 0; i < 10; i++ {
		groups[i] = &VolunteerGroup{
			GroupKey:    string(rune('a' + i)),
			HasTeamLead: true,
		}
	}

	state := &RotaState{
		VolunteerGroups: groups,
	}

	shift := &Shift{
		Index:                  0,
		TeamLead:               nil,
		AvailableGroupIndices:  []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, // All 10 available
	}

	// Affinity: 1 / 10 = 0.1 (low priority - many team leads available)
	affinity := criterion.CalculateShiftAffinity(state, groups[0], shift)
	assert.Equal(t, 0.1, affinity)
}

func TestTeamLeadCriterion_CalculateShiftAffinity_FewTeamLeadsAvailable(t *testing.T) {
	criterion := NewTeamLeadCriterion(1.0, 1.0)

	// Create 2 groups with team leads
	groups := make([]*VolunteerGroup, 2)
	for i := 0; i < 2; i++ {
		groups[i] = &VolunteerGroup{
			GroupKey:    string(rune('a' + i)),
			HasTeamLead: true,
		}
	}

	state := &RotaState{
		VolunteerGroups: groups,
	}

	shift := &Shift{
		Index:                  0,
		TeamLead:               nil,
		AvailableGroupIndices:  []int{0, 1}, // Only 2 available
	}

	// Affinity: 1 / 2 = 0.5 (moderate priority)
	affinity := criterion.CalculateShiftAffinity(state, groups[0], shift)
	assert.Equal(t, 0.5, affinity)
}

func TestTeamLeadCriterion_CalculateShiftAffinity_OnlyOneTeamLeadAvailable(t *testing.T) {
	criterion := NewTeamLeadCriterion(1.0, 1.0)

	group := &VolunteerGroup{
		GroupKey:    "group_a",
		HasTeamLead: true,
	}

	state := &RotaState{
		VolunteerGroups: []*VolunteerGroup{group},
	}

	shift := &Shift{
		Index:                  0,
		TeamLead:               nil,
		AvailableGroupIndices:  []int{0}, // Only 1 available
	}

	// Affinity: 1 / 1 = 1.0 (urgent!)
	affinity := criterion.CalculateShiftAffinity(state, group, shift)
	assert.Equal(t, 1.0, affinity)
}

func TestTeamLeadCriterion_CalculateShiftAffinity_ExcludesExhaustedGroups(t *testing.T) {
	criterion := NewTeamLeadCriterion(1.0, 1.0)

	// Create 5 groups with team leads
	groups := make([]*VolunteerGroup, 5)
	for i := 0; i < 5; i++ {
		groups[i] = &VolunteerGroup{
			GroupKey:    string(rune('a' + i)),
			HasTeamLead: true,
		}
	}

	state := &RotaState{
		VolunteerGroups:       groups,
		ExhaustedGroupIndices: []int{1, 2, 3}, // 3 groups exhausted
	}

	shift := &Shift{
		Index:                  0,
		TeamLead:               nil,
		AvailableGroupIndices:  []int{0, 1, 2, 3, 4}, // All 5 originally available
	}

	// Should only count non-exhausted groups: 0, 4
	// Affinity: 1 / 2 = 0.5
	affinity := criterion.CalculateShiftAffinity(state, groups[0], shift)
	assert.Equal(t, 0.5, affinity)
}

func TestTeamLeadCriterion_CalculateShiftAffinity_ExcludesAllocatedGroups(t *testing.T) {
	criterion := NewTeamLeadCriterion(1.0, 1.0)

	// Create 3 groups with team leads
	groups := make([]*VolunteerGroup, 3)
	for i := 0; i < 3; i++ {
		groups[i] = &VolunteerGroup{
			GroupKey:    string(rune('a' + i)),
			HasTeamLead: true,
		}
	}

	// One group already allocated to this shift
	allocatedGroup := &VolunteerGroup{
		GroupKey:    "b",
		HasTeamLead: true,
	}

	state := &RotaState{
		VolunteerGroups: groups,
	}

	shift := &Shift{
		Index:                  0,
		TeamLead:               nil,
		AllocatedGroups:        []*VolunteerGroup{allocatedGroup}, // Group 'b' already allocated
		AvailableGroupIndices:  []int{0, 1, 2},
	}

	// Should only count groups not already allocated: 'a', 'c'
	// Affinity: 1 / 2 = 0.5
	affinity := criterion.CalculateShiftAffinity(state, groups[0], shift)
	assert.Equal(t, 0.5, affinity)
}

func TestTeamLeadCriterion_CalculateShiftAffinity_MixedGroupsOnlyCountsTeamLeads(t *testing.T) {
	criterion := NewTeamLeadCriterion(1.0, 1.0)

	// 3 groups with team leads
	teamLeadGroups := make([]*VolunteerGroup, 3)
	for i := 0; i < 3; i++ {
		teamLeadGroups[i] = &VolunteerGroup{
			GroupKey:    string(rune('a' + i)),
			HasTeamLead: true,
		}
	}

	// 5 groups without team leads
	nonTeamLeadGroups := make([]*VolunteerGroup, 5)
	for i := 0; i < 5; i++ {
		nonTeamLeadGroups[i] = &VolunteerGroup{
			GroupKey:    string(rune('d' + i)),
			HasTeamLead: false,
		}
	}

	allGroups := append(teamLeadGroups, nonTeamLeadGroups...)

	state := &RotaState{
		VolunteerGroups: allGroups,
	}

	shift := &Shift{
		Index:                  0,
		TeamLead:               nil,
		AvailableGroupIndices:  []int{0, 1, 2, 3, 4, 5, 6, 7}, // All 8 available
	}

	// Should only count team lead groups: 3
	// Affinity: 1 / 3 = 0.333...
	affinity := criterion.CalculateShiftAffinity(state, teamLeadGroups[0], shift)
	assert.InDelta(t, 0.333, affinity, 0.001)
}

func TestTeamLeadCriterion_PrefersUnpopularShifts(t *testing.T) {
	criterion := NewTeamLeadCriterion(1.0, 1.0)

	// Create 10 groups with team leads
	groups := make([]*VolunteerGroup, 10)
	for i := 0; i < 10; i++ {
		groups[i] = &VolunteerGroup{
			GroupKey:    string(rune('a' + i)),
			HasTeamLead: true,
		}
	}

	state := &RotaState{
		VolunteerGroups: groups,
	}

	// Popular shift - many team leads available
	popularShift := &Shift{
		Index:                  0,
		TeamLead:               nil,
		AvailableGroupIndices:  []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, // All 10 available
	}

	// Unpopular shift - only 2 team leads available
	unpopularShift := &Shift{
		Index:                  1,
		TeamLead:               nil,
		AvailableGroupIndices:  []int{0, 1}, // Only 2 available
	}

	popularAffinity := criterion.CalculateShiftAffinity(state, groups[0], popularShift)
	unpopularAffinity := criterion.CalculateShiftAffinity(state, groups[0], unpopularShift)

	// Unpopular shift should have higher affinity
	assert.Greater(t, unpopularAffinity, popularAffinity)
	assert.Equal(t, 0.1, popularAffinity)   // 1/10
	assert.Equal(t, 0.5, unpopularAffinity) // 1/2
}

func TestTeamLeadCriterion_ValidateRotaState_AllShiftsHaveTeamLead(t *testing.T) {
	criterion := NewTeamLeadCriterion(1.0, 1.0)

	state := &RotaState{
		Shifts: []*Shift{
			{
				Index: 0,
				Date:  "2024-01-01",
				AllocatedGroups: []*VolunteerGroup{
					{
						HasTeamLead: true,
						Members: []Volunteer{
							{ID: "tl1", IsTeamLead: true},
						},
					},
				},
			},
			{
				Index: 1,
				Date:  "2024-01-08",
				TeamLead: &Volunteer{ID: "tl2", IsTeamLead: true},
			},
		},
	}

	errors := criterion.ValidateRotaState(state)
	assert.Empty(t, errors, "Should have no errors when all shifts have team leads")
}

func TestTeamLeadCriterion_ValidateRotaState_MissingTeamLead(t *testing.T) {
	criterion := NewTeamLeadCriterion(1.0, 1.0)

	state := &RotaState{
		Shifts: []*Shift{
			{
				Index: 0,
				Date:  "2024-01-01",
				AllocatedGroups: []*VolunteerGroup{
					{
						HasTeamLead: false,
						Members: []Volunteer{
							{ID: "v1", IsTeamLead: false},
						},
					},
				},
			},
			{
				Index: 1,
				Date:  "2024-01-08",
				AllocatedGroups: []*VolunteerGroup{
					{
						HasTeamLead: false,
						Members: []Volunteer{
							{ID: "v2", IsTeamLead: false},
						},
					},
				},
			},
		},
	}

	errors := criterion.ValidateRotaState(state)
	assert.Len(t, errors, 2, "Should detect two shifts missing team leads")

	// Check first error
	assert.Equal(t, 0, errors[0].ShiftIndex)
	assert.Equal(t, "2024-01-01", errors[0].ShiftDate)
	assert.Equal(t, "TeamLead", errors[0].CriterionName)
	assert.Equal(t, "Shift has no team lead", errors[0].Description)

	// Check second error
	assert.Equal(t, 1, errors[1].ShiftIndex)
	assert.Equal(t, "2024-01-08", errors[1].ShiftDate)
	assert.Equal(t, "TeamLead", errors[1].CriterionName)
	assert.Equal(t, "Shift has no team lead", errors[1].Description)
}

func TestTeamLeadCriterion_ValidateRotaState_MultipleTeamLeads(t *testing.T) {
	criterion := NewTeamLeadCriterion(1.0, 1.0)

	state := &RotaState{
		Shifts: []*Shift{
			{
				Index:    0,
				Date:     "2024-01-01",
				TeamLead: &Volunteer{ID: "tl1", IsTeamLead: true},
				AllocatedGroups: []*VolunteerGroup{
					{
						HasTeamLead: true,
						Members: []Volunteer{
							{ID: "tl2", IsTeamLead: true},
						},
					},
				},
			},
		},
	}

	errors := criterion.ValidateRotaState(state)
	assert.Len(t, errors, 1, "Should detect shift with multiple team leads")

	assert.Equal(t, 0, errors[0].ShiftIndex)
	assert.Equal(t, "2024-01-01", errors[0].ShiftDate)
	assert.Equal(t, "TeamLead", errors[0].CriterionName)
	assert.Contains(t, errors[0].Description, "has 2 team leads")
}

func TestTeamLeadCriterion_ValidateRotaState_ThreeTeamLeads(t *testing.T) {
	criterion := NewTeamLeadCriterion(1.0, 1.0)

	state := &RotaState{
		Shifts: []*Shift{
			{
				Index:    0,
				Date:     "2024-01-01",
				TeamLead: &Volunteer{ID: "tl1", IsTeamLead: true},
				AllocatedGroups: []*VolunteerGroup{
					{
						HasTeamLead: true,
						Members: []Volunteer{
							{ID: "tl2", IsTeamLead: true},
						},
					},
					{
						HasTeamLead: true,
						Members: []Volunteer{
							{ID: "tl3", IsTeamLead: true},
						},
					},
				},
			},
		},
	}

	errors := criterion.ValidateRotaState(state)
	assert.Len(t, errors, 1, "Should detect shift with three team leads")

	assert.Equal(t, 0, errors[0].ShiftIndex)
	assert.Equal(t, "2024-01-01", errors[0].ShiftDate)
	assert.Equal(t, "TeamLead", errors[0].CriterionName)
	assert.Contains(t, errors[0].Description, "has 3 team leads")
}

func TestTeamLeadCriterion_ValidateRotaState_MixedValidAndInvalid(t *testing.T) {
	criterion := NewTeamLeadCriterion(1.0, 1.0)

	state := &RotaState{
		Shifts: []*Shift{
			{
				Index: 0,
				Date:  "2024-01-01",
				AllocatedGroups: []*VolunteerGroup{
					{
						HasTeamLead: true,
						Members: []Volunteer{
							{ID: "tl1", IsTeamLead: true},
						},
					},
				},
			},
			{
				Index: 1,
				Date:  "2024-01-08",
				AllocatedGroups: []*VolunteerGroup{
					{
						HasTeamLead: false,
						Members: []Volunteer{
							{ID: "v1", IsTeamLead: false},
						},
					},
				},
			},
			{
				Index:    2,
				Date:     "2024-01-15",
				TeamLead: &Volunteer{ID: "tl2", IsTeamLead: true},
			},
		},
	}

	errors := criterion.ValidateRotaState(state)
	assert.Len(t, errors, 1, "Should detect only the shift missing a team lead")

	assert.Equal(t, 1, errors[0].ShiftIndex)
	assert.Equal(t, "2024-01-08", errors[0].ShiftDate)
	assert.Contains(t, errors[0].Description, "no team lead")
}

func TestTeamLeadCriterion_ValidateRotaState_GroupWithoutTeamLeadDoesNotCount(t *testing.T) {
	criterion := NewTeamLeadCriterion(1.0, 1.0)

	state := &RotaState{
		Shifts: []*Shift{
			{
				Index: 0,
				Date:  "2024-01-01",
				AllocatedGroups: []*VolunteerGroup{
					{
						HasTeamLead: true,
						Members: []Volunteer{
							{ID: "tl1", IsTeamLead: true},
						},
					},
					{
						HasTeamLead: false,
						Members: []Volunteer{
							{ID: "v1", IsTeamLead: false},
							{ID: "v2", IsTeamLead: false},
						},
					},
				},
			},
		},
	}

	errors := criterion.ValidateRotaState(state)
	assert.Empty(t, errors, "Groups without team leads should not be counted")
}
