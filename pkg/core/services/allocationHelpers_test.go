package services

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/clients/sheetsclient"
	"github.com/jakechorley/ilford-drop-in/pkg/core/allocator"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services/utils"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// sundayShifts builds the shift rows a rota's backfill would have produced:
// one shift per consecutive Sunday from start, mirroring the old date
// arithmetic so tests can feed shift rows through the mock stores. The shift id
// is the date string: dates are globally unique (the real schema's date UNIQUE
// constraint), so a date doubles as a stable shift id in tests.
func sundayShifts(rotaID, start string, count int) []db.Shift {
	startDate, err := time.Parse("2006-01-02", start)
	if err != nil {
		panic(err)
	}
	shifts := make([]db.Shift, count)
	for i := 0; i < count; i++ {
		date := startDate.AddDate(0, 0, i*7).Format("2006-01-02")
		shifts[i] = db.Shift{ID: date, RotaID: rotaID, Date: date}
	}
	return shifts
}

// shiftsOnDates builds shift rows for a rota on the given dates, using each date
// as its shift id (see sundayShifts).
func shiftsOnDates(rotaID string, dates ...string) []db.Shift {
	shifts := make([]db.Shift, len(dates))
	for i, d := range dates {
		shifts[i] = db.Shift{ID: d, RotaID: rotaID, Date: d}
	}
	return shifts
}

// mockAllocateRotaStore implements AllocateRotaStore for testing
type mockAllocateRotaStore struct {
	rotations                  []db.Rotation
	shifts                     []db.Shift
	availabilityRequests     []db.AvailabilityRequest
	generations                map[string]db.AvailabilityGeneration // keyed by request id
	allocations                []db.Allocation
	alterations                []db.Alteration
	manualPreallocations       []db.ManualPreallocation
	insertedAllocations        []db.Allocation
	getRotationsErr            error
	getAvailabilityErr         error
	getLatestAvailabilityErr   error
	getAllocationsErr          error
	getAlterationsErr          error
	getManualPreallocationsErr error
	insertAllocationsErr       error
}

func (m *mockAllocateRotaStore) GetRotations(ctx context.Context) ([]db.Rotation, error) {
	if m.getRotationsErr != nil {
		return nil, m.getRotationsErr
	}
	return m.rotations, nil
}

func (m *mockAllocateRotaStore) GetShiftsByRotaID(ctx context.Context, rotaID string) ([]db.Shift, error) {
	var filtered []db.Shift
	for _, s := range m.shifts {
		if s.RotaID == rotaID {
			filtered = append(filtered, s)
		}
	}
	return filtered, nil
}

func (m *mockAllocateRotaStore) GetAvailabilityRequestsByRotaID(ctx context.Context, rotaID string) ([]db.AvailabilityRequest, error) {
	if m.getAvailabilityErr != nil {
		return nil, m.getAvailabilityErr
	}
	var filtered []db.AvailabilityRequest
	for _, r := range m.availabilityRequests {
		if r.RotaID == rotaID {
			filtered = append(filtered, r)
		}
	}
	return filtered, nil
}

func (m *mockAllocateRotaStore) GetLatestAvailability(ctx context.Context, requestIDs []string, cutoff *time.Time) (map[string]db.AvailabilityGeneration, error) {
	if m.getLatestAvailabilityErr != nil {
		return nil, m.getLatestAvailabilityErr
	}
	latest := make(map[string]db.AvailabilityGeneration)
	for _, id := range requestIDs {
		generation, ok := m.generations[id]
		if !ok {
			continue
		}
		if cutoff != nil && generation.SubmittedAt.After(*cutoff) {
			continue
		}
		latest[id] = generation
	}
	return latest, nil
}

func (m *mockAllocateRotaStore) GetAllocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.Allocation, error) {
	if m.getAllocationsErr != nil {
		return nil, m.getAllocationsErr
	}
	want := idSet(shiftIDs)
	var filtered []db.Allocation
	for _, a := range m.allocations {
		if want[a.ShiftID] {
			filtered = append(filtered, a)
		}
	}
	return filtered, nil
}

