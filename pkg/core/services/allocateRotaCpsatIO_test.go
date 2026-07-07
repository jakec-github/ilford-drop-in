package services

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/core/allocator"
)

// TestCpsatInputContractGolden pins the JSON field names of the Go->Python
// contract. If this test breaks, pyallocator's serialization must change in
// lockstep (see pyallocator/README.md).
func TestCpsatInputContractGolden(t *testing.T) {
	input := &CpsatInput{
		MaxAllocationCount: 2,
		Shifts: []CpsatShift{{
			Index:                    0,
			Date:                     "2026-07-13",
			Size:                     3,
			Closed:                   false,
			CustomPreallocations:     []string{"St John's team"},
			PreallocatedVolunteerIDs: []string{"vol-1"},
			PreallocatedTeamLeadID:   "vol-9",
		}},
		Groups: []CpsatGroup{{
			GroupKey: "couple_alice_bob",
			Members: []CpsatMember{{
				ID: "vol-1", FirstName: "Alice", LastName: "Smith",
				DisplayName: "Alice S", Gender: "Female", IsTeamLead: false,
			}},
			AvailableShiftIndices:     []int{0, 2},
			HistoricalAllocationCount: 3,
		}},
		HistoricalShifts: []CpsatHistoricalShift{{
			Date: "2026-06-29", GroupKeys: []string{"couple_x"},
		}},
	}

	golden := `{
		"max_allocation_count": 2,
		"shifts": [{
			"index": 0, "date": "2026-07-13", "size": 3, "closed": false,
			"custom_preallocations": ["St John's team"],
			"preallocated_volunteer_ids": ["vol-1"],
			"preallocated_team_lead_id": "vol-9"
		}],
		"groups": [{
			"group_key": "couple_alice_bob",
			"members": [{
				"id": "vol-1", "first_name": "Alice", "last_name": "Smith",
				"display_name": "Alice S", "gender": "Female", "is_team_lead": false
			}],
			"available_shift_indices": [0, 2],
			"historical_allocation_count": 3
		}],
		"historical_shifts": [{"date": "2026-06-29", "group_keys": ["couple_x"]}]
	}`

	got, err := json.Marshal(input)
	require.NoError(t, err)
	assert.JSONEq(t, golden, string(got))
}

func TestCpsatOutputContractGolden(t *testing.T) {
	payload := `{
		"solver_status": "OPTIMAL", "success": true, "error": "", "objective_value": 23,
		"shifts": [{
			"index": 0, "date": "2026-07-13", "size": 4, "closed": false,
			"team_lead_id": "vol-9", "volunteer_ids": ["vol-1", "vol-2"],
			"custom_preallocations": ["St John's team"],
			"allocated_group_keys": ["couple_alice_bob", "Diana Green"]
		}],
		"diagnostics": {"solve_time_seconds": 0.12, "num_groups": 18,
			"num_variables": 126, "constraints_applied": ["availability"]}
	}`

	var output CpsatOutput
	require.NoError(t, json.Unmarshal([]byte(payload), &output))
	assert.Equal(t, "OPTIMAL", output.SolverStatus)
	assert.True(t, output.Success)
	assert.Equal(t, 23, output.ObjectiveValue)
	require.Len(t, output.Shifts, 1)
	assert.Equal(t, "vol-9", output.Shifts[0].TeamLeadID)
	assert.Equal(t, []string{"vol-1", "vol-2"}, output.Shifts[0].VolunteerIDs)
	assert.Equal(t, []string{"St John's team"}, output.Shifts[0].CustomPreallocations)
	assert.Equal(t, 0.12, output.Diagnostics.SolveTimeSeconds)
	assert.Equal(t, []string{"availability"}, output.Diagnostics.ConstraintsApplied)
}

