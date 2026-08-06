package services

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/allocator"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// openShifts turns bare dates into specs for the ordinary case, where every
// shift is open and only the closed ones are worth spelling out.
func openShifts(dates ...string) []allocator.ShiftSpec {
	specs := make([]allocator.ShiftSpec, len(dates))
	for i, date := range dates {
		specs[i] = allocator.ShiftSpec{Date: date}
	}
	return specs
}

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
			"assignments": [
				{"volunteer_id": "vol-9", "custom": "", "role": "Team lead"},
				{"volunteer_id": "vol-1", "custom": "", "role": "Service volunteer"},
				{"volunteer_id": "", "custom": "St John's team", "role": "Service volunteer"}
			],
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
	assert.Equal(t, []allocator.CpsatAssignment{
		{VolunteerID: "vol-9", Role: "Team lead"},
		{VolunteerID: "vol-1", Role: "Service volunteer"},
		{Custom: "St John's team", Role: "Service volunteer"},
	}, output.Shifts[0].Assignments)
	assert.Equal(t, 0.12, output.Diagnostics.SolveTimeSeconds)
	assert.Equal(t, []string{"availability"}, output.Diagnostics.ConstraintsApplied)
}

func TestBuildCpsatInput(t *testing.T) {
	volunteers := []allocator.Volunteer{
		{ID: "alice", FirstName: "Alice", LastName: "Smith", DisplayName: "Alice", Gender: "Female", GroupKey: "couple_ab"},
		{ID: "bob", FirstName: "Bob", LastName: "Smith", DisplayName: "Bob", Gender: "Male", GroupKey: "couple_ab"},
		{ID: "diana", FirstName: "Diana", LastName: "Green", DisplayName: "Diana", Gender: "Female", GroupKey: ""},
		{ID: "silent", FirstName: "Silent", LastName: "Bob", DisplayName: "Silent", Gender: "Male", GroupKey: ""},
	}
	groupAvailability := map[string][]int{
		"couple_ab":   {0, 2, 3},
		"Diana Green": {0, 1, 2, 3},
		// "Silent Bob" was never answered for: absent from the map, so their
		// group must be discarded.
	}
	// 2026-07-27 is closed, which is a fact about the Shift rather than
	// anything an override says.
	shiftSpecs := []allocator.ShiftSpec{
		{Date: "2026-07-13"},
		{Date: "2026-07-20"},
		{Date: "2026-07-27", Closed: true},
		{Date: "2026-08-03"},
	}
	overrides := []allocator.ShiftOverride{
		{
			AppliesTo: func(date string) bool { return date == "2026-07-20" },
			Preallocations: []allocator.Preallocation{
				{Custom: "external_john", Role: "Service volunteer"},
			},
		},
	}
	historical := []*allocator.Shift{
		{Date: "2026-07-06", AllocatedGroups: []*allocator.VolunteerGroup{
			allocator.BuildVolunteerGroup(volunteers[:2]),
		}},
		{Date: "2026-06-29", AllocatedGroups: []*allocator.VolunteerGroup{
			allocator.BuildVolunteerGroup(volunteers[2:3]),
		}},
	}

	leadMax := 1
	roles := []allocator.Role{
		{Name: "Team lead", Max: &leadMax, Priority: 1},
		{Name: "Service volunteer", Max: nil, Priority: 2},
	}

	shape := []allocator.Seat{
		{Role: "Team lead", Count: 1},
		{Role: "Service volunteer", Count: 2},
	}

	input, err := allocator.BuildCpsatInput(volunteers, groupAvailability, shiftSpecs, shape, overrides, historical, 0.5, roles, true)
	require.NoError(t, err)

	// max = floor(4 * 0.5)
	assert.Equal(t, 2, input.MaxAllocationCount)

	// Grouping via InitVolunteerGroups: couple grouped, individual keyed
	// by name, group nobody answered for discarded; sorted by group key.
	require.Len(t, input.Groups, 2)
	assert.Equal(t, "Diana Green", input.Groups[0].GroupKey)
	assert.Equal(t, []int{0, 1, 2, 3}, input.Groups[0].AvailableShiftIndices)
	assert.Equal(t, 1, input.Groups[0].HistoricalAllocationCount)
	assert.Equal(t, "couple_ab", input.Groups[1].GroupKey)
	require.Len(t, input.Groups[1].Members, 2)
	// The group's settled answer, applied as given.
	assert.Equal(t, []int{0, 2, 3}, input.Groups[1].AvailableShiftIndices)

	// Every shift carries the default Shape, stated rather than derived from a
	// size; an override contributes pins and nothing else.
	require.Len(t, input.Shifts, 4)
	for i, shift := range input.Shifts {
		assert.Equal(t, []allocator.CpsatSeat{
			{Role: "Team lead", Count: 1},
			{Role: "Service volunteer", Count: 2},
		}, shift.Shape, "shift %d", i)
	}
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

// The keys the solver matches history against are the keys of the rota being
// allocated, so the two must be minted by the same rule. This walks the whole
// seam — database allocations through buildHistoricalShifts into the contract —
// and asserts every volunteer on the previous rota's final shift carries their
// own key into last_historical_group_keys, grouped or not. Grouping history on
// the raw GroupKey merged the ungrouped ones into one bucket, and the rest were
// then absent from history entirely: no_back_to_back never bound for them
// (#108).
func TestBuildCpsatInput_HistoryKeysMatchCurrentRotaKeys(t *testing.T) {
	volunteers := []allocator.Volunteer{
		{ID: "alice", FirstName: "Alice", LastName: "Smith", DisplayName: "Alice", Gender: "Female", GroupKey: "couple_ab"},
		{ID: "bob", FirstName: "Bob", LastName: "Smith", DisplayName: "Bob", Gender: "Male", GroupKey: "couple_ab"},
		{ID: "diana", FirstName: "Diana", LastName: "Green", DisplayName: "Diana", Gender: "Female"},
		{ID: "eve", FirstName: "Eve", LastName: "Hall", DisplayName: "Eve", Gender: "Female"},
	}

	// All four worked the previous rota's final shift, 2026-07-06.
	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{
			{ID: "rota-0", Start: "2026-06-29", ShiftCount: 2},
			{ID: "rota-1", Start: "2026-07-13", ShiftCount: 1},
		},
		shifts: shiftsOnDates("rota-0", "2026-06-29", "2026-07-06"),
		allocations: []db.Allocation{
			{ID: "a-0", ShiftID: "2026-06-29", VolunteerID: "alice", Role: "Service volunteer"},
			{ID: "a-1", ShiftID: "2026-07-06", VolunteerID: "alice", Role: "Service volunteer"},
			{ID: "a-2", ShiftID: "2026-07-06", VolunteerID: "bob", Role: "Service volunteer"},
			{ID: "a-3", ShiftID: "2026-07-06", VolunteerID: "diana", Role: "Service volunteer"},
			{ID: "a-4", ShiftID: "2026-07-06", VolunteerID: "eve", Role: "Service volunteer"},
		},
	}

	targetRota := &db.Rotation{ID: "rota-1", Start: "2026-07-13", ShiftCount: 1}
	historical, err := buildHistoricalShifts(
		context.Background(), store, store.rotations, targetRota, volunteers,
		"Service volunteer", zap.NewNop())
	require.NoError(t, err)

	groupAvailability := map[string][]int{
		"couple_ab":   {0},
		"Diana Green": {0},
		"Eve Hall":    {0},
	}
	leadMax := 1
	roles := []allocator.Role{
		{Name: "Team lead", Max: &leadMax, Priority: 1},
		{Name: "Service volunteer", Max: nil, Priority: 2},
	}

	input, err := allocator.BuildCpsatInput(
		volunteers, groupAvailability, openShifts("2026-07-13"),
		[]allocator.Seat{{Role: "Service volunteer", Count: 4}}, nil,
		historical, 1, roles, false)
	require.NoError(t, err)

	// The final historical shift is the one no_back_to_back reads.
	require.Len(t, input.HistoricalShifts, 2)
	last := input.HistoricalShifts[len(input.HistoricalShifts)-1]
	require.Equal(t, "2026-07-06", last.Date)
	assert.ElementsMatch(t, []string{"couple_ab", "Diana Green", "Eve Hall"}, last.GroupKeys)

	// Every group in the problem is on that boundary, so none may take shift 0.
	require.Len(t, input.Groups, 3)
	for _, group := range input.Groups {
		assert.Contains(t, last.GroupKeys, group.GroupKey,
			"group %q is missing from history and would be free to work the first shift", group.GroupKey)
	}
}

