package rotageneration

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestShiftSizeCriterion_Name(t *testing.T) {
	criterion := NewShiftSizeCriterion(1.0, 1.0)
	assert.Equal(t, "ShiftSize", criterion.Name())
}

func TestShiftSizeCriterion_Weights(t *testing.T) {
	criterion := NewShiftSizeCriterion(5.0, 10.0)
	assert.Equal(t, 5.0, criterion.GroupWeight())
	assert.Equal(t, 10.0, criterion.AffinityWeight())
}

func TestShiftSizeCriterion_PromoteVolunteerGroup(t *testing.T) {
	criterion := NewShiftSizeCriterion(1.0, 1.0)
	state := &RotaState{}
	group := &VolunteerGroup{}

	// Should always return 0 (no promotion logic)
	promotion := criterion.PromoteVolunteerGroup(state, group)
	assert.Equal(t, 0.0, promotion)
}

func TestShiftSizeCriterion_IsShiftValid_EmptyShift(t *testing.T) {
	criterion := NewShiftSizeCriterion(1.0, 1.0)

	state := &RotaState{}

	shift := &Shift{
		Index:                  0,
		Size:                   5,
		AllocatedGroups:        []*VolunteerGroup{},
		PreAllocatedVolunteers: []string{},
	}

	// Group with 3 ordinary volunteers
	group := &VolunteerGroup{
		Members: []Volunteer{
			{ID: "v1", IsTeamLead: false},
			{ID: "v2", IsTeamLead: false},
			{ID: "v3", IsTeamLead: false},
		},
	}

	// Should be valid - 3 volunteers fit in shift of size 5
	valid := criterion.IsShiftValid(state, group, shift)
	assert.True(t, valid)
}

func TestShiftSizeCriterion_IsShiftValid_ExactFit(t *testing.T) {
	criterion := NewShiftSizeCriterion(1.0, 1.0)

	state := &RotaState{}

	// Shift with 2 volunteers already allocated
	shift := &Shift{
		Index: 0,
		Size:  5,
		AllocatedGroups: []*VolunteerGroup{
			{
				Members: []Volunteer{
					{ID: "v1", IsTeamLead: false},
					{ID: "v2", IsTeamLead: false},
				},
			},
		},
		PreAllocatedVolunteers: []string{},
	}

	// Group with 3 ordinary volunteers - exactly fills remaining capacity
	group := &VolunteerGroup{
		Members: []Volunteer{
			{ID: "v3", IsTeamLead: false},
			{ID: "v4", IsTeamLead: false},
			{ID: "v5", IsTeamLead: false},
		},
	}

	// Should be valid - exactly fits
	valid := criterion.IsShiftValid(state, group, shift)
	assert.True(t, valid)
}

func TestShiftSizeCriterion_IsShiftValid_WouldOverfill(t *testing.T) {
	criterion := NewShiftSizeCriterion(1.0, 1.0)

	state := &RotaState{}

	// Shift with 4 volunteers already allocated
	shift := &Shift{
		Index: 0,
		Size:  5,
		AllocatedGroups: []*VolunteerGroup{
			{
				Members: []Volunteer{
					{ID: "v1", IsTeamLead: false},
					{ID: "v2", IsTeamLead: false},
					{ID: "v3", IsTeamLead: false},
					{ID: "v4", IsTeamLead: false},
				},
			},
		},
		PreAllocatedVolunteers: []string{},
	}

	// Group with 2 ordinary volunteers - would exceed capacity
	group := &VolunteerGroup{
		Members: []Volunteer{
			{ID: "v5", IsTeamLead: false},
			{ID: "v6", IsTeamLead: false},
		},
	}

	// Should be invalid - would overfill
	valid := criterion.IsShiftValid(state, group, shift)
	assert.False(t, valid)
}

func TestShiftSizeCriterion_IsShiftValid_TeamLeadDoesNotCount(t *testing.T) {
	criterion := NewShiftSizeCriterion(1.0, 1.0)

	state := &RotaState{}

	// Shift with 4 ordinary volunteers already
	shift := &Shift{
		Index: 0,
		Size:  5,
		AllocatedGroups: []*VolunteerGroup{
			{
				Members: []Volunteer{
					{ID: "v1", IsTeamLead: false},
					{ID: "v2", IsTeamLead: false},
					{ID: "v3", IsTeamLead: false},
					{ID: "v4", IsTeamLead: false},
				},
			},
		},
		PreAllocatedVolunteers: []string{},
	}

	// Group with 1 team lead and 1 ordinary volunteer
	// Only the ordinary volunteer counts toward size
	group := &VolunteerGroup{
		Members: []Volunteer{
			{ID: "v5", IsTeamLead: true},  // Doesn't count
			{ID: "v6", IsTeamLead: false}, // Counts
		},
	}

	// Should be valid - only 1 ordinary volunteer, fits in remaining capacity of 1
	valid := criterion.IsShiftValid(state, group, shift)
	assert.True(t, valid)
}

