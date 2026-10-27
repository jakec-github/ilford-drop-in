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

// mockPinStore implements PinStore. WithRotaPinLock
// records the requested lock and hands the mock itself to the callback as the
// transaction-bound store, mirroring the WithRotaLock mock in changeRota_test.
type mockPinStore struct {
	testRoleStore

	shifts      []db.Shift
	allocated   map[string]bool // rota id → allocated
	pins        []db.Pin
	shiftRanges []db.ShiftInRange
	// roles replaces the default pair, for a test about the Roles moving under
	// pins that were made before they did.
	roles []db.Role
	// shapes is what each shift asks for, by shift id. It is the ceiling a pin
	// is checked against (issue #185), so a test about anything else takes the
	// roomy default from oneShiftStore.
	shapes map[string][]db.ShiftRequirement

	lockedRotaIDs [][]string
	inserted      []db.Pin
	deletedIDs    []string
}

func (m *mockPinStore) ListRoles(ctx context.Context) ([]db.Role, error) {
	if m.roles != nil {
		return m.roles, nil
	}
	return m.testRoleStore.ListRoles(ctx)
}

func (m *mockPinStore) GetShiftByID(ctx context.Context, id string) (*db.ShiftInRange, error) {
	for i := range m.shifts {
		if m.shifts[i].ID == id {
			return &db.ShiftInRange{Shift: m.shifts[i], Allocated: m.allocated[m.shifts[i].RotaID]}, nil
		}
	}
	return nil, nil
}

