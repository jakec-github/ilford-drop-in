package allocator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitVolunteerGroups_BasicGrouping(t *testing.T) {
	input := InitVolunteerGroupsInput{
		Volunteers: []Volunteer{
			{ID: "v1", FirstName: "Alice", LastName: "Smith", Gender: "Male", IsTeamLead: false, GroupKey: "group_a"},
			{ID: "v2", FirstName: "Bob", LastName: "Jones", Gender: "Male", IsTeamLead: false, GroupKey: "group_a"},
			{ID: "v3", FirstName: "Charlie", LastName: "Brown", Gender: "Female", IsTeamLead: true, GroupKey: "group_b"},
		},
		Availability: []VolunteerAvailability{
			{VolunteerID: "v1", HasResponded: true, UnavailableShiftIndices: []int{0}},
			{VolunteerID: "v2", HasResponded: true, UnavailableShiftIndices: []int{1}},
			{VolunteerID: "v3", HasResponded: true, UnavailableShiftIndices: []int{}},
		},
		TotalShifts:      3,
		HistoricalShifts: []*Shift{},
	}

	volunteerState, err := InitVolunteerGroups(input)

	require.NoError(t, err)
	require.Len(t, volunteerState.VolunteerGroups, 2) // group_a and group_b

	// Find groups
	var groupA, groupB *VolunteerGroup
	for _, g := range volunteerState.VolunteerGroups {
		if g.GroupKey == "group_a" {
			groupA = g
		} else if g.GroupKey == "group_b" {
			groupB = g
		}
	}

	require.NotNil(t, groupA)
	require.NotNil(t, groupB)

	// Verify group_a
	assert.Len(t, groupA.Members, 2)
	assert.Equal(t, 2, groupA.MaleCount)
	assert.False(t, groupA.HasTeamLead)
	// Available on shifts where NO member is unavailable: shift 2 only (v1 unavailable on 0, v2 on 1)
	assert.Equal(t, []int{2}, groupA.AvailableShiftIndices)

	// Verify group_b
	assert.Len(t, groupB.Members, 1)
	assert.Equal(t, 0, groupB.MaleCount)
	assert.True(t, groupB.HasTeamLead)
	// Available on all shifts (no unavailability)
	assert.ElementsMatch(t, []int{0, 1, 2}, groupB.AvailableShiftIndices)
}

func TestInitVolunteerGroups_IndividualVolunteers(t *testing.T) {
	input := InitVolunteerGroupsInput{
		Volunteers: []Volunteer{
			{ID: "v1", FirstName: "Alice", LastName: "Smith", Gender: "Female", IsTeamLead: false, GroupKey: ""},
			{ID: "v2", FirstName: "Bob", LastName: "Jones", Gender: "Male", IsTeamLead: true, GroupKey: ""},
		},
		Availability: []VolunteerAvailability{
			{VolunteerID: "v1", HasResponded: true, UnavailableShiftIndices: []int{0}},
			{VolunteerID: "v2", HasResponded: true, UnavailableShiftIndices: []int{1}},
		},
		TotalShifts:      3,
		HistoricalShifts: []*Shift{},
	}

	volunteerState, err := InitVolunteerGroups(input)

	require.NoError(t, err)
	require.Len(t, volunteerState.VolunteerGroups, 2) // Two individual groups

	for _, g := range volunteerState.VolunteerGroups {
		assert.Len(t, g.Members, 1, "Individual volunteers should be in single-member groups")
	}
}

