package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// Sundays a seeding test can point an rrule at. 2026-08-02 is the first Sunday
// of August 2026, so a MONTHLY;BYDAY=1SU rule lands on it and on nothing else in
// the run below.
var seedShifts = []db.Shift{
	{ID: "shift-1", RotaID: "rota-1", Date: "2026-08-02"},
	{ID: "shift-2", RotaID: "rota-1", Date: "2026-08-09"},
	{ID: "shift-3", RotaID: "rota-1", Date: "2026-08-16"},
	{ID: "shift-4", RotaID: "rota-1", Date: "2026-08-23"},
}

func TestSeedPreallocations_MatchingShiftsOnly(t *testing.T) {
	standing := []db.StandingPreallocation{{
		ID:          "standing-1",
		RRule:       "FREQ=MONTHLY;BYDAY=1SU",
		RoleID:      "role-service-volunteer",
		CustomValue: "St John's team",
	}}

	seeded, err := seedPreallocations(standing, seedShifts, testRoles, zap.NewNop())
	require.NoError(t, err)

	require.Len(t, seeded, 1, "the rule matches only the first Sunday")
	assert.Equal(t, "shift-1", seeded[0].ShiftID)
	assert.Equal(t, "St John's team", seeded[0].CustomValue)
	assert.Equal(t, "role-service-volunteer", seeded[0].RoleID, "the seeded pin references the Role the promise named")
	assert.NotEmpty(t, seeded[0].ID, "a seeded Preallocation is an ordinary row with its own identity")
}

func TestSeedPreallocations_EveryMatchingShift(t *testing.T) {
	standing := []db.StandingPreallocation{{
		ID:          "standing-1",
		RRule:       "FREQ=WEEKLY;BYDAY=SU",
		RoleID:      "role-team-lead",
		VolunteerID: "vol-1",
	}}

	seeded, err := seedPreallocations(standing, seedShifts, testRoles, zap.NewNop())
	require.NoError(t, err)

	require.Len(t, seeded, 4)
	for i, p := range seeded {
		assert.Equal(t, seedShifts[i].ID, p.ShiftID)
		assert.Equal(t, "vol-1", p.VolunteerID)
		assert.Equal(t, "role-team-lead", p.RoleID)
	}
}

// Two rules can name the same person on the same date, and a person fills at
// most one Seat on a Shift — so what reaches the rota is one pin, in the Role
// whose Seats are filled first.
func TestSeedPreallocations_CollapsesOverlappingRules(t *testing.T) {
	standing := []db.StandingPreallocation{
		{ID: "standing-1", RRule: "FREQ=WEEKLY;BYDAY=SU", RoleID: "role-service-volunteer", VolunteerID: "vol-1"},
		{ID: "standing-2", RRule: "FREQ=MONTHLY;BYDAY=1SU", RoleID: "role-team-lead", VolunteerID: "vol-1"},
	}

	seeded, err := seedPreallocations(standing, seedShifts, testRoles, zap.NewNop())
	require.NoError(t, err)

	require.Len(t, seeded, 4, "one pin per shift, not five")
	byShift := make(map[string]db.Preallocation, len(seeded))
	for _, p := range seeded {
		byShift[p.ShiftID] = p
	}
	assert.Equal(t, "role-team-lead", byShift["shift-1"].RoleID, "the higher-priority Role wins where both apply")
	assert.Equal(t, "role-service-volunteer", byShift["shift-2"].RoleID)
}

// A rule nobody can parse would silently drop a promise an admin has made, so
// definition refuses rather than minting a rota missing its pins.
func TestSeedPreallocations_UnparseableRuleFails(t *testing.T) {
	standing := []db.StandingPreallocation{{
		ID:          "standing-1",
		RRule:       "NOT AN RRULE",
		RoleID:      "role-team-lead",
		VolunteerID: "vol-1",
	}}

	_, err := seedPreallocations(standing, seedShifts, testRoles, zap.NewNop())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "standing-1")
}

func TestSeedPreallocations_NoneIsNotAnError(t *testing.T) {
	seeded, err := seedPreallocations(nil, seedShifts, testRoles, zap.NewNop())
	require.NoError(t, err)
	assert.Empty(t, seeded)
}

// mockStandingStore implements StandingPreallocationStore over slices.
type mockStandingStore struct {
	testRoleStore

	standing  []db.StandingPreallocation
	insertErr error

	inserted   []db.StandingPreallocation
	deletedIDs []string
}

func (m *mockStandingStore) GetStandingPreallocations(context.Context) ([]db.StandingPreallocation, error) {
	return m.standing, nil
}

func (m *mockStandingStore) InsertStandingPreallocation(_ context.Context, s db.StandingPreallocation) error {
	if m.insertErr != nil {
		return m.insertErr
	}
	m.inserted = append(m.inserted, s)
	m.standing = append(m.standing, s)
	return nil
}

func (m *mockStandingStore) DeleteStandingPreallocationByID(_ context.Context, id string) (bool, error) {
	for i := range m.standing {
		if m.standing[i].ID == id {
			m.standing = append(m.standing[:i], m.standing[i+1:]...)
			m.deletedIDs = append(m.deletedIDs, id)
			return true, nil
		}
	}
	return false, nil
}

func addStanding(t *testing.T, store *mockStandingStore, params AddStandingPreallocationParams) (*StandingPreallocationView, error) {
	t.Helper()
	return AddStandingPreallocation(context.Background(), store, preallocVolunteers(), testCfg, params, zap.NewNop())
}