func (m *mockAllocateRotaStore) GetAlterationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.Alteration, error) {
	if m.getAlterationsErr != nil {
		return nil, m.getAlterationsErr
	}
	want := idSet(shiftIDs)
	var filtered []db.Alteration
	for _, a := range m.alterations {
		if want[a.ShiftID] {
			filtered = append(filtered, a)
		}
	}
	return filtered, nil
}

func (m *mockAllocateRotaStore) GetManualPreallocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.ManualPreallocation, error) {
	if m.getManualPreallocationsErr != nil {
		return nil, m.getManualPreallocationsErr
	}
	want := idSet(shiftIDs)
	var filtered []db.ManualPreallocation
	for _, p := range m.manualPreallocations {
		if want[p.ShiftID] {
			filtered = append(filtered, p)
		}
	}
	return filtered, nil
}

func (m *mockAllocateRotaStore) InsertAllocationsAndSetAllocated(ctx context.Context, allocations []db.Allocation, rotaID string, datetime time.Time) error {
	if m.insertAllocationsErr != nil {
		return m.insertAllocationsErr
	}
	m.insertedAllocations = append(m.insertedAllocations, allocations...)
	return nil
}

// mockVolClient implements VolunteerClient for testing
type mockVolClient struct {
	volunteers []model.Volunteer
	listErr    error
}

func (m *mockVolClient) ListVolunteers(cfg *config.Config) ([]model.Volunteer, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	// Compute display names like the real client does
	sheetsclient.ComputeDisplayNames(m.volunteers)
	return m.volunteers, nil
}

func TestConvertToDBAllocations(t *testing.T) {
	// One row per filled Seat, each keeping the Role the solver gave it —
	// including Roles this code has never heard of.
	alice := allocator.Volunteer{ID: "alice"}
	bob := allocator.Volunteer{ID: "bob"}
	shifts := []*allocator.Shift{
		{
			Date:  "2025-01-05",
			Index: 0,
			Size:  2,
			AllocatedGroups: []*allocator.VolunteerGroup{
				{
					GroupKey: "group_a",
					Members:  []allocator.Volunteer{alice, bob},
				},
			},
			Assignments: []allocator.Assignment{
				{Volunteer: &alice, Role: "Team lead"},
				{Volunteer: &bob, Role: "Hot food"},
				{Custom: "external_john", Role: "Service volunteer"},
			},
		},
	}

	shiftIDByDate := map[string]string{"2025-01-05": "shift-jan-5"}
	allocations, err := convertToDBAllocations(shiftIDByDate, shifts)
	require.NoError(t, err)
	require.Len(t, allocations, 3)

	roles := make(map[string]int)
	for _, alloc := range allocations {
		roles[alloc.Role]++
		assert.Equal(t, "shift-jan-5", alloc.ShiftID)
	}
	assert.Equal(t, map[string]int{
		"Team lead":         1,
		"Hot food":          1,
		"Service volunteer": 1,
	}, roles)

	// A custom entry is a name, not a volunteer.
	found := false
	for _, alloc := range allocations {
		if alloc.CustomEntry == "external_john" {
			found = true
			assert.Equal(t, "", alloc.VolunteerID, "Pre-allocated should have empty VolunteerID")
		}
	}
	assert.True(t, found, "Should have pre-allocated volunteer")
}

func TestConvertToDBAllocations_MissingShiftFails(t *testing.T) {
	// The solver only ever sees minted dates, so a date absent from the shift
	// map is a broken invariant: convertToDBAllocations must fail loudly rather
	// than emit an allocation that would trip the shift_id FK on insert.
	alice := allocator.Volunteer{ID: "alice"}
	shifts := []*allocator.Shift{
		{
			Date: "2025-01-05",
			Assignments: []allocator.Assignment{
				{Volunteer: &alice, Role: "Team lead"},
			},
		},
	}

	_, err := convertToDBAllocations(map[string]string{"2025-01-12": "shift-other"}, shifts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "2025-01-05")
}

