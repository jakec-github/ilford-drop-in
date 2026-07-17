package services

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// mockPreallocationStore implements PreallocationStore. WithRotaPreallocationLock
// records the requested lock and hands the mock itself to the callback as the
// transaction-bound store, mirroring the WithRotaLock mock in changeRota_test.
type mockPreallocationStore struct {
	shifts      []db.Shift
	allocated   map[string]bool // rota id → allocated
	preallocs   []db.ManualPreallocation
	shiftRanges []db.ShiftInRange

	lockedRotaIDs [][]string
	inserted      []db.ManualPreallocation
	deletedIDs    []string
}

func (m *mockPreallocationStore) GetShiftByDate(ctx context.Context, date time.Time) (*db.Shift, error) {
	dateStr := date.Format("2006-01-02")
	for i := range m.shifts {
		if m.shifts[i].Date == dateStr {
			return &m.shifts[i], nil
		}
	}
	return nil, nil
}

func (m *mockPreallocationStore) GetManualPreallocationByID(ctx context.Context, id string) (*db.ManualPreallocation, *db.Shift, error) {
	for i := range m.preallocs {
		if m.preallocs[i].ID == id {
			p := m.preallocs[i]
			for j := range m.shifts {
				if m.shifts[j].ID == p.ShiftID {
					return &p, &m.shifts[j], nil
				}
			}
			return &p, nil, nil
		}
	}
	return nil, nil, nil
}

func (m *mockPreallocationStore) GetManualPreallocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.ManualPreallocation, error) {
	want := idSet(shiftIDs)
	var out []db.ManualPreallocation
	for _, p := range m.preallocs {
		if want[p.ShiftID] {
			out = append(out, p)
		}
	}
	return out, nil
}

func (m *mockPreallocationStore) GetShiftsInRange(ctx context.Context, from, to time.Time) ([]db.ShiftInRange, error) {
	var out []db.ShiftInRange
	for _, s := range m.shiftRanges {
		if shiftDateInRange(s.Date, from, to) {
			out = append(out, s)
		}
	}
	return out, nil
}

func (m *mockPreallocationStore) WithRotaPreallocationLock(ctx context.Context, rotaIDs []string, fn func(store db.PreallocationTxStore) error) error {
	m.lockedRotaIDs = append(m.lockedRotaIDs, rotaIDs)
	return fn(m)
}

// PreallocationTxStore methods (the mock plays both roles, like changeRota_test).

func (m *mockPreallocationStore) RotaAllocated(ctx context.Context, rotaID string) (bool, error) {
	return m.allocated[rotaID], nil
}

func (m *mockPreallocationStore) InsertManualPreallocation(ctx context.Context, mp db.ManualPreallocation) error {
	m.inserted = append(m.inserted, mp)
	m.preallocs = append(m.preallocs, mp)
	return nil
}

func (m *mockPreallocationStore) DeleteManualPreallocationByID(ctx context.Context, id string) (bool, error) {
	for i := range m.preallocs {
		if m.preallocs[i].ID == id {
			m.preallocs = append(m.preallocs[:i], m.preallocs[i+1:]...)
			m.deletedIDs = append(m.deletedIDs, id)
			return true, nil
		}
	}
	return false, nil
}

// preallocVolClient serves a fixed volunteer list.
type preallocVolClient struct {
	volunteers []model.Volunteer
}

func (c *preallocVolClient) ListVolunteers(cfg *config.Config) ([]model.Volunteer, error) {
	return c.volunteers, nil
}

func preallocVolunteers() *preallocVolClient {
	return &preallocVolClient{
		volunteers: []model.Volunteer{
			{ID: "alice", FirstName: "Alice", Role: model.RoleTeamLead, Status: "Active"},
			{ID: "bob", FirstName: "Bob", Role: model.RoleVolunteer, Status: "Active"},
			{ID: "carol", FirstName: "Carol", Role: model.RoleVolunteer, Status: "Inactive"},
			{ID: "dan", FirstName: "Dan", Role: model.RoleTeamLead, Status: "Active"},
		},
	}
}

func oneShiftStore() *mockPreallocationStore {
	return &mockPreallocationStore{
		shifts:    []db.Shift{{ID: "shift-1", Date: "2026-08-02", RotaID: "rota-1"}},
		allocated: map[string]bool{},
	}
}

