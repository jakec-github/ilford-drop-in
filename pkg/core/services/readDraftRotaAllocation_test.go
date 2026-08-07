package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// draftViewStore is the read half of a Draft Rota Allocation: the rota in
// flight, its Shifts and their Shapes, and the draft solved against them. It
// answers with what it was told — the derivations under test are the service's,
// not the database's.
type draftViewStore struct {
	inFlight *db.RotaInFlight
	shifts   []db.Shift
	// shapeByShiftID is what each Shift asks for, as Role id to Seat count. The
	// roles below give those ids meaning.
	shapeByShiftID map[string][]db.ShiftRequirement
	roles          []db.Role
	draft          *db.DraftRotaAllocation
	seats          []db.DraftAllocation

	inFlightErr error
	draftErr    error
	// seatsRead records the shift ids draft Seats were asked for, so a test can
	// tell "no draft, so nothing was read" from "read and came back empty".
	seatsRead [][]string
}

func (s *draftViewStore) GetRotaInFlight(context.Context) (*db.RotaInFlight, error) {
	return s.inFlight, s.inFlightErr
}

func (s *draftViewStore) GetShiftsByRotaID(_ context.Context, rotaID string) ([]db.Shift, error) {
	var out []db.Shift
	for _, shift := range s.shifts {
		if shift.RotaID == rotaID {
			out = append(out, shift)
		}
	}
	return out, nil
}

func (s *draftViewStore) GetShiftShapes(_ context.Context, shiftIDs []string) (map[string][]db.ShiftRequirement, error) {
	shapes := make(map[string][]db.ShiftRequirement)
	for _, id := range shiftIDs {
		if seats, ok := s.shapeByShiftID[id]; ok {
			shapes[id] = seats
		}
	}
	return shapes, nil
}

func (s *draftViewStore) ListRoles(context.Context) ([]db.Role, error) {
	return s.roles, nil
}

func (s *draftViewStore) GetDraftRotaAllocation(context.Context, string) (*db.DraftRotaAllocation, error) {
	return s.draft, s.draftErr
}

func (s *draftViewStore) GetDraftAllocationsByShiftIDs(_ context.Context, shiftIDs []string) ([]db.DraftAllocation, error) {
	s.seatsRead = append(s.seatsRead, shiftIDs)
	wanted := make(map[string]bool, len(shiftIDs))
	for _, id := range shiftIDs {
		wanted[id] = true
	}
	var out []db.DraftAllocation
	for _, seat := range s.seats {
		if wanted[seat.ShiftID] {
			out = append(out, seat)
		}
	}
	return out, nil
}

// draftViewVolunteers is the roster the draft's Seats are read against: it is
// what turns a stored volunteer id into the name a chip shows.
func draftViewVolunteers() *mockVolClient {
	return &mockVolClient{volunteers: []model.Volunteer{
		{ID: "alice", FirstName: "Alice", LastName: "Adams", Roles: []string{"Team lead", "Service volunteer"}, Status: "Active"},
		{ID: "bob", FirstName: "Bob", LastName: "Barnes", Roles: []string{"Service volunteer"}, Status: "Active"},
	}}
}

// draftViewRoles are the two Roles the fixtures below draw on: the capped one a
// shift has a single Seat of, and the uncapped one its size is spent on.
func draftViewRoles() []db.Role {
	return []db.Role{
		{ID: "role-lead", Name: "Team lead", Max: intPtr(1), Priority: 1},
		{ID: "role-sv", Name: "Service volunteer", Priority: 2},
	}
}

// twoShiftDraftStore is a rota in flight of two open Shifts, each asking for one
// Team lead and two Service volunteers — six Seats in all.
func twoShiftDraftStore() *draftViewStore {
	return &draftViewStore{
		inFlight: &db.RotaInFlight{
			Rotation: db.Rotation{ID: "rota-1", Start: "2026-08-02", End: "2026-08-09", ShiftCount: 2},
		},
		shifts: []db.Shift{
			{ID: "shift-1", RotaID: "rota-1", Date: "2026-08-02", StartAt: "2026-08-02T19:30:00", EndAt: "2026-08-02T21:30:00"},
			{ID: "shift-2", RotaID: "rota-1", Date: "2026-08-09", StartAt: "2026-08-09T19:30:00", EndAt: "2026-08-09T21:30:00"},
		},
		shapeByShiftID: map[string][]db.ShiftRequirement{
			"shift-1": {{ShiftID: "shift-1", RoleID: "role-lead", Seats: 1}, {ShiftID: "shift-1", RoleID: "role-sv", Seats: 2}},
			"shift-2": {{ShiftID: "shift-2", RoleID: "role-lead", Seats: 1}, {ShiftID: "shift-2", RoleID: "role-sv", Seats: 2}},
		},
		roles: draftViewRoles(),
	}
}