func TestFilterActiveVols(t *testing.T) {
	volunteers := []model.Volunteer{
		{ID: "alice", Status: "Active"},
		{ID: "bob", Status: "Inactive"},
		{ID: "charlie", Status: "Active"},
		{ID: "diana", Status: ""},
	}

	active := utils.FilterActiveVolunteers(volunteers)
	assert.Len(t, active, 2)
	assert.Equal(t, "alice", active[0].ID)
	assert.Equal(t, "charlie", active[1].ID)
}

func TestBuildHistoricalShifts_SkipsUnknownVolunteers(t *testing.T) {
	// Allocations whose volunteer id no longer exists in the sheet are
	// skipped, but the shift itself is still emitted. (Inactive volunteers
	// are NOT filtered here — callers pass the full volunteer list.)
	ctx := context.Background()
	logger := zap.NewNop()

	// Setup: Two rotas - old (rota-0) and current (rota-1)
	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{
			{ID: "rota-0", Start: "2024-12-01", ShiftCount: 2}, // Old rota
			{ID: "rota-1", Start: "2025-01-05", ShiftCount: 2}, // Current rota
		},
		shifts: shiftsOnDates("rota-0", "2024-12-01", "2024-12-08"),
		// Allocations from the old rota (rota-0)
		// Alice and Bob are a couple (group_alice_bob)
		// Charlie has been deleted from the volunteer sheet (unknown id)
		// Dave is individual
		allocations: []db.Allocation{
			// Shift 1 - Dec 1: Alice (group), Bob (group), Charlie (individual)
			{ID: "alloc-1", ShiftID: "2024-12-01", VolunteerID: "alice", Role: string(model.RoleVolunteer)},
			{ID: "alloc-2", ShiftID: "2024-12-01", VolunteerID: "bob", Role: string(model.RoleTeamLead)},
			{ID: "alloc-3", ShiftID: "2024-12-01", VolunteerID: "charlie", Role: string(model.RoleVolunteer)}, // Unknown
			// Shift 2 - Dec 8: Dave (individual), Charlie (individual)
			{ID: "alloc-4", ShiftID: "2024-12-08", VolunteerID: "dave", Role: string(model.RoleVolunteer)},
			{ID: "alloc-5", ShiftID: "2024-12-08", VolunteerID: "charlie", Role: string(model.RoleVolunteer)}, // Unknown
			// Allocations from current rota (not one of rota-0's shifts): ignored
			{ID: "alloc-6", ShiftID: "2025-01-05", VolunteerID: "alice", Role: string(model.RoleVolunteer)},
		},
	}

	// Known volunteers (Charlie has been deleted from the sheet)
	volunteers := []allocator.Volunteer{
		{ID: "alice", FirstName: "Alice", LastName: "A", Gender: "Female", GroupKey: "group_alice_bob"},
		{ID: "bob", FirstName: "Bob", LastName: "B", Gender: "Male", GroupKey: "group_alice_bob"},
		{ID: "dave", FirstName: "Dave", LastName: "D", Gender: "Male", GroupKey: ""}, // Individual
		// Charlie is NOT in the list (deleted)
	}

	targetRota := &db.Rotation{ID: "rota-1", Start: "2025-01-05", ShiftCount: 2}

	// Call buildHistoricalShifts
	historicalShifts, err := buildHistoricalShifts(ctx, store, store.rotations, targetRota, volunteers, string(model.RoleVolunteer), logger)
	require.NoError(t, err)

	// Assertions
	require.Len(t, historicalShifts, 2, "Should have 2 historical shifts")

	// Check first shift (Dec 1) - should only have Alice and Bob (Charlie skipped)
	shift1 := historicalShifts[0]
	assert.Equal(t, "2024-12-01", shift1.Date)
	assert.Len(t, shift1.AllocatedGroups, 1, "Should have 1 group (Alice+Bob)")

	// Verify the group
	group1 := shift1.AllocatedGroups[0]
	assert.Equal(t, "group_alice_bob", group1.GroupKey)
	assert.Len(t, group1.Members, 2, "Group should have 2 members")
	assert.Equal(t, 1, group1.MaleCount, "Group should have 1 male (Bob)")

	// Verify members
	memberIDs := make([]string, len(group1.Members))
	for i, member := range group1.Members {
		memberIDs[i] = member.ID
	}
	assert.Contains(t, memberIDs, "alice")
	assert.Contains(t, memberIDs, "bob")

	// Check second shift (Dec 8) - should only have Dave (Charlie skipped)
	shift2 := historicalShifts[1]
	assert.Equal(t, "2024-12-08", shift2.Date)
	assert.Len(t, shift2.AllocatedGroups, 1, "Should have 1 group (Dave)")

	// Verify Dave's individual group (should have individual_ prefix)
	group2 := shift2.AllocatedGroups[0]
	assert.Len(t, group2.Members, 1, "Group should have 1 member")
	assert.Equal(t, "dave", group2.Members[0].ID)
	assert.Equal(t, 1, group2.MaleCount, "Dave is male")
}

