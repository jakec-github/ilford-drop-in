package rotageneration

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMaleBalanceCriterion_Name(t *testing.T) {
	criterion := NewMaleBalanceCriterion(1.0, 1.0)
	assert.Equal(t, "MaleBalance", criterion.Name())
}

func TestMaleBalanceCriterion_Weights(t *testing.T) {
	criterion := NewMaleBalanceCriterion(5.0, 10.0)
	assert.Equal(t, 5.0, criterion.GroupWeight())
	assert.Equal(t, 10.0, criterion.AffinityWeight())
}

func TestMaleBalanceCriterion_PromoteVolunteerGroup_WithMales(t *testing.T) {
	criterion := NewMaleBalanceCriterion(1.0, 1.0)
	state := &RotaState{}

	group := &VolunteerGroup{
		MaleCount: 1,
	}

	promotion := criterion.PromoteVolunteerGroup(state, group)
	assert.Equal(t, 1.0, promotion)
}

func TestMaleBalanceCriterion_PromoteVolunteerGroup_WithoutMales(t *testing.T) {
	criterion := NewMaleBalanceCriterion(1.0, 1.0)
	state := &RotaState{}

	group := &VolunteerGroup{
		MaleCount: 0,
	}

	promotion := criterion.PromoteVolunteerGroup(state, group)
	assert.Equal(t, 0.0, promotion)
}

func TestMaleBalanceCriterion_IsShiftValid_GroupWithMales(t *testing.T) {
	criterion := NewMaleBalanceCriterion(1.0, 1.0)
	state := &RotaState{}

	shift := &Shift{
		Size:      5,
		MaleCount: 0,
	}

	group := &VolunteerGroup{
		MaleCount: 1,
		Members:   []Volunteer{{ID: "v1", Gender: "Male"}},
	}

	// Groups with males are always valid
	assert.True(t, criterion.IsShiftValid(state, group, shift))
}

func TestMaleBalanceCriterion_IsShiftValid_ShiftHasMale_GroupWithoutMale(t *testing.T) {
	criterion := NewMaleBalanceCriterion(1.0, 1.0)
	state := &RotaState{}

	shift := &Shift{
		Size:      5,
		MaleCount: 1,
	}

	group := &VolunteerGroup{
		MaleCount: 0,
		Members:   []Volunteer{{ID: "v1", Gender: "F"}},
	}

	// Valid because shift already has a male
	assert.True(t, criterion.IsShiftValid(state, group, shift))
}

func TestMaleBalanceCriterion_IsShiftValid_WouldFillWithoutMale(t *testing.T) {
	criterion := NewMaleBalanceCriterion(1.0, 1.0)
	state := &RotaState{}

	shift := &Shift{
		Size:                  5,
		MaleCount:             0,
		TeamLead:              &Volunteer{Gender: "Female"},
		PreAllocatedVolunteers: []string{"pre1", "pre2", "pre3"}, // 3 already allocated
	}

	// Group of 2 females would fill the shift (3 + 2 = 5)
	group := &VolunteerGroup{
		MaleCount: 0,
		Members: []Volunteer{
			{ID: "v1", Gender: "F"},
			{ID: "v2", Gender: "F"},
		},
	}

	// Invalid because allocating this group would fill the shift with no males
	assert.False(t, criterion.IsShiftValid(state, group, shift))
}

func TestMaleBalanceCriterion_IsShiftValid_WouldNotFillShift(t *testing.T) {
	criterion := NewMaleBalanceCriterion(1.0, 1.0)
	state := &RotaState{}

	shift := &Shift{
		Size:                  5,
		MaleCount:             0,
		TeamLead:              &Volunteer{Gender: "Female"},
		PreAllocatedVolunteers: []string{"pre1"}, // Only 1 allocated
	}

	// Group of 2 females would not fill the shift (1 + 2 = 3 < 5)
	group := &VolunteerGroup{
		MaleCount: 0,
		Members: []Volunteer{
			{ID: "v1", Gender: "F"},
			{ID: "v2", Gender: "F"},
		},
	}

	// Valid because allocating this group doesn't fill the shift
	// (still room for males to be allocated later)
	assert.True(t, criterion.IsShiftValid(state, group, shift))
}

