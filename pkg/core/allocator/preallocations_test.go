package allocator

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyPreallocations_OrdinaryVolunteer(t *testing.T) {
	// Test preallocating an ordinary volunteer to a shift
	config := AllocationConfig{
		Volunteers: []Volunteer{
			{ID: "v1", FirstName: "Alice", LastName: "Smith", Gender: "Female", IsTeamLead: false, GroupKey: ""},
			{ID: "v2", FirstName: "Bob", LastName: "Jones", Gender: "Male", IsTeamLead: true, GroupKey: ""},
		},
		Availability: []VolunteerAvailability{
			{VolunteerID: "v1", HasResponded: true, UnavailableShiftIndices: []int{0}}, // Alice unavailable for shift 0
			{VolunteerID: "v2", HasResponded: true, UnavailableShiftIndices: []int{}},
		},
		ShiftDates: []string{"2025-01-05", "2025-01-12"},
		Overrides: []ShiftOverride{
			{
				AppliesTo: func(date string) bool {
					return date == "2025-01-05" // First shift
				},
				PreallocatedVolunteerIDs: []string{"v1"}, // Preallocate Alice despite unavailability
			},
		},
		DefaultShiftSize:               2,
		MaxAllocationFrequency:         1.0,
		HistoricalShifts:               []*Shift{},
		Criteria:                       []Criterion{},
		WeightCurrentRotaUrgency:       1.0,
		WeightOverallFrequencyFairness: 1.0,
		WeightPromoteGroup:             1.0,
	}

	allocator, err := InitAllocation(config)
	require.NoError(t, err)

	// Apply preallocations
	err = allocator.ApplyPreallocations(allocator.state)
	require.NoError(t, err)

	// Verify Alice was preallocated to shift 0 despite being unavailable
	shift0 := allocator.state.Shifts[0]
	require.Len(t, shift0.AllocatedGroups, 1, "Shift 0 should have one allocated group")

	// Find Alice in the allocated group
	foundAlice := false
	for _, group := range shift0.AllocatedGroups {
		for _, member := range group.Members {
			if member.ID == "v1" {
				foundAlice = true
				break
			}
		}
	}
	assert.True(t, foundAlice, "Alice should be allocated to shift 0")

	// Verify the group knows it's allocated to shift 0
	aliceGroup := shift0.AllocatedGroups[0]
	assert.Contains(t, aliceGroup.AllocatedShiftIndices, 0, "Alice's group should have shift 0 in allocated indices")
}

func TestApplyPreallocations_TeamLead(t *testing.T) {
	// Test preallocating a team lead to a shift
	config := AllocationConfig{
		Volunteers: []Volunteer{
			{ID: "tl1", FirstName: "Alice", LastName: "Smith", Gender: "Female", IsTeamLead: true, GroupKey: ""},
			{ID: "v1", FirstName: "Bob", LastName: "Jones", Gender: "Male", IsTeamLead: false, GroupKey: ""},
		},
		Availability: []VolunteerAvailability{
			{VolunteerID: "tl1", HasResponded: true, UnavailableShiftIndices: []int{0}}, // Unavailable
			{VolunteerID: "v1", HasResponded: true, UnavailableShiftIndices: []int{}},
		},
		ShiftDates: []string{"2025-01-05", "2025-01-12"},
		Overrides: []ShiftOverride{
			{
				AppliesTo: func(date string) bool {
					return date == "2025-01-05"
				},
				PreallocatedTeamLeadID: "tl1", // Preallocate team lead
			},
		},
		DefaultShiftSize:               2,
		MaxAllocationFrequency:         1.0,
		HistoricalShifts:               []*Shift{},
		Criteria:                       []Criterion{},
		WeightCurrentRotaUrgency:       1.0,
		WeightOverallFrequencyFairness: 1.0,
		WeightPromoteGroup:             1.0,
	}

	allocator, err := InitAllocation(config)
	require.NoError(t, err)

	err = allocator.ApplyPreallocations(allocator.state)
	require.NoError(t, err)

	shift0 := allocator.state.Shifts[0]

	// Verify team lead was set
	require.NotNil(t, shift0.TeamLead, "Shift should have a team lead")
	assert.Equal(t, "tl1", shift0.TeamLead.ID, "Team lead should be Alice")
	assert.True(t, shift0.TeamLead.IsTeamLead, "Team lead should be marked as team lead")

	// Verify group was allocated
	require.Len(t, shift0.AllocatedGroups, 1, "Shift should have one allocated group")

	// Verify group knows it's allocated
	group := shift0.AllocatedGroups[0]
	assert.Contains(t, group.AllocatedShiftIndices, 0)
}

