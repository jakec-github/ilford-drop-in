package services

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// A store that records what it was asked to write, so a test can assert on the
// Role that reached the database rather than only on what came back.
type stubRoleWriteStore struct {
	stubRoleStore
	inserted []db.Role
	updated  []db.Role
	// missing makes UpdateRole report that no row matched, which is how the
	// database says an id names nothing.
	missing  bool
	writeErr error
}

func (s *stubRoleWriteStore) InsertRole(_ context.Context, role db.Role) error {
	if s.writeErr != nil {
		return s.writeErr
	}
	s.inserted = append(s.inserted, role)
	return nil
}

func (s *stubRoleWriteStore) UpdateRole(_ context.Context, role db.Role) (bool, error) {
	if s.writeErr != nil {
		return false, s.writeErr
	}
	s.updated = append(s.updated, role)
	return !s.missing, nil
}

func roleParams() RoleParams {
	return RoleParams{Name: "Food collector", Max: intPtr(2), Priority: 3, Colour: model.ColourAmber}
}

// Creating a Role mints its identity here rather than taking one from the
// caller: the id is what every later reference is written against, so it is the
// app's to issue.
func TestCreateRole(t *testing.T) {
	store := &stubRoleWriteStore{}

	role, err := CreateRole(context.Background(), store, roleParams(), zap.NewNop())
	require.NoError(t, err)

	require.Len(t, store.inserted, 1)
	written := store.inserted[0]
	assert.NotEmpty(t, written.ID)
	_, parseErr := uuid.Parse(written.ID)
	assert.NoError(t, parseErr, "the id is a UUID, which is what the column holds")
	assert.Equal(t, "Food collector", written.Name)
	require.NotNil(t, written.Max)
	assert.Equal(t, 2, *written.Max)
	assert.Equal(t, 3, written.Priority)
	assert.Equal(t, model.ColourAmber, written.Colour)

	assert.Equal(t, written.ID, role.ID, "the caller is told the id it now has to address")
	assert.Equal(t, "Food collector", role.Name)
}

// Surrounding space in a name is invisible on screen and fatal in the roster,
// where a held Role is matched against this string exactly. Trimming at the
// only place names are written keeps every reader from having to.
func TestCreateRoleTrimsTheName(t *testing.T) {
	store := &stubRoleWriteStore{}
	params := roleParams()
	params.Name = "  Food collector \n"

	role, err := CreateRole(context.Background(), store, params, zap.NewNop())
	require.NoError(t, err)

	assert.Equal(t, "Food collector", store.inserted[0].Name)
	assert.Equal(t, "Food collector", role.Name)
}

// A nameless Role could not be held: the roster names Roles by string, so a
// blank name is one nobody can ever spell.
func TestCreateRoleRejectsABlankName(t *testing.T) {
	store := &stubRoleWriteStore{}
	params := roleParams()
	params.Name = "   "

	_, err := CreateRole(context.Background(), store, params, zap.NewNop())
	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Empty(t, store.inserted)
}

// A Role with no ceiling is the ordinary case — it is the Role a Shift's size
// is spent on — so an absent max is not a missing field.
func TestCreateRoleWithoutACeiling(t *testing.T) {
	store := &stubRoleWriteStore{}
	params := roleParams()
	params.Max = nil

	role, err := CreateRole(context.Background(), store, params, zap.NewNop())
	require.NoError(t, err)
	assert.Nil(t, store.inserted[0].Max)
	assert.Nil(t, role.Max)
}

// A ceiling of zero is a Role no Shift may ever hold, which is a Role that does
// nothing; the database refuses it too, and being told why here is better than
// a constraint violation.
func TestCreateRoleRejectsACeilingBelowOne(t *testing.T) {
	store := &stubRoleWriteStore{}
	params := roleParams()
	params.Max = intPtr(0)

	_, err := CreateRole(context.Background(), store, params, zap.NewNop())
	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Empty(t, store.inserted)
}

// Colours are palette tokens, not colour values, and the closed set is what
// keeps contrast in both themes a decision this repo made. A token this build
// has no rule for would render as nothing at all.
func TestCreateRoleRejectsAColourOutsideThePalette(t *testing.T) {
	store := &stubRoleWriteStore{}
	params := roleParams()
	params.Colour = "#ff0000"

	_, err := CreateRole(context.Background(), store, params, zap.NewNop())
	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Empty(t, store.inserted)
}