func TestAddStandingPreallocation_VolunteerHappyPath(t *testing.T) {
	store := &mockStandingStore{}
	view, err := addStanding(t, store, AddStandingPreallocationParams{
		RRule: "FREQ=MONTHLY;BYDAY=1SU", RoleID: "role-team-lead", VolunteerID: "alice",
	})
	require.NoError(t, err)

	require.Len(t, store.inserted, 1)
	assert.Equal(t, "role-team-lead", store.inserted[0].RoleID)
	assert.Equal(t, "alice", store.inserted[0].VolunteerID)
	assert.Equal(t, "Team lead", view.Role, "the view names the Role an admin recognises")
	assert.Equal(t, "Alice", view.Name)
}

func TestAddStandingPreallocation_CustomHappyPath(t *testing.T) {
	store := &mockStandingStore{}
	view, err := addStanding(t, store, AddStandingPreallocationParams{
		RRule: "FREQ=MONTHLY;BYDAY=1SU", RoleID: "role-service-volunteer", Custom: "St John's team",
	})
	require.NoError(t, err)
	assert.Equal(t, "St John's team", view.Custom)
	assert.Equal(t, "St John's team", view.Name, "a custom entry is its own name")
}

func TestAddStandingPreallocation_Refusals(t *testing.T) {
	tests := []struct {
		name    string
		params  AddStandingPreallocationParams
		wantErr string
	}{
		{
			name:    "neither volunteer nor custom",
			params:  AddStandingPreallocationParams{RRule: "FREQ=WEEKLY;BYDAY=SU", RoleID: "role-team-lead"},
			wantErr: "exactly one of",
		},
		{
			name:    "both volunteer and custom",
			params:  AddStandingPreallocationParams{RRule: "FREQ=WEEKLY;BYDAY=SU", RoleID: "role-team-lead", VolunteerID: "alice", Custom: "Scouts"},
			wantErr: "exactly one of",
		},
		{
			name:    "no rule",
			params:  AddStandingPreallocationParams{RoleID: "role-team-lead", VolunteerID: "alice"},
			wantErr: "which shifts it applies to",
		},
		{
			name:    "unparseable rule",
			params:  AddStandingPreallocationParams{RRule: "EVERY OTHER TUESDAY", RoleID: "role-team-lead", VolunteerID: "alice"},
			wantErr: "not a recurrence rule",
		},
		{
			name:    "unknown role",
			params:  AddStandingPreallocationParams{RRule: "FREQ=WEEKLY;BYDAY=SU", RoleID: "role-imaginary", VolunteerID: "alice"},
			wantErr: "not a known role",
		},
		{
			name:    "volunteer does not hold the role",
			params:  AddStandingPreallocationParams{RRule: "FREQ=WEEKLY;BYDAY=SU", RoleID: "role-team-lead", VolunteerID: "bob"},
			wantErr: "does not hold the role",
		},
		{
			name:    "inactive volunteer",
			params:  AddStandingPreallocationParams{RRule: "FREQ=WEEKLY;BYDAY=SU", RoleID: "role-service-volunteer", VolunteerID: "carol"},
			wantErr: "is not active",
		},
		{
			name:    "unknown volunteer",
			params:  AddStandingPreallocationParams{RRule: "FREQ=WEEKLY;BYDAY=SU", RoleID: "role-service-volunteer", VolunteerID: "ghost"},
			wantErr: "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &mockStandingStore{}
			_, err := addStanding(t, store, tt.params)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.Empty(t, store.inserted)
		})
	}
}

// The same promise made twice is a slip, and the message says so in the admin's
// own terms rather than in the database's.
func TestAddStandingPreallocation_Duplicate(t *testing.T) {
	store := &mockStandingStore{insertErr: db.ErrDuplicateStandingPreallocation}
	_, err := addStanding(t, store, AddStandingPreallocationParams{
		RRule: "FREQ=WEEKLY;BYDAY=SU", RoleID: "role-team-lead", VolunteerID: "alice",
	})
	assert.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "Alice is already pinned")
}

// Listed in the order Seats are filled: the lead first, then by name.
func TestListStandingPreallocations_Order(t *testing.T) {
	store := &mockStandingStore{standing: []db.StandingPreallocation{
		{ID: "s1", RRule: "FREQ=WEEKLY;BYDAY=SU", RoleID: "role-service-volunteer", VolunteerID: "bob"},
		{ID: "s2", RRule: "FREQ=WEEKLY;BYDAY=SU", RoleID: "role-service-volunteer", CustomValue: "Aardvark Group"},
		{ID: "s3", RRule: "FREQ=MONTHLY;BYDAY=1SU", RoleID: "role-team-lead", VolunteerID: "alice"},
	}}

	views, err := ListStandingPreallocations(context.Background(), store, preallocVolunteers(), testCfg, zap.NewNop())
	require.NoError(t, err)

	names := make([]string, 0, len(views))
	for _, v := range views {
		names = append(names, v.Name)
	}
	assert.Equal(t, []string{"Alice", "Aardvark Group", "Bob"}, names)
	assert.Equal(t, "Team lead", views[0].Role)
	assert.Equal(t, "role-team-lead", views[0].RoleID, "the id travels with the name, since it is what an edit names")
}

func TestDeleteStandingPreallocation(t *testing.T) {
	store := &mockStandingStore{standing: []db.StandingPreallocation{
		{ID: "s1", RRule: "FREQ=WEEKLY;BYDAY=SU", RoleID: "role-team-lead", VolunteerID: "alice"},
	}}

	require.NoError(t, DeleteStandingPreallocation(context.Background(), store, "s1", zap.NewNop()))
	assert.Equal(t, []string{"s1"}, store.deletedIDs)

	err := DeleteStandingPreallocation(context.Background(), store, "s1", zap.NewNop())
	assert.ErrorIs(t, err, ErrNotFound)
}