func TestInitVolunteerGroups_ErrorOnGroupWithTwoTeamLeads(t *testing.T) {
	input := InitVolunteerGroupsInput{
		Volunteers: []Volunteer{
			{ID: "v1", FirstName: "Alice", LastName: "Smith", Gender: "Female", IsTeamLead: true, GroupKey: "invalid_group"},
			{ID: "v2", FirstName: "Bob", LastName: "Jones", Gender: "Male", IsTeamLead: true, GroupKey: "invalid_group"},
			{ID: "v3", FirstName: "Charlie", LastName: "Brown", Gender: "Male", IsTeamLead: false, GroupKey: "valid_group"},
		},
		Availability: []VolunteerAvailability{
			{VolunteerID: "v1", HasResponded: true, UnavailableShiftIndices: []int{}},
			{VolunteerID: "v2", HasResponded: true, UnavailableShiftIndices: []int{}},
			{VolunteerID: "v3", HasResponded: true, UnavailableShiftIndices: []int{}},
		},
		TotalShifts:      3,
		HistoricalShifts: []*Shift{},
	}

	_, err := InitVolunteerGroups(input)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_group")
	assert.Contains(t, err.Error(), "2 team leads")
	assert.Contains(t, err.Error(), "max 1 allowed")
}

func TestInitVolunteerGroups_DiscardGroupWithNoResponses(t *testing.T) {
	input := InitVolunteerGroupsInput{
		Volunteers: []Volunteer{
			{ID: "v1", FirstName: "Alice", LastName: "Smith", Gender: "Female", IsTeamLead: false, GroupKey: "no_response_group"},
			{ID: "v2", FirstName: "Bob", LastName: "Jones", Gender: "Male", IsTeamLead: false, GroupKey: "no_response_group"},
			{ID: "v3", FirstName: "Charlie", LastName: "Brown", Gender: "Male", IsTeamLead: false, GroupKey: "has_response_group"},
		},
		Availability: []VolunteerAvailability{
			{VolunteerID: "v1", HasResponded: false, UnavailableShiftIndices: []int{}},
			{VolunteerID: "v2", HasResponded: false, UnavailableShiftIndices: []int{}},
			{VolunteerID: "v3", HasResponded: true, UnavailableShiftIndices: []int{}},
		},
		TotalShifts:      3,
		HistoricalShifts: []*Shift{},
	}

	volunteerState, err := InitVolunteerGroups(input)

	require.NoError(t, err)
	require.Len(t, volunteerState.VolunteerGroups, 1) // Only has_response_group should remain

	assert.Equal(t, "has_response_group", volunteerState.VolunteerGroups[0].GroupKey)
}

func TestInitVolunteerGroups_DiscardGroupWithNoAvailability(t *testing.T) {
	input := InitVolunteerGroupsInput{
		Volunteers: []Volunteer{
			{ID: "v1", FirstName: "Alice", LastName: "Smith", Gender: "Female", IsTeamLead: false, GroupKey: "unavailable_group"},
		},
		Availability: []VolunteerAvailability{
			{VolunteerID: "v1", HasResponded: true, UnavailableShiftIndices: []int{0, 1, 2}}, // Unavailable for all shifts
		},
		TotalShifts:      3,
		HistoricalShifts: []*Shift{},
	}

	_, err := InitVolunteerGroups(input)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no valid volunteer groups")
}

func TestInitVolunteerGroups_GroupAvailabilityLogic(t *testing.T) {
	// Test that group availability = all shifts EXCEPT those where ANY responding member is unavailable
	input := InitVolunteerGroupsInput{
		Volunteers: []Volunteer{
			{ID: "v1", FirstName: "Alice", LastName: "Smith", Gender: "Female", IsTeamLead: false, GroupKey: "group_a"},
			{ID: "v2", FirstName: "Bob", LastName: "Jones", Gender: "Male", IsTeamLead: false, GroupKey: "group_a"},
			{ID: "v3", FirstName: "Charlie", LastName: "Brown", Gender: "Male", IsTeamLead: false, GroupKey: "group_a"},
		},
		Availability: []VolunteerAvailability{
			{VolunteerID: "v1", HasResponded: true, UnavailableShiftIndices: []int{0, 1}},
			{VolunteerID: "v2", HasResponded: true, UnavailableShiftIndices: []int{2}},
			{VolunteerID: "v3", HasResponded: false, UnavailableShiftIndices: []int{}}, // Non-responder doesn't affect availability
		},
		TotalShifts:      5,
		HistoricalShifts: []*Shift{},
	}

	volunteerState, err := InitVolunteerGroups(input)

	require.NoError(t, err)
	require.Len(t, volunteerState.VolunteerGroups, 1)

	group := volunteerState.VolunteerGroups[0]
	// Group unavailable on: 0, 1 (v1), 2 (v2)
	// Group available on: 3, 4
	assert.ElementsMatch(t, []int{3, 4}, group.AvailableShiftIndices)
}

