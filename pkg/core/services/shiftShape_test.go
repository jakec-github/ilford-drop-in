package services

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// stubShiftShapeStore holds one rota's Shifts, their Shapes and their pins in
// memory, so a test can assert on what reached the database as well as on what
// came back. The lock is a pass-through: what it serialises is a concurrency
// property, and the checks inside it are what these tests are about.
type stubShiftShapeStore struct {
	roles    []db.Role
	shift    *db.ShiftInRange
	shapes   map[string][]db.ShiftRequirement
	pins     []db.Preallocation
	saved    [][]db.ShiftRequirement
	missing  bool // SetShiftShape reports no such Shift, as a lost race would
	writeErr error
}

func (s *stubShiftShapeStore) ListRoles(context.Context) ([]db.Role, error) {
	return s.roles, nil
}

func (s *stubShiftShapeStore) GetShiftShapes(_ context.Context, shiftIDs []string) (map[string][]db.ShiftRequirement, error) {
	shapes := make(map[string][]db.ShiftRequirement, len(shiftIDs))
	for _, id := range shiftIDs {
		if seats, ok := s.shapes[id]; ok {
			shapes[id] = seats
		}
	}
	return shapes, nil
}

func (s *stubShiftShapeStore) GetShiftByID(_ context.Context, id string) (*db.ShiftInRange, error) {
	if s.shift == nil || s.shift.ID != id {
		return nil, nil
	}
	return s.shift, nil
}

func (s *stubShiftShapeStore) WithRotaShapeLock(_ context.Context, _ []string, fn func(db.ShapeTxStore) error) error {
	return fn(s)
}

func (s *stubShiftShapeStore) RotaAllocated(_ context.Context, _ string) (bool, error) {
	return s.shift != nil && s.shift.Allocated, nil
}

func (s *stubShiftShapeStore) GetPreallocationsByShiftIDs(_ context.Context, shiftIDs []string) ([]db.Preallocation, error) {
	var pins []db.Preallocation
	for _, pin := range s.pins {
		for _, id := range shiftIDs {
			if pin.ShiftID == id {
				pins = append(pins, pin)
			}
		}
	}
	return pins, nil
}

func (s *stubShiftShapeStore) SetShiftShape(_ context.Context, shiftID string, seats []db.ShiftRequirement) (bool, error) {
	if s.writeErr != nil {
		return false, s.writeErr
	}
	if s.missing {
		return false, nil
	}
	s.saved = append(s.saved, seats)
	if s.shapes == nil {
		s.shapes = map[string][]db.ShiftRequirement{}
	}
	s.shapes[shiftID] = seats
	return true, nil
}

// An unallocated Shift asking for one lead and four ordinary volunteers, which
// is the state most of these tests start from.
func shapeEditStore() *stubShiftShapeStore {
	return &stubShiftShapeStore{
		roles: shapeRoles(),
		shift: &db.ShiftInRange{Shift: db.Shift{
			ID:     "shift-1",
			RotaID: "rota-1",
			Date:   "2026-02-01",
		}},
		shapes: map[string][]db.ShiftRequirement{
			"shift-1": {
				{ShiftID: "shift-1", RoleID: leadRoleID, Seats: 1},
				{ShiftID: "shift-1", RoleID: ordinaryRole, Seats: 4},
			},
		},
	}
}

func TestSaveShiftShape(t *testing.T) {
	store := shapeEditStore()

	shape, err := SaveShiftShape(context.Background(), store, "shift-1", []SeatParams{
		{RoleID: leadRoleID, Count: 1},
		{RoleID: ordinaryRole, Count: 6},
	}, zap.NewNop())
	require.NoError(t, err)

	require.Len(t, store.saved, 1)
	assert.Equal(t, []db.ShiftRequirement{
		{ShiftID: "shift-1", RoleID: leadRoleID, Seats: 1},
		{ShiftID: "shift-1", RoleID: ordinaryRole, Seats: 6},
	}, store.saved[0])
	require.Len(t, shape, 2)
	assert.Equal(t, "Team lead", shape[0].Role.Name)
	assert.Equal(t, 6, shape[1].Count)
}