func TestApplyPreallocations_BothOrdinaryAndTeamLead(t *testing.T) {
	// Test preallocating both ordinary volunteers and team lead to same shift
	config := AllocationConfig{
		Volunteers: []Volunteer{
			{ID: "tl1", FirstName: "Alice", LastName: "Smith", Gender: "Female", IsTeamLead: true, GroupKey: ""},
			{ID: "v1", FirstName: "Bob", LastName: "Jones", Gender: "Male", IsTeamLead: false, GroupKey: ""},
			{ID: "v2", FirstName: "Charlie", LastName: "Brown", Gender: "Male", IsTeamLead: false, GroupKey: ""},
		},
		Availability: []VolunteerAvailability{
			{VolunteerID: "tl1", HasResponded: true, UnavailableShiftIndices: []int{}},
			{VolunteerID: "v1", HasResponded: true, UnavailableShiftIndices: []int{}},
			{VolunteerID: "v2", HasResponded: true, UnavailableShiftIndices: []int{}},
		},
		ShiftDates: []string{"2025-01-05"},
		Overrides: []ShiftOverride{
			{
				AppliesTo: func(date string) bool {
					return date == "2025-01-05"
				},
				PreallocatedTeamLeadID:   "tl1",
				PreallocatedVolunteerIDs: []string{"v1", "v2"},
			},
		},
		DefaultShiftSize:               3,
		MaxAllocationFrequency:         1.0,
		HistoricalShifts:               []*Shift{},
		Criteria:                       []Criterion{},
		WeightCurrentRotaUrgency:       1.0,
		WeightOverallFrequencyFairness: 1.0,
		WeightPromoteGroup:             1.0,
	}

	allocator, err := InitAllocation(config)
	require.NoError(t, err)

	err = allocator.ApplyPreallocations(allocator.state)
	require.NoError(t, err)

	shift0 := allocator.state.Shifts[0]

	// Verify team lead
	require.NotNil(t, shift0.TeamLead)
	assert.Equal(t, "tl1", shift0.TeamLead.ID)

	// Verify all three groups are allocated
	assert.Len(t, shift0.AllocatedGroups, 3, "Should have 3 allocated groups (one per volunteer)")

	// Verify all volunteers are allocated
	allocatedIDs := make(map[string]bool)
	for _, group := range shift0.AllocatedGroups {
		for _, member := range group.Members {
			allocatedIDs[member.ID] = true
		}
	}
	assert.True(t, allocatedIDs["tl1"], "Alice should be allocated")
	assert.True(t, allocatedIDs["v1"], "Bob should be allocated")
	assert.True(t, allocatedIDs["v2"], "Charlie should be allocated")
}

func TestApplyPreallocations_VolunteerNotFound(t *testing.T) {
	// Test error when preallocated volunteer doesn't exist
	config := AllocationConfig{
		Volunteers: []Volunteer{
			{ID: "v1", FirstName: "Alice", LastName: "Smith", Gender: "Female", IsTeamLead: false, GroupKey: ""},
		},
		Availability: []VolunteerAvailability{
			{VolunteerID: "v1", HasResponded: true, UnavailableShiftIndices: []int{}},
		},
		ShiftDates: []string{"2025-01-05"},
		Overrides: []ShiftOverride{
			{
				AppliesTo: func(date string) bool {
					return date == "2025-01-05"
				},
				PreallocatedVolunteerIDs: []string{"v999"}, // Non-existent volunteer
			},
		},
		DefaultShiftSize:               2,
		MaxAllocationFrequency:         1.0,
		HistoricalShifts:               []*Shift{},
		Criteria:                       []Criterion{},
		WeightCurrentRotaUrgency:       1.0,
		WeightOverallFrequencyFairness: 1.0,
		WeightPromoteGroup:             1.0,
	}

	allocator, err := InitAllocation(config)
	require.NoError(t, err)

	err = allocator.ApplyPreallocations(allocator.state)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "preallocated volunteer not found")
	assert.Contains(t, err.Error(), "v999")
}

func TestApplyPreallocations_TeamLeadNotMarked(t *testing.T) {
	// Test error when preallocated team lead is not marked as team lead
	config := AllocationConfig{
		Volunteers: []Volunteer{
			{ID: "v1", FirstName: "Alice", LastName: "Smith", Gender: "Female", IsTeamLead: false, GroupKey: ""}, // Not a team lead!
		},
		Availability: []VolunteerAvailability{
			{VolunteerID: "v1", HasResponded: true, UnavailableShiftIndices: []int{}},
		},
		ShiftDates: []string{"2025-01-05"},
		Overrides: []ShiftOverride{
			{
				AppliesTo: func(date string) bool {
					return date == "2025-01-05"
				},
				PreallocatedTeamLeadID: "v1", // Trying to preallocate non-team-lead
			},
		},
		DefaultShiftSize:               2,
		MaxAllocationFrequency:         1.0,
		HistoricalShifts:               []*Shift{},
		Criteria:                       []Criterion{},
		WeightCurrentRotaUrgency:       1.0,
		WeightOverallFrequencyFairness: 1.0,
		WeightPromoteGroup:             1.0,
	}

	allocator, err := InitAllocation(config)
	require.NoError(t, err)

	err = allocator.ApplyPreallocations(allocator.state)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not marked as team lead")
}