func TestInitVolunteerGroups_HistoricalFrequencyCalculation(t *testing.T) {
	historicalShifts := []*Shift{
		{
			Index: 0,
			AllocatedGroups: []*VolunteerGroup{
				{GroupKey: "group_a"},
				{GroupKey: "group_b"},
			},
		},
		{
			Index: 1,
			AllocatedGroups: []*VolunteerGroup{
				{GroupKey: "group_a"},
			},
		},
		{
			Index: 2,
			AllocatedGroups: []*VolunteerGroup{
				{GroupKey: "group_a"},
			},
		},
	}

	input := InitVolunteerGroupsInput{
		Volunteers: []Volunteer{
			{ID: "v1", FirstName: "Alice", LastName: "Smith", Gender: "Female", IsTeamLead: false, GroupKey: "group_a"},
			{ID: "v2", FirstName: "Bob", LastName: "Jones", Gender: "Male", IsTeamLead: false, GroupKey: "group_b"},
		},
		Availability: []VolunteerAvailability{
			{VolunteerID: "v1", HasResponded: true, UnavailableShiftIndices: []int{}},
			{VolunteerID: "v2", HasResponded: true, UnavailableShiftIndices: []int{}},
		},
		TotalShifts:      3,
		HistoricalShifts: historicalShifts,
	}

	volunteerState, err := InitVolunteerGroups(input)

	require.NoError(t, err)
	require.Len(t, volunteerState.VolunteerGroups, 2)

	// Find groups
	var groupA, groupB *VolunteerGroup
	for _, g := range volunteerState.VolunteerGroups {
		if g.GroupKey == "group_a" {
			groupA = g
		} else if g.GroupKey == "group_b" {
			groupB = g
		}
	}

	// group_a was allocated 3 times
	assert.Equal(t, 3, groupA.HistoricalAllocationCount)

	// group_b was allocated 1 time
	assert.Equal(t, 1, groupB.HistoricalAllocationCount)
}

func TestInitVolunteerGroups_MaleCountAccuracy(t *testing.T) {
	input := InitVolunteerGroupsInput{
		Volunteers: []Volunteer{
			{ID: "v1", FirstName: "Alice", LastName: "Smith", Gender: "Female", IsTeamLead: false, GroupKey: "group_a"},
			{ID: "v2", FirstName: "Bob", LastName: "Jones", Gender: "Male", IsTeamLead: false, GroupKey: "group_a"},
			{ID: "v3", FirstName: "Charlie", LastName: "Brown", Gender: "Male", IsTeamLead: false, GroupKey: "group_a"},
		},
		Availability: []VolunteerAvailability{
			{VolunteerID: "v1", HasResponded: true, UnavailableShiftIndices: []int{}},
			{VolunteerID: "v2", HasResponded: true, UnavailableShiftIndices: []int{}},
			{VolunteerID: "v3", HasResponded: true, UnavailableShiftIndices: []int{}},
		},
		TotalShifts:      3,
		HistoricalShifts: []*Shift{},
	}

	volunteerState, err := InitVolunteerGroups(input)

	require.NoError(t, err)
	require.Len(t, volunteerState.VolunteerGroups, 1)

	assert.Equal(t, 2, volunteerState.VolunteerGroups[0].MaleCount, "Should count 2 males in group")
}