// Dropping a Role is how a Shift stops asking for one: there is no other way to
// say it, and the whole Shape goes at once so that leaving it out is the saying.
func TestSaveShiftShapeDropsARole(t *testing.T) {
	store := shapeEditStore()

	shape, err := SaveShiftShape(context.Background(), store, "shift-1", []SeatParams{
		{RoleID: ordinaryRole, Count: 4},
	}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, shape, 1)
	assert.Equal(t, "Service volunteer", shape[0].Role.Name)
	require.Len(t, store.saved, 1)
	assert.Len(t, store.saved[0], 1)
}

// The Seats come back in the order they are filled, whatever order they were
// stated in, so the screen that saved them reads the same list back.
func TestSaveShiftShapeOrdersByPriority(t *testing.T) {
	store := shapeEditStore()

	shape, err := SaveShiftShape(context.Background(), store, "shift-1", []SeatParams{
		{RoleID: ordinaryRole, Count: 4},
		{RoleID: leadRoleID, Count: 1},
	}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, shape, 2)
	assert.Equal(t, "Team lead", shape[0].Role.Name)
	assert.Equal(t, "Service volunteer", shape[1].Role.Name)
}

// One Shift's Shape is the only thing that says how many of a Role it asks for,
// so an evening that wants two Team leads says so (issue #185).
func TestSaveShiftShapeAllowsAnyCountOfAnyRole(t *testing.T) {
	store := shapeEditStore()

	shape, err := SaveShiftShape(context.Background(), store, "shift-1", []SeatParams{
		{RoleID: leadRoleID, Count: 2},
	}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, shape, 1)
	assert.Equal(t, 2, shape[0].Count)
}

func TestSaveShiftShapeRefusesBadInput(t *testing.T) {
	cases := map[string][]SeatParams{
		"unknown role": {{RoleID: "33333333-3333-3333-3333-333333333333", Count: 1}},
		"no role":      {{RoleID: "", Count: 1}},
		"no seats":     {{RoleID: leadRoleID, Count: 0}},
		"fewer than none": {
			{RoleID: ordinaryRole, Count: -1},
		},
		"the same role twice": {
			{RoleID: ordinaryRole, Count: 2},
			{RoleID: ordinaryRole, Count: 3},
		},
	}

	for name, seats := range cases {
		t.Run(name, func(t *testing.T) {
			store := shapeEditStore()

			_, err := SaveShiftShape(context.Background(), store, "shift-1", seats, zap.NewNop())
			require.ErrorIs(t, err, ErrInvalidInput)
			assert.Empty(t, store.saved)
		})
	}
}

// Emptying a Shift's Shape is allowed for the same reason emptying the default
// one is: it is a state the Shift can be in, and what it costs is said where
// allocation refuses over it rather than here.
func TestSaveShiftShapeAcceptsNothing(t *testing.T) {
	store := shapeEditStore()

	shape, err := SaveShiftShape(context.Background(), store, "shift-1", nil, zap.NewNop())
	require.NoError(t, err)
	assert.Empty(t, shape)
	require.Len(t, store.saved, 1)
	assert.Empty(t, store.saved[0])
}

func TestSaveShiftShapeUnknownShift(t *testing.T) {
	store := shapeEditStore()

	_, err := SaveShiftShape(context.Background(), store, "ghost", []SeatParams{
		{RoleID: ordinaryRole, Count: 4},
	}, zap.NewNop())
	require.ErrorIs(t, err, ErrNotFound)
	assert.Empty(t, store.saved)
}

// The Shape is an allocator input: the solver filled Seats against it, so once
// the Rotation is allocated it is what the rota was made from and cannot move.
func TestSaveShiftShapeRefusedOnAnAllocatedRota(t *testing.T) {
	store := shapeEditStore()
	store.shift.Allocated = true

	_, err := SaveShiftShape(context.Background(), store, "shift-1", []SeatParams{
		{RoleID: ordinaryRole, Count: 4},
	}, zap.NewNop())
	require.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "1 February 2026")
	assert.Empty(t, store.saved)
}

