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
	testRoleStore

	shifts      []db.Shift
	allocated   map[string]bool // rota id → allocated
	preallocs   []db.Preallocation
	shiftRanges []db.ShiftInRange

	lockedRotaIDs [][]string
	inserted      []db.Preallocation
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

func (m *mockPreallocationStore) GetPreallocationByID(ctx context.Context, id string) (*db.Preallocation, *db.Shift, error) {
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

func (m *mockPreallocationStore) GetPreallocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.Preallocation, error) {
	want := idSet(shiftIDs)
	var out []db.Preallocation
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

func (m *mockPreallocationStore) InsertPreallocation(ctx context.Context, mp db.Preallocation) error {
	m.inserted = append(m.inserted, mp)
	m.preallocs = append(m.preallocs, mp)
	return nil
}

func (m *mockPreallocationStore) DeletePreallocationByID(ctx context.Context, id string) (bool, error) {
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

func (c *preallocVolClient) ListVolunteers(cfg *config.Config, roles model.Roles) ([]model.Volunteer, error) {
	return c.volunteers, nil
}

func preallocVolunteers() *preallocVolClient {
	return &preallocVolClient{
		volunteers: []model.Volunteer{
			{ID: "alice", FirstName: "Alice", DisplayName: "Alice", Roles: []string{"Team lead", "Service volunteer"}, Status: "Active"},
			{ID: "bob", FirstName: "Bob", DisplayName: "Bob", Roles: []string{"Service volunteer"}, Status: "Active"},
			{ID: "carol", FirstName: "Carol", DisplayName: "Carol", Roles: []string{"Service volunteer"}, Status: "Inactive"},
			{ID: "dan", FirstName: "Dan", DisplayName: "Dan", Roles: []string{"Team lead", "Service volunteer"}, Status: "Active"},
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
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "bob", Role: "Service volunteer"}, zap.NewNop())
	require.NoError(t, err)
	require.NotNil(t, view)

	require.Len(t, store.inserted, 1)
	got := store.inserted[0]
	assert.Equal(t, "shift-1", got.ShiftID)
	assert.Equal(t, "Service volunteer", got.Role)
	assert.Equal(t, "bob", got.VolunteerID)
	assert.Empty(t, got.CustomValue)
	assert.Equal(t, "2026-08-02", view.Date)
	assert.Equal(t, [][]string{{"rota-1"}}, store.lockedRotaIDs)
}

func TestAddPreallocation_TeamLeadHappyPath(t *testing.T) {
	store := oneShiftStore()
	view, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "alice", Role: "Team lead"}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, store.inserted, 1)
	assert.Equal(t, "Team lead", store.inserted[0].Role)
	assert.Equal(t, "alice", view.VolunteerID)
}

func TestAddPreallocation_CustomHappyPath(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", Custom: "External Org", Role: "Service volunteer"}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, store.inserted, 1)
	assert.Equal(t, "Service volunteer", store.inserted[0].Role)
	assert.Equal(t, "External Org", store.inserted[0].CustomValue)
	assert.Empty(t, store.inserted[0].VolunteerID)
}

func TestAddPreallocation_RejectsBothVolunteerAndCustom(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "bob", Custom: "X", Role: "Service volunteer"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrInvalidInput)
	assert.Empty(t, store.inserted)
}

func TestAddPreallocation_RejectsNeitherVolunteerNorCustom(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", Role: "Service volunteer"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrInvalidInput)
}

func TestAddPreallocation_RejectsMissingRole(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "bob"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrInvalidInput)
	assert.Contains(t, err.Error(), "role is required")
}

func TestAddPreallocation_RejectsUnknownRole(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "bob", Role: "Food collector"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrInvalidInput)
	assert.Contains(t, err.Error(), "not a known role")
	assert.Empty(t, store.inserted)
}

// A custom entry holds no roster record, so nothing can say which Roles it
// holds — only that the Role exists. Pinning one into a capped Seat is allowed.
func TestAddPreallocation_CustomInACappedRole(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", Custom: "Visiting lead", Role: "Team lead"}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, store.inserted, 1)
	assert.Equal(t, "Team lead", store.inserted[0].Role)
}