func TestInitVolunteerGroups_NonRespondingMembersIgnored(t *testing.T) {
	// Test that non-responding members don't make the group unavailable on all dates
	input := InitVolunteerGroupsInput{
		Volunteers: []Volunteer{
			{ID: "v1", FirstName: "Alice", LastName: "Smith", Gender: "Female", IsTeamLead: false, GroupKey: "group_a"},
			{ID: "v2", FirstName: "Bob", LastName: "Jones", Gender: "Male", IsTeamLead: false, GroupKey: "group_a"},
		},
		Availability: []VolunteerAvailability{
			{VolunteerID: "v1", HasResponded: true, UnavailableShiftIndices: []int{0}},
			{VolunteerID: "v2", HasResponded: false, UnavailableShiftIndices: []int{}}, // Not responded
		},
		TotalShifts:      3,
		HistoricalShifts: []*Shift{},
	}

	volunteerState, err := InitVolunteerGroups(input)

	require.NoError(t, err)
	require.Len(t, volunteerState.VolunteerGroups, 1)

	group := volunteerState.VolunteerGroups[0]
	// Group should be available on 1, 2 (not 0 due to Alice)
	// Bob's non-response should NOT make all dates unavailable
	assert.ElementsMatch(t, []int{1, 2}, group.AvailableShiftIndices)
}

func TestInitShifts_ClosedShifts(t *testing.T) {
	volunteers := []Volunteer{
		{ID: "v1", FirstName: "Alice", LastName: "Smith", Gender: "Female", IsTeamLead: false, GroupKey: "group_a"},
	}
	availability := []VolunteerAvailability{
		{VolunteerID: "v1", HasResponded: true, UnavailableShiftIndices: []int{}},
	}

	volunteerState, err := InitVolunteerGroups(InitVolunteerGroupsInput{
		Volunteers:       volunteers,
		Availability:     availability,
		TotalShifts:      3,
		HistoricalShifts: []*Shift{},
	})
	require.NoError(t, err)

	// Create overrides - shift 1 is closed
	overrides := []ShiftOverride{
		{
			AppliesTo: func(date string) bool {
				return date == "2025-01-12" // Second shift
			},
			ShiftSize: nil,
			Closed:    true,
		},
	}

	input := InitShiftsInput{
		ShiftDates:       []string{"2025-01-05", "2025-01-12", "2025-01-19"},
		DefaultShiftSize: 4,
		Overrides:        overrides,
		VolunteerState:   volunteerState,
	}

	shifts, err := InitShifts(input)
	require.NoError(t, err)
	require.Len(t, shifts, 3)

	// Shift 0 should be open
	assert.False(t, shifts[0].Closed)
	assert.NotEmpty(t, shifts[0].AvailableGroups, "Open shift should have available groups")
	assert.Equal(t, 4, shifts[0].Size)

	// Shift 1 should be closed
	assert.True(t, shifts[1].Closed, "Shift 1 should be marked as closed")
	assert.Empty(t, shifts[1].AvailableGroups, "Closed shift should have no available groups")
	assert.Equal(t, 4, shifts[1].Size, "Closed shift should still have default size")

	// Shift 2 should be open
	assert.False(t, shifts[2].Closed)
	assert.NotEmpty(t, shifts[2].AvailableGroups, "Open shift should have available groups")
}