func (m *mockPinStore) GetPinByID(ctx context.Context, id string) (*db.Pin, *db.Shift, error) {
	for i := range m.pins {
		if m.pins[i].ID == id {
			p := m.pins[i]
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

func (m *mockPinStore) GetPinsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.Pin, error) {
	want := idSet(shiftIDs)
	var out []db.Pin
	for _, p := range m.pins {
		if want[p.ShiftID] {
			out = append(out, p)
		}
	}
	return out, nil
}

func (m *mockPinStore) GetShiftsInRange(ctx context.Context, from, to time.Time) ([]db.ShiftInRange, error) {
	var out []db.ShiftInRange
	for _, s := range m.shiftRanges {
		if shiftDateInRange(s.Date, from, to) {
			out = append(out, s)
		}
	}
	return out, nil
}

func (m *mockPinStore) WithRotaPinLock(ctx context.Context, rotaIDs []string, fn func(store db.PinTxStore) error) error {
	m.lockedRotaIDs = append(m.lockedRotaIDs, rotaIDs)
	return fn(m)
}

// PinTxStore methods (the mock plays both roles, like changeRota_test).

func (m *mockPinStore) RotaAllocated(ctx context.Context, rotaID string) (bool, error) {
	return m.allocated[rotaID], nil
}

func (m *mockPinStore) InsertPin(ctx context.Context, mp db.Pin) error {
	m.inserted = append(m.inserted, mp)
	m.pins = append(m.pins, mp)
	return nil
}

func (m *mockPinStore) GetShiftShapes(ctx context.Context, shiftIDs []string) (map[string][]db.ShiftRequirement, error) {
	want := idSet(shiftIDs)
	out := map[string][]db.ShiftRequirement{}
	for shiftID, shape := range m.shapes {
		if want[shiftID] {
			out[shiftID] = shape
		}
	}
	return out, nil
}

func (m *mockPinStore) DeletePinByID(ctx context.Context, id string) (bool, error) {
	for i := range m.pins {
		if m.pins[i].ID == id {
			m.pins = append(m.pins[:i], m.pins[i+1:]...)
			m.deletedIDs = append(m.deletedIDs, id)
			return true, nil
		}
	}
	return false, nil
}

// pinVolClient serves a fixed volunteer list.
type pinVolClient struct {
	volunteers []model.Volunteer
}

func (c *pinVolClient) ListVolunteers(organisation *config.Organisation, roles model.Roles) ([]model.Volunteer, error) {
	return c.volunteers, nil
}

func pinVolunteers() *pinVolClient {
	return &pinVolClient{
		volunteers: []model.Volunteer{
			{ID: "alice", FirstName: "Alice", DisplayName: "Alice", Roles: []string{"Team lead", "Service volunteer"}, Status: "Active"},
			{ID: "bob", FirstName: "Bob", DisplayName: "Bob", Roles: []string{"Service volunteer"}, Status: "Active"},
			{ID: "carol", FirstName: "Carol", DisplayName: "Carol", Roles: []string{"Service volunteer"}, Status: "Inactive"},
			{ID: "dan", FirstName: "Dan", DisplayName: "Dan", Roles: []string{"Team lead", "Service volunteer"}, Status: "Active"},
		},
	}
}

func oneShiftStore() *mockPinStore {
	return &mockPinStore{
		shifts: []db.Shift{{
			ID: "shift-1", Date: "2026-08-02", RotaID: "rota-1",
			StartAt: "2026-08-02T19:30:00", EndAt: "2026-08-02T21:30:00",
		}},
		allocated: map[string]bool{},
		shapes: map[string][]db.ShiftRequirement{
			"shift-1": {
				{ShiftID: "shift-1", RoleID: "role-team-lead", Seats: 1},
				{ShiftID: "shift-1", RoleID: "role-service-volunteer", Seats: 4},
			},
		},
	}
}

func TestAddPin_VolunteerHappyPath(t *testing.T) {
	store := oneShiftStore()
	view, err := AddPin(context.Background(), store, pinVolunteers(), testCfg,
		AddPinParams{ShiftID: "shift-1", VolunteerID: "bob", RoleID: "role-service-volunteer"}, zap.NewNop())
	require.NoError(t, err)
	require.NotNil(t, view)

	require.Len(t, store.inserted, 1)
	got := store.inserted[0]
	assert.Equal(t, "shift-1", got.ShiftID)
	assert.Equal(t, "role-service-volunteer", got.RoleID)
	assert.Equal(t, "bob", got.VolunteerID)
	assert.Empty(t, got.CustomValue)
	assert.Equal(t, "shift-1", view.ShiftID)
	assert.Equal(t, "2026-08-02", view.Date)
	assert.Equal(t, [][]string{{"rota-1"}}, store.lockedRotaIDs)
}

func TestAddPin_TeamLeadHappyPath(t *testing.T) {
	store := oneShiftStore()
	view, err := AddPin(context.Background(), store, pinVolunteers(), testCfg,
		AddPinParams{ShiftID: "shift-1", VolunteerID: "alice", RoleID: "role-team-lead"}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, store.inserted, 1)
	assert.Equal(t, "role-team-lead", store.inserted[0].RoleID)
	assert.Equal(t, "alice", view.VolunteerID)
}

func TestAddPin_CustomHappyPath(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPin(context.Background(), store, pinVolunteers(), testCfg,
		AddPinParams{ShiftID: "shift-1", Custom: "External Org", RoleID: "role-service-volunteer"}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, store.inserted, 1)
	assert.Equal(t, "role-service-volunteer", store.inserted[0].RoleID)
	assert.Equal(t, "External Org", store.inserted[0].CustomValue)
	assert.Empty(t, store.inserted[0].VolunteerID)
}

func TestAddPin_RejectsBothVolunteerAndCustom(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPin(context.Background(), store, pinVolunteers(), testCfg,
		AddPinParams{ShiftID: "shift-1", VolunteerID: "bob", Custom: "X", RoleID: "role-service-volunteer"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrInvalidInput)
	assert.Empty(t, store.inserted)
}

func TestAddPin_RejectsNeitherVolunteerNorCustom(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPin(context.Background(), store, pinVolunteers(), testCfg,
		AddPinParams{ShiftID: "shift-1", RoleID: "role-service-volunteer"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrInvalidInput)
}

func TestAddPin_RejectsMissingRole(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPin(context.Background(), store, pinVolunteers(), testCfg,
		AddPinParams{ShiftID: "shift-1", VolunteerID: "bob"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrInvalidInput)
	assert.Contains(t, err.Error(), "role is required")
}

func TestAddPin_RejectsUnknownRole(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPin(context.Background(), store, pinVolunteers(), testCfg,
		AddPinParams{ShiftID: "shift-1", VolunteerID: "bob", RoleID: "role-food-collector"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrInvalidInput)
	assert.Contains(t, err.Error(), "not a known role")
	assert.Empty(t, store.inserted)
}

// A custom entry holds no roster record, so nothing can say which Roles it
// holds — only that the Role exists. Pinning one into a capped Seat is allowed.
func TestAddPin_CustomInACappedRole(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPin(context.Background(), store, pinVolunteers(), testCfg,
		AddPinParams{ShiftID: "shift-1", Custom: "Visiting lead", RoleID: "role-team-lead"}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, store.inserted, 1)
	assert.Equal(t, "role-team-lead", store.inserted[0].RoleID)
}

func TestAddPin_UnknownVolunteer(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPin(context.Background(), store, pinVolunteers(), testCfg,
		AddPinParams{ShiftID: "shift-1", VolunteerID: "nobody", RoleID: "role-service-volunteer"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrNotFound)
}

// Being inactive is roster data, not a representation limit, so it never
// refuses a pin (ADR 0010).
func TestAddPin_InactiveVolunteer(t *testing.T) {
	store := oneShiftStore()
	view, err := AddPin(context.Background(), store, pinVolunteers(), testCfg,
		AddPinParams{ShiftID: "shift-1", VolunteerID: "carol", RoleID: "role-service-volunteer"}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, store.inserted, 1)
	assert.Equal(t, "carol", store.inserted[0].VolunteerID)
	assert.Equal(t, "Carol", view.Name)
}

// Not holding a Role is a rule, and rules bind the allocator rather than the
// Organiser (ADR 0010): the pin goes in, under the Role it names.
func TestAddPin_RoleTheVolunteerDoesNotHold(t *testing.T) {
	store := oneShiftStore()
	view, err := AddPin(context.Background(), store, pinVolunteers(), testCfg,
		AddPinParams{ShiftID: "shift-1", VolunteerID: "bob", RoleID: "role-team-lead"}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, store.inserted, 1)
	assert.Equal(t, "role-team-lead", store.inserted[0].RoleID)
	assert.Equal(t, "bob", store.inserted[0].VolunteerID)
	assert.Equal(t, "Team lead", view.Role)
}

func TestAddPin_UnknownShift(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPin(context.Background(), store, pinVolunteers(), testCfg,
		AddPinParams{ShiftID: "shift-ghost", VolunteerID: "bob", RoleID: "role-service-volunteer"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrNotFound)
	assert.Contains(t, err.Error(), "shift-ghost")
}

func TestAddPin_RequiresAShift(t *testing.T) {
	store := oneShiftStore()
	_, err := AddPin(context.Background(), store, pinVolunteers(), testCfg,
		AddPinParams{VolunteerID: "bob", RoleID: "role-service-volunteer"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrInvalidInput)
	assert.Empty(t, store.inserted)
}

// A pin names its Shift, not its date: with two Shifts on one day, pinning
// somebody to the evening leaves the morning alone (issue #293).
func TestAddPin_PinsTheShiftNamedNotItsDate(t *testing.T) {
	store := oneShiftStore()
	store.shifts = append(store.shifts, db.Shift{
		ID: "shift-1-evening", Date: "2026-08-02", RotaID: "rota-1",
		StartAt: "2026-08-02T18:00:00", EndAt: "2026-08-02T20:00:00",
	})
	view, err := AddPin(context.Background(), store, pinVolunteers(), testCfg,
		AddPinParams{ShiftID: "shift-1-evening", VolunteerID: "bob", RoleID: "role-service-volunteer"}, zap.NewNop())
	require.NoError(t, err)

	require.Len(t, store.inserted, 1)
	assert.Equal(t, "shift-1-evening", store.inserted[0].ShiftID)
	assert.Equal(t, "shift-1-evening", view.ShiftID)
}

// A closed shift has no Seats to promise anyone, so a pin on one is refused
// rather than stored and then quietly stripped at allocation.
func TestAddPin_ClosedShift(t *testing.T) {
	store := oneShiftStore()
	store.shifts[0].Closed = true
	_, err := AddPin(context.Background(), store, pinVolunteers(), testCfg,
		AddPinParams{ShiftID: "shift-1", VolunteerID: "bob", RoleID: "role-service-volunteer"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "closed")
	assert.Empty(t, store.inserted)
}

func TestAddPin_DuplicateVolunteer(t *testing.T) {
	store := oneShiftStore()
	store.pins = []db.Pin{
		{ID: "p1", ShiftID: "shift-1", RoleID: "role-service-volunteer", VolunteerID: "bob"},
	}
	_, err := AddPin(context.Background(), store, pinVolunteers(), testCfg,
		AddPinParams{ShiftID: "shift-1", VolunteerID: "bob", RoleID: "role-service-volunteer"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "already pinned to 2 August 2026 at 19:30",
		"a refusal names the Shift by its date and start, not its id")
}

// An organisation sending two people is the ordinary case, and both of them are
// "St John's team" (issue #195). The second pin is a second promise, not a
// repeat of the first, and it takes a Seat of its own.
func TestAddPin_TheSameCustomEntryTwice(t *testing.T) {
	store := oneShiftStore()
	store.pins = []db.Pin{
		{ID: "p1", ShiftID: "shift-1", RoleID: "role-service-volunteer", CustomValue: "External Org"},
	}
	_, err := AddPin(context.Background(), store, pinVolunteers(), testCfg,
		AddPinParams{ShiftID: "shift-1", Custom: "External Org", RoleID: "role-service-volunteer"}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, store.inserted, 1)
	assert.Equal(t, "External Org", store.inserted[0].CustomValue)
	assert.NotEqual(t, "p1", store.inserted[0].ID, "the second pin is a row of its own")
}

// A Shape bounds the allocator, not people's decisions (ADR 0010), so an
// organisation may be promised more Seats than the Shape has: the fifth pin
// on a Shape asking for four is kept, and the allocator adds nobody else.
func TestAddPin_TheSameCustomEntryPastItsSeats(t *testing.T) {
	store := oneShiftStore()
	store.pins = []db.Pin{
		{ID: "p1", ShiftID: "shift-1", RoleID: "role-service-volunteer", CustomValue: "External Org"},
		{ID: "p2", ShiftID: "shift-1", RoleID: "role-service-volunteer", CustomValue: "External Org"},
		{ID: "p3", ShiftID: "shift-1", RoleID: "role-service-volunteer", CustomValue: "External Org"},
		{ID: "p4", ShiftID: "shift-1", RoleID: "role-service-volunteer", CustomValue: "External Org"},
	}
	_, err := AddPin(context.Background(), store, pinVolunteers(), testCfg,
		AddPinParams{ShiftID: "shift-1", Custom: "External Org", RoleID: "role-service-volunteer"}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, store.inserted, 1)
}

// A Role whose Seats are all pinned still takes another pin (ADR 0010): the
// Shape bounds the allocator, and people decide past it.
func TestAddPin_PastTheRolesSeats(t *testing.T) {
	store := oneShiftStore()
	store.pins = []db.Pin{
		{ID: "p1", ShiftID: "shift-1", RoleID: "role-team-lead", VolunteerID: "alice"},
	}
	_, err := AddPin(context.Background(), store, pinVolunteers(), testCfg,
		AddPinParams{ShiftID: "shift-1", VolunteerID: "dan", RoleID: "role-team-lead"}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, store.inserted, 1)
	assert.Equal(t, "role-team-lead", store.inserted[0].RoleID)
}

// A Role the Shape does not name is no reason to refuse either: the pinned
// person sits past the Shape, and the solver honours it (ADR 0010).
func TestAddPin_ARoleTheShapeDoesNotAskFor(t *testing.T) {
	store := oneShiftStore()
	store.shapes["shift-1"] = []db.ShiftRequirement{
		{ShiftID: "shift-1", RoleID: "role-service-volunteer", Seats: 4},
	}
	_, err := AddPin(context.Background(), store, pinVolunteers(), testCfg,
		AddPinParams{ShiftID: "shift-1", VolunteerID: "alice", RoleID: "role-team-lead"}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, store.inserted, 1)
}

func TestAddPin_AlreadyAllocated(t *testing.T) {
	store := oneShiftStore()
	store.allocated["rota-1"] = true
	_, err := AddPin(context.Background(), store, pinVolunteers(), testCfg,
		AddPinParams{ShiftID: "shift-1", VolunteerID: "bob", RoleID: "role-service-volunteer"}, zap.NewNop())
	assert.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "already allocated")
	assert.Empty(t, store.inserted)
}

func TestDeletePin_HappyPath(t *testing.T) {
	store := oneShiftStore()
	store.pins = []db.Pin{
		{ID: "p1", ShiftID: "shift-1", RoleID: "role-service-volunteer", VolunteerID: "bob"},
	}
	err := DeletePin(context.Background(), store, "p1", zap.NewNop())
	require.NoError(t, err)
	assert.Equal(t, []string{"p1"}, store.deletedIDs)
	assert.Equal(t, [][]string{{"rota-1"}}, store.lockedRotaIDs)
}

func TestDeletePin_NotFound(t *testing.T) {
	store := oneShiftStore()
	err := DeletePin(context.Background(), store, "missing", zap.NewNop())
	assert.ErrorIs(t, err, ErrNotFound)
	assert.Empty(t, store.lockedRotaIDs, "unknown id resolved before any lock")
}

func TestDeletePin_AlreadyAllocated(t *testing.T) {
	store := oneShiftStore()
	store.allocated["rota-1"] = true
	store.pins = []db.Pin{
		{ID: "p1", ShiftID: "shift-1", RoleID: "role-service-volunteer", VolunteerID: "bob"},
	}
	err := DeletePin(context.Background(), store, "p1", zap.NewNop())
	assert.ErrorIs(t, err, ErrConflict)
	assert.Empty(t, store.deletedIDs)
}

func TestListPins(t *testing.T) {
	store := &mockPinStore{
		shiftRanges: []db.ShiftInRange{
			{Shift: db.Shift{ID: "shift-1", Date: "2026-08-02", RotaID: "rota-1"}},
			{Shift: db.Shift{ID: "shift-2", Date: "2026-08-09", RotaID: "rota-1"}},
		},
		pins: []db.Pin{
			{ID: "p1", ShiftID: "shift-1", RoleID: "role-team-lead", VolunteerID: "alice"},
			{ID: "p2", ShiftID: "shift-2", RoleID: "role-service-volunteer", CustomValue: "External"},
		},
	}
	views, err := ListPins(context.Background(), store, pinVolunteers(), testCfg, ListPinsParams{}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, views, 2)

	byID := map[string]PinView{}
	for _, v := range views {
		byID[v.ID] = v
	}
	assert.Equal(t, "shift-1", byID["p1"].ShiftID)
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
func TestListPins_ReadsARenamedRoleUnderItsNewName(t *testing.T) {
	store := &mockPinStore{
		roles: []db.Role{
			{ID: "role-team-lead", Name: "Shift lead", Priority: 1, Colour: "violet"},
			{ID: "role-service-volunteer", Name: "Service volunteer", Priority: 2, Colour: "teal"},
		},
		shiftRanges: []db.ShiftInRange{
			{Shift: db.Shift{ID: "shift-1", Date: "2026-08-02", RotaID: "rota-1"}},
		},
		pins: []db.Pin{
			{ID: "p1", ShiftID: "shift-1", RoleID: "role-team-lead", VolunteerID: "alice"},
		},
	}
	views, err := ListPins(context.Background(), store, pinVolunteers(), testCfg, ListPinsParams{}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, views, 1)
	assert.Equal(t, "role-team-lead", views[0].RoleID, "the reference is untouched")
	assert.Equal(t, "Shift lead", views[0].Role)
}

func TestListPins_BoundsFilterShifts(t *testing.T) {
	store := &mockPinStore{
		shiftRanges: []db.ShiftInRange{
			{Shift: db.Shift{ID: "shift-1", Date: "2026-08-02", RotaID: "rota-1"}},
			{Shift: db.Shift{ID: "shift-2", Date: "2026-08-09", RotaID: "rota-1"}},
		},
		pins: []db.Pin{
			{ID: "p1", ShiftID: "shift-1", RoleID: "role-service-volunteer", VolunteerID: "alice"},
			{ID: "p2", ShiftID: "shift-2", RoleID: "role-service-volunteer", VolunteerID: "bob"},
		},
	}
	views, err := ListPins(context.Background(), store, pinVolunteers(), testCfg,
		ListPinsParams{From: "2026-08-05", To: "2026-08-12"}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, views, 1)
	assert.Equal(t, "p2", views[0].ID)
}

// twoSundayStore is the listing fixture: two consecutive Sundays in one rota,
// no pins.
func twoSundayStore() *mockPinStore {
	return &mockPinStore{
		shiftRanges: []db.ShiftInRange{
			{Shift: db.Shift{ID: "shift-1", Date: "2026-08-02", RotaID: "rota-1"}},
			{Shift: db.Shift{ID: "shift-2", Date: "2026-08-09", RotaID: "rota-1"}},
		},
	}
}

func listPins(t *testing.T, store *mockPinStore) []PinView {
	t.Helper()
	views, err := ListPins(context.Background(), store, pinVolunteers(), testCfg, ListPinsParams{}, zap.NewNop())
	require.NoError(t, err)
	return views
}

// A closed shift carries no pins into allocation (InitShifts clears them), so
// it must not show any either.
func TestListPins_SkipsClosedShifts(t *testing.T) {
	store := twoSundayStore()
	store.shiftRanges[0].Closed = true
	store.pins = []db.Pin{
		{ID: "p1", ShiftID: "shift-1", RoleID: "role-service-volunteer", VolunteerID: "dan"},
	}

	assert.Empty(t, listPins(t, store))
}

// A pin naming someone the roster no longer knows still has to be visible — it
// is the reason allocation will fail, and hiding it hides the fix. It is the
// pins nobody typed recently that reach this state: a Standing Preallocation
// seeds one at definition and the person can have left by allocation.
func TestListPins_UnknownVolunteerShowsID(t *testing.T) {
	store := twoSundayStore()
	store.pins = []db.Pin{
		{ID: "p1", ShiftID: "shift-1", RoleID: "role-service-volunteer", VolunteerID: "ghost"},
	}

	views := listPins(t, store)
	require.Len(t, views, 1)
	assert.Equal(t, "ghost", views[0].VolunteerID)
	assert.Equal(t, "ghost", views[0].Name)
}

// Ordered by date and then team lead first — the order a shift is read in, not
// the order the rows came back in.
func TestListPins_OrderedByDateThenRole(t *testing.T) {
	store := twoSundayStore()
	store.pins = []db.Pin{
		{ID: "p1", ShiftID: "shift-2", RoleID: "role-service-volunteer", VolunteerID: "bob"},
		{ID: "p2", ShiftID: "shift-1", RoleID: "role-service-volunteer", CustomValue: "Aardvark Group"},
		{ID: "p3", ShiftID: "shift-1", RoleID: "role-team-lead", VolunteerID: "dan"},
		{ID: "p4", ShiftID: "shift-2", RoleID: "role-team-lead", VolunteerID: "alice"},
	}

	views := listPins(t, store)
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