func TestBuildHistoricalShifts_KeepsShiftsWithNoKnownVolunteers(t *testing.T) {
	// A date whose workers are all unknown (deleted from the sheet) must
	// still appear — with empty groups — so the last historical shift is
	// the true last shift, and the result is sorted ascending by date.
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{
			{ID: "rota-0", Start: "2024-12-01", ShiftCount: 3},
			{ID: "rota-1", Start: "2025-01-05", ShiftCount: 3},
		},
		shifts: shiftsOnDates("rota-0", "2024-12-01", "2024-12-08", "2024-12-15"),
		allocations: []db.Allocation{
			{ID: "alloc-1", ShiftID: "2024-12-01", VolunteerID: "alice", Role: string(model.RoleVolunteer)},
			{ID: "alloc-2", ShiftID: "2024-12-08", VolunteerID: "alice", Role: string(model.RoleVolunteer)},
			// Dec 15 (the true last shift) was worked only by a deleted volunteer.
			{ID: "alloc-3", ShiftID: "2024-12-15", VolunteerID: "ghost", Role: string(model.RoleVolunteer)},
		},
	}

	volunteers := []allocator.Volunteer{
		{ID: "alice", FirstName: "Alice", LastName: "A", Gender: "Female", GroupKey: "Alice A"},
	}

	targetRota := &db.Rotation{ID: "rota-1", Start: "2025-01-05", ShiftCount: 3}

	historicalShifts, err := buildHistoricalShifts(ctx, store, store.rotations, targetRota, volunteers, string(model.RoleVolunteer), logger)
	require.NoError(t, err)
	require.Len(t, historicalShifts, 3)

	// Sorted ascending by date, no manual reordering needed.
	assert.Equal(t, "2024-12-01", historicalShifts[0].Date)
	assert.Equal(t, "2024-12-08", historicalShifts[1].Date)
	assert.Equal(t, "2024-12-15", historicalShifts[2].Date)

	// The last shift survives with no groups: Alice (on Dec 8) is NOT on
	// the boundary, and the boundary doesn't fall back to Dec 8.
	assert.Empty(t, historicalShifts[2].AllocatedGroups)
	assert.Len(t, historicalShifts[1].AllocatedGroups, 1)
}

