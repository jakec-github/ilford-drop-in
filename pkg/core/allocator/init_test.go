package allocator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitVolunteerGroups_BasicGrouping(t *testing.T) {
	input := InitVolunteerGroupsInput{
		Volunteers: []Volunteer{
			{ID: "v1", FirstName: "Alice", LastName: "Smith", Gender: "Male", GroupKey: "group_a"},
			{ID: "v2", FirstName: "Bob", LastName: "Jones", Gender: "Male", GroupKey: "group_a"},
			{ID: "v3", FirstName: "Charlie", LastName: "Brown", Gender: "Female", GroupKey: "group_b"},
		},
		GroupAvailability: map[string][]int{
			"group_a": {2},
			"group_b": {0, 1, 2},
		},
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
	// Available on shifts where NO member is unavailable: shift 2 only (v1 unavailable on 0, v2 on 1)
	assert.Equal(t, []int{2}, groupA.AvailableShiftIndices)

	// Verify group_b
	assert.Len(t, groupB.Members, 1)
	assert.Equal(t, 0, groupB.MaleCount)
	// Available on all shifts (no unavailability)
	assert.ElementsMatch(t, []int{0, 1, 2}, groupB.AvailableShiftIndices)
}

func TestInitVolunteerGroups_IndividualVolunteers(t *testing.T) {
	input := InitVolunteerGroupsInput{
		Volunteers: []Volunteer{
			{ID: "v1", FirstName: "Alice", LastName: "Smith", Gender: "Female", GroupKey: ""},
			{ID: "v2", FirstName: "Bob", LastName: "Jones", Gender: "Male", GroupKey: ""},
		},
		GroupAvailability: map[string][]int{
			"Alice Smith": {1, 2},
			"Bob Jones":   {0, 2},
		},
		HistoricalShifts: []*Shift{},
	}

	volunteerState, err := InitVolunteerGroups(input)

	require.NoError(t, err)
	require.Len(t, volunteerState.VolunteerGroups, 2) // Two individual groups

	for _, g := range volunteerState.VolunteerGroups {
		assert.Len(t, g.Members, 1, "Individual volunteers should be in single-member groups")
	}
}

// A couple who both lead used to be rejected outright, because the ceiling on
// leads was counted per group. Seats carry that ceiling now, and the second
// lead is simply someone who can also take an ordinary Seat, so the group is
// ordinary too (#89).
func TestInitVolunteerGroups_GroupWithTwoTeamLeadsIsAllowed(t *testing.T) {
	input := InitVolunteerGroupsInput{
		Volunteers: []Volunteer{
			{ID: "v1", FirstName: "Alice", LastName: "Smith", Gender: "Female", GroupKey: "lead_couple"},
			{ID: "v2", FirstName: "Bob", LastName: "Jones", Gender: "Male", GroupKey: "lead_couple"},
		},
		GroupAvailability: map[string][]int{"lead_couple": {0, 1, 2}},
		HistoricalShifts:  []*Shift{},
	}

	volunteerState, err := InitVolunteerGroups(input)

	require.NoError(t, err)
	require.Len(t, volunteerState.VolunteerGroups, 1)
	assert.Len(t, volunteerState.VolunteerGroups[0].Members, 2)
}

func TestInitVolunteerGroups_DiscardGroupWithNoResponses(t *testing.T) {
	input := InitVolunteerGroupsInput{
		Volunteers: []Volunteer{
			{ID: "v1", FirstName: "Alice", LastName: "Smith", Gender: "Female", GroupKey: "no_response_group"},
			{ID: "v2", FirstName: "Bob", LastName: "Jones", Gender: "Male", GroupKey: "no_response_group"},
			{ID: "v3", FirstName: "Charlie", LastName: "Brown", Gender: "Male", GroupKey: "has_response_group"},
		},
		// no_response_group is simply absent from the map.
		GroupAvailability: map[string][]int{"has_response_group": {0, 1, 2}},
		HistoricalShifts:  []*Shift{},
	}

	volunteerState, err := InitVolunteerGroups(input)

	require.NoError(t, err)
	require.Len(t, volunteerState.VolunteerGroups, 1) // Only has_response_group should remain

	assert.Equal(t, "has_response_group", volunteerState.VolunteerGroups[0].GroupKey)
}

func TestInitVolunteerGroups_DiscardGroupWithNoAvailability(t *testing.T) {
	input := InitVolunteerGroupsInput{
		Volunteers: []Volunteer{
			{ID: "v1", FirstName: "Alice", LastName: "Smith", Gender: "Female", GroupKey: "unavailable_group"},
		},
		// Answered, but for nothing: present in the map with no shifts.
		GroupAvailability: map[string][]int{"unavailable_group": {}},
		HistoricalShifts:  []*Shift{},
	}

	_, err := InitVolunteerGroups(input)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no valid volunteer groups")
}

// A volunteer with no group is their own group of one — the key every caller
// has to agree on to line availability up with the groups it belongs to.
func TestGroupKeyFor(t *testing.T) {
	assert.Equal(t, "couple_ab", GroupKeyFor(Volunteer{FirstName: "Alice", LastName: "Smith", GroupKey: "couple_ab"}))
	assert.Equal(t, "Alice Smith", GroupKeyFor(Volunteer{FirstName: "Alice", LastName: "Smith"}))
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
			{ID: "v1", FirstName: "Alice", LastName: "Smith", Gender: "Female", GroupKey: "group_a"},
			{ID: "v2", FirstName: "Bob", LastName: "Jones", Gender: "Male", GroupKey: "group_b"},
		},
		GroupAvailability: map[string][]int{
			"group_a": {0, 1, 2},
			"group_b": {0, 1, 2},
		},
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
			{ID: "v1", FirstName: "Alice", LastName: "Smith", Gender: "Female", GroupKey: "group_a"},
			{ID: "v2", FirstName: "Bob", LastName: "Jones", Gender: "Male", GroupKey: "group_a"},
			{ID: "v3", FirstName: "Charlie", LastName: "Brown", Gender: "Male", GroupKey: "group_a"},
		},
		GroupAvailability: map[string][]int{"group_a": {0, 1, 2}},
		HistoricalShifts:  []*Shift{},
	}

	volunteerState, err := InitVolunteerGroups(input)

	require.NoError(t, err)
	require.Len(t, volunteerState.VolunteerGroups, 1)

	assert.Equal(t, 2, volunteerState.VolunteerGroups[0].MaleCount, "Should count 2 males in group")
}