func TestAddPreallocation_UnknownVolunteer(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "nobody", Role: "Service volunteer"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestAddPreallocation_InactiveVolunteer(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "carol", Role: "Service volunteer"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrInvalidInput)
	assert.Contains(t, err.Error(), "not active")
}

func TestAddPreallocation_RoleTheVolunteerDoesNotHold(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "bob", Role: "Team lead"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrInvalidInput)
	assert.Contains(t, err.Error(), "does not hold the role")
}

func TestAddPreallocation_UnknownDate(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-09-09", VolunteerID: "bob", Role: "Service volunteer"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrNotFound)
	assert.Contains(t, err.Error(), "not in any rota")
}

// A closed shift has no Seats to promise anyone, so a pin on one is refused
// rather than stored and then quietly stripped at allocation.
func TestAddPreallocation_ClosedShift(t *testing.T) {
	store := oneShiftStore()
	store.shifts[0].Closed = true
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "bob", Role: "Service volunteer"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "closed")
	assert.Empty(t, store.inserted)
}

func TestAddPreallocation_DuplicateVolunteer(t *testing.T) {
	store := oneShiftStore()
	store.preallocs = []db.Preallocation{
		{ID: "p1", ShiftID: "shift-1", Role: "Service volunteer", VolunteerID: "bob"},
	}
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "bob", Role: "Service volunteer"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "already pinned")
}

func TestAddPreallocation_DuplicateCustom(t *testing.T) {
	store := oneShiftStore()
	store.preallocs = []db.Preallocation{
		{ID: "p1", ShiftID: "shift-1", Role: "Service volunteer", CustomValue: "External Org"},
	}
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", Custom: "External Org", Role: "Service volunteer"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrConflict)
}

func TestAddPreallocation_CappedRoleAlreadyFull(t *testing.T) {
	store := oneShiftStore()
	store.preallocs = []db.Preallocation{
		{ID: "p1", ShiftID: "shift-1", Role: "Team lead", VolunteerID: "alice"},
	}
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "dan", Role: "Team lead"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "every Team lead seat for 2026-08-02 is already pinned")
}

// The uncapped Role has no ceiling to hit, however many are already pinned.
func TestAddPreallocation_UncappedRoleHasNoCeiling(t *testing.T) {
	store := oneShiftStore()
	store.preallocs = []db.Preallocation{
		{ID: "p1", ShiftID: "shift-1", Role: "Service volunteer", VolunteerID: "alice"},
		{ID: "p2", ShiftID: "shift-1", Role: "Service volunteer", CustomValue: "Scouts"},
	}
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "bob", Role: "Service volunteer"}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, store.inserted, 1)
}

func TestAddPreallocation_AlreadyAllocated(t *testing.T) {
	store := oneShiftStore()
	store.allocated["rota-1"] = true
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "bob", Role: "Service volunteer"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "already allocated")
	assert.Empty(t, store.inserted)
}

