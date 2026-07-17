package services

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// mockListShiftsStore implements ListShiftsStore for testing. When shifts is
// nil, the store synthesises one allocated shift per distinct allocation shift
// id, so tests that only care about allocated shifts need not spell out the
// shift table; tests exercising unallocated shifts set shifts explicitly.
// Fixtures use the date string as the shift id for convenience, so a derived
// shift's id doubles as its date. Allocation/alteration fetches are scoped by
// shift id, mirroring production.
type mockListShiftsStore struct {
	shifts      []db.ShiftInRange
	allocations []db.Allocation
	alterations []db.Alteration
}

// allShifts is the canonical shift set the store would hold, each with an id.
// Explicit shifts without an id default to date-as-id for convenience; derived
// shifts use the allocation's shift id as both id and date.
func (m *mockListShiftsStore) allShifts() []db.ShiftInRange {
	if m.shifts != nil {
		out := make([]db.ShiftInRange, len(m.shifts))
		for i, s := range m.shifts {
			if s.ID == "" {
				s.ID = s.Date
			}
			out[i] = s
		}
		return out
	}

	seen := make(map[string]bool)
	var derived []db.ShiftInRange
	for _, a := range m.allocations {
		if seen[a.ShiftID] {
			continue
		}
		seen[a.ShiftID] = true
		derived = append(derived, db.ShiftInRange{
			Shift:     db.Shift{ID: a.ShiftID, Date: a.ShiftID},
			Allocated: true,
		})
	}
	return derived
}

func idSet(ids []string) map[string]bool {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}

func (m *mockListShiftsStore) GetShiftsInRange(ctx context.Context, from, to time.Time) ([]db.ShiftInRange, error) {
	var filtered []db.ShiftInRange
	for _, s := range m.allShifts() {
		if shiftDateInRange(s.Date, from, to) {
			filtered = append(filtered, s)
		}
	}
	// Mirror the DB's ORDER BY date: production trusts this ordering rather than
	// sorting itself.
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].Date < filtered[j].Date })
	return filtered, nil
}

func (m *mockListShiftsStore) GetAllocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.Allocation, error) {
	want := idSet(shiftIDs)
	var filtered []db.Allocation
	for _, a := range m.allocations {
		if want[a.ShiftID] {
			filtered = append(filtered, a)
		}
	}
	return filtered, nil
}

func (m *mockListShiftsStore) GetAlterationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.Alteration, error) {
	want := idSet(shiftIDs)
	var filtered []db.Alteration
	for _, a := range m.alterations {
		if want[a.ShiftID] {
			filtered = append(filtered, a)
		}
	}
	return filtered, nil
}

// listShiftsVolunteers returns a volunteer client with display names computed
func listShiftsVolunteers() *mockChangeRotaVolClient {
	return &mockChangeRotaVolClient{
		volunteers: []model.Volunteer{
			{ID: "alice", DisplayName: "Alice", Role: model.RoleTeamLead, GroupKey: "smith-family"},
			{ID: "bob", DisplayName: "Bob", Role: model.RoleVolunteer, GroupKey: "smith-family"},
			{ID: "charlie", DisplayName: "Charlie", Role: model.RoleVolunteer},
		},
	}
}

