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
	leadMax := 1
	input := &allocator.CpsatInput{
		MaxAllocationCount: 2,
		Roles: []allocator.CpsatRole{
			{Name: "Team lead", Max: &leadMax, Priority: 1},
			{Name: "Service volunteer", Max: nil, Priority: 2},
		},
		RequiresMale: true,
		Shifts: []allocator.CpsatShift{{
			Index:  0,
			Date:   "2026-07-13",
			Closed: false,
			Shape: []allocator.CpsatSeat{
				{Role: "Team lead", Count: 1},
				{Role: "Service volunteer", Count: 3},
			},
			Preallocations: []allocator.CpsatPreallocation{
				{Custom: "St John's team", Role: "Service volunteer"},
				{VolunteerID: "vol-1", Role: "Service volunteer"},
				{VolunteerID: "vol-9", Role: "Team lead"},
			},
		}},
		Groups: []allocator.CpsatGroup{{
			GroupKey: "couple_alice_bob",
			Members: []allocator.CpsatMember{{
				ID: "vol-1", FirstName: "Alice", LastName: "Smith",
				DisplayName: "Alice S", Gender: "Female",
				Roles: []string{"Service volunteer"},
			}},
			AvailableShiftIndices:     []int{0, 2},
			HistoricalAllocationCount: 3,
		}},
		HistoricalShifts: []allocator.CpsatHistoricalShift{{
			Date: "2026-06-29", GroupKeys: []string{"couple_x"},
		}},
	}

	golden := `{
		"max_allocation_count": 2,
		"roles": [
			{"name": "Team lead", "max": 1, "priority": 1},
			{"name": "Service volunteer", "max": null, "priority": 2}
		],
		"requires_male": true,
		"shifts": [{
			"index": 0, "date": "2026-07-13", "closed": false,
			"shape": [
				{"role": "Team lead", "count": 1},
				{"role": "Service volunteer", "count": 3}
			],
			"preallocations": [
				{"volunteer_id": "", "custom": "St John's team", "role": "Service volunteer"},
				{"volunteer_id": "vol-1", "custom": "", "role": "Service volunteer"},
				{"volunteer_id": "vol-9", "custom": "", "role": "Team lead"}
			]
		}],
		"groups": [{
			"group_key": "couple_alice_bob",
			"members": [{
				"id": "vol-1", "first_name": "Alice", "last_name": "Smith",
				"display_name": "Alice S", "gender": "Female",
				"roles": ["Service volunteer"]
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

	var output allocator.CpsatOutput
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
			AppliesTo: func(date string) bool { return date == "2026-07-20" },
			ShiftSize: &size,
			Preallocations: []allocator.Preallocation{
				{Custom: "external_john", Role: "Service volunteer"},
			},
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

	leadMax := 1
	roles := []allocator.Role{
		{Name: "Team lead", Max: &leadMax, Priority: 1},
		{Name: "Service volunteer", Max: nil, Priority: 2},
	}

	input, err := allocator.BuildCpsatInput(volunteers, availability, shiftDates, 2, overrides, historical, 0.5, roles, true)
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

	// Shift overrides applied via InitShifts. The Shape spends the shift's size
	// on the uncapped Role and gives each capped Role its ceiling.
	require.Len(t, input.Shifts, 4)
	assert.Equal(t, []allocator.CpsatSeat{
		{Role: "Team lead", Count: 1},
		{Role: "Service volunteer", Count: 2},
	}, input.Shifts[0].Shape)
	assert.Equal(t, []allocator.CpsatSeat{
		{Role: "Team lead", Count: 1},
		{Role: "Service volunteer", Count: 5},
	}, input.Shifts[1].Shape)
	assert.Equal(t, []allocator.CpsatPreallocation{
		{Custom: "external_john", Role: "Service volunteer"},
	}, input.Shifts[1].Preallocations)
	assert.True(t, input.Shifts[2].Closed)
	assert.Empty(t, input.Shifts[2].Preallocations)

	// Roles travel with the problem, in priority order.
	assert.Equal(t, []allocator.CpsatRole{
		{Name: "Team lead", Max: &leadMax, Priority: 1},
		{Name: "Service volunteer", Max: nil, Priority: 2},
	}, input.Roles)
	assert.True(t, input.RequiresMale)

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
	output := &allocator.CpsatOutput{
		SolverStatus: "OPTIMAL",
		Success:      true,
		Shifts: []allocator.CpsatOutputShift{{
			Index:                0,
			Date:                 "2026-07-13",
			Size:                 3,
			TeamLeadID:           "alice",
			VolunteerIDs:         []string{"bob", "diana"},
			CustomPreallocations: []string{"external_john"},
			AllocatedGroupKeys:   []string{"couple_ab", "Diana Green"},
		}},
	}

	shifts, err := allocator.CpsatOutputToShifts(output, volunteers)
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
	dbAllocations, err := convertToDBAllocations(map[string]string{"2026-07-13": "shift-1"}, shifts)
	require.NoError(t, err)
	assert.Len(t, dbAllocations, 4)
	for _, a := range dbAllocations {
		assert.Equal(t, "shift-1", a.ShiftID)
	}

	// Unknown IDs from the solver are rejected.
	output.Shifts[0].VolunteerIDs = []string{"nobody"}
	_, err = allocator.CpsatOutputToShifts(output, volunteers)
	assert.ErrorContains(t, err, "nobody")
}
