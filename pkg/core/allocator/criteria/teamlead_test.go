package criteria

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
		VolunteerState: &VolunteerState{
			VolunteerGroups:          []*VolunteerGroup{group},
			ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
		},
	}

	shift := &Shift{
		Index:           0,
		TeamLead:        nil,
		AvailableGroups: []*VolunteerGroup{group},
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
		VolunteerState: &VolunteerState{
			VolunteerGroups:          []*VolunteerGroup{group},
			ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
		},
	}

	shift := &Shift{
		Index:           0,
		TeamLead:        &Volunteer{ID: "tl1", IsTeamLead: true}, // Already has team lead
		AvailableGroups: []*VolunteerGroup{group},
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
		VolunteerState: &VolunteerState{
			VolunteerGroups:          groups,
			ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
		},
	}

	shift := &Shift{
		Index:           0,
		TeamLead:        nil,
		AvailableGroups: groups, // All 10 available
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
		VolunteerState: &VolunteerState{
			VolunteerGroups:          groups,
			ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
		},
	}

	shift := &Shift{
		Index:           0,
		TeamLead:        nil,
		AvailableGroups: groups[:2], // Only 2 available
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
		VolunteerState: &VolunteerState{
			VolunteerGroups:          []*VolunteerGroup{group},
			ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
		},
	}

	shift := &Shift{
		Index:           0,
		TeamLead:        nil,
		AvailableGroups: []*VolunteerGroup{group}, // Only 1 available
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

	// Mark groups 1, 2, 3 as exhausted
	exhaustedMap := make(map[*VolunteerGroup]bool)
	exhaustedMap[groups[1]] = true
	exhaustedMap[groups[2]] = true
	exhaustedMap[groups[3]] = true

	state := &RotaState{
		VolunteerState: &VolunteerState{
			VolunteerGroups:          groups,
			ExhaustedVolunteerGroups: exhaustedMap,
		},
	}

	shift := &Shift{
		Index:           0,
		TeamLead:        nil,
		AvailableGroups: groups, // All 5 originally available
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

	state := &RotaState{
		VolunteerState: &VolunteerState{
			VolunteerGroups:          groups,
			ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
		},
	}

	shift := &Shift{
		Index:           0,
		TeamLead:        nil,
		AllocatedGroups: []*VolunteerGroup{groups[1]}, // Group 'b' (groups[1]) already allocated
		AvailableGroups: groups[:3],
	}

	// Should only count groups not already allocated: 'a' (groups[0]), 'c' (groups[2])
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
		VolunteerState: &VolunteerState{
			VolunteerGroups:          allGroups,
			ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
		},
	}

	shift := &Shift{
		Index:           0,
		TeamLead:        nil,
		AvailableGroups: allGroups[:8], // All 8 available
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
		VolunteerState: &VolunteerState{
			VolunteerGroups:          groups,
			ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
		},
	}

	// Popular shift - many team leads available
	popularShift := &Shift{
		Index:           0,
		TeamLead:        nil,
		AvailableGroups: groups, // All 10 available
	}

	// Unpopular shift - only 2 team leads available
	unpopularShift := &Shift{
		Index:           1,
		TeamLead:        nil,
		AvailableGroups: groups[:2], // Only 2 available
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
				Index:    0,
				Date:     "2024-01-01",
				TeamLead: &Volunteer{ID: "tl1", FirstName: "Alice", LastName: "Smith", IsTeamLead: true},
				AllocatedGroups: []*VolunteerGroup{
					{
						HasTeamLead: true,
						Members: []Volunteer{
							{ID: "tl1", FirstName: "Alice", LastName: "Smith", IsTeamLead: true},
							{ID: "v1", FirstName: "Bob", LastName: "Smith", IsTeamLead: false},
						},
					},
				},
			},
			{
				Index:    1,
				Date:     "2024-01-08",
				TeamLead: &Volunteer{ID: "tl2", FirstName: "Charlie", LastName: "Jones", IsTeamLead: true},
				AllocatedGroups: []*VolunteerGroup{
					{
						HasTeamLead: false,
						Members: []Volunteer{
							{ID: "v2", FirstName: "Diana", LastName: "Green", IsTeamLead: false},
						},
					},
				},
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
				TeamLead: &Volunteer{ID: "tl1", FirstName: "Alice", LastName: "Smith", IsTeamLead: true},
				AllocatedGroups: []*VolunteerGroup{
					{
						HasTeamLead: true,
						Members: []Volunteer{
							{ID: "tl2", FirstName: "Bob", LastName: "Jones", DisplayName: "Bob Jones", IsTeamLead: true}, // Different team lead as ordinary volunteer
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
	assert.Contains(t, errors[0].Description, "team lead")
	assert.Contains(t, errors[0].Description, "Bob Jones")
}

func TestTeamLeadCriterion_ValidateRotaState_ThreeTeamLeads(t *testing.T) {
	criterion := NewTeamLeadCriterion(1.0, 1.0)

	state := &RotaState{
		Shifts: []*Shift{
			{
				Index:    0,
				Date:     "2024-01-01",
				TeamLead: &Volunteer{ID: "tl1", FirstName: "Alice", LastName: "Smith", IsTeamLead: true},
				AllocatedGroups: []*VolunteerGroup{
					{
						HasTeamLead: true,
						Members: []Volunteer{
							{ID: "tl2", FirstName: "Bob", LastName: "Jones", DisplayName: "Bob", IsTeamLead: true}, // Different team lead
						},
					},
					{
						HasTeamLead: true,
						Members: []Volunteer{
							{ID: "tl3", FirstName: "Charlie", LastName: "Brown", DisplayName: "Charlie", IsTeamLead: true}, // Another different team lead
						},
					},
				},
			},
		},
	}

	errors := criterion.ValidateRotaState(state)
	assert.Len(t, errors, 2, "Should detect shift with two extra team leads")

	// Both Bob and Charlie are team leads allocated as ordinary volunteers
	assert.Equal(t, 0, errors[0].ShiftIndex)
	assert.Equal(t, "2024-01-01", errors[0].ShiftDate)
	assert.Equal(t, "TeamLead", errors[0].CriterionName)
	assert.Contains(t, errors[0].Description, "team lead")

	assert.Equal(t, 0, errors[1].ShiftIndex)
	assert.Equal(t, "2024-01-01", errors[1].ShiftDate)
	assert.Equal(t, "TeamLead", errors[1].CriterionName)
	assert.Contains(t, errors[1].Description, "team lead")
}

func TestTeamLeadCriterion_ValidateRotaState_MixedValidAndInvalid(t *testing.T) {
	criterion := NewTeamLeadCriterion(1.0, 1.0)

	state := &RotaState{
		Shifts: []*Shift{
			{
				Index:    0,
				Date:     "2024-01-01",
				TeamLead: &Volunteer{ID: "tl1", FirstName: "Alice", LastName: "Smith", IsTeamLead: true},
				AllocatedGroups: []*VolunteerGroup{
					{
						HasTeamLead: true,
						Members: []Volunteer{
							{ID: "tl1", FirstName: "Alice", LastName: "Smith", IsTeamLead: true},
							{ID: "v1", FirstName: "Bob", LastName: "Jones", IsTeamLead: false},
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
				TeamLead: &Volunteer{ID: "tl2", FirstName: "Charlie", LastName: "Brown", IsTeamLead: true},
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
				Index:    0,
				Date:     "2024-01-01",
				TeamLead: &Volunteer{ID: "tl1", FirstName: "Alice", LastName: "Smith", IsTeamLead: true},
				AllocatedGroups: []*VolunteerGroup{
					{
						HasTeamLead: true,
						Members: []Volunteer{
							{ID: "tl1", FirstName: "Alice", LastName: "Smith", IsTeamLead: true},
							{ID: "v0", FirstName: "Bob", LastName: "Smith", IsTeamLead: false},
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

func TestTeamLeadCriterion_ValidateRotaState_SkipsClosedShifts(t *testing.T) {
	criterion := NewTeamLeadCriterion(1.0, 1.0)

	state := &RotaState{
		Shifts: []*Shift{
			{
				Index:           0,
				Date:            "2024-01-01",
				TeamLead:        nil, // No team lead
				AllocatedGroups: []*VolunteerGroup{},
				Closed:          true, // Closed shift - should be skipped
			},
			{
				Index:    1,
				Date:     "2024-01-08",
				TeamLead: nil, // No team lead
				AllocatedGroups: []*VolunteerGroup{
					{
						HasTeamLead: false,
						Members: []Volunteer{
							{ID: "v1", IsTeamLead: false},
						},
					},
				},
				Closed: false, // Regular shift - should be validated
			},
		},
	}

	errors := criterion.ValidateRotaState(state)

	// Should only detect the missing team lead in the open shift, not the closed shift
	assert.Len(t, errors, 1, "Should skip closed shift validation")
	assert.Equal(t, 1, errors[0].ShiftIndex, "Error should be for shift 1 (the open shift)")
	assert.Equal(t, "2024-01-08", errors[0].ShiftDate)
	assert.Equal(t, "Shift has no team lead", errors[0].Description)
}