func TestInitShifts_ClosedShifts_IgnoresPreallocations(t *testing.T) {
	volunteers := []Volunteer{
		{ID: "v1", FirstName: "Alice", LastName: "Smith", Gender: "Female", IsTeamLead: false, GroupKey: "group_a"},
	}
	availability := []VolunteerAvailability{
		{VolunteerID: "v1", HasResponded: true, UnavailableShiftIndices: []int{}},
	}

	volunteerState, err := InitVolunteerGroups(InitVolunteerGroupsInput{
		Volunteers:       volunteers,
		Availability:     availability,
		TotalShifts:      2,
		HistoricalShifts: []*Shift{},
	})
	require.NoError(t, err)

	// Create override with preallocations on a closed shift
	overrides := []ShiftOverride{
		{
			AppliesTo: func(date string) bool {
				return date == "2025-01-05"
			},
			ShiftSize:            nil,
			CustomPreallocations: []string{"John", "Jane"}, // Should be ignored
			Closed:               true,
		},
	}

	input := InitShiftsInput{
		ShiftDates:       []string{"2025-01-05", "2025-01-12"},
		DefaultShiftSize: 4,
		Overrides:        overrides,
		VolunteerState:   volunteerState,
	}

	shifts, err := InitShifts(input)
	require.NoError(t, err)
	require.Len(t, shifts, 2)

	// Closed shift should ignore preallocations
	assert.True(t, shifts[0].Closed)
	assert.Empty(t, shifts[0].CustomPreallocations, "Closed shift should ignore preallocations")

	// Non-closed shift should be normal
	assert.False(t, shifts[1].Closed)
	assert.Empty(t, shifts[1].CustomPreallocations)
}

func TestCalculateCapacityMetrics_BasicCalculation(t *testing.T) {
	// Create volunteer groups
	group1 := &VolunteerGroup{
		GroupKey: "group_1",
		Members: []Volunteer{
			{ID: "v1", IsTeamLead: false},
			{ID: "v2", IsTeamLead: false},
		},
		AvailableShiftIndices: []int{0, 1, 2}, // Available for 3 shifts
	}
	group2 := &VolunteerGroup{
		GroupKey: "group_2",
		Members: []Volunteer{
			{ID: "v3", IsTeamLead: false},
		},
		AvailableShiftIndices: []int{0, 1}, // Available for 2 shifts
	}

	volunteerState := &VolunteerState{
		VolunteerGroups:          []*VolunteerGroup{group1, group2},
		ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
	}

	shifts := []*Shift{
		{Index: 0, Size: 4, Closed: false},
		{Index: 1, Size: 3, Closed: false},
		{Index: 2, Size: 4, Closed: false},
	}

	// maxAllocationCount = 2 (assume 50% frequency with 4 shifts = 2)
	maxAllocationCount := 2

	totalCapacity, totalSlotsNeeded, openShiftCount := calculateCapacityMetrics(volunteerState, shifts, maxAllocationCount)

	// group1: min(2, 3) = 2 allocations * 2 ordinary volunteers = 4 slot-fills
	// group2: min(2, 2) = 2 allocations * 1 ordinary volunteer = 2 slot-fills
	// Total capacity: 4 + 2 = 6
	assert.Equal(t, 6, totalCapacity)

	// Slots needed: 4 + 3 + 4 = 11
	assert.Equal(t, 11, totalSlotsNeeded)

	// Non-closed shifts: 3
	assert.Equal(t, 3, openShiftCount)
}

func TestCalculateCapacityMetrics_ClosedShiftsExcluded(t *testing.T) {
	group := &VolunteerGroup{
		GroupKey: "group_1",
		Members: []Volunteer{
			{ID: "v1", IsTeamLead: false},
		},
		AvailableShiftIndices: []int{0, 1, 2},
	}

	volunteerState := &VolunteerState{
		VolunteerGroups:          []*VolunteerGroup{group},
		ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
	}

	shifts := []*Shift{
		{Index: 0, Size: 4, Closed: false},
		{Index: 1, Size: 4, Closed: true}, // Closed - should not count toward needed
		{Index: 2, Size: 4, Closed: false},
	}

	maxAllocationCount := 3

	_, totalSlotsNeeded, openShiftCount := calculateCapacityMetrics(volunteerState, shifts, maxAllocationCount)

	// Only count non-closed shifts: 4 + 4 = 8 (not 12)
	assert.Equal(t, 8, totalSlotsNeeded)

	// Non-closed shifts: 2 (not 3)
	assert.Equal(t, 2, openShiftCount)
}

