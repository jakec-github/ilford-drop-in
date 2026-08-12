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
	// roles replaces the default pair, for a test about the Roles moving under
	// pins that were made before they did.
	roles []db.Role
	// shapes is what each shift asks for, by shift id. It is the ceiling a pin
	// is checked against (issue #185), so a test about anything else takes the
	// roomy default from oneShiftStore.
	shapes map[string][]db.ShiftRequirement

	lockedRotaIDs [][]string
	inserted      []db.Preallocation
	deletedIDs    []string
}

func (m *mockPreallocationStore) ListRoles(ctx context.Context) ([]db.Role, error) {
	if m.roles != nil {
		return m.roles, nil
	}
	return m.testRoleStore.ListRoles(ctx)
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

func (m *mockPreallocationStore) GetShiftShapes(ctx context.Context, shiftIDs []string) (map[string][]db.ShiftRequirement, error) {
	want := idSet(shiftIDs)
	out := map[string][]db.ShiftRequirement{}
	for shiftID, shape := range m.shapes {
		if want[shiftID] {
			out[shiftID] = shape
		}
	}
	return out, nil
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
		shapes: map[string][]db.ShiftRequirement{
			"shift-1": {
				{ShiftID: "shift-1", RoleID: "role-team-lead", Seats: 1},
				{ShiftID: "shift-1", RoleID: "role-service-volunteer", Seats: 4},
			},
		},
	}
}

func TestAddPreallocation_VolunteerHappyPath(t *testing.T) {
	store := oneShiftStore()
	view, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "bob", RoleID: "role-service-volunteer"}, zap.NewNop())
	require.NoError(t, err)
	require.NotNil(t, view)

	require.Len(t, store.inserted, 1)
	got := store.inserted[0]
	assert.Equal(t, "shift-1", got.ShiftID)
	assert.Equal(t, "role-service-volunteer", got.RoleID)
	assert.Equal(t, "bob", got.VolunteerID)
	assert.Empty(t, got.CustomValue)
	assert.Equal(t, "2026-08-02", view.Date)
	assert.Equal(t, [][]string{{"rota-1"}}, store.lockedRotaIDs)
}

func TestAddPreallocation_TeamLeadHappyPath(t *testing.T) {
	store := oneShiftStore()
	view, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "alice", RoleID: "role-team-lead"}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, store.inserted, 1)
	assert.Equal(t, "role-team-lead", store.inserted[0].RoleID)
	assert.Equal(t, "alice", view.VolunteerID)
}

func TestAddPreallocation_CustomHappyPath(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", Custom: "External Org", RoleID: "role-service-volunteer"}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, store.inserted, 1)
	assert.Equal(t, "role-service-volunteer", store.inserted[0].RoleID)
	assert.Equal(t, "External Org", store.inserted[0].CustomValue)
	assert.Empty(t, store.inserted[0].VolunteerID)
}

func TestAddPreallocation_RejectsBothVolunteerAndCustom(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "bob", Custom: "X", RoleID: "role-service-volunteer"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrInvalidInput)
	assert.Empty(t, store.inserted)
}

func TestAddPreallocation_RejectsNeitherVolunteerNorCustom(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", RoleID: "role-service-volunteer"}, zap.NewNop())
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
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "bob", RoleID: "role-food-collector"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrInvalidInput)
	assert.Contains(t, err.Error(), "not a known role")
	assert.Empty(t, store.inserted)
}

// A custom entry holds no roster record, so nothing can say which Roles it
// holds — only that the Role exists. Pinning one into a capped Seat is allowed.
func TestAddPreallocation_CustomInACappedRole(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", Custom: "Visiting lead", RoleID: "role-team-lead"}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, store.inserted, 1)
	assert.Equal(t, "role-team-lead", store.inserted[0].RoleID)
}

func TestAddPreallocation_UnknownVolunteer(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "nobody", RoleID: "role-service-volunteer"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestAddPreallocation_InactiveVolunteer(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "carol", RoleID: "role-service-volunteer"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrInvalidInput)
	assert.Contains(t, err.Error(), "not active")
}

func TestAddPreallocation_RoleTheVolunteerDoesNotHold(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "bob", RoleID: "role-team-lead"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrInvalidInput)
	assert.Contains(t, err.Error(), "does not hold the role")
}