// The whole of what a draft is for: an admin watching the rota take shape sees
// who the solver put where, and how much of the rota it managed to staff.
func TestReadDraftRotaAllocation(t *testing.T) {
	store := twoShiftDraftStore()
	solvedAt := time.Date(2026, 8, 5, 6, 0, 0, 0, time.UTC)
	store.draft = &db.DraftRotaAllocation{
		RotaID: "rota-1", SolvedAt: solvedAt, Success: true, SolverStatus: "OPTIMAL",
	}
	store.seats = []db.DraftAllocation{
		{ID: "seat-1", ShiftID: "shift-1", Role: "Service volunteer", VolunteerID: "bob"},
		{ID: "seat-2", ShiftID: "shift-1", Role: "Team lead", VolunteerID: "alice"},
		{ID: "seat-3", ShiftID: "shift-2", Role: "Service volunteer", CustomEntry: "Visiting group"},
	}

	view, err := ReadDraftRotaAllocation(context.Background(), store, draftViewVolunteers(), &config.Config{}, zap.NewNop())
	require.NoError(t, err)
	require.NotNil(t, view)

	assert.Equal(t, "rota-1", view.RotaID)
	assert.Equal(t, 6, view.SeatsAsked, "two shifts asking for three Seats each")

	require.NotNil(t, view.Draft)
	assert.Equal(t, solvedAt, view.Draft.SolvedAt)
	assert.True(t, view.Draft.Success)
	assert.Equal(t, "OPTIMAL", view.Draft.SolverStatus)
	assert.Equal(t, 3, view.Draft.SeatsFilled, "three of the six Seats were filled")

	// One entry per Shift the draft placed anybody on, each carrying its people
	// in the order the rota shows them: by Role priority, then by name.
	require.Len(t, view.Draft.Shifts, 2)
	assert.Equal(t, "shift-1", view.Draft.Shifts[0].ShiftID)
	assert.Equal(t, []ShiftAssignee{
		{VolunteerID: "alice", Name: "Alice", Role: "Team lead"},
		{VolunteerID: "bob", Name: "Bob", Role: "Service volunteer"},
	}, view.Draft.Shifts[0].Assignees)

	assert.Equal(t, "shift-2", view.Draft.Shifts[1].ShiftID)
	assert.Equal(t, []ShiftAssignee{
		{CustomEntry: "Visiting group", Name: "Visiting group", Role: "Service volunteer"},
	}, view.Draft.Shifts[1].Assignees)
}

// A rota nobody has solved for yet is where every rota starts. It is not an
// absence to guard against: the Seats it is asking for are already known, which
// is what the screen says while there is nothing else to say.
func TestReadDraftRotaAllocation_NotSolvedYet(t *testing.T) {
	store := twoShiftDraftStore()

	view, err := ReadDraftRotaAllocation(context.Background(), store, draftViewVolunteers(), &config.Config{}, zap.NewNop())
	require.NoError(t, err)
	require.NotNil(t, view)

	assert.Equal(t, "rota-1", view.RotaID)
	assert.Equal(t, 6, view.SeatsAsked)
	assert.Nil(t, view.Draft, "nothing has been solved")
	assert.Empty(t, store.seatsRead, "and no Seats were read looking for one")
}

// An infeasible solve stores no Seats, exactly as a rota nobody has solved
// stores none. The outcome is what tells them apart, and it is the answer an
// admin most needs while there is still time to fix the input.
func TestReadDraftRotaAllocation_Infeasible(t *testing.T) {
	store := twoShiftDraftStore()
	store.draft = &db.DraftRotaAllocation{
		RotaID: "rota-1", SolvedAt: time.Now().UTC(), Success: false, SolverStatus: "INFEASIBLE",
	}

	view, err := ReadDraftRotaAllocation(context.Background(), store, draftViewVolunteers(), &config.Config{}, zap.NewNop())
	require.NoError(t, err)
	require.NotNil(t, view)

	require.NotNil(t, view.Draft, "an infeasible solve is a draft, not the absence of one")
	assert.False(t, view.Draft.Success)
	assert.Equal(t, "INFEASIBLE", view.Draft.SolverStatus)
	assert.Zero(t, view.Draft.SeatsFilled)
	assert.Empty(t, view.Draft.Shifts)
}

// A closed Shift asks for nobody however it is shaped, so it is not part of what
// the rota is short of. Same rule the solve counts by, because the two numbers
// are read side by side.
func TestReadDraftRotaAllocation_ClosedShiftsAskForNobody(t *testing.T) {
	store := twoShiftDraftStore()
	store.shifts[1].Closed = true

	view, err := ReadDraftRotaAllocation(context.Background(), store, draftViewVolunteers(), &config.Config{}, zap.NewNop())
	require.NoError(t, err)
	require.NotNil(t, view)
	assert.Equal(t, 3, view.SeatsAsked, "only the open shift is asking for anybody")
}

// No rota in flight is an answer rather than a miss: between one rota going out
// and the next being defined there is nothing to draft, and the screen has
// nothing to show rather than something that failed to load.
func TestReadDraftRotaAllocation_NothingInFlight(t *testing.T) {
	view, err := ReadDraftRotaAllocation(context.Background(), &draftViewStore{}, draftViewVolunteers(), &config.Config{}, zap.NewNop())
	require.NoError(t, err)
	assert.Nil(t, view)
}

func TestReadDraftRotaAllocation_StoreFailure(t *testing.T) {
	store := &draftViewStore{inFlightErr: errors.New("connection refused")}

	view, err := ReadDraftRotaAllocation(context.Background(), store, draftViewVolunteers(), &config.Config{}, zap.NewNop())
	require.Error(t, err)
	assert.Nil(t, view)
}