// A pin is a decision already taken, so it settles the availability question
// for the shift it names rather than waiting on an answer that may never come.
// Without this a pinned volunteer who had not replied was discarded with the
// rest of the unanswered groups, and the solver then failed on a pin naming
// somebody who was not in the problem at all.
func TestBuildCpsatInput_PreallocationImpliesAvailability(t *testing.T) {
	volunteers := []allocator.Volunteer{
		{ID: "alice", FirstName: "Alice", LastName: "Smith", DisplayName: "Alice", Gender: "Female", GroupKey: "couple_ab"},
		{ID: "bob", FirstName: "Bob", LastName: "Smith", DisplayName: "Bob", Gender: "Male", GroupKey: "couple_ab"},
		{ID: "silent", FirstName: "Silent", LastName: "Jones", DisplayName: "Silent", Gender: "Male", GroupKey: ""},
		{ID: "refused", FirstName: "Ruth", LastName: "Grey", DisplayName: "Ruth", Gender: "Female", GroupKey: ""},
	}
	groupAvailability := map[string][]int{
		"couple_ab": {3},
		// Silent Jones was never answered for: absent from the map.
		// Ruth Grey answered, for nothing.
		"Ruth Grey": {},
	}
	shiftSpecs := []allocator.ShiftSpec{
		{Date: "2026-07-13"},
		{Date: "2026-07-20"},
		{Date: "2026-07-27", Closed: true},
		{Date: "2026-08-03"},
	}

	overrides := []allocator.ShiftOverride{
		// Shift 0 pins one of the couple, a volunteer nobody answered for, and
		// a volunteer who answered "none of these".
		{
			AppliesTo: func(date string) bool { return date == "2026-07-13" },
			Preallocations: []allocator.Preallocation{
				{VolunteerID: "alice", Role: "Team lead"},
				{VolunteerID: "silent", Role: "Service volunteer"},
				{VolunteerID: "refused", Role: "Service volunteer"},
				{Custom: "St John's team", Role: "Service volunteer"},
				{VolunteerID: "ghost", Role: "Service volunteer"},
			},
		},
		// Shift 2 is closed, so the pins landing on it are stripped and it
		// implies nothing.
		{
			AppliesTo: func(date string) bool { return date == "2026-07-27" },
			Preallocations: []allocator.Preallocation{
				{VolunteerID: "silent", Role: "Service volunteer"},
			},
		},
	}

	leadMax := 1
	roles := []allocator.Role{
		{Name: "Team lead", Max: &leadMax, Priority: 1},
		{Name: "Service volunteer", Max: nil, Priority: 2},
	}

	shape := []allocator.Seat{
		{Role: "Team lead", Count: 1},
		{Role: "Service volunteer", Count: 2},
	}

	input, err := allocator.BuildCpsatInput(volunteers, groupAvailability, shiftSpecs, shape, overrides, nil, 0.5, roles, true)
	require.NoError(t, err)

	byKey := make(map[string]allocator.CpsatGroup, len(input.Groups))
	for _, g := range input.Groups {
		byKey[g.GroupKey] = g
	}

	// Two groups that would have been discarded now survive, each available for
	// the pinned shift and nothing else — the pin says where they are needed,
	// not that they are free all rota. Silent Jones is pinned to shift 2 as
	// well, which is closed: its pins are stripped, so it grants nothing.
	require.Contains(t, byKey, "Silent Jones")
	assert.Equal(t, []int{0}, byKey["Silent Jones"].AvailableShiftIndices)
	require.Contains(t, byKey, "Ruth Grey")
	assert.Equal(t, []int{0}, byKey["Ruth Grey"].AvailableShiftIndices)

	// A group that did answer keeps its answer, with the pinned shift added in
	// order. The pin is group-atomic, so Bob comes with Alice.
	require.Contains(t, byKey, "couple_ab")
	assert.Equal(t, []int{0, 3}, byKey["couple_ab"].AvailableShiftIndices)

	// A pin naming nobody on the roster stays the solver's error to report; it
	// must not invent a group here.
	assert.NotContains(t, byKey, "ghost")
	assert.Len(t, input.Groups, 3)
}