// The map is taken as given: whatever settled a group's answer, the allocator
// applies it verbatim rather than second-guessing it against the members.
func TestInitVolunteerGroups_AppliesGroupAvailabilityVerbatim(t *testing.T) {
	input := InitVolunteerGroupsInput{
		Volunteers: []Volunteer{
			{ID: "v1", FirstName: "Alice", LastName: "Smith", Gender: "Female", GroupKey: "group_a"},
			{ID: "v2", FirstName: "Bob", LastName: "Jones", Gender: "Male", GroupKey: "group_a"},
		},
		GroupAvailability: map[string][]int{"group_a": {1, 2}},
		HistoricalShifts:  []*Shift{},
	}

	volunteerState, err := InitVolunteerGroups(input)

	require.NoError(t, err)
	require.Len(t, volunteerState.VolunteerGroups, 1)
	assert.Equal(t, []int{1, 2}, volunteerState.VolunteerGroups[0].AvailableShiftIndices)
}

func TestInitShifts_ClosedShifts(t *testing.T) {
	volunteers := []Volunteer{
		{ID: "v1", FirstName: "Alice", LastName: "Smith", Gender: "Female", GroupKey: "group_a"},
	}
	volunteerState, err := InitVolunteerGroups(InitVolunteerGroupsInput{
		Volunteers:        volunteers,
		GroupAvailability: map[string][]int{"group_a": {0, 1, 2}},
		HistoricalShifts:  []*Shift{},
	})
	require.NoError(t, err)

	// Shift 1 is closed — a field on the Shift, not something an override says.
	input := InitShiftsInput{
		Shifts: []ShiftSpec{
			{Date: "2025-01-05"},
			{Date: "2025-01-12", Closed: true},
			{Date: "2025-01-19"},
		},
		DefaultShiftSize: 4,
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
		{ID: "v1", FirstName: "Alice", LastName: "Smith", Gender: "Female", GroupKey: "group_a"},
	}
	volunteerState, err := InitVolunteerGroups(InitVolunteerGroupsInput{
		Volunteers:        volunteers,
		GroupAvailability: map[string][]int{"group_a": {0, 1}},
		HistoricalShifts:  []*Shift{},
	})
	require.NoError(t, err)

	// Pins landing on a shift that is closed: the pins come from an override,
	// the closure from the shift, and the shift wins.
	overrides := []ShiftOverride{
		{
			AppliesTo: func(date string) bool {
				return date == "2025-01-05"
			},
			ShiftSize: nil,
			Preallocations: []Preallocation{ // Should be ignored
				{Custom: "John", Role: "Service volunteer"},
				{Custom: "Jane", Role: "Service volunteer"},
			},
		},
	}

	input := InitShiftsInput{
		Shifts: []ShiftSpec{
			{Date: "2025-01-05", Closed: true},
			{Date: "2025-01-12"},
		},
		DefaultShiftSize: 4,
		Overrides:        overrides,
		VolunteerState:   volunteerState,
	}

	shifts, err := InitShifts(input)
	require.NoError(t, err)
	require.Len(t, shifts, 2)

	// Closed shift should ignore preallocations
	assert.True(t, shifts[0].Closed)
	assert.Empty(t, shifts[0].Preallocations, "Closed shift should ignore preallocations")

	// Non-closed shift should be normal
	assert.False(t, shifts[1].Closed)
	assert.Empty(t, shifts[1].Preallocations)
}
