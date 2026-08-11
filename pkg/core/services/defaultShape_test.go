package services

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// The Roles a shape test is stated against: one capped lead, one uncapped
// ordinary Role, matching the pair the app ships with.
var (
	leadRoleID    = "11111111-1111-1111-1111-111111111111"
	ordinaryRole  = "22222222-2222-2222-2222-222222222222"
	shapeTestMax1 = 1
)

func shapeRoles() []db.Role {
	return []db.Role{
		{ID: leadRoleID, Name: "Team lead", Priority: 1, Colour: "violet"},
		{ID: ordinaryRole, Name: "Service volunteer", Priority: 2, Colour: "teal"},
	}
}

// A store holding the Roles and the Shape in memory, so a test can assert on
// what reached the database as well as on what came back.
type stubDefaultShapeStore struct {
	roles    []db.Role
	shape    []db.DefaultShapeSeat
	rolesErr error
	readErr  error
	writeErr error
	saved    [][]db.DefaultShapeSeat
}

func (s *stubDefaultShapeStore) ListRoles(context.Context) ([]db.Role, error) {
	if s.rolesErr != nil {
		return nil, s.rolesErr
	}
	return s.roles, nil
}

func (s *stubDefaultShapeStore) GetDefaultShape(context.Context) ([]db.DefaultShapeSeat, error) {
	if s.readErr != nil {
		return nil, s.readErr
	}
	return s.shape, nil
}

func (s *stubDefaultShapeStore) SaveDefaultShape(_ context.Context, shape []db.DefaultShapeSeat) error {
	if s.writeErr != nil {
		return s.writeErr
	}
	s.saved = append(s.saved, shape)
	s.shape = shape
	return nil
}

// A deployment nobody has configured asks for nothing. That is where every
// deployment starts, and only allocation refuses over it.
func TestDefaultShapeUnset(t *testing.T) {
	store := &stubDefaultShapeStore{roles: shapeRoles()}

	shape, err := DefaultShape(context.Background(), store)
	require.NoError(t, err)
	assert.Empty(t, shape)
}

// A Seat carries the whole Role, so a screen can colour it and the solver can
// see its ceiling without either going back to the Roles table.
func TestDefaultShapeResolvesRoles(t *testing.T) {
	store := &stubDefaultShapeStore{
		roles: shapeRoles(),
		shape: []db.DefaultShapeSeat{
			{RoleID: leadRoleID, Seats: 1},
			{RoleID: ordinaryRole, Seats: 4},
		},
	}

	shape, err := DefaultShape(context.Background(), store)
	require.NoError(t, err)
	require.Len(t, shape, 2)
	assert.Equal(t, "Team lead", shape[0].Role.Name)
	assert.Equal(t, 1, shape[0].Count)
	assert.Equal(t, "Service volunteer", shape[1].Role.Name)
	assert.Equal(t, 4, shape[1].Count)
	assert.Equal(t, "violet", shape[0].Role.Colour)
}

func TestSaveDefaultShape(t *testing.T) {
	store := &stubDefaultShapeStore{roles: shapeRoles()}

	shape, err := SaveDefaultShape(context.Background(), store, []SeatParams{
		{RoleID: leadRoleID, Count: 1},
		{RoleID: ordinaryRole, Count: 4},
	}, zap.NewNop())
	require.NoError(t, err)

	require.Len(t, store.saved, 1)
	assert.Equal(t, []db.DefaultShapeSeat{
		{RoleID: leadRoleID, Seats: 1},
		{RoleID: ordinaryRole, Seats: 4},
	}, store.saved[0])
	require.Len(t, shape, 2)
	assert.Equal(t, "Team lead", shape[0].Role.Name)
}

// The Seats come back in the order they are filled, whatever order they were
// stated in, so the screen that saved them reads the same list back.
func TestSaveDefaultShapeOrdersByPriority(t *testing.T) {
	store := &stubDefaultShapeStore{roles: shapeRoles()}

	shape, err := SaveDefaultShape(context.Background(), store, []SeatParams{
		{RoleID: ordinaryRole, Count: 4},
		{RoleID: leadRoleID, Count: 1},
	}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, shape, 2)
	assert.Equal(t, "Team lead", shape[0].Role.Name)
	assert.Equal(t, "Service volunteer", shape[1].Role.Name)
}

// Saving nothing is how a Shape is emptied. It is allowed, and what it costs —
// allocation — is said where allocation is refused rather than here.
func TestSaveDefaultShapeAcceptsNothing(t *testing.T) {
	store := &stubDefaultShapeStore{roles: shapeRoles(), shape: []db.DefaultShapeSeat{{RoleID: leadRoleID, Seats: 1}}}

	shape, err := SaveDefaultShape(context.Background(), store, nil, zap.NewNop())
	require.NoError(t, err)
	assert.Empty(t, shape)
	require.Len(t, store.saved, 1)
	assert.Empty(t, store.saved[0])
}

// The Shape is the only thing that says how many of a Role a Shift asks for, so
// there is no ceiling above it to bump into: an admin who wants two Team leads
// on a shift says so here (issue #185).
func TestSaveDefaultShapeAllowsAnyCountOfAnyRole(t *testing.T) {
	store := &stubDefaultShapeStore{roles: shapeRoles()}

	shape, err := SaveDefaultShape(context.Background(), store, []SeatParams{
		{RoleID: leadRoleID, Count: 2},
		{RoleID: ordinaryRole, Count: 40},
	}, zap.NewNop())
	require.NoError(t, err)
	require.Len(t, shape, 2)
	assert.Equal(t, 2, shape[0].Count)
	assert.Equal(t, 40, shape[1].Count)
}

func TestSaveDefaultShapeRefusesBadInput(t *testing.T) {
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
			store := &stubDefaultShapeStore{roles: shapeRoles()}

			_, err := SaveDefaultShape(context.Background(), store, seats, zap.NewNop())
			require.ErrorIs(t, err, ErrInvalidInput)
			assert.Empty(t, store.saved)
		})
	}
}

// A write the database refuses is a failure rather than an admin's mistake, and
// stays one all the way out — the API answers 500, not 400.
func TestSaveDefaultShapeSurfacesAWriteFailure(t *testing.T) {
	store := &stubDefaultShapeStore{roles: shapeRoles(), writeErr: errors.New("connection refused")}

	_, err := SaveDefaultShape(context.Background(), store, []SeatParams{
		{RoleID: leadRoleID, Count: 1},
	}, zap.NewNop())
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrInvalidInput)
}

// The Seats the solver is given are the ones the settings state, named the way
// the roster and the pins name a Role.
func TestShapeSeatsForAllocator(t *testing.T) {
	shape := model.Shape{
		{Role: model.Role{Name: "Team lead"}, Count: 1},
		{Role: model.Role{Name: "Service volunteer"}, Count: 4},
	}

	seats := convertShape(shape)
	require.Len(t, seats, 2)
	assert.Equal(t, "Team lead", seats[0].Role)
	assert.Equal(t, 1, seats[0].Count)
	assert.Equal(t, "Service volunteer", seats[1].Role)
	assert.Equal(t, 4, seats[1].Count)
}