func TestCalculateCapacityMetrics_TeamLeadsExcluded(t *testing.T) {
	// Group with team lead - only ordinary volunteers count
	group := &VolunteerGroup{
		GroupKey: "group_1",
		Members: []Volunteer{
			{ID: "v1", IsTeamLead: true},  // Team lead - doesn't count
			{ID: "v2", IsTeamLead: false}, // Ordinary - counts
		},
		AvailableShiftIndices: []int{0, 1, 2},
	}

	volunteerState := &VolunteerState{
		VolunteerGroups:          []*VolunteerGroup{group},
		ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
	}

	shifts := []*Shift{
		{Index: 0, Size: 4, Closed: false},
		{Index: 1, Size: 4, Closed: false},
	}

	maxAllocationCount := 2

	totalCapacity, _, _ := calculateCapacityMetrics(volunteerState, shifts, maxAllocationCount)

	// group: min(2, 3) = 2 allocations * 1 ordinary volunteer = 2 slot-fills
	assert.Equal(t, 2, totalCapacity)
}

func TestCalculateCapacityMetrics_LimitedAvailability(t *testing.T) {
	// Group with limited availability (less than maxAllocationCount)
	group := &VolunteerGroup{
		GroupKey: "group_1",
		Members: []Volunteer{
			{ID: "v1", IsTeamLead: false},
			{ID: "v2", IsTeamLead: false},
		},
		AvailableShiftIndices: []int{0}, // Only available for 1 shift
	}

	volunteerState := &VolunteerState{
		VolunteerGroups:          []*VolunteerGroup{group},
		ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
	}

	shifts := []*Shift{
		{Index: 0, Size: 4, Closed: false},
		{Index: 1, Size: 4, Closed: false},
		{Index: 2, Size: 4, Closed: false},
	}

	maxAllocationCount := 3 // Could do 3, but only available for 1

	totalCapacity, _, _ := calculateCapacityMetrics(volunteerState, shifts, maxAllocationCount)

	// group: min(3, 1) = 1 allocation * 2 ordinary volunteers = 2 slot-fills
	assert.Equal(t, 2, totalCapacity)
}

func TestIsResourceConstrained(t *testing.T) {
	// Resource constrained case
	constrainedState := &RotaState{
		TotalVolunteerCapacity: 10,
		TotalSlotsNeeded:       20,
	}
	assert.True(t, constrainedState.IsResourceConstrained())

	// Not constrained case - exact match
	exactState := &RotaState{
		TotalVolunteerCapacity: 20,
		TotalSlotsNeeded:       20,
	}
	assert.False(t, exactState.IsResourceConstrained())

	// Not constrained case - surplus
	surplusState := &RotaState{
		TotalVolunteerCapacity: 30,
		TotalSlotsNeeded:       20,
	}
	assert.False(t, surplusState.IsResourceConstrained())
}

func TestExpectedFillPerShift(t *testing.T) {
	// Basic calculation: 12 capacity / 4 shifts = 3.0
	state := &RotaState{
		TotalVolunteerCapacity: 12,
		OpenShiftCount:         4,
	}
	assert.Equal(t, 3.0, state.ExpectedFillPerShift())

	// Fractional result: 10 capacity / 4 shifts = 2.5
	fractionalState := &RotaState{
		TotalVolunteerCapacity: 10,
		OpenShiftCount:         4,
	}
	assert.Equal(t, 2.5, fractionalState.ExpectedFillPerShift())

	// Zero shifts (edge case): returns 0
	zeroShiftsState := &RotaState{
		TotalVolunteerCapacity: 10,
		OpenShiftCount:         0,
	}
	assert.Equal(t, 0.0, zeroShiftsState.ExpectedFillPerShift())
}