func TestBuildHistoricalShifts_AppliesAlterations(t *testing.T) {
	// History must reflect who actually worked: a volunteer who dropped
	// out via an alteration disappears, and their cover appears.
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{
			{ID: "rota-0", Start: "2024-12-01", ShiftCount: 2},
			{ID: "rota-1", Start: "2025-01-05", ShiftCount: 2},
		},
		shifts: shiftsOnDates("rota-0", "2024-12-01", "2024-12-08"),
		allocations: []db.Allocation{
			// Dec 1: Alice worked as published.
			{ID: "alloc-1", ShiftID: "2024-12-01", VolunteerID: "alice", Role: string(model.RoleVolunteer)},
			// Dec 8: Alice dropped out and Dave covered (see alterations).
			{ID: "alloc-2", ShiftID: "2024-12-08", VolunteerID: "alice", Role: string(model.RoleVolunteer)},
		},
		alterations: []db.Alteration{
			{ID: "alt-1", ShiftID: "2024-12-08", Direction: "remove", VolunteerID: "alice", SetTime: "2024-12-05T10:00:00Z"},
			{ID: "alt-2", ShiftID: "2024-12-08", Direction: "add", VolunteerID: "dave", Role: string(model.RoleVolunteer), SetTime: "2024-12-05T10:00:01Z"},
			// Alteration on another rota's shift (not one of rota-0's): must be ignored.
			{ID: "alt-3", ShiftID: "2025-01-05", Direction: "remove", VolunteerID: "alice", SetTime: "2024-12-05T10:00:02Z"},
		},
	}

	activeVolunteers := []allocator.Volunteer{
		{ID: "alice", FirstName: "Alice", LastName: "A", Gender: "Female", GroupKey: "Alice A"},
		{ID: "dave", FirstName: "Dave", LastName: "D", Gender: "Male", GroupKey: "Dave D"},
	}

	targetRota := &db.Rotation{ID: "rota-1", Start: "2025-01-05", ShiftCount: 2}

	historicalShifts, err := buildHistoricalShifts(ctx, store, store.rotations, targetRota, activeVolunteers, string(model.RoleVolunteer), logger)
	require.NoError(t, err)
	require.Len(t, historicalShifts, 2)

	// Dec 1 untouched: Alice worked it (the rota-1 alteration is ignored).
	require.Len(t, historicalShifts[0].AllocatedGroups, 1)
	assert.Equal(t, "alice", historicalShifts[0].AllocatedGroups[0].Members[0].ID)

	// Dec 8 altered: Dave worked it, not Alice.
	require.Len(t, historicalShifts[1].AllocatedGroups, 1)
	assert.Equal(t, "dave", historicalShifts[1].AllocatedGroups[0].Members[0].ID)
}

func TestBuildHistoricalShifts_NoPreviousRota(t *testing.T) {
	// Test that buildHistoricalShifts returns empty array when there's no previous rota
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2025-01-05", ShiftCount: 2}, // Only one rota (current)
		},
	}

	targetRota := &db.Rotation{ID: "rota-1", Start: "2025-01-05", ShiftCount: 2}
	activeVolunteers := []allocator.Volunteer{
		{ID: "alice", FirstName: "Alice", LastName: "A", Gender: "Female"},
	}

	historicalShifts, err := buildHistoricalShifts(ctx, store, store.rotations, targetRota, activeVolunteers, string(model.RoleVolunteer), logger)
	require.NoError(t, err)
	assert.Empty(t, historicalShifts, "Should have no historical shifts when there's no previous rota")
}

func TestBuildHistoricalShifts_NoPreviousAllocations(t *testing.T) {
	// Test that buildHistoricalShifts returns empty array when previous rota has no allocations
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{
			{ID: "rota-0", Start: "2024-12-01", ShiftCount: 2}, // Old rota
			{ID: "rota-1", Start: "2025-01-05", ShiftCount: 2}, // Current rota
		},
		allocations: []db.Allocation{}, // No allocations
	}

	targetRota := &db.Rotation{ID: "rota-1", Start: "2025-01-05", ShiftCount: 2}
	activeVolunteers := []allocator.Volunteer{
		{ID: "alice", FirstName: "Alice", LastName: "A", Gender: "Female"},
	}

	historicalShifts, err := buildHistoricalShifts(ctx, store, store.rotations, targetRota, activeVolunteers, string(model.RoleVolunteer), logger)
	require.NoError(t, err)
	assert.Empty(t, historicalShifts, "Should have no historical shifts when previous rota has no allocations")
}