func TestDeletePreallocation_HappyPath(t *testing.T) {
	store := oneShiftStore()
	store.preallocs = []db.Preallocation{
		{ID: "p1", ShiftID: "shift-1", Role: "Service volunteer", VolunteerID: "bob"},
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
	store.preallocs = []db.Preallocation{
		{ID: "p1", ShiftID: "shift-1", Role: "Service volunteer", VolunteerID: "bob"},
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
		preallocs: []db.Preallocation{
			{ID: "p1", ShiftID: "shift-1", Role: "Team lead", VolunteerID: "alice"},
			{ID: "p2", ShiftID: "shift-2", Role: "Service volunteer", CustomValue: "External"},
		},
	}
	views, err := ListPreallocations(context.Background(), store, preallocVolunteers(), testCfg, ListPreallocationsParams{}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, views, 2)

	byID := map[string]PreallocationView{}
	for _, v := range views {
		byID[v.ID] = v
	}
	assert.Equal(t, "2026-08-02", byID["p1"].Date)
	assert.Equal(t, "alice", byID["p1"].VolunteerID)
	assert.Equal(t, "Alice", byID["p1"].Name)
	assert.Equal(t, "2026-08-09", byID["p2"].Date)
	assert.Equal(t, "External", byID["p2"].Custom)
	assert.Equal(t, "External", byID["p2"].Name, "a custom pin is its own name")
}

func TestListPreallocations_BoundsFilterShifts(t *testing.T) {
	store := &mockPreallocationStore{
		shiftRanges: []db.ShiftInRange{
			{Shift: db.Shift{ID: "shift-1", Date: "2026-08-02", RotaID: "rota-1"}},
			{Shift: db.Shift{ID: "shift-2", Date: "2026-08-09", RotaID: "rota-1"}},
		},
		preallocs: []db.Preallocation{
			{ID: "p1", ShiftID: "shift-1", Role: "Service volunteer", VolunteerID: "alice"},
			{ID: "p2", ShiftID: "shift-2", Role: "Service volunteer", VolunteerID: "bob"},
		},
	}
	views, err := ListPreallocations(context.Background(), store, preallocVolunteers(), testCfg,
		ListPreallocationsParams{From: "2026-08-05", To: "2026-08-12"}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, views, 1)
	assert.Equal(t, "p2", views[0].ID)
}

// twoSundayStore is the listing fixture: two consecutive Sundays in one rota,
// no pins.
func twoSundayStore() *mockPreallocationStore {
	return &mockPreallocationStore{
		shiftRanges: []db.ShiftInRange{
			{Shift: db.Shift{ID: "shift-1", Date: "2026-08-02", RotaID: "rota-1"}},
			{Shift: db.Shift{ID: "shift-2", Date: "2026-08-09", RotaID: "rota-1"}},
		},
	}
}

func listPreallocs(t *testing.T, store *mockPreallocationStore) []PreallocationView {
	t.Helper()
	views, err := ListPreallocations(context.Background(), store, preallocVolunteers(), testCfg, ListPreallocationsParams{}, zap.NewNop())
	require.NoError(t, err)
	return views
}

// A closed shift carries no pins into allocation (InitShifts clears them), so
// it must not show any either.
func TestListPreallocations_SkipsClosedShifts(t *testing.T) {
	store := twoSundayStore()
	store.shiftRanges[0].Closed = true
	store.preallocs = []db.Preallocation{
		{ID: "p1", ShiftID: "shift-1", Role: "Service volunteer", VolunteerID: "dan"},
	}

	assert.Empty(t, listPreallocs(t, store))
}

// A pin naming someone the roster no longer knows still has to be visible — it
// is the reason allocation will fail, and hiding it hides the fix. It is the
// pins nobody typed recently that reach this state: a Standing Preallocation
// seeds one at definition and the person can have left by allocation.
func TestListPreallocations_UnknownVolunteerShowsID(t *testing.T) {
	store := twoSundayStore()
	store.preallocs = []db.Preallocation{
		{ID: "p1", ShiftID: "shift-1", Role: "Service volunteer", VolunteerID: "ghost"},
	}

	views := listPreallocs(t, store)
	require.Len(t, views, 1)
	assert.Equal(t, "ghost", views[0].VolunteerID)
	assert.Equal(t, "ghost", views[0].Name)
}

// Ordered by date and then team lead first — the order a shift is read in, not
// the order the rows came back in.
func TestListPreallocations_OrderedByDateThenRole(t *testing.T) {
	store := twoSundayStore()
	store.preallocs = []db.Preallocation{
		{ID: "p1", ShiftID: "shift-2", Role: "Service volunteer", VolunteerID: "bob"},
		{ID: "p2", ShiftID: "shift-1", Role: "Service volunteer", CustomValue: "Aardvark Group"},
		{ID: "p3", ShiftID: "shift-1", Role: "Team lead", VolunteerID: "dan"},
		{ID: "p4", ShiftID: "shift-2", Role: "Team lead", VolunteerID: "alice"},
	}

	views := listPreallocs(t, store)
	require.Len(t, views, 4)

	type entry struct{ date, name string }
	got := make([]entry, 0, len(views))
	for _, v := range views {
		got = append(got, entry{v.Date, v.Name})
	}
	assert.Equal(t, []entry{
		{"2026-08-02", "Dan"},
		{"2026-08-02", "Aardvark Group"},
		{"2026-08-09", "Alice"},
		{"2026-08-09", "Bob"},
	}, got)
}