// A pin is a promise to do a named job on this Shift, and the solver rejects a
// pin naming a Role the Shift has no Seat for. So the Seat cannot be taken away
// underneath one: the refusal names the Role and says how many are promised it,
// and removing the pin is the way through.
func TestSaveShiftShapeRefusedWhenAPinWouldLoseItsSeat(t *testing.T) {
	store := shapeEditStore()
	store.pins = []db.Preallocation{
		{ID: "pin-1", ShiftID: "shift-1", Role: "Team lead", VolunteerID: "vol-1"},
	}

	_, err := SaveShiftShape(context.Background(), store, "shift-1", []SeatParams{
		{RoleID: ordinaryRole, Count: 4},
	}, zap.NewNop())
	require.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "Team lead")
	assert.Empty(t, store.saved)
}

// The same rule short of nought: two people promised a Role and one Seat left
// for them is a Shift the solver cannot fill either, so the count has to hold
// every pin rather than merely one.
func TestSaveShiftShapeRefusedWhenSeatsFallBelowThePins(t *testing.T) {
	store := shapeEditStore()
	store.pins = []db.Preallocation{
		{ID: "pin-1", ShiftID: "shift-1", Role: "Service volunteer", VolunteerID: "vol-1"},
		{ID: "pin-2", ShiftID: "shift-1", Role: "Service volunteer", CustomValue: "Redbridge youth group"},
	}

	_, err := SaveShiftShape(context.Background(), store, "shift-1", []SeatParams{
		{RoleID: ordinaryRole, Count: 1},
	}, zap.NewNop())
	require.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "2 people are pinned as Service volunteer")
	assert.Empty(t, store.saved)
}

// Seats enough for everyone promised them is an ordinary edit, pins or no pins.
func TestSaveShiftShapeAllowsSeatsForEveryPin(t *testing.T) {
	store := shapeEditStore()
	store.pins = []db.Preallocation{
		{ID: "pin-1", ShiftID: "shift-1", Role: "Service volunteer", VolunteerID: "vol-1"},
	}

	_, err := SaveShiftShape(context.Background(), store, "shift-1", []SeatParams{
		{RoleID: ordinaryRole, Count: 1},
	}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, store.saved, 1)
}

// Nobody works a day the drop-in is shut, and allocation strips a closed
// Shift's pins before the solver sees them — so a pin there promises nothing
// and cannot stand in the way of an edit.
func TestSaveShiftShapeIgnoresThePinsOfAClosedShift(t *testing.T) {
	store := shapeEditStore()
	store.shift.Closed = true
	store.pins = []db.Preallocation{
		{ID: "pin-1", ShiftID: "shift-1", Role: "Team lead", VolunteerID: "vol-1"},
	}

	_, err := SaveShiftShape(context.Background(), store, "shift-1", []SeatParams{
		{RoleID: ordinaryRole, Count: 4},
	}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, store.saved, 1)
}

// A pin naming a Role nobody offers any more still has to be honoured: it is
// the pin that will fail the solve, and quietly letting its Seat go would hide
// the one thing that explains why.
func TestSaveShiftShapeRefusesOverAPinNamingARetiredRole(t *testing.T) {
	store := shapeEditStore()
	store.pins = []db.Preallocation{
		{ID: "pin-1", ShiftID: "shift-1", Role: "Kitchen", VolunteerID: "vol-1"},
	}

	_, err := SaveShiftShape(context.Background(), store, "shift-1", []SeatParams{
		{RoleID: ordinaryRole, Count: 4},
	}, zap.NewNop())
	require.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "Kitchen")
}

// Losing the race with something that removed the Shift under the lock reads as
// the Shift being gone, not as a write that half happened.
func TestSaveShiftShapeShiftVanishesUnderTheLock(t *testing.T) {
	store := shapeEditStore()
	store.missing = true

	_, err := SaveShiftShape(context.Background(), store, "shift-1", []SeatParams{
		{RoleID: ordinaryRole, Count: 4},
	}, zap.NewNop())
	require.ErrorIs(t, err, ErrNotFound)
}

// A write the database refuses is a failure rather than an admin's mistake, and
// stays one all the way out — the API answers 500, not 400.
func TestSaveShiftShapeSurfacesAWriteFailure(t *testing.T) {
	store := shapeEditStore()
	store.writeErr = errors.New("connection refused")

	_, err := SaveShiftShape(context.Background(), store, "shift-1", []SeatParams{
		{RoleID: ordinaryRole, Count: 4},
	}, zap.NewNop())
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrInvalidInput)
	assert.NotErrorIs(t, err, ErrConflict)
}