func TestApplyPreallocations_SkipsClosedShifts(t *testing.T) {
	// Test that preallocations are ignored for closed shifts
	config := AllocationConfig{
		Volunteers: []Volunteer{
			{ID: "v1", FirstName: "Alice", LastName: "Smith", Gender: "Female", IsTeamLead: false, GroupKey: ""},
		},
		Availability: []VolunteerAvailability{
			{VolunteerID: "v1", HasResponded: true, UnavailableShiftIndices: []int{}},
		},
		ShiftDates: []string{"2025-01-05"},
		Overrides: []ShiftOverride{
			{
				AppliesTo: func(date string) bool {
					return date == "2025-01-05"
				},
				Closed:                   true,
				PreallocatedVolunteerIDs: []string{"v1"}, // Should be ignored
			},
		},
		DefaultShiftSize:               2,
		MaxAllocationFrequency:         1.0,
		HistoricalShifts:               []*Shift{},
		Criteria:                       []Criterion{},
		WeightCurrentRotaUrgency:       1.0,
		WeightOverallFrequencyFairness: 1.0,
		WeightPromoteGroup:             1.0,
	}

	allocator, err := InitAllocation(config)
	require.NoError(t, err)

	err = allocator.ApplyPreallocations(allocator.state)
	require.NoError(t, err)

	// Verify shift is closed and empty
	shift0 := allocator.state.Shifts[0]
	assert.True(t, shift0.Closed)
	assert.Empty(t, shift0.AllocatedGroups, "Closed shift should remain empty despite preallocations")
}

func TestApplyPreallocations_CountsTowardAllocationFrequency(t *testing.T) {
	// Test that preallocated volunteers count toward MaxAllocationFrequency
	config := AllocationConfig{
		Volunteers: []Volunteer{
			{ID: "v1", FirstName: "Alice", LastName: "Smith", Gender: "Female", IsTeamLead: false, GroupKey: ""},
			{ID: "v2", FirstName: "Bob", LastName: "Jones", Gender: "Male", IsTeamLead: true, GroupKey: ""},
		},
		Availability: []VolunteerAvailability{
			{VolunteerID: "v1", HasResponded: true, UnavailableShiftIndices: []int{}},
			{VolunteerID: "v2", HasResponded: true, UnavailableShiftIndices: []int{}},
		},
		ShiftDates: []string{"2025-01-05", "2025-01-12", "2025-01-19"},
		Overrides: []ShiftOverride{
			{
				AppliesTo: func(date string) bool {
					return date == "2025-01-05"
				},
				PreallocatedVolunteerIDs: []string{"v1"}, // Preallocate Alice to shift 0
			},
		},
		DefaultShiftSize:       1,
		MaxAllocationFrequency: 0.33, // Max 1 shift out of 3
		HistoricalShifts:       []*Shift{},
		Criteria: []Criterion{
			&mockCriterion{
				name:           "test",
				affinityValue:  1.0,
				affinityWeight: 1.0,
			},
		},
		WeightCurrentRotaUrgency:       1.0,
		WeightOverallFrequencyFairness: 1.0,
		WeightPromoteGroup:             1.0,
	}

	outcome, err := Allocate(config)
	require.NoError(t, err)

	// Verify Alice was preallocated to shift 0
	shift0 := outcome.State.Shifts[0]
	foundAlice := false
	for _, group := range shift0.AllocatedGroups {
		for _, member := range group.Members {
			if member.ID == "v1" {
				foundAlice = true
				break
			}
		}
	}
	assert.True(t, foundAlice, "Alice should be preallocated to shift 0")

	// Verify Alice is not allocated to any other shifts (due to max frequency)
	for i, shift := range outcome.State.Shifts {
		if i == 0 {
			continue // Skip shift 0, already checked
		}
		for _, group := range shift.AllocatedGroups {
			for _, member := range group.Members {
				assert.NotEqual(t, "v1", member.ID,
					"Alice should not be allocated to shift %d due to max allocation frequency", i)
			}
		}
	}
}