func TestMaleBalanceCriterion_IsShiftValid_NoTeamLeadAssigned(t *testing.T) {
	criterion := NewMaleBalanceCriterion(1.0, 1.0)
	state := &RotaState{}

	shift := &Shift{
		Size:                  5,
		MaleCount:             0,
		TeamLead:              nil, // No team lead assigned yet
		PreAllocatedVolunteers: []string{"pre1", "pre2", "pre3", "pre4"}, // Shift is full
	}

	// Group with only a female team lead (no ordinary volunteers)
	group := &VolunteerGroup{
		MaleCount: 0,
		Members: []Volunteer{
			{ID: "tl1", Gender: "F", IsTeamLead: false},
		},
	}

	// Valid because team lead hasn't been allocated yet
	// Even though shift will be full, we can still assign a male team lead later
	assert.True(t, criterion.IsShiftValid(state, group, shift))
}

func TestMaleBalanceCriterion_CalculateShiftAffinity_GroupWithoutMales(t *testing.T) {
	criterion := NewMaleBalanceCriterion(1.0, 1.0)

	group := &VolunteerGroup{
		MaleCount: 0,
	}

	state := &RotaState{
		VolunteerState: &VolunteerState{
		VolunteerGroups: []*VolunteerGroup{group},
		ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
	},
	}

	shift := &Shift{
		Index:           0,
		MaleCount:       0,
		TeamLead:        &Volunteer{Gender: "Female"},
		AvailableGroups: []*VolunteerGroup{group},
	}

	// Should return 0 for groups without males
	affinity := criterion.CalculateShiftAffinity(state, group, shift)
	assert.Equal(t, 0.0, affinity)
}

func TestMaleBalanceCriterion_CalculateShiftAffinity_ShiftAlreadyHasMale(t *testing.T) {
	criterion := NewMaleBalanceCriterion(1.0, 1.0)

	group := &VolunteerGroup{
		GroupKey:  "group_a",
		MaleCount: 1,
		Members:   []Volunteer{{ID: "v1", Gender: "Male"}},
	}

	state := &RotaState{
		VolunteerState: &VolunteerState{
		VolunteerGroups: []*VolunteerGroup{group},
		ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
	},
	}

	shift := &Shift{
		Index:           0,
		Size:            5,
		MaleCount:       1, // Already has a male
		AvailableGroups: []*VolunteerGroup{group},
	}

	// With 1 male already allocated and 1 male volunteer available
	// Need = 1.0 - (1 * 0.5) = 0.5
	// Affinity = 0.5 / 1 = 0.5
	affinity := criterion.CalculateShiftAffinity(state, group, shift)
	assert.Equal(t, 0.5, affinity)
}

func TestMaleBalanceCriterion_CalculateShiftAffinity_ManyMaleVolunteersAvailable(t *testing.T) {
	criterion := NewMaleBalanceCriterion(1.0, 1.0)

	// Create 10 groups with 1 male each = 10 male volunteers
	groups := make([]*VolunteerGroup, 10)
	for i := 0; i < 10; i++ {
		groups[i] = &VolunteerGroup{
			GroupKey:  string(rune('a' + i)),
			MaleCount: 1,
			Members:   []Volunteer{{ID: string(rune('a' + i)), Gender: "Male"}},
		}
	}

	state := &RotaState{
		VolunteerState: &VolunteerState{
		VolunteerGroups: groups,
		ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
	},
	}

	shift := &Shift{
		Index:           0,
		Size:            15,
		MaleCount:       0,
		AvailableGroups: groups, // All 10 available
	}

	// Need = 1.0 (no males yet)
	// Affinity: 1.0 / 10 = 0.1 (low priority - many male volunteers available)
	affinity := criterion.CalculateShiftAffinity(state, groups[0], shift)
	assert.Equal(t, 0.1, affinity)
}

func TestMaleBalanceCriterion_CalculateShiftAffinity_FewMaleVolunteersAvailable(t *testing.T) {
	criterion := NewMaleBalanceCriterion(1.0, 1.0)

	// Create 2 groups with 1 male each = 2 male volunteers
	groups := make([]*VolunteerGroup, 2)
	for i := 0; i < 2; i++ {
		groups[i] = &VolunteerGroup{
			GroupKey:  string(rune('a' + i)),
			MaleCount: 1,
			Members:   []Volunteer{{ID: string(rune('a' + i)), Gender: "Male"}},
		}
	}

	state := &RotaState{
		VolunteerState: &VolunteerState{
		VolunteerGroups: groups,
		ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
	},
	}

	shift := &Shift{
		Index:           0,
		Size:            5,
		MaleCount:       0,
		AvailableGroups: groups, // Only 2 available
	}

	// Need = 1.0 (no males yet)
	// Affinity: 1.0 / 2 = 0.5 (moderate priority)
	affinity := criterion.CalculateShiftAffinity(state, groups[0], shift)
	assert.Equal(t, 0.5, affinity)
}