func TestShiftSizeCriterion_IsShiftValid_WithPreAllocated(t *testing.T) {
	criterion := NewShiftSizeCriterion(1.0, 1.0)

	state := &RotaState{}

	// Shift with 3 pre-allocated volunteers and 1 in a group
	shift := &Shift{
		Index: 0,
		Size:  5,
		AllocatedGroups: []*VolunteerGroup{
			{
				Members: []Volunteer{
					{ID: "v1", IsTeamLead: false},
				},
			},
		},
		PreAllocatedVolunteers: []string{"p1", "p2", "p3"},
	}

	// Group with 2 ordinary volunteers - would exceed capacity (3 + 1 + 2 = 6 > 5)
	group := &VolunteerGroup{
		Members: []Volunteer{
			{ID: "v2", IsTeamLead: false},
			{ID: "v3", IsTeamLead: false},
		},
	}

	// Should be invalid
	valid := criterion.IsShiftValid(state, group, shift)
	assert.False(t, valid)
}

func TestShiftSizeCriterion_IsShiftValid_TeamLeadOnlyGroup(t *testing.T) {
	criterion := NewShiftSizeCriterion(1.0, 1.0)

	state := &RotaState{}

	// Full shift
	shift := &Shift{
		Index: 0,
		Size:  5,
		AllocatedGroups: []*VolunteerGroup{
			{
				Members: []Volunteer{
					{ID: "v1", IsTeamLead: false},
					{ID: "v2", IsTeamLead: false},
					{ID: "v3", IsTeamLead: false},
					{ID: "v4", IsTeamLead: false},
					{ID: "v5", IsTeamLead: false},
				},
			},
		},
		PreAllocatedVolunteers: []string{},
	}

	// Group with only a team lead
	group := &VolunteerGroup{
		Members: []Volunteer{
			{ID: "tl1", IsTeamLead: true},
		},
	}

	// Should be valid - team lead doesn't count toward size
	valid := criterion.IsShiftValid(state, group, shift)
	assert.True(t, valid)
}

func TestShiftSizeCriterion_CalculateShiftAffinity_EmptyShift(t *testing.T) {
	criterion := NewShiftSizeCriterion(1.0, 1.0)

	// Group with ordinary volunteers
	group := &VolunteerGroup{
		Members: []Volunteer{
			{ID: "v1", IsTeamLead: false},
			{ID: "v2", IsTeamLead: false},
		},
	}

	state := &RotaState{
		VolunteerGroups: []*VolunteerGroup{group},
	}

	// Empty shift with 5 remaining capacity and 1 available group (2 volunteers)
	shift := &Shift{
		Index:                  0,
		Size:                   5,
		AllocatedGroups:        []*VolunteerGroup{},
		PreAllocatedVolunteers: []string{},
		AvailableGroupIndices:  []int{0}, // group is available
	}

	// Empty shift = 5 capacity / 2 available volunteers = 2.5, clamped to 1.0
	affinity := criterion.CalculateShiftAffinity(state, group, shift)
	assert.Equal(t, 1.0, affinity)
}

func TestShiftSizeCriterion_CalculateShiftAffinity_HalfFull(t *testing.T) {
	criterion := NewShiftSizeCriterion(1.0, 1.0)

	// Group with ordinary volunteers
	group := &VolunteerGroup{
		GroupKey: "test_group",
		Members: []Volunteer{
			{ID: "v6", IsTeamLead: false},
		},
	}

	// Already allocated group (separate from available groups)
	allocatedGroup := &VolunteerGroup{
		GroupKey: "allocated_group",
		Members: []Volunteer{
			{ID: "v1", IsTeamLead: false},
			{ID: "v2", IsTeamLead: false},
			{ID: "v3", IsTeamLead: false},
			{ID: "v4", IsTeamLead: false},
			{ID: "v5", IsTeamLead: false},
		},
	}

	state := &RotaState{
		VolunteerGroups: []*VolunteerGroup{group},
	}

	// Half-full shift (5 spots remaining)
	shift := &Shift{
		Index:                  0,
		Size:                   10,
		AllocatedGroups:        []*VolunteerGroup{allocatedGroup},
		PreAllocatedVolunteers: []string{},
		AvailableGroupIndices:  []int{0}, // group is available
	}

	// Half full = 5 capacity / 1 available volunteer = 5.0, clamped to 1.0
	affinity := criterion.CalculateShiftAffinity(state, group, shift)
	assert.Equal(t, 1.0, affinity)
}

