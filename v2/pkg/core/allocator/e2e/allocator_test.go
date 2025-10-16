package e2e

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllocator_EndToEnd(t *testing.T) {
	// Setup volunteers - enough to fill all shifts
	// 7 shifts * 2 volunteers per shift = 14 volunteer-shifts needed
	// With 33% frequency, we need: 14 / 0.33 ≈ 42 volunteer-capacity
	volunteers := []Volunteer{
		// Team lead couple
		{ID: "alice", FirstName: "Alice", LastName: "Smith", Gender: "Female", IsTeamLead: true, GroupKey: "couple_alice_bob"},
		{ID: "bob", FirstName: "Bob", LastName: "Smith", Gender: "Male", IsTeamLead: false, GroupKey: "couple_alice_bob"},
		// Team lead couple 2
		{ID: "george", FirstName: "George", LastName: "Johnson", Gender: "Male", IsTeamLead: true, GroupKey: "couple_george_helen"},
		{ID: "helen", FirstName: "Helen", LastName: "Johnson", Gender: "Female", IsTeamLead: false, GroupKey: "couple_george_helen"},
		// Team lead couple 3
		{ID: "karen", FirstName: "Karen", LastName: "Davis", Gender: "Female", IsTeamLead: true, GroupKey: "couple_karen_larry"},
		{ID: "larry", FirstName: "Larry", LastName: "Davis", Gender: "Male", IsTeamLead: false, GroupKey: "couple_karen_larry"},
		// Individual volunteers
		{ID: "charlie", FirstName: "Charlie", LastName: "Brown", Gender: "Male", IsTeamLead: false, GroupKey: ""},
		{ID: "diana", FirstName: "Diana", LastName: "Green", Gender: "Female", IsTeamLead: false, GroupKey: ""},
		// Another couple
		{ID: "eve", FirstName: "Eve", LastName: "White", Gender: "Female", IsTeamLead: false, GroupKey: "couple_eve_frank"},
		{ID: "frank", FirstName: "Frank", LastName: "White", Gender: "Male", IsTeamLead: false, GroupKey: "couple_eve_frank"},
		// More individuals
		{ID: "ivan", FirstName: "Ivan", LastName: "Black", Gender: "Male", IsTeamLead: false, GroupKey: ""},
		{ID: "judy", FirstName: "Judy", LastName: "Blue", Gender: "Female", IsTeamLead: false, GroupKey: ""},
		// Another couple
		{ID: "mike", FirstName: "Mike", LastName: "Gray", Gender: "Male", IsTeamLead: false, GroupKey: "couple_mike_nancy"},
		{ID: "nancy", FirstName: "Nancy", LastName: "Gray", Gender: "Female", IsTeamLead: false, GroupKey: "couple_mike_nancy"},
		// More individuals
		{ID: "oliver", FirstName: "Oliver", LastName: "Red", Gender: "Male", IsTeamLead: false, GroupKey: ""},
		{ID: "paula", FirstName: "Paula", LastName: "Yellow", Gender: "Female", IsTeamLead: false, GroupKey: ""},
		{ID: "quinn", FirstName: "Quinn", LastName: "Purple", Gender: "Male", IsTeamLead: false, GroupKey: ""},
		{ID: "rachel", FirstName: "Rachel", LastName: "Orange", Gender: "Female", IsTeamLead: false, GroupKey: ""},
		{ID: "steve", FirstName: "Steve", LastName: "Pink", Gender: "Male", IsTeamLead: false, GroupKey: ""},
		{ID: "tina", FirstName: "Tina", LastName: "Brown", Gender: "Female", IsTeamLead: false, GroupKey: ""},
		{ID: "uma", FirstName: "Uma", LastName: "Silver", Gender: "Female", IsTeamLead: false, GroupKey: ""},
		{ID: "victor", FirstName: "Victor", LastName: "Gold", Gender: "Male", IsTeamLead: false, GroupKey: ""},
		// Team lead couple 4
		{ID: "wendy", FirstName: "Wendy", LastName: "Teal", Gender: "Female", IsTeamLead: true, GroupKey: "couple_wendy_xavier"},
		{ID: "xavier", FirstName: "Xavier", LastName: "Violet", Gender: "Male", IsTeamLead: false, GroupKey: "couple_wendy_xavier"},
	}

	// Setup availability - realistic with ~50% average availability
	// Shift 3 (index 3, date 2024-01-22) will be the constrained shift with minimal availability:
	//   - Only 1 team lead couple available (Wendy/Xavier)
	//   - Only 2 individual volunteers available (Charlie, Ivan)
	// Total shifts: 7 (indices 0-6)
	// Average availability: 74/168 = 44%
	availability := []VolunteerAvailability{
		// Team lead couples - varied availability
		{VolunteerID: "alice", HasResponded: true, UnavailableShiftIndices: []int{1, 3, 5}},           // Available: 0,2,4,6 (4/7) - NOT shift 3
		{VolunteerID: "bob", HasResponded: true, UnavailableShiftIndices: []int{1, 3, 5}},             // Available: 0,2,4,6 (4/7) - NOT shift 3
		{VolunteerID: "george", HasResponded: true, UnavailableShiftIndices: []int{0, 2, 3}},          // Available: 1,4,5,6 (4/7) - NOT shift 3
		{VolunteerID: "helen", HasResponded: true, UnavailableShiftIndices: []int{0, 2, 3}},           // Available: 1,4,5,6 (4/7) - NOT shift 3
		{VolunteerID: "karen", HasResponded: true, UnavailableShiftIndices: []int{1, 2, 3, 5}},        // Available: 0,4,6 (3/7) - NOT shift 3
		{VolunteerID: "larry", HasResponded: true, UnavailableShiftIndices: []int{1, 2, 3, 5}},        // Available: 0,4,6 (3/7) - NOT shift 3
		{VolunteerID: "wendy", HasResponded: true, UnavailableShiftIndices: []int{0, 1, 2, 4, 5, 6}},  // Available: ONLY 3 (1/7) - ONLY team lead for shift 3!
		{VolunteerID: "xavier", HasResponded: true, UnavailableShiftIndices: []int{0, 1, 2, 4, 5, 6}}, // Available: ONLY 3 (1/7) - one of 2 for shift 3!

		// Individual volunteers - varied availability
		{VolunteerID: "charlie", HasResponded: true, UnavailableShiftIndices: []int{0, 1, 3, 4, 5, 6}}, // Available: ONLY 2(1/7) - NOT shift 3
		{VolunteerID: "diana", HasResponded: true, UnavailableShiftIndices: []int{2, 3, 5}},            // Available: 0,1,4,6 (4/7) - NOT shift 3
		{VolunteerID: "ivan", HasResponded: true, UnavailableShiftIndices: []int{0, 1, 2, 4, 5, 6}},    // Available: ONLY 3 (1/7) - one of 2 for shift 3!
		{VolunteerID: "judy", HasResponded: true, UnavailableShiftIndices: []int{0, 3, 4, 5}},          // Available: 1,2,6 (4/7) - NOT shift 3
		{VolunteerID: "oliver", HasResponded: true, UnavailableShiftIndices: []int{1, 3, 5}},           // Available: 0,2,4,6 (4/7) - NOT shift 3
		{VolunteerID: "paula", HasResponded: true, UnavailableShiftIndices: []int{1, 3, 6}},            // Available: 0,2,4,5 (4/7) - NOT shift 3
		{VolunteerID: "quinn", HasResponded: true, UnavailableShiftIndices: []int{0, 2, 3, 5}},         // Available: 1,4,6 (3/7) - NOT shift 3
		{VolunteerID: "rachel", HasResponded: true, UnavailableShiftIndices: []int{0, 1, 2, 3}},        // Available: 4,5,6 (3/7) - NOT shift 3
		{VolunteerID: "steve", HasResponded: true, UnavailableShiftIndices: []int{3, 4, 5, 6}},         // Available: 0,1,2 (3/7) - NOT shift 3
		{VolunteerID: "tina", HasResponded: true, UnavailableShiftIndices: []int{0, 1, 3, 4}},          // Available: 2,5,6 (3/7) - NOT shift 3
		{VolunteerID: "uma", HasResponded: true, UnavailableShiftIndices: []int{0, 1, 3, 4, 6}},        // Available: 2,5 (3/7) - NOT shift 3
		{VolunteerID: "victor", HasResponded: true, UnavailableShiftIndices: []int{0, 3, 5, 6}},        // Available: 1,2,4 (3/7) - NOT shift 3

		// Couples - varied availability
		{VolunteerID: "eve", HasResponded: true, UnavailableShiftIndices: []int{1, 2, 3}},      // Available: 0,4,5,6 (4/7) - NOT shift 3
		{VolunteerID: "frank", HasResponded: true, UnavailableShiftIndices: []int{1, 2, 3}},    // Available: 0,4,5,6 (4/7) - NOT shift 3
		{VolunteerID: "mike", HasResponded: true, UnavailableShiftIndices: []int{0, 2, 3, 4}},  // Available: 1,5,6 (3/7) - NOT shift 3
		{VolunteerID: "nancy", HasResponded: true, UnavailableShiftIndices: []int{0, 2, 3, 4}}, // Available: 1,5,6 (3/7) - NOT shift 3
	}

	// Setup shift dates
	shiftDates := []string{
		"2024-01-01",
		"2024-01-08",
		"2024-01-15",
		"2024-01-22",
		"2024-01-29",
		"2024-02-05",
		"2024-02-12",
	}

	// Setup criteria with reasonable weights
	criteria := []Criterion{
		NewShiftSizeCriterion(2.0, 2.0),
		NewTeamLeadCriterion(0.5, 2.0),
		NewMaleBalanceCriterion(0.5, 1.0),
		NewNoDoubleShiftsCriterion(1.0),
		NewShiftSpreadCriterion(0.5),
	}

	// Setup historical shifts - the week before the rota starts (2023-12-25)
	// This tests NoDoubleShifts criterion across rota boundaries and ShiftSpread with historical data
	historicalShifts := []*Shift{
		{
			Index: 0, // Only shift of previous rota
			Date:  "2023-12-25",
			Size:  2,
			AllocatedGroups: []*VolunteerGroup{
				// Alice/Bob were allocated to the last historical shift
				// This should prevent them from being allocated to shift 0 (2024-01-01) due to NoDoubleShifts
				{
					GroupKey:    "couple_alice_bob",
					HasTeamLead: true,
					Members: []Volunteer{
						{ID: "alice", FirstName: "Alice", LastName: "Smith", Gender: "Female", IsTeamLead: true, GroupKey: "couple_alice_bob"},
						{ID: "bob", FirstName: "Bob", LastName: "Smith", Gender: "Male", IsTeamLead: false, GroupKey: "couple_alice_bob"},
					},
					MaleCount:                 1,
					AllocatedShiftIndices:     []int{-1},
					AvailableShiftIndices:     []int{}, // Historical, so no current availability
					HistoricalAllocationCount: 1,
				},
				{
					GroupKey:    "individual_diana",
					HasTeamLead: false,
					Members: []Volunteer{
						{ID: "diana", FirstName: "Diana", LastName: "Green", Gender: "Female", IsTeamLead: false, GroupKey: ""},
					},
					MaleCount:                 0,
					AllocatedShiftIndices:     []int{-1},
					AvailableShiftIndices:     []int{},
					HistoricalAllocationCount: 1,
				},
			},
			TeamLead: &Volunteer{
				ID:         "alice",
				FirstName:  "Alice",
				LastName:   "Smith",
				Gender:     "Female",
				IsTeamLead: true,
				GroupKey:   "couple_alice_bob",
			},
			MaleCount: 1,
		},
	}

	// Setup overrides
	// First shift (2024-01-01) needs an extra volunteer
	firstShiftSize := 3
	// Last shift (2024-02-12) has an external volunteer pre-allocated
	overrides := []ShiftOverride{
		{
			AppliesTo: func(date string) bool {
				return date == "2024-01-01" // First shift
			},
			ShiftSize:              &firstShiftSize,
			PreAllocatedVolunteers: []string{}, // No pre-allocations for first shift
		},
		{
			AppliesTo: func(date string) bool {
				return date == "2024-02-12" // Last shift
			},
			ShiftSize:              nil,                       // Use default size
			PreAllocatedVolunteers: []string{"external_john"}, // External volunteer from outside the regular group
		},
	}

	// Create allocation config
	config := AllocationConfig{
		Criteria:               criteria,
		MaxAllocationFrequency: 0.33, // Each group can be allocated to 33% of shifts (2 out of 7)
		HistoricalShifts:       historicalShifts,
		Volunteers:             volunteers,
		Availability:           availability,
		ShiftDates:             shiftDates,
		DefaultShiftSize:       2, // 2 volunteers per shift (excludes team lead)
		Overrides:              overrides,
	}

	// Run the allocator
	outcome, err := Allocate(config)
	require.NoError(t, err, "Allocation should not error")
	require.NotNil(t, outcome, "Outcome should not be nil")

	// Print detailed outcome for inspection
	t.Logf("\n=== ALLOCATION OUTCOME ===")
	t.Logf("Success: %v", outcome.Success)
	t.Logf("Validation Errors: %d", len(outcome.ValidationErrors))
	for _, verr := range outcome.ValidationErrors {
		t.Logf("  [%s] Shift %d (%s): %s", verr.CriterionName, verr.ShiftIndex, verr.ShiftDate, verr.Description)
	}

	t.Logf("\nShifts:")
	for _, shift := range outcome.State.Shifts {
		t.Logf("  Shift %d (%s): Size=%d/%d, TeamLead=%v, Males=%d, Groups=%d, PreAllocated=%v",
			shift.Index,
			shift.Date,
			shift.CurrentSize(),
			shift.Size,
			shift.TeamLead != nil,
			shift.MaleCount,
			len(shift.AllocatedGroups),
			shift.PreAllocatedVolunteers)
		for _, group := range shift.AllocatedGroups {
			memberNames := ""
			for i, member := range group.Members {
				if i > 0 {
					memberNames += ", "
				}
				memberNames += member.FirstName
			}
			t.Logf("    - %s (%s)", group.GroupKey, memberNames)
		}
	}

	t.Logf("\nUnderutilized Groups: %d", len(outcome.UnderutilizedGroups))
	for _, group := range outcome.UnderutilizedGroups {
		t.Logf("  - %s: allocated %d/%d available shifts",
			group.GroupKey,
			len(group.AllocatedShiftIndices),
			len(group.AvailableShiftIndices))
	}

	// Assert allocation was successful
	assert.True(t, outcome.Success, "Allocation should be successful")
	assert.Empty(t, outcome.ValidationErrors, "Should have no validation errors")

	// Assert all shifts are filled
	for _, shift := range outcome.State.Shifts {
		assert.True(t, shift.IsFull(), "Shift %d should be full", shift.Index)

		// First shift should have 3 volunteers, others should have 2
		expectedSize := 2
		if shift.Date == "2024-01-01" {
			expectedSize = 3
		}
		assert.Equal(t, expectedSize, shift.CurrentSize(), "Shift %d should have %d volunteers", shift.Index, expectedSize)

		// Last shift should have the pre-allocated external volunteer
		if shift.Date == "2024-02-12" {
			assert.Contains(t, shift.PreAllocatedVolunteers, "external_john", "Last shift should have external_john pre-allocated")
		}
	}

	// Assert each shift has a team lead (either in TeamLead field or in a group)
	for _, shift := range outcome.State.Shifts {
		hasTeamLead := shift.TeamLead != nil
		for _, group := range shift.AllocatedGroups {
			if group.HasTeamLead {
				hasTeamLead = true
				break
			}
		}
		assert.True(t, hasTeamLead, "Shift %d should have a team lead", shift.Index)
	}

	// Assert each shift has at least one male
	for _, shift := range outcome.State.Shifts {
		assert.Greater(t, shift.MaleCount, 0, "Shift %d should have at least one male", shift.Index)
	}

	// Assert no double shifts (no adjacent allocations)
	for _, group := range outcome.State.VolunteerState.VolunteerGroups {
		allocations := group.AllocatedShiftIndices
		for i := 0; i < len(allocations)-1; i++ {
			diff := allocations[i+1] - allocations[i]
			assert.NotEqual(t, 1, diff, "Group %s has adjacent allocations at shifts %d and %d",
				group.GroupKey, allocations[i], allocations[i+1])
		}
	}

	// Check exhausted groups too
	for group := range outcome.State.VolunteerState.ExhaustedVolunteerGroups {
		allocations := group.AllocatedShiftIndices
		for i := 0; i < len(allocations)-1; i++ {
			diff := allocations[i+1] - allocations[i]
			assert.NotEqual(t, 1, diff, "Group %s has adjacent allocations at shifts %d and %d",
				group.GroupKey, allocations[i], allocations[i+1])
		}
	}

	// Assert allocation frequency is respected (max 2 shifts per group)
	maxAllocationCount := outcome.State.MaxAllocationCount()
	assert.Equal(t, 2, maxAllocationCount, "Max allocation count should be 2 (33% of 7 shifts)")

	for _, group := range outcome.State.VolunteerState.VolunteerGroups {
		assert.LessOrEqual(t, len(group.AllocatedShiftIndices), maxAllocationCount,
			"Group %s should not exceed max allocation count", group.GroupKey)
	}

	for group := range outcome.State.VolunteerState.ExhaustedVolunteerGroups {
		assert.LessOrEqual(t, len(group.AllocatedShiftIndices), maxAllocationCount,
			"Group %s should not exceed max allocation count", group.GroupKey)
	}
}