func TestAddPreallocation_UnknownDate(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-09-09", VolunteerID: "bob", RoleID: "role-service-volunteer"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrNotFound)
	assert.Contains(t, err.Error(), "not in any rota")
}

// A closed shift has no Seats to promise anyone, so a pin on one is refused
// rather than stored and then quietly stripped at allocation.
func TestAddPreallocation_ClosedShift(t *testing.T) {
	store := oneShiftStore()
	store.shifts[0].Closed = true
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "bob", RoleID: "role-service-volunteer"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "closed")
	assert.Empty(t, store.inserted)
}

func TestAddPreallocation_DuplicateVolunteer(t *testing.T) {
	store := oneShiftStore()
	store.preallocs = []db.Preallocation{
		{ID: "p1", ShiftID: "shift-1", RoleID: "role-service-volunteer", VolunteerID: "bob"},
	}
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "bob", RoleID: "role-service-volunteer"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "already pinned")
}

// An organisation sending two people is the ordinary case, and both of them are
// "St John's team" (issue #195). The second pin is a second promise, not a
// repeat of the first, and it takes a Seat of its own.
func TestAddPreallocation_TheSameCustomEntryTwice(t *testing.T) {
	store := oneShiftStore()
	store.preallocs = []db.Preallocation{
		{ID: "p1", ShiftID: "shift-1", RoleID: "role-service-volunteer", CustomValue: "External Org"},
	}
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", Custom: "External Org", RoleID: "role-service-volunteer"}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, store.inserted, 1)
	assert.Equal(t, "External Org", store.inserted[0].CustomValue)
	assert.NotEqual(t, "p1", store.inserted[0].ID, "the second pin is a row of its own")
}

// The Seats are what bound a repeated custom entry: four of them fit a Shape
// asking for four, and the fifth is refused as any fifth pin would be.
func TestAddPreallocation_TheSameCustomEntryPastItsSeats(t *testing.T) {
	store := oneShiftStore()
	store.preallocs = []db.Preallocation{
		{ID: "p1", ShiftID: "shift-1", RoleID: "role-service-volunteer", CustomValue: "External Org"},
		{ID: "p2", ShiftID: "shift-1", RoleID: "role-service-volunteer", CustomValue: "External Org"},
		{ID: "p3", ShiftID: "shift-1", RoleID: "role-service-volunteer", CustomValue: "External Org"},
		{ID: "p4", ShiftID: "shift-1", RoleID: "role-service-volunteer", CustomValue: "External Org"},
	}
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", Custom: "External Org", RoleID: "role-service-volunteer"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "every Service volunteer seat")
	assert.Empty(t, store.inserted)
}

// A Role has only the Seats the shift's Shape gives it, and pinning past them
// would hand the solver a shift it cannot fill legally (issue #185). It used to
// be the Role's own ceiling that said so.
func TestAddPreallocation_RoleSeatsAlreadyFull(t *testing.T) {
	store := oneShiftStore()
	store.preallocs = []db.Preallocation{
		{ID: "p1", ShiftID: "shift-1", RoleID: "role-team-lead", VolunteerID: "alice"},
	}
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "dan", RoleID: "role-team-lead"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "every Team lead seat for 2026-08-02 is already pinned")
}

// A Role with Seats to spare takes another pin, however many it already holds.
func TestAddPreallocation_RoleWithSeatsLeftTakesAnother(t *testing.T) {
	store := oneShiftStore()
	store.preallocs = []db.Preallocation{
		{ID: "p1", ShiftID: "shift-1", RoleID: "role-service-volunteer", VolunteerID: "alice"},
		{ID: "p2", ShiftID: "shift-1", RoleID: "role-service-volunteer", CustomValue: "Scouts"},
	}
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "bob", RoleID: "role-service-volunteer"}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, store.inserted, 1)
}

// A Shape the shift's own admin widened seats more of a Role, and the pins
// follow it: the Shape is the ceiling, so raising it raises what may be pinned.
func TestAddPreallocation_FollowsTheShiftsOwnShape(t *testing.T) {
	store := oneShiftStore()
	store.shapes["shift-1"] = []db.ShiftRequirement{
		{ShiftID: "shift-1", RoleID: "role-team-lead", Seats: 2},
	}
	store.preallocs = []db.Preallocation{
		{ID: "p1", ShiftID: "shift-1", RoleID: "role-team-lead", VolunteerID: "alice"},
	}
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "dan", RoleID: "role-team-lead"}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, store.inserted, 1)
}