func TestShiftSizeCriterion_CalculateShiftAffinity_NearlyFull(t *testing.T) {
	criterion := NewShiftSizeCriterion(1.0, 1.0)

	// Create 5 available groups for this shift
	groups := make([]*VolunteerGroup, 5)
	for i := 0; i < 5; i++ {
		groups[i] = &VolunteerGroup{
			GroupKey: fmt.Sprintf("group_%d", i),
			Members: []Volunteer{
				{ID: fmt.Sprintf("v%d", i+5), IsTeamLead: false},
			},
		}
	}

	// Already allocated group (separate)
	allocatedGroup := &VolunteerGroup{
		GroupKey: "allocated_group",
		Members: []Volunteer{
			{ID: "v1", IsTeamLead: false},
			{ID: "v2", IsTeamLead: false},
			{ID: "v3", IsTeamLead: false},
			{ID: "v4", IsTeamLead: false},
		},
	}

	state := &RotaState{
		VolunteerGroups: groups,
	}

	// Nearly full shift (1 spot remaining)
	shift := &Shift{
		Index:                  0,
		Size:                   5,
		AllocatedGroups:        []*VolunteerGroup{allocatedGroup},
		PreAllocatedVolunteers: []string{},
		AvailableGroupIndices:  []int{0, 1, 2, 3, 4}, // 5 groups available
	}

	// Nearly full = 1 capacity / 5 available volunteers (5 groups x 1 volunteer each) = 0.2 affinity
	affinity := criterion.CalculateShiftAffinity(state, groups[0], shift)
	assert.Equal(t, 0.2, affinity)
}

func TestShiftSizeCriterion_CalculateShiftAffinity_TeamLeadOnlyGroup(t *testing.T) {
	criterion := NewShiftSizeCriterion(1.0, 1.0)

	state := &RotaState{}

	// Empty shift
	shift := &Shift{
		Index:                  0,
		Size:                   5,
		AllocatedGroups:        []*VolunteerGroup{},
		PreAllocatedVolunteers: []string{},
	}

	// Group with only team lead
	group := &VolunteerGroup{
		Members: []Volunteer{
			{ID: "tl1", IsTeamLead: true},
		},
	}

	// No ordinary volunteers = 0 affinity
	affinity := criterion.CalculateShiftAffinity(state, group, shift)
	assert.Equal(t, 0.0, affinity)
}

func TestShiftSizeCriterion_CalculateShiftAffinity_MixedGroup(t *testing.T) {
	criterion := NewShiftSizeCriterion(1.0, 1.0)

	// Group with team lead + ordinary volunteers
	group := &VolunteerGroup{
		Members: []Volunteer{
			{ID: "tl1", IsTeamLead: true},
			{ID: "v1", IsTeamLead: false},
			{ID: "v2", IsTeamLead: false},
		},
	}

	state := &RotaState{
		VolunteerGroups: []*VolunteerGroup{group},
	}

	// Empty shift
	shift := &Shift{
		Index:                  0,
		Size:                   5,
		AllocatedGroups:        []*VolunteerGroup{},
		PreAllocatedVolunteers: []string{},
		AvailableGroupIndices:  []int{0},
	}

	// Has ordinary volunteers, so should calculate affinity normally
	// 5 capacity / 2 available volunteers (2 ordinary in group) = 2.5, clamped to 1.0
	affinity := criterion.CalculateShiftAffinity(state, group, shift)
	assert.Equal(t, 1.0, affinity)
}

func TestShiftSizeCriterion_CalculateShiftAffinity_ZeroSizeShift(t *testing.T) {
	criterion := NewShiftSizeCriterion(1.0, 1.0)

	state := &RotaState{}

	// Shift with size 0 (edge case)
	shift := &Shift{
		Index:                  0,
		Size:                   0,
		AllocatedGroups:        []*VolunteerGroup{},
		PreAllocatedVolunteers: []string{},
	}

	// Group with ordinary volunteers
	group := &VolunteerGroup{
		Members: []Volunteer{
			{ID: "v1", IsTeamLead: false},
		},
	}

	// Zero size shift = 0 affinity (avoid division by zero)
	affinity := criterion.CalculateShiftAffinity(state, group, shift)
	assert.Equal(t, 0.0, affinity)
}