// The caller's availability map is shared with the coverage and roster reads,
// so building the solver's input must not rewrite what they see.
func TestBuildCpsatInput_DoesNotMutateCallerAvailability(t *testing.T) {
	volunteers := []allocator.Volunteer{
		{ID: "alice", FirstName: "Alice", LastName: "Smith", DisplayName: "Alice", Gender: "Female", GroupKey: ""},
	}
	groupAvailability := map[string][]int{"Alice Smith": {1}}

	overrides := []allocator.ShiftOverride{{
		AppliesTo:      func(date string) bool { return date == "2026-07-13" },
		Preallocations: []allocator.Preallocation{{VolunteerID: "alice", Role: "Service volunteer"}},
	}}

	roles := []allocator.Role{{Name: "Service volunteer", Max: nil, Priority: 1}}

	_, err := allocator.BuildCpsatInput(volunteers, groupAvailability, openShifts("2026-07-13", "2026-07-20"),
		[]allocator.Seat{{Role: "Service volunteer", Count: 2}}, overrides, nil, 0.5, roles, false)
	require.NoError(t, err)

	assert.Equal(t, map[string][]int{"Alice Smith": {1}}, groupAvailability)
}

func TestCpsatOutputToAllocatorShifts(t *testing.T) {
	volunteers := []allocator.Volunteer{
		{ID: "alice", FirstName: "Alice", LastName: "Smith", DisplayName: "Alice", Gender: "Female", GroupKey: "couple_ab"},
		{ID: "bob", FirstName: "Bob", LastName: "Smith", DisplayName: "Bob", Gender: "Male", GroupKey: "couple_ab"},
		{ID: "diana", FirstName: "Diana", LastName: "Green", DisplayName: "Diana", Gender: "Female", GroupKey: ""},
	}
	output := &allocator.CpsatOutput{
		SolverStatus: "OPTIMAL",
		Success:      true,
		Shifts: []allocator.CpsatOutputShift{{
			Index: 0,
			Date:  "2026-07-13",
			Size:  3,
			Assignments: []allocator.CpsatAssignment{
				{VolunteerID: "alice", Role: "Team lead"},
				{VolunteerID: "bob", Role: "Service volunteer"},
				{VolunteerID: "diana", Role: "Service volunteer"},
				{Custom: "external_john", Role: "Service volunteer"},
			},
			AllocatedGroupKeys: []string{"couple_ab", "Diana Green"},
		}},
	}

	shifts, err := allocator.CpsatOutputToShifts(output, volunteers)
	require.NoError(t, err)
	require.Len(t, shifts, 1)
	shift := shifts[0]

	// Each filled Seat keeps the Role the solver gave it; volunteers are
	// resolved, custom entries carry their free text.
	require.Len(t, shift.Assignments, 4)
	require.NotNil(t, shift.Assignments[0].Volunteer)
	assert.Equal(t, "alice", shift.Assignments[0].Volunteer.ID)
	assert.Equal(t, "Team lead", shift.Assignments[0].Role)
	assert.Nil(t, shift.Assignments[3].Volunteer)
	assert.Equal(t, "external_john", shift.Assignments[3].Custom)
	assert.Equal(t, "Service volunteer", shift.Assignments[3].Role)

	// Couple regrouped (alice+bob), individual keyed by name; the custom
	// entry belongs to nobody and so joins no group.
	require.Len(t, shift.AllocatedGroups, 2)

	// convertToDBAllocations reuses the rebuilt shifts: one row per filled
	// Seat, each carrying its own Role.
	dbAllocations, err := convertToDBAllocations(map[string]string{"2026-07-13": "shift-1"}, shifts)
	require.NoError(t, err)
	require.Len(t, dbAllocations, 4)
	roles := map[string]int{}
	for _, a := range dbAllocations {
		assert.Equal(t, "shift-1", a.ShiftID)
		roles[a.Role]++
	}
	assert.Equal(t, map[string]int{"Team lead": 1, "Service volunteer": 3}, roles)

	// Unknown IDs from the solver are rejected.
	output.Shifts[0].Assignments = []allocator.CpsatAssignment{
		{VolunteerID: "nobody", Role: "Service volunteer"},
	}
	_, err = allocator.CpsatOutputToShifts(output, volunteers)
	assert.ErrorContains(t, err, "nobody")
}