func TestAllocator_ErrorHandling(t *testing.T) {
	t.Run("no shift dates", func(t *testing.T) {
		config := AllocationConfig{
			Criteria:               []Criterion{},
			MaxAllocationFrequency: 0.5,
			Volunteers:             []Volunteer{{ID: "v1"}},
			ShiftDates:             []string{},
		}
		_, err := Allocate(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no shift dates")
	})

	t.Run("no volunteers", func(t *testing.T) {
		config := AllocationConfig{
			Criteria:               []Criterion{},
			MaxAllocationFrequency: 0.5,
			Volunteers:             []Volunteer{},
			ShiftDates:             []string{"2024-01-01"},
		}
		_, err := Allocate(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no volunteers")
	})

	t.Run("invalid frequency", func(t *testing.T) {
		config := AllocationConfig{
			Criteria:               []Criterion{},
			MaxAllocationFrequency: 1.5,
			Volunteers:             []Volunteer{{ID: "v1"}},
			ShiftDates:             []string{"2024-01-01"},
		}
		_, err := Allocate(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "max allocation frequency")
	})

	t.Run("negative shift size", func(t *testing.T) {
		config := AllocationConfig{
			Criteria:               []Criterion{},
			MaxAllocationFrequency: 0.5,
			Volunteers:             []Volunteer{{ID: "v1"}},
			ShiftDates:             []string{"2024-01-01"},
			DefaultShiftSize:       -1,
		}
		_, err := Allocate(config)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "shift size")
	})
}

func TestAllocator_SmallScenario(t *testing.T) {
	// Simple scenario: 2 shifts, 2 volunteers, frequency allows both
	volunteers := []Volunteer{
		{ID: "v1", FirstName: "Alice", LastName: "A", Gender: "Female", IsTeamLead: true, GroupKey: ""},
		{ID: "v2", FirstName: "Bob", LastName: "B", Gender: "Male", IsTeamLead: false, GroupKey: ""},
	}

	availability := []VolunteerAvailability{
		{VolunteerID: "v1", HasResponded: true, UnavailableShiftIndices: []int{}},
		{VolunteerID: "v2", HasResponded: true, UnavailableShiftIndices: []int{}},
	}

	config := AllocationConfig{
		Criteria: []Criterion{
			NewShiftSizeCriterion(1.0, 1.0),
			NewTeamLeadCriterion(1.0, 1.0),
			NewMaleBalanceCriterion(1.0, 1.0),
		},
		MaxAllocationFrequency: 1.0, // 100% - everyone can do every shift
		Volunteers:             volunteers,
		Availability:           availability,
		ShiftDates:             []string{"2024-01-01", "2024-01-08"},
		DefaultShiftSize:       1, // Just 1 volunteer per shift
	}

	outcome, err := Allocate(config)
	require.NoError(t, err)
	require.NotNil(t, outcome)

	// Assert allocation was successful
	assert.True(t, outcome.Success, "Allocation should be successful")
	assert.Empty(t, outcome.ValidationErrors, "Should have no validation errors")

	for i, shift := range outcome.State.Shifts {
		t.Logf("Shift %d: CurrentSize=%d, TeamLead=%v, MaleCount=%d",
			i, shift.CurrentSize(), shift.TeamLead != nil, shift.MaleCount)
		assert.True(t, shift.IsFull(), "Shift %d should be full", i)
	}
}

func TestAllocator_ImpossibleScenario(t *testing.T) {
	// Create an impossible scenario where we have:
	// - 3 shifts needing 2 volunteers each
	// - Shift 0: only has 1 team lead available (Alice)
	// - Shift 1: only has 1 male available (Bob) + 1 female (Carol), but no team lead
	// - Shift 2: has volunteers but no team lead and no male
	// This makes it impossible to satisfy all constraints
	volunteers := []Volunteer{
		// Team lead (female) - only available for shift 0
		{ID: "tl1", FirstName: "Alice", LastName: "A", Gender: "Female", IsTeamLead: true, GroupKey: ""},
		// Male volunteer (not team lead) - only available for shift 1
		{ID: "m1", FirstName: "Bob", LastName: "B", Gender: "Male", IsTeamLead: false, GroupKey: ""},
		// Female volunteers (not team leads)
		{ID: "f1", FirstName: "Carol", LastName: "C", Gender: "Female", IsTeamLead: false, GroupKey: ""},
		{ID: "f2", FirstName: "Diana", LastName: "D", Gender: "Female", IsTeamLead: false, GroupKey: ""},
		{ID: "f3", FirstName: "Eve", LastName: "E", Gender: "Female", IsTeamLead: false, GroupKey: ""},
	}

	// Create impossible availability constraints:
	// Shift 0: Only Alice (team lead) available - shift will be underfilled (1/2)
	// Shift 1: Bob, Carol available - no team lead available
	// Shift 2: Diana, Eve available - no team lead and no males
	availability := []VolunteerAvailability{
		{VolunteerID: "tl1", HasResponded: true, UnavailableShiftIndices: []int{1, 2}}, // Alice: only shift 0
		{VolunteerID: "m1", HasResponded: true, UnavailableShiftIndices: []int{0, 2}},  // Bob: only shift 1
		{VolunteerID: "f1", HasResponded: true, UnavailableShiftIndices: []int{0, 2}},  // Carol: only shift 1
		{VolunteerID: "f2", HasResponded: true, UnavailableShiftIndices: []int{0, 1}},  // Diana: only shift 2
		{VolunteerID: "f3", HasResponded: true, UnavailableShiftIndices: []int{0, 1}},  // Eve: only shift 2
	}

	config := AllocationConfig{
		Criteria: []Criterion{
			NewShiftSizeCriterion(1.0, 1.0),
			NewTeamLeadCriterion(1.0, 1.0),
			NewMaleBalanceCriterion(1.0, 1.0),
		},
		MaxAllocationFrequency: 1.0, // 100% - everyone can do every shift
		Volunteers:             volunteers,
		Availability:           availability,
		ShiftDates:             []string{"2024-01-01", "2024-01-08", "2024-01-15"},
		DefaultShiftSize:       2, // 2 volunteers per shift
	}

	outcome, err := Allocate(config)
	require.NoError(t, err, "Allocate should not return an error")
	require.NotNil(t, outcome)

	// Print detailed outcome for debugging
	t.Logf("\n=== IMPOSSIBLE SCENARIO OUTCOME ===")
	t.Logf("Success: %v", outcome.Success)
	t.Logf("Validation Errors: %d", len(outcome.ValidationErrors))
	for _, verr := range outcome.ValidationErrors {
		t.Logf("  [%s] Shift %d (%s): %s", verr.CriterionName, verr.ShiftIndex, verr.ShiftDate, verr.Description)
	}

	t.Logf("\nShifts:")
	for _, shift := range outcome.State.Shifts {
		t.Logf("  Shift %d (%s): Size=%d/%d, TeamLead=%v, Males=%d",
			shift.Index,
			shift.Date,
			shift.CurrentSize(),
			shift.Size,
			shift.TeamLead != nil,
			shift.MaleCount)
	}

	// Assert allocation was NOT successful
	assert.False(t, outcome.Success, "Allocation should not be successful in impossible scenario")
	assert.NotEmpty(t, outcome.ValidationErrors, "Should have validation errors")

	// Verify that validation errors are one of the expected types:
	// - ShiftSize: shift not filled to target size
	// - TeamLead: shift missing team lead
	// - MaleBalance: shift has no male volunteers
	validCriteriaNames := map[string]bool{
		"ShiftSize":   true,
		"TeamLead":    true,
		"MaleBalance": true,
	}

	for _, verr := range outcome.ValidationErrors {
		assert.Contains(t, validCriteriaNames, verr.CriterionName,
			"Validation error should be from ShiftSize, TeamLead, or MaleBalance criterion")
	}

	// At least one shift should not be full
	hasUnfullShift := false
	for _, shift := range outcome.State.Shifts {
		if !shift.IsFull() {
			hasUnfullShift = true
			break
		}
	}
	assert.True(t, hasUnfullShift, "At least one shift should not be full")

	// Count specific validation error types
	var shiftSizeErrors, teamLeadErrors, maleBalanceErrors int
	for _, verr := range outcome.ValidationErrors {
		switch verr.CriterionName {
		case "ShiftSize":
			shiftSizeErrors++
		case "TeamLead":
			teamLeadErrors++
		case "MaleBalance":
			maleBalanceErrors++
		}
	}

	t.Logf("\nError breakdown:")
	t.Logf("  ShiftSize errors: %d", shiftSizeErrors)
	t.Logf("  TeamLead errors: %d", teamLeadErrors)
	t.Logf("  MaleBalance errors: %d", maleBalanceErrors)

	// We expect at least 2 team lead errors (only 1 team lead for 3 shifts)
	assert.GreaterOrEqual(t, teamLeadErrors, 2, "Should have at least 2 team lead errors")

	// We expect at least 2 male balance errors (only 1 male for 3 shifts)
	assert.GreaterOrEqual(t, maleBalanceErrors, 2, "Should have at least 2 male balance errors")
}