// Not choosing a colour is allowed and means the default, so a caller with no
// picker — a script, an early client — still writes a Role that renders.
func TestCreateRoleDefaultsAnUnsetColour(t *testing.T) {
	store := &stubRoleWriteStore{}
	params := roleParams()
	params.Colour = ""

	role, err := CreateRole(context.Background(), store, params, zap.NewNop())
	require.NoError(t, err)
	assert.Equal(t, model.DefaultRoleColour, store.inserted[0].Colour)
	assert.Equal(t, model.DefaultRoleColour, role.Colour)
}

// The name is how the roster spells a Role, so two Roles cannot share one. The
// refusal is a conflict rather than a bad request: the input is well formed,
// the world just already contains it.
func TestCreateRoleReportsADuplicateName(t *testing.T) {
	store := &stubRoleWriteStore{writeErr: db.ErrDuplicateRoleName}

	_, err := CreateRole(context.Background(), store, roleParams(), zap.NewNop())
	require.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "Food collector", "the message names what clashed")
}

// Anything else the database says is nobody's mistake, and must not be dressed
// up as one.
func TestCreateRoleReportsAWriteFailure(t *testing.T) {
	store := &stubRoleWriteStore{writeErr: errors.New("connection refused")}

	_, err := CreateRole(context.Background(), store, roleParams(), zap.NewNop())
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrInvalidInput)
	assert.NotErrorIs(t, err, ErrConflict)
}

// An edit moves every editable field and never the id: allocations, alterations
// and pins were written against that id, and a rename must leave them intact.
func TestUpdateRole(t *testing.T) {
	store := &stubRoleWriteStore{}
	id := uuid.New().String()

	role, err := UpdateRole(context.Background(), store, id, RoleParams{
		Name: "Shift lead", Max: intPtr(1), Priority: 1, Colour: model.ColourViolet,
	}, zap.NewNop())
	require.NoError(t, err)

	require.Len(t, store.updated, 1)
	assert.Equal(t, db.Role{
		ID: id, Name: "Shift lead", Max: intPtr(1), Priority: 1, Colour: model.ColourViolet,
	}, store.updated[0])
	assert.Equal(t, id, role.ID)
}

// Editing validates exactly as creating does — a Role must not be able to reach
// a state through an edit that it could not have been created in.
func TestUpdateRoleValidatesTheSameWayAsCreate(t *testing.T) {
	store := &stubRoleWriteStore{}

	for _, tc := range []struct {
		name   string
		params RoleParams
	}{
		{"blank name", RoleParams{Name: " ", Colour: model.ColourTeal}},
		{"ceiling below one", RoleParams{Name: "Team lead", Max: intPtr(0), Colour: model.ColourTeal}},
		{"colour outside the palette", RoleParams{Name: "Team lead", Colour: "puce"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := UpdateRole(context.Background(), store, uuid.New().String(), tc.params, zap.NewNop())
			assert.ErrorIs(t, err, ErrInvalidInput)
		})
	}
	assert.Empty(t, store.updated)
}

// Roles are never deleted, so an id nothing matches is a wrong id rather than a
// race with a removal — and it is the caller's mistake, reported as a miss.
func TestUpdateRoleReportsAnUnknownID(t *testing.T) {
	store := &stubRoleWriteStore{missing: true}

	_, err := UpdateRole(context.Background(), store, uuid.New().String(), roleParams(), zap.NewNop())
	assert.ErrorIs(t, err, ErrNotFound)
}

// An id that is not a UUID cannot name a Role, and reaching the database with
// it would turn a caller's typo into a driver error and a 500.
func TestUpdateRoleRejectsAnIDThatIsNotAUUID(t *testing.T) {
	store := &stubRoleWriteStore{}

	_, err := UpdateRole(context.Background(), store, "Team lead", roleParams(), zap.NewNop())
	require.ErrorIs(t, err, ErrNotFound)
	assert.Empty(t, store.updated)
}

// Renaming onto a name another Role holds is the same clash a create meets.
func TestUpdateRoleReportsADuplicateName(t *testing.T) {
	store := &stubRoleWriteStore{writeErr: db.ErrDuplicateRoleName}

	_, err := UpdateRole(context.Background(), store, uuid.New().String(), roleParams(), zap.NewNop())
	assert.ErrorIs(t, err, ErrConflict)
}