func TestBuildCpsatInput(t *testing.T) {
	volunteers := []allocator.Volunteer{
		{ID: "alice", FirstName: "Alice", LastName: "Smith", DisplayName: "Alice", Gender: "Female", IsTeamLead: true, GroupKey: "couple_ab"},
		{ID: "bob", FirstName: "Bob", LastName: "Smith", DisplayName: "Bob", Gender: "Male", IsTeamLead: false, GroupKey: "couple_ab"},
		{ID: "diana", FirstName: "Diana", LastName: "Green", DisplayName: "Diana", Gender: "Female", IsTeamLead: false, GroupKey: ""},
		{ID: "silent", FirstName: "Silent", LastName: "Bob", DisplayName: "Silent", Gender: "Male", IsTeamLead: false, GroupKey: ""},
	}
	availability := []allocator.VolunteerAvailability{
		{VolunteerID: "alice", HasResponded: true, UnavailableShiftIndices: []int{1}},
		{VolunteerID: "diana", HasResponded: true, UnavailableShiftIndices: []int{}},
		// "silent" never responded: their group must be discarded.
	}
	shiftDates := []string{"2026-07-13", "2026-07-20", "2026-07-27", "2026-08-03"}
	size := 5
	overrides := []allocator.ShiftOverride{
		{
			AppliesTo:            func(date string) bool { return date == "2026-07-20" },
			ShiftSize:            &size,
			CustomPreallocations: []string{"external_john"},
		},
		{
			AppliesTo: func(date string) bool { return date == "2026-07-27" },
			Closed:    true,
		},
	}
	historical := []*allocator.Shift{
		{Date: "2026-07-06", AllocatedGroups: []*allocator.VolunteerGroup{
			allocator.BuildVolunteerGroup("couple_ab", volunteers[:2]),
		}},
		{Date: "2026-06-29", AllocatedGroups: []*allocator.VolunteerGroup{
			allocator.BuildVolunteerGroup("", volunteers[2:3]),
		}},
	}

	input, err := buildCpsatInput(volunteers, availability, shiftDates, 2, overrides, historical, 0.5)
	require.NoError(t, err)

	// max = floor(4 * 0.5)
	assert.Equal(t, 2, input.MaxAllocationCount)

	// Grouping via InitVolunteerGroups: couple grouped, individual keyed
	// by name, non-responder discarded; sorted by group key.
	require.Len(t, input.Groups, 2)
	assert.Equal(t, "Diana Green", input.Groups[0].GroupKey)
	assert.Equal(t, []int{0, 1, 2, 3}, input.Groups[0].AvailableShiftIndices)
	assert.Equal(t, 1, input.Groups[0].HistoricalAllocationCount)
	assert.Equal(t, "couple_ab", input.Groups[1].GroupKey)
	require.Len(t, input.Groups[1].Members, 2)
	// Group availability = union of responding members' unavailability.
	assert.Equal(t, []int{0, 2, 3}, input.Groups[1].AvailableShiftIndices)

	// Shift overrides applied via InitShifts.
	require.Len(t, input.Shifts, 4)
	assert.Equal(t, 2, input.Shifts[0].Size)
	assert.Equal(t, 5, input.Shifts[1].Size)
	assert.Equal(t, []string{"external_john"}, input.Shifts[1].CustomPreallocations)
	assert.True(t, input.Shifts[2].Closed)
	assert.Empty(t, input.Shifts[2].CustomPreallocations)

	// Historical shifts sorted ascending by date with derived group keys.
	require.Len(t, input.HistoricalShifts, 2)
	assert.Equal(t, "2026-06-29", input.HistoricalShifts[0].Date)
	assert.Equal(t, []string{"Diana Green"}, input.HistoricalShifts[0].GroupKeys)
	assert.Equal(t, "2026-07-06", input.HistoricalShifts[1].Date)
	assert.Equal(t, []string{"couple_ab"}, input.HistoricalShifts[1].GroupKeys)
}

func TestCpsatOutputToAllocatorShifts(t *testing.T) {
	volunteers := []allocator.Volunteer{
		{ID: "alice", FirstName: "Alice", LastName: "Smith", DisplayName: "Alice", Gender: "Female", IsTeamLead: true, GroupKey: "couple_ab"},
		{ID: "bob", FirstName: "Bob", LastName: "Smith", DisplayName: "Bob", Gender: "Male", IsTeamLead: false, GroupKey: "couple_ab"},
		{ID: "diana", FirstName: "Diana", LastName: "Green", DisplayName: "Diana", Gender: "Female", IsTeamLead: false, GroupKey: ""},
	}
	output := &CpsatOutput{
		SolverStatus: "OPTIMAL",
		Success:      true,
		Shifts: []CpsatOutputShift{{
			Index:                0,
			Date:                 "2026-07-13",
			Size:                 3,
			TeamLeadID:           "alice",
			VolunteerIDs:         []string{"bob", "diana"},
			CustomPreallocations: []string{"external_john"},
			AllocatedGroupKeys:   []string{"couple_ab", "Diana Green"},
		}},
	}

	shifts, err := cpsatOutputToAllocatorShifts(output, volunteers)
	require.NoError(t, err)
	require.Len(t, shifts, 1)
	shift := shifts[0]

	require.NotNil(t, shift.TeamLead)
	assert.Equal(t, "alice", shift.TeamLead.ID)
	assert.Equal(t, []string{"external_john"}, shift.CustomPreallocations)
	// Couple regrouped (alice+bob), individual keyed by name.
	require.Len(t, shift.AllocatedGroups, 2)
	// Ordinary size: bob + diana + external_john custom entry.
	assert.Equal(t, 3, shift.CurrentSize())

	// convertToDBAllocations reuses the rebuilt shifts: 2 volunteer rows
	// (bob, diana) + 1 team lead row (alice) + 1 custom entry row.
	dbAllocations := convertToDBAllocations("rota-1", shifts)
	assert.Len(t, dbAllocations, 4)

	// Unknown IDs from the solver are rejected.
	output.Shifts[0].VolunteerIDs = []string{"nobody"}
	_, err = cpsatOutputToAllocatorShifts(output, volunteers)
	assert.ErrorContains(t, err, "nobody")
}