func TestBuildHistoricalShifts_CustomEntriesIgnored(t *testing.T) {
	// Test that custom entries (allocations with empty VolunteerID) are ignored
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{
			{ID: "rota-0", Start: "2024-12-01", ShiftCount: 1},
			{ID: "rota-1", Start: "2025-01-05", ShiftCount: 1},
		},
		shifts: shiftsOnDates("rota-0", "2024-12-01"),
		allocations: []db.Allocation{
			// Regular allocation
			{ID: "alloc-1", ShiftID: "2024-12-01", VolunteerID: "alice", Role: string(model.RoleVolunteer)},
			// Custom entry (should be ignored)
			{ID: "alloc-2", ShiftID: "2024-12-01", VolunteerID: "", CustomEntry: "External John", Role: string(model.RoleVolunteer)},
		},
	}

	activeVolunteers := []allocator.Volunteer{
		{ID: "alice", FirstName: "Alice", LastName: "A", Gender: "Female", GroupKey: ""},
	}

	targetRota := &db.Rotation{ID: "rota-1", Start: "2025-01-05", ShiftCount: 1}

	historicalShifts, err := buildHistoricalShifts(ctx, store, store.rotations, targetRota, activeVolunteers, string(model.RoleVolunteer), logger)
	require.NoError(t, err)
	require.Len(t, historicalShifts, 1)

	// Should only have Alice's individual group (custom entry ignored)
	assert.Len(t, historicalShifts[0].AllocatedGroups, 1)
	assert.Equal(t, "Alice A", historicalShifts[0].AllocatedGroups[0].GroupKey)
}

// availabilityShiftIDs are the shift ids of a three-shift rota, in the order
// the solver indexes them.
var availabilityShiftIDs = []string{"2026-08-02", "2026-08-09", "2026-08-16"}

// availabilityRound sets a store up with one minted request per volunteer and
// the generations behind them, so the group rule can be exercised against the
// store the allocator now reads.
func availabilityRound(generations map[string][]string) *mockAllocateRotaStore {
	store := &mockAllocateRotaStore{
		rotations:   []db.Rotation{{ID: "rota-1", Start: "2026-08-02", ShiftCount: 3}},
		shifts:      shiftsOnDates("rota-1", availabilityShiftIDs...),
		generations: map[string]db.AvailabilityGeneration{},
	}
	for volunteerID, shiftIDs := range generations {
		requestID := "req-" + volunteerID
		store.availabilityRequests = append(store.availabilityRequests, db.AvailabilityRequest{
			ID: requestID, RotaID: "rota-1", VolunteerID: volunteerID, Token: "tok-" + volunteerID,
		})
		if shiftIDs == nil {
			continue // minted but never answered
		}
		answers := make([]db.ShiftAnswer, 0, len(shiftIDs))
		for _, shiftID := range shiftIDs {
			answers = append(answers, db.ShiftAnswer{ShiftID: shiftID, Answer: db.AnswerYes})
		}
		store.generations[requestID] = db.AvailabilityGeneration{
			RequestID: requestID, ResponseID: "gen-" + volunteerID, Answers: answers,
		}
	}
	return store
}