// A Role the Shape does not name has no Seat to promise anybody, so a pin
// naming it is refused here rather than failing the solve.
func TestAddPreallocation_RefusesARoleTheShapeDoesNotAskFor(t *testing.T) {
	store := oneShiftStore()
	store.shapes["shift-1"] = []db.ShiftRequirement{
		{ShiftID: "shift-1", RoleID: "role-service-volunteer", Seats: 4},
	}
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "alice", RoleID: "role-team-lead"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrConflict)
	assert.Empty(t, store.inserted)
}

func TestAddPreallocation_AlreadyAllocated(t *testing.T) {
	store := oneShiftStore()
	store.allocated["rota-1"] = true
	_, err := AddPreallocation(context.Background(), store, preallocVolunteers(), testCfg,
		AddPreallocationParams{Date: "2026-08-02", VolunteerID: "bob", RoleID: "role-service-volunteer"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "already allocated")
	assert.Empty(t, store.inserted)
}

func TestDeletePreallocation_HappyPath(t *testing.T) {
	store := oneShiftStore()
	store.preallocs = []db.Preallocation{
		{ID: "p1", ShiftID: "shift-1", RoleID: "role-service-volunteer", VolunteerID: "bob"},
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
		{ID: "p1", ShiftID: "shift-1", RoleID: "role-service-volunteer", VolunteerID: "bob"},
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
			{ID: "p1", ShiftID: "shift-1", RoleID: "role-team-lead", VolunteerID: "alice"},
			{ID: "p2", ShiftID: "shift-2", RoleID: "role-service-volunteer", CustomValue: "External"},
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
	assert.Equal(t, "role-team-lead", byID["p1"].RoleID)
	assert.Equal(t, "Team lead", byID["p1"].Role)
}

// A pin references its Role, so renaming the Role renames what the pin reads as
// (issue #195). Nothing about the row moves: it is the same promise, and the
// listing says it in today's words.
func TestListPreallocations_ReadsARenamedRoleUnderItsNewName(t *testing.T) {
	store := &mockPreallocationStore{
		roles: []db.Role{
			{ID: "role-team-lead", Name: "Shift lead", Priority: 1, Colour: "violet"},
			{ID: "role-service-volunteer", Name: "Service volunteer", Priority: 2, Colour: "teal"},
		},
		shiftRanges: []db.ShiftInRange{
			{Shift: db.Shift{ID: "shift-1", Date: "2026-08-02", RotaID: "rota-1"}},
		},
		preallocs: []db.Preallocation{
			{ID: "p1", ShiftID: "shift-1", RoleID: "role-team-lead", VolunteerID: "alice"},
		},
	}
	views, err := ListPreallocations(context.Background(), store, preallocVolunteers(), testCfg, ListPreallocationsParams{}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, views, 1)
	assert.Equal(t, "role-team-lead", views[0].RoleID, "the reference is untouched")
	assert.Equal(t, "Shift lead", views[0].Role)
}

func TestListPreallocations_BoundsFilterShifts(t *testing.T) {
	store := &mockPreallocationStore{
		shiftRanges: []db.ShiftInRange{
			{Shift: db.Shift{ID: "shift-1", Date: "2026-08-02", RotaID: "rota-1"}},
			{Shift: db.Shift{ID: "shift-2", Date: "2026-08-09", RotaID: "rota-1"}},
		},
		preallocs: []db.Preallocation{
			{ID: "p1", ShiftID: "shift-1", RoleID: "role-service-volunteer", VolunteerID: "alice"},
			{ID: "p2", ShiftID: "shift-2", RoleID: "role-service-volunteer", VolunteerID: "bob"},
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
		{ID: "p1", ShiftID: "shift-1", RoleID: "role-service-volunteer", VolunteerID: "dan"},
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
		{ID: "p1", ShiftID: "shift-1", RoleID: "role-service-volunteer", VolunteerID: "ghost"},
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
		{ID: "p1", ShiftID: "shift-2", RoleID: "role-service-volunteer", VolunteerID: "bob"},
		{ID: "p2", ShiftID: "shift-1", RoleID: "role-service-volunteer", CustomValue: "Aardvark Group"},
		{ID: "p3", ShiftID: "shift-1", RoleID: "role-team-lead", VolunteerID: "dan"},
		{ID: "p4", ShiftID: "shift-2", RoleID: "role-team-lead", VolunteerID: "alice"},
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