func TestShiftSizeCriterion_PrefersUnpopularShifts(t *testing.T) {
	criterion := NewShiftSizeCriterion(1.0, 1.0)

	group := &VolunteerGroup{
		GroupKey: "test_group",
		Members: []Volunteer{
			{ID: "v5", IsTeamLead: false},
		},
	}

	// Create 5 available groups
	groups := make([]*VolunteerGroup, 5)
	for i := 0; i < 5; i++ {
		groups[i] = &VolunteerGroup{
			GroupKey: fmt.Sprintf("group_%d", i),
			Members: []Volunteer{
				{ID: fmt.Sprintf("v%d", i+10), IsTeamLead: false},
			},
		}
	}
	groups[0] = group // Include our test group

	// Already allocated group (separate)
	allocatedGroup := &VolunteerGroup{
		GroupKey: "allocated_group",
		Members: []Volunteer{
			{ID: "v1", IsTeamLead: false},
			{ID: "v2", IsTeamLead: false},
			{ID: "v3", IsTeamLead: false},
			{ID: "v4", IsTeamLead: false},
		},
	}

	state := &RotaState{
		VolunteerGroups: groups,
	}

	// Empty shift (unpopular) - 5 capacity / 5 available volunteers = 1.0
	emptyShift := &Shift{
		Index:                  0,
		Size:                   5,
		AllocatedGroups:        []*VolunteerGroup{},
		PreAllocatedVolunteers: []string{},
		AvailableGroupIndices:  []int{0, 1, 2, 3, 4}, // 5 groups available
	}

	// Nearly full shift (popular) - 1 capacity / 5 available volunteers = 0.2
	fullShift := &Shift{
		Index:                  1,
		Size:                   5,
		AllocatedGroups:        []*VolunteerGroup{allocatedGroup},
		PreAllocatedVolunteers: []string{},
		AvailableGroupIndices:  []int{0, 1, 2, 3, 4}, // 5 groups available
	}

	emptyAffinity := criterion.CalculateShiftAffinity(state, group, emptyShift)
	fullAffinity := criterion.CalculateShiftAffinity(state, group, fullShift)

	// Empty shift should have higher affinity than nearly full shift
	assert.Greater(t, emptyAffinity, fullAffinity)
	assert.Equal(t, 1.0, emptyAffinity) // 5 capacity / 5 volunteers = 1.0
	assert.Equal(t, 0.2, fullAffinity)  // 1 capacity / 5 volunteers = 0.2
}

func TestShiftSizeCriterion_CalculateShiftAffinity_ExcludesTooLargeGroups(t *testing.T) {
	criterion := NewShiftSizeCriterion(1.0, 1.0)

	// Group that will be queried
	smallGroup := &VolunteerGroup{
		GroupKey: "small_group",
		Members: []Volunteer{
			{ID: "v1", IsTeamLead: false},
		},
	}

	// Group that is too large to fit (3 volunteers, but only 2 spots remaining)
	largeGroup := &VolunteerGroup{
		GroupKey: "large_group",
		Members: []Volunteer{
			{ID: "v2", IsTeamLead: false},
			{ID: "v3", IsTeamLead: false},
			{ID: "v4", IsTeamLead: false},
		},
	}

	// Another small group that fits
	anotherSmallGroup := &VolunteerGroup{
		GroupKey: "another_small",
		Members: []Volunteer{
			{ID: "v5", IsTeamLead: false},
		},
	}

	state := &RotaState{
		VolunteerGroups: []*VolunteerGroup{smallGroup, largeGroup, anotherSmallGroup},
	}

	// Shift with 3 already allocated, size 5 (so 2 spots remaining)
	shift := &Shift{
		Index: 0,
		Size:  5,
		AllocatedGroups: []*VolunteerGroup{
			{
				GroupKey: "allocated",
				Members: []Volunteer{
					{ID: "a1", IsTeamLead: false},
					{ID: "a2", IsTeamLead: false},
					{ID: "a3", IsTeamLead: false},
				},
			},
		},
		PreAllocatedVolunteers: []string{},
		AvailableGroupIndices:  []int{0, 1, 2}, // all 3 groups available
	}

	// Should only count volunteers from groups that fit
	// smallGroup: 1 volunteer
	// largeGroup: 3 volunteers (EXCLUDED - too large)
	// anotherSmallGroup: 1 volunteer
	// Total: 2 volunteers
	// Affinity: 2 capacity / 2 volunteers = 1.0
	affinity := criterion.CalculateShiftAffinity(state, smallGroup, shift)
	assert.Equal(t, 1.0, affinity)
}