func TestListShifts_BaseAllocations(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockListShiftsStore{
		allocations: []db.Allocation{
			{ID: "a1", ShiftID:"2025-01-12", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
			{ID: "a2", ShiftID:"2025-01-05", Role: string(model.RoleTeamLead), VolunteerID: "alice"},
			{ID: "a3", ShiftID:"2025-01-05", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
			{ID: "a4", ShiftID:"2025-01-05", CustomEntry: "External Org"},
		},
	}

	shifts, err := ListShifts(ctx, store, listShiftsVolunteers(), testCfg, ListShiftsParams{}, logger)
	require.NoError(t, err)
	require.Len(t, shifts, 2)

	// Sorted by date ascending
	assert.Equal(t, "2025-01-05", shifts[0].Date)
	assert.Equal(t, "2025-01-12", shifts[1].Date)

	first := shifts[0]
	assert.False(t, first.Closed)
	assert.Zero(t, first.AlterationCount)
	assert.True(t, first.LastChanged.IsZero())
	require.Len(t, first.Assignees, 3)

	// Team lead sorted first, then alphabetical
	assert.Equal(t, "alice", first.Assignees[0].VolunteerID)
	assert.Equal(t, "Alice", first.Assignees[0].Name)
	assert.Equal(t, string(model.RoleTeamLead), first.Assignees[0].Role)
	assert.Equal(t, "Bob", first.Assignees[1].Name)
	assert.Equal(t, "External Org", first.Assignees[2].Name)
	assert.Equal(t, "External Org", first.Assignees[2].CustomEntry)
	assert.Empty(t, first.Assignees[2].VolunteerID)

	// A volunteer's group key rides along on the assignee; custom entries carry none.
	assert.Equal(t, "smith-family", first.Assignees[0].Group)
	assert.Empty(t, first.Assignees[2].Group)
}

func TestListShifts_UnallocatedRotaShiftsAppear(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	// rota-1 is allocated (has assignees); rota-2 has been minted but not yet
	// allocated, so its shifts must still appear, with allocated=false and no
	// assignees.
	store := &mockListShiftsStore{
		shifts: []db.ShiftInRange{
			{Shift: db.Shift{Date: "2025-01-05", RotaID: "rota-1"}, Allocated: true},
			{Shift: db.Shift{Date: "2025-01-12", RotaID: "rota-2"}, Allocated: false},
			{Shift: db.Shift{Date: "2025-01-19", RotaID: "rota-2"}, Allocated: false},
		},
		allocations: []db.Allocation{
			{ID: "a1", ShiftID:"2025-01-05", Role: string(model.RoleTeamLead), VolunteerID: "alice"},
		},
	}

	shifts, err := ListShifts(ctx, store, listShiftsVolunteers(), testCfg, ListShiftsParams{}, logger)
	require.NoError(t, err)
	require.Len(t, shifts, 3)

	// Allocated rota unchanged: assignees resolved as before.
	assert.Equal(t, "2025-01-05", shifts[0].Date)
	assert.True(t, shifts[0].Allocated)
	require.Len(t, shifts[0].Assignees, 1)
	assert.Equal(t, "alice", shifts[0].Assignees[0].VolunteerID)

	// Unallocated rota's shifts appear with no assignees.
	for _, s := range shifts[1:] {
		assert.False(t, s.Allocated, "shift %s should be unallocated", s.Date)
		assert.Empty(t, s.Assignees, "unallocated shift %s should have no assignees", s.Date)
	}
}

func TestListShifts_BaseAllocationsReportAllocated(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockListShiftsStore{
		allocations: []db.Allocation{
			{ID: "a1", ShiftID:"2025-01-05", Role: string(model.RoleTeamLead), VolunteerID: "alice"},
		},
	}

	shifts, err := ListShifts(ctx, store, listShiftsVolunteers(), testCfg, ListShiftsParams{}, logger)
	require.NoError(t, err)
	require.Len(t, shifts, 1)
	assert.True(t, shifts[0].Allocated)
}

func TestListShifts_AlterationsApplied(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockListShiftsStore{
		allocations: []db.Allocation{
			{ID: "a1", ShiftID:"2025-01-05", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
		},
		alterations: []db.Alteration{
			{ID: "alt1", ShiftID:"2025-01-05", Direction: "remove", VolunteerID: "bob", SetTime: "2025-01-01T10:00:00Z"},
			{ID: "alt2", ShiftID:"2025-01-05", Direction: "add", VolunteerID: "charlie", SetTime: "2025-01-02T10:00:00Z"},
		},
	}

	shifts, err := ListShifts(ctx, store, listShiftsVolunteers(), testCfg, ListShiftsParams{}, logger)
	require.NoError(t, err)
	require.Len(t, shifts, 1)

	shift := shifts[0]
	require.Len(t, shift.Assignees, 1)
	assert.Equal(t, "charlie", shift.Assignees[0].VolunteerID)
	assert.Equal(t, 2, shift.AlterationCount)
	assert.Equal(t, time.Date(2025, 1, 2, 10, 0, 0, 0, time.UTC), shift.LastChanged)
}

func TestListShifts_DateFilters(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockListShiftsStore{
		allocations: []db.Allocation{
			{ID: "a1", ShiftID: "2025-01-05", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
			{ID: "a2", ShiftID: "2025-01-12", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
			{ID: "a3", ShiftID: "2025-01-19", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
		},
	}

	tests := []struct {
		name      string
		params    ListShiftsParams
		wantDates []string
	}{
		{"no filters", ListShiftsParams{}, []string{"2025-01-05", "2025-01-12", "2025-01-19"}},
		{"from only", ListShiftsParams{From: "2025-01-12"}, []string{"2025-01-12", "2025-01-19"}},
		{"to only", ListShiftsParams{To: "2025-01-12"}, []string{"2025-01-05", "2025-01-12"}},
		{"from and to inclusive", ListShiftsParams{From: "2025-01-12", To: "2025-01-12"}, []string{"2025-01-12"}},
		{"empty range", ListShiftsParams{From: "2025-02-01"}, []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			shifts, err := ListShifts(ctx, store, listShiftsVolunteers(), testCfg, tt.params, logger)
			require.NoError(t, err)
			dates := make([]string, 0, len(shifts))
			for _, s := range shifts {
				dates = append(dates, s.Date)
			}
			assert.Equal(t, tt.wantDates, dates)
		})
	}
}

func TestListShifts_InvalidDateFilters(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()
	store := &mockListShiftsStore{}

	_, err := ListShifts(ctx, store, listShiftsVolunteers(), testCfg, ListShiftsParams{From: "12/01/2025"}, logger)
	assert.ErrorIs(t, err, ErrInvalidInput)

	_, err = ListShifts(ctx, store, listShiftsVolunteers(), testCfg, ListShiftsParams{To: "not-a-date"}, logger)
	assert.ErrorIs(t, err, ErrInvalidInput)
}

func TestListShifts_ClosedShift(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	cfg := &config.Config{
		RotaOverrides: []config.RotaOverride{
			// 2025-01-05 is the first Sunday of January 2025
			{RRule: "FREQ=MONTHLY;BYDAY=1SU", Closed: true},
		},
	}

	store := &mockListShiftsStore{
		allocations: []db.Allocation{
			{ID: "a1", ShiftID: "2025-01-05", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
			{ID: "a2", ShiftID: "2025-01-12", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
		},
	}

	shifts, err := ListShifts(ctx, store, listShiftsVolunteers(), cfg, ListShiftsParams{}, logger)
	require.NoError(t, err)
	require.Len(t, shifts, 2)

	assert.True(t, shifts[0].Closed)
	assert.Empty(t, shifts[0].Assignees)
	assert.False(t, shifts[1].Closed)
	assert.Len(t, shifts[1].Assignees, 1)
}

func TestListShifts_UnknownVolunteerDegradesToRawID(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	store := &mockListShiftsStore{
		allocations: []db.Allocation{
			{ID: "a1", ShiftID: "2025-01-05", Role: string(model.RoleVolunteer), VolunteerID: "ghost-id"},
		},
	}

	shifts, err := ListShifts(ctx, store, listShiftsVolunteers(), testCfg, ListShiftsParams{}, logger)
	require.NoError(t, err)
	require.Len(t, shifts, 1)
	require.Len(t, shifts[0].Assignees, 1)
	assert.Equal(t, "ghost-id", shifts[0].Assignees[0].Name)
}

func TestFilterShiftsByVolunteer(t *testing.T) {
	shifts := []Shift{
		{Date: "2025-01-05", Assignees: []ShiftAssignee{{VolunteerID: "alice"}, {VolunteerID: "bob"}}},
		{Date: "2025-01-12", Assignees: []ShiftAssignee{{VolunteerID: "bob"}}},
		{Date: "2025-01-19", Closed: true, Assignees: nil},
		{Date: "2025-01-26", Assignees: []ShiftAssignee{{CustomEntry: "External Org", Name: "External Org"}}},
	}

	filtered := FilterShiftsByVolunteer(shifts, "alice")
	require.Len(t, filtered, 1)
	assert.Equal(t, "2025-01-05", filtered[0].Date)

	filtered = FilterShiftsByVolunteer(shifts, "bob")
	require.Len(t, filtered, 2)

	assert.Empty(t, FilterShiftsByVolunteer(shifts, "nobody"))
}