func TestMaleBalanceCriterion_CalculateShiftAffinity_OnlyOneMaleVolunteerAvailable(t *testing.T) {
	criterion := NewMaleBalanceCriterion(1.0, 1.0)

	group := &VolunteerGroup{
		GroupKey:  "group_a",
		MaleCount: 1,
		Members:   []Volunteer{{ID: "v1", Gender: "Male"}},
	}

	state := &RotaState{
		VolunteerState: &VolunteerState{
		VolunteerGroups: []*VolunteerGroup{group},
		ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
	},
	}

	shift := &Shift{
		Index:           0,
		Size:            5,
		MaleCount:       0,
		AvailableGroups: []*VolunteerGroup{group}, // Only 1 available
	}

	// Need = 1.0 (no males yet)
	// Affinity: 1.0 / 1 = 1.0 (urgent!)
	affinity := criterion.CalculateShiftAffinity(state, group, shift)
	assert.Equal(t, 1.0, affinity)
}

func TestMaleBalanceCriterion_CalculateShiftAffinity_ExcludesExhaustedGroups(t *testing.T) {
	criterion := NewMaleBalanceCriterion(1.0, 1.0)

	// Create 5 groups with 1 male each
	groups := make([]*VolunteerGroup, 5)
	for i := 0; i < 5; i++ {
		groups[i] = &VolunteerGroup{
			GroupKey:  string(rune('a' + i)),
			MaleCount: 1,
			Members:   []Volunteer{{ID: string(rune('a' + i)), Gender: "Male"}},
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
		Size:            5,
		MaleCount:       0,
		AvailableGroups: groups, // All 5 originally available
	}

	// Should only count non-exhausted groups: 0, 4 (2 male volunteers)
	// Need = 1.0
	// Affinity: 1.0 / 2 = 0.5
	affinity := criterion.CalculateShiftAffinity(state, groups[0], shift)
	assert.Equal(t, 0.5, affinity)
}

func TestMaleBalanceCriterion_CalculateShiftAffinity_ExcludesAllocatedGroups(t *testing.T) {
	criterion := NewMaleBalanceCriterion(1.0, 1.0)

	// Create 3 groups with 1 male each
	groups := make([]*VolunteerGroup, 3)
	for i := 0; i < 3; i++ {
		groups[i] = &VolunteerGroup{
			GroupKey:  string(rune('a' + i)),
			MaleCount: 1,
			Members:   []Volunteer{{ID: string(rune('a' + i)), Gender: "Male"}},
		}
	}

	state := &RotaState{
		VolunteerState: &VolunteerState{
		VolunteerGroups: groups,
		ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
	},
	}

	shift := &Shift{
		Index:           0,
		Size:            5,
		MaleCount:       0,
		AllocatedGroups: []*VolunteerGroup{groups[1]}, // Group 'b' (groups[1]) already allocated
		AvailableGroups: groups,
	}

	// Should only count groups not already allocated: 'a' (groups[0]), 'c' (groups[2]) = 2 male volunteers
	// Need = 1.0
	// Affinity: 1.0 / 2 = 0.5
	affinity := criterion.CalculateShiftAffinity(state, groups[0], shift)
	assert.Equal(t, 0.5, affinity)
}

func TestMaleBalanceCriterion_CalculateShiftAffinity_MixedGroupsOnlyCountsMales(t *testing.T) {
	criterion := NewMaleBalanceCriterion(1.0, 1.0)

	// 3 groups with 1 male each = 3 male volunteers
	maleGroups := make([]*VolunteerGroup, 3)
	for i := 0; i < 3; i++ {
		maleGroups[i] = &VolunteerGroup{
			GroupKey:  string(rune('a' + i)),
			MaleCount: 1,
			Members:   []Volunteer{{ID: string(rune('a' + i)), Gender: "Male"}},
		}
	}

	// 5 groups without males
	femaleGroups := make([]*VolunteerGroup, 5)
	for i := 0; i < 5; i++ {
		femaleGroups[i] = &VolunteerGroup{
			GroupKey:  string(rune('d' + i)),
			MaleCount: 0,
			Members:   []Volunteer{{ID: string(rune('d' + i)), Gender: "F"}},
		}
	}

	allGroups := append(maleGroups, femaleGroups...)

	state := &RotaState{
		VolunteerState: &VolunteerState{
		VolunteerGroups: allGroups,
		ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
	},
	}

	shift := &Shift{
		Index:           0,
		Size:            10,
		MaleCount:       0,
		AvailableGroups: allGroups[:8], // First 8 groups
	}

	// Should only count male volunteers: 3
	// Need = 1.0
	// Affinity: 1.0 / 3 = 0.333...
	affinity := criterion.CalculateShiftAffinity(state, maleGroups[0], shift)
	assert.InDelta(t, 0.333, affinity, 0.001)
}

func TestMaleBalanceCriterion_PrefersUnpopularShifts(t *testing.T) {
	criterion := NewMaleBalanceCriterion(1.0, 1.0)

	// Create 10 groups with 1 male each = 10 male volunteers
	groups := make([]*VolunteerGroup, 10)
	for i := 0; i < 10; i++ {
		groups[i] = &VolunteerGroup{
			GroupKey:  string(rune('a' + i)),
			MaleCount: 1,
			Members:   []Volunteer{{ID: string(rune('a' + i)), Gender: "Male"}},
		}
	}

	state := &RotaState{
		VolunteerState: &VolunteerState{
		VolunteerGroups: groups,
		ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
	},
	}

	// Popular shift - many male volunteers available
	popularShift := &Shift{
		Index:           0,
		Size:            15,
		MaleCount:       0,
		AvailableGroups: groups, // All 10 available
	}

	// Unpopular shift - only 2 male volunteers available
	unpopularShift := &Shift{
		Index:           1,
		Size:            5,
		MaleCount:       0,
		AvailableGroups: groups[:2], // Only 2 available
	}

	popularAffinity := criterion.CalculateShiftAffinity(state, groups[0], popularShift)
	unpopularAffinity := criterion.CalculateShiftAffinity(state, groups[0], unpopularShift)

	// Unpopular shift should have higher affinity
	assert.Greater(t, unpopularAffinity, popularAffinity)
	assert.Equal(t, 0.1, popularAffinity)   // 1.0/10
	assert.Equal(t, 0.5, unpopularAffinity) // 1.0/2
}

func TestMaleBalanceCriterion_ValidateRotaState_AllShiftsHaveMales(t *testing.T) {
	criterion := NewMaleBalanceCriterion(1.0, 1.0)

	state := &RotaState{
		Shifts: []*Shift{
			{
				Index:     0,
				Date:      "2024-01-01",
				MaleCount: 1,
			},
			{
				Index:     1,
				Date:      "2024-01-08",
				MaleCount: 2,
			},
		},
	}

	errors := criterion.ValidateRotaState(state)
	assert.Empty(t, errors, "Should have no errors when all shifts have at least one male")
}

func TestMaleBalanceCriterion_ValidateRotaState_MissingMales(t *testing.T) {
	criterion := NewMaleBalanceCriterion(1.0, 1.0)

	state := &RotaState{
		Shifts: []*Shift{
			{
				Index:     0,
				Date:      "2024-01-01",
				MaleCount: 0,
			},
			{
				Index:     1,
				Date:      "2024-01-08",
				MaleCount: 0,
			},
		},
	}

	errors := criterion.ValidateRotaState(state)
	assert.Len(t, errors, 2, "Should detect two shifts without males")

	// Check first error
	assert.Equal(t, 0, errors[0].ShiftIndex)
	assert.Equal(t, "2024-01-01", errors[0].ShiftDate)
	assert.Equal(t, "MaleBalance", errors[0].CriterionName)
	assert.Contains(t, errors[0].Description, "no male volunteers")

	// Check second error
	assert.Equal(t, 1, errors[1].ShiftIndex)
	assert.Equal(t, "2024-01-08", errors[1].ShiftDate)
	assert.Equal(t, "MaleBalance", errors[1].CriterionName)
	assert.Contains(t, errors[1].Description, "no male volunteers")
}

func TestMaleBalanceCriterion_ValidateRotaState_MixedValidAndInvalid(t *testing.T) {
	criterion := NewMaleBalanceCriterion(1.0, 1.0)

	state := &RotaState{
		Shifts: []*Shift{
			{
				Index:     0,
				Date:      "2024-01-01",
				MaleCount: 1,
			},
			{
				Index:     1,
				Date:      "2024-01-08",
				MaleCount: 0,
			},
			{
				Index:     2,
				Date:      "2024-01-15",
				MaleCount: 3,
			},
		},
	}

	errors := criterion.ValidateRotaState(state)
	assert.Len(t, errors, 1, "Should detect only the shift without males")

	assert.Equal(t, 1, errors[0].ShiftIndex)
	assert.Equal(t, "2024-01-08", errors[0].ShiftDate)
	assert.Contains(t, errors[0].Description, "no male volunteers")
}