func TestAddPreallocation_VolunteerHappyPath(t *testing.T) {
	store := oneShiftStore()
	view, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "bob"}, zap.NewNop())
	require.NoError(t, err)
	require.NotNil(t, view)

	require.Len(t, store.inserted, 1)
	got := store.inserted[0]
	assert.Equal(t, "shift-1", got.ShiftID)
	assert.Equal(t, string(model.RoleVolunteer), got.Role)
	assert.Equal(t, "bob", got.VolunteerID)
	assert.Empty(t, got.CustomValue)
	assert.Equal(t, "2026-08-02", view.Date)
	assert.Equal(t, [][]string{{"rota-1"}}, store.lockedRotaIDs)
}

func TestAddPreallocation_TeamLeadHappyPath(t *testing.T) {
	store := oneShiftStore()
	view, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "alice", TeamLead: true}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, store.inserted, 1)
	assert.Equal(t, string(model.RoleTeamLead), store.inserted[0].Role)
	assert.Equal(t, "alice", view.VolunteerID)
}

func TestAddPreallocation_CustomHappyPath(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", Custom: "External Org"}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, store.inserted, 1)
	assert.Equal(t, string(model.RoleVolunteer), store.inserted[0].Role)
	assert.Equal(t, "External Org", store.inserted[0].CustomValue)
	assert.Empty(t, store.inserted[0].VolunteerID)
}

func TestAddPreallocation_RejectsBothVolunteerAndCustom(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "bob", Custom: "X"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrInvalidInput)
	assert.Empty(t, store.inserted)
}

func TestAddPreallocation_RejectsNeitherVolunteerNorCustom(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrInvalidInput)
}

func TestAddPreallocation_RejectsTeamLeadWithCustom(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", Custom: "X", TeamLead: true}, zap.NewNop())
	assert.ErrorIs(t, err, ErrInvalidInput)
}

func TestAddPreallocation_UnknownVolunteer(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "nobody"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestAddPreallocation_InactiveVolunteer(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "carol"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrInvalidInput)
	assert.Contains(t, err.Error(), "not active")
}

func TestAddPreallocation_NonTeamLeadAsTeamLead(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "bob", TeamLead: true}, zap.NewNop())
	assert.ErrorIs(t, err, ErrInvalidInput)
	assert.Contains(t, err.Error(), "not a team lead")
}

func TestAddPreallocation_UnknownDate(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-09-09", VolunteerID: "bob"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrNotFound)
	assert.Contains(t, err.Error(), "not in any rota")
}

func TestAddPreallocation_ClosedShift(t *testing.T) {
	store := oneShiftStore()
	cfg := &config.Config{RotaOverrides: []config.RotaOverride{
		{RRule: "FREQ=WEEKLY;BYDAY=SU", Closed: true},
	}}
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), cfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "bob"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "closed")
}

func TestAddPreallocation_ConfigPinsTeamLead(t *testing.T) {
	store := oneShiftStore()
	cfg := &config.Config{RotaOverrides: []config.RotaOverride{
		{RRule: "FREQ=WEEKLY;BYDAY=SU", PreallocatedTeamLeadID: "someone"},
	}}
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), cfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "alice", TeamLead: true}, zap.NewNop())
	assert.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "config already pins a team lead")
}

// A config team-lead pin must not block an ordinary volunteer pin on the same date.
func TestAddPreallocation_ConfigTeamLeadDoesNotBlockVolunteer(t *testing.T) {
	store := oneShiftStore()
	cfg := &config.Config{RotaOverrides: []config.RotaOverride{
		{RRule: "FREQ=WEEKLY;BYDAY=SU", PreallocatedTeamLeadID: "someone"},
	}}
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), cfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "bob"}, zap.NewNop())
	require.NoError(t, err)
}

func TestAddPreallocation_DuplicateVolunteer(t *testing.T) {
	store := oneShiftStore()
	store.preallocs = []db.ManualPreallocation{
		{ID: "p1", ShiftID: "shift-1", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
	}
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "bob"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "already pinned")
}