// The group rule of ADR 0004, read off the store: a group can work a shift iff
// at least one member answered and every member who answered said yes. Each row
// of the plan's Michael/Emma table is one shift here, so the whole rule is
// checked in one allocation-shaped read.
//
// This is the switchover's central claim — that DB-backed availability resolves
// to what the equivalent Forms answers resolved to. Forms encoded the dual
// (union the responders' unavailability); the columns below are that union's
// complement, shift for shift.
func TestFetchGroupAvailability_GroupRule(t *testing.T) {
	store := availabilityRound(map[string][]string{
		// Both answered: where they agree the group can work, and Emma's
		// silence on s1 and s2 is a NO that takes the group out of them.
		"michael": availabilityShiftIDs,
		"emma":    {availabilityShiftIDs[0]},
		// Only Jack answered. Kate is opted in by his answer rather than
		// blocking it — a partially-responding group is still a responding one.
		"jack": {availabilityShiftIDs[0], availabilityShiftIDs[1]},
		"kate": nil,
		// An individual, and someone nobody has heard from at all.
		"lonely": {availabilityShiftIDs[1]},
		"silent": nil,
	})

	volunteers := []allocator.Volunteer{
		{ID: "michael", FirstName: "Michael", LastName: "Smith", GroupKey: "couple_me"},
		{ID: "emma", FirstName: "Emma", LastName: "Williams", GroupKey: "couple_me"},
		{ID: "jack", FirstName: "Jack", LastName: "Green", GroupKey: "couple_jk"},
		{ID: "kate", FirstName: "Kate", LastName: "Green", GroupKey: "couple_jk"},
		{ID: "lonely", FirstName: "Lonely", LastName: "Jones"},
		{ID: "silent", FirstName: "Silent", LastName: "Brown"},
	}

	availability, err := fetchGroupAvailability(
		context.Background(), store, "rota-1", volunteers, availabilityShiftIDs, zap.NewNop())
	require.NoError(t, err)

	assert.Equal(t, []int{0}, availability["couple_me"])
	assert.Equal(t, []int{0, 1}, availability["couple_jk"])
	assert.Equal(t, []int{1}, availability["Lonely Jones"])
	assert.NotContains(t, availability, "Silent Brown",
		"a group nobody answered for must be absent, not empty")
}

// "Available for nothing" is a real answer and reads differently from silence:
// the group is in the map, with no shifts. The allocator discards both, but only
// one of them is a non-reply.
func TestFetchGroupAvailability_AnsweredWithNothing(t *testing.T) {
	store := availabilityRound(map[string][]string{"nobody": {}})
	volunteers := []allocator.Volunteer{{ID: "nobody", FirstName: "No", LastName: "Body"}}

	availability, err := fetchGroupAvailability(
		context.Background(), store, "rota-1", volunteers, availabilityShiftIDs, zap.NewNop())
	require.NoError(t, err)

	indices, present := availability["No Body"]
	assert.True(t, present, "an answer of none-of-these is still an answer")
	assert.Empty(t, indices)
}

// A volunteer who has gone inactive since the round was minted still holds a
// working link, but cannot be allocated — so their answer must not speak for the
// group they used to be in.
func TestFetchGroupAvailability_IgnoresInactiveVolunteers(t *testing.T) {
	store := availabilityRound(map[string][]string{
		"michael": {availabilityShiftIDs[0], availabilityShiftIDs[1]},
		"emma":    {availabilityShiftIDs[0]},
	})

	// Emma is no longer in the active set handed to the allocator.
	volunteers := []allocator.Volunteer{
		{ID: "michael", FirstName: "Michael", LastName: "Smith", GroupKey: "couple_me"},
	}

	availability, err := fetchGroupAvailability(
		context.Background(), store, "rota-1", volunteers, availabilityShiftIDs, zap.NewNop())
	require.NoError(t, err)

	assert.Equal(t, []int{0, 1}, availability["couple_me"],
		"Emma's answer must not narrow a group she is no longer part of")
}

// Indices are positions in the solver's shift order, not in the order the
// answers happen to come back in.
func TestFetchGroupAvailability_IndicesFollowShiftOrder(t *testing.T) {
	store := availabilityRound(map[string][]string{
		"vol": {availabilityShiftIDs[2], availabilityShiftIDs[0]},
	})
	volunteers := []allocator.Volunteer{{ID: "vol", FirstName: "Vol", LastName: "Unteer"}}

	availability, err := fetchGroupAvailability(
		context.Background(), store, "rota-1", volunteers, availabilityShiftIDs, zap.NewNop())
	require.NoError(t, err)

	assert.Equal(t, []int{0, 2}, availability["Vol Unteer"])
}

// Allocating a rota nobody was asked about is an operator mistake, not an empty
// result: it would otherwise solve against silence and produce an empty rota.
func TestFetchGroupAvailability_NoRoundMinted(t *testing.T) {
	store := availabilityRound(nil)

	_, err := fetchGroupAvailability(
		context.Background(), store, "rota-1", nil, availabilityShiftIDs, zap.NewNop())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rota-1")
	assert.Contains(t, err.Error(), "minted")
}