func TestAddPreallocation_DuplicateCustom(t *testing.T) {
	store := oneShiftStore()
	store.preallocs = []db.ManualPreallocation{
		{ID: "p1", ShiftID: "shift-1", Role: string(model.RoleVolunteer), CustomValue: "External Org"},
	}
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", Custom: "External Org"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrConflict)
}

func TestAddPreallocation_SecondTeamLead(t *testing.T) {
	store := oneShiftStore()
	store.preallocs = []db.ManualPreallocation{
		{ID: "p1", ShiftID: "shift-1", Role: string(model.RoleTeamLead), VolunteerID: "alice"},
	}
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "dan", TeamLead: true}, zap.NewNop())
	assert.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "team lead is already pinned")
}

func TestAddPreallocation_AlreadyAllocated(t *testing.T) {
	store := oneShiftStore()
	store.allocated["rota-1"] = true
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "bob"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "already allocated")
	assert.Empty(t, store.inserted)
}

func TestDeletePreallocation_HappyPath(t *testing.T) {
	store := oneShiftStore()
	store.preallocs = []db.ManualPreallocation{
		{ID: "p1", ShiftID: "shift-1", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
	}
	err := DeletePreallocation(context.Background(), store, "p1", zap.NewNop())
	require.NoError(t, err)
	assert.Equal(t, []string{"p1"}, store.deletedIDs)
	assert.Equal(t, [][]string{{"rota-1"}}, store.lockedRotaIDs)
}

func TestDeletePreallocation_NotFound(t *testing.T) {
	store := oneShiftStore()
	err := DeletePreallocation(context.Background(), store, "missing", zap.NewNop())
	assert.ErrorIs(t, err, ErrNotFound)
	assert.Empty(t, store.lockedRotaIDs, "unknown id resolved before any lock")
}

func TestDeletePreallocation_AlreadyAllocated(t *testing.T) {
	store := oneShiftStore()
	store.allocated["rota-1"] = true
	store.preallocs = []db.ManualPreallocation{
		{ID: "p1", ShiftID: "shift-1", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
	}
	err := DeletePreallocation(context.Background(), store, "p1", zap.NewNop())
	assert.ErrorIs(t, err, ErrConflict)
	assert.Empty(t, store.deletedIDs)
}

func TestListPreallocations(t *testing.T) {
	store := &mockPreallocationStore{
		shiftRanges: []db.ShiftInRange{
			{Shift: db.Shift{ID: "shift-1", Date: "2026-08-02", RotaID: "rota-1"}},
			{Shift: db.Shift{ID: "shift-2", Date: "2026-08-09", RotaID: "rota-1"}},
		},
		preallocs: []db.ManualPreallocation{
			{ID: "p1", ShiftID: "shift-1", Role: string(model.RoleTeamLead), VolunteerID: "alice"},
			{ID: "p2", ShiftID: "shift-2", Role: string(model.RoleVolunteer), CustomValue: "External"},
		},
	}
	views, err := ListPreallocations(context.Background(), store, ListPreallocationsParams{})
	require.NoError(t, err)
	require.Len(t, views, 2)

	byID := map[string]PreallocationView{}
	for _, v := range views {
		byID[v.ID] = v
	}
	assert.Equal(t, "2026-08-02", byID["p1"].Date)
	assert.Equal(t, "alice", byID["p1"].VolunteerID)
	assert.Equal(t, "2026-08-09", byID["p2"].Date)
	assert.Equal(t, "External", byID["p2"].Custom)
}

func TestListPreallocations_BoundsFilterShifts(t *testing.T) {
	store := &mockPreallocationStore{
		shiftRanges: []db.ShiftInRange{
			{Shift: db.Shift{ID: "shift-1", Date: "2026-08-02", RotaID: "rota-1"}},
			{Shift: db.Shift{ID: "shift-2", Date: "2026-08-09", RotaID: "rota-1"}},
		},
		preallocs: []db.ManualPreallocation{
			{ID: "p1", ShiftID: "shift-1", Role: string(model.RoleVolunteer), VolunteerID: "alice"},
			{ID: "p2", ShiftID: "shift-2", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
		},
	}
	views, err := ListPreallocations(context.Background(), store, ListPreallocationsParams{From: "2026-08-05", To: "2026-08-12"})
	require.NoError(t, err)
	require.Len(t, views, 1)
	assert.Equal(t, "p2", views[0].ID)
}
