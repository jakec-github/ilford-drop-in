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
	"github.com/jakechorley/ilford-drop-in/pkg/core/allocator"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// A draft is a guess at a rota that has not been decided yet. Once it has, the
// allocation is the rota and a draft beside it could only contradict it — so a
// solve for an allocated Rotation is refused before it starts, and nothing is
// written. The store enforces the same thing under a row lock, where a rota
// allocated *during* a solve is caught too.
func TestSolveDraftRotaAllocationRefusesAnAllocatedRota(t *testing.T) {
	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2026-08-02", ShiftCount: 2, AllocatedDatetime: "2026-08-01T10:00:00Z"},
		},
		shifts: sundayShifts("rota-1", "2026-08-02", 2),
	}

	result, err := SolveDraftRotaAllocation(
		context.Background(),
		store,
		&mockVolClient{},
		&config.Config{},
		zap.NewNop(),
		"", // pythonFlag
	)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "rota-1", "the refusal names the rota")
	assert.Contains(t, err.Error(), "already allocated")
	assert.Empty(t, store.storedDrafts, "no draft is written")
	assert.Empty(t, store.insertedAllocations, "and nothing is allocated either")
}

// Drafting and allocating solve one problem, so they refuse over one set of
// inputs: a Shift asking for nobody stops a draft exactly as it stops an
// allocation, naming the same dates. A draft that quietly staffed nothing would
// be worse than no draft, because it looks like an answer.
func TestSolveDraftRotaAllocationSharesTheAllocationGates(t *testing.T) {
	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{{ID: "rota-1", Start: "2026-08-02", ShiftCount: 2}},
		shifts:    sundayShifts("rota-1", "2026-08-02", 2),
	}
	store.noShape = true

	result, err := SolveDraftRotaAllocation(
		context.Background(),
		store,
		&mockVolClient{},
		&config.Config{},
		zap.NewNop(),
		"", // pythonFlag
	)

	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "2026-08-02")
	assert.Contains(t, err.Error(), "ask for nobody")
	assert.Empty(t, store.storedDrafts, "no draft is written")
}

// A draft is dirty when an allocator input has moved under the Rotation since it
// was solved, and that is the whole of the test: the two stamps disagreeing.
//
// Not "solved before the last change", which is the comparison this deliberately
// is not. A change landing while the solver runs is timestamped before the solve
// finishes, so a clock comparison would call the draft fresh and the change
// would be lost until the next one.
func TestDraftRotaAllocationDirtiness(t *testing.T) {
	solved := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	moved := time.Date(2026, 8, 5, 11, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name       string
		rotaStamp  time.Time
		draftStamp time.Time
		dirty      bool
	}{
		{
			name:  "nothing has ever moved under the rota",
			dirty: false,
		},
		{
			name:       "the draft was solved from the inputs as they stand",
			rotaStamp:  moved,
			draftStamp: moved,
			dirty:      false,
		},
		{
			name:       "something moved after the draft was solved",
			rotaStamp:  moved,
			draftStamp: solved,
			dirty:      true,
		},
		{
			// The rota was drafted, then something moved for the first time.
			name:      "the first change under a freshly drafted rota",
			rotaStamp: moved,
			dirty:     true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &mockAllocateRotaStore{
				rotations: []db.Rotation{{ID: "rota-1", Start: "2026-08-02", ShiftCount: 2, InputsChangedAt: tc.rotaStamp}},
				shifts:    sundayShifts("rota-1", "2026-08-02", 2),
				storedDrafts: []db.DraftRotaAllocation{{
					RotaID:          "rota-1",
					SolvedAt:        solved,
					Success:         true,
					SolverStatus:    "OPTIMAL",
					Diagnostics:     []byte(`{"solve_time_seconds":1.25}`),
					InputsChangedAt: tc.draftStamp,
					SeatsAsked:      10,
					SeatsFilled:     8,
				}},
			}

			status, err := DraftRotaAllocationInFlight(context.Background(), store, &mockVolClient{}, &config.Config{}, zap.NewNop())

			require.NoError(t, err)
			assert.Equal(t, tc.dirty, status.Dirty)
			assert.True(t, status.Solved)
			assert.Equal(t, "rota-1", status.RotaID)
			assert.Equal(t, "2026-08-02", status.RotaStart)
			assert.True(t, status.SolvedAt.Equal(solved))
			assert.Equal(t, "OPTIMAL", status.SolverStatus)
			assert.Equal(t, 10, status.SeatsAsked)
			assert.Equal(t, 8, status.SeatsFilled, "two Seats short, which is what an admin acts on")
			assert.Equal(t, 1.25, status.Diagnostics.SolveTimeSeconds, "read back out of the solver's own JSON")
		})
	}
}

// A rota nobody has drafted yet reads as unsolved and dirty: there is nothing
// that speaks for it, which is the same thing a caller does about as a draft
// whose inputs have moved — solve it.
func TestDraftRotaAllocationOfAnUndraftedRota(t *testing.T) {
	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{{ID: "rota-1", Start: "2026-08-02", ShiftCount: 2}},
		shifts:    sundayShifts("rota-1", "2026-08-02", 2),
	}

	status, err := DraftRotaAllocationInFlight(context.Background(), store, &mockVolClient{}, &config.Config{}, zap.NewNop())

	require.NoError(t, err)
	assert.False(t, status.Solved)
	assert.True(t, status.Dirty, "nothing speaks for this rota yet")
	assert.True(t, status.SolvedAt.IsZero())
	assert.Empty(t, status.SolverStatus)
}

// The rota a draft drafted, which is what an admin watching it take shape reads:
// who the solver put where, keyed by Shift and named against the roster, with
// the Shifts it staffed nobody on left out.
func TestDraftRotaAllocationCarriesTheRotaItDrafted(t *testing.T) {
	moved := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{{ID: "rota-1", Start: "2026-08-02", ShiftCount: 2, InputsChangedAt: moved}},
		shifts:    sundayShifts("rota-1", "2026-08-02", 2),
		storedDrafts: []db.DraftRotaAllocation{{
			RotaID:          "rota-1",
			SolvedAt:        moved,
			Success:         true,
			SolverStatus:    "OPTIMAL",
			Diagnostics:     []byte(`{}`),
			InputsChangedAt: moved,
			SeatsAsked:      10,
			SeatsFilled:     2,
		}},
		storedDraftSeats: [][]db.DraftAllocation{{
			{ID: "seat-1", ShiftID: "2026-08-02", Role: "Service volunteer", VolunteerID: "bob"},
			{ID: "seat-2", ShiftID: "2026-08-02", Role: "Team lead", VolunteerID: "alice"},
		}},
	}
	volunteers := &mockVolClient{volunteers: []model.Volunteer{
		{ID: "alice", FirstName: "Alice", LastName: "Adams", Status: "Active"},
		{ID: "bob", FirstName: "Bob", LastName: "Barnes", Status: "Active"},
	}}

	status, err := DraftRotaAllocationInFlight(context.Background(), store, volunteers, &config.Config{}, zap.NewNop())

	require.NoError(t, err)
	require.Len(t, status.Shifts, 1, "the Shift the solver staffed, not the one it left empty")
	assert.Equal(t, "2026-08-02", status.Shifts[0].ShiftID)
	require.Len(t, status.Shifts[0].Assignees, 2)
	// In the order the rota is read in: by Role priority, so the team lead leads.
	assert.Equal(t, "Alice", status.Shifts[0].Assignees[0].Name)
	assert.Equal(t, "Team lead", status.Shifts[0].Assignees[0].Role)
	assert.Equal(t, "Bob", status.Shifts[0].Assignees[1].Name)
}

// A rota nobody has drafted for has no Seats to name, so nothing goes looking for
// the roster: the Sheet is a network call, and it would be made to name nobody.
func TestDraftRotaAllocationOfAnUndraftedRotaReadsNoRoster(t *testing.T) {
	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{{ID: "rota-1", Start: "2026-08-02", ShiftCount: 2}},
		shifts:    sundayShifts("rota-1", "2026-08-02", 2),
	}

	status, err := DraftRotaAllocationInFlight(
		context.Background(), store,
		&mockVolClient{listErr: errors.New("the sheet was read")},
		&config.Config{}, zap.NewNop(),
	)

	require.NoError(t, err)
	assert.Empty(t, status.Shifts)
}

// No rota in flight, nothing to draft. Said as the missing step rather than as
// an empty answer, because that is what an admin has to do about it — and it is
// what stops anything solving when there is no unallocated Rotation.
func TestDraftRotaAllocationWithNoRotaInFlight(t *testing.T) {
	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2026-08-02", ShiftCount: 2, AllocatedDatetime: "2026-08-01T10:00:00Z"},
		},
		shifts: sundayShifts("rota-1", "2026-08-02", 2),
	}

	status, err := DraftRotaAllocationInFlight(context.Background(), store, &mockVolClient{}, &config.Config{}, zap.NewNop())

	require.ErrorIs(t, err, ErrNotFound)
	assert.Nil(t, status)
	assert.Contains(t, err.Error(), "no rota in flight")
}

// A store that cannot answer is a failure rather than an empty draft: "nothing
// has been solved" and "the database is down" would otherwise look identical to
// a screen, and only one of them means solve it again.
func TestDraftRotaAllocationSurfacesAReadFailure(t *testing.T) {
	store := &mockAllocateRotaStore{
		rotations:   []db.Rotation{{ID: "rota-1", Start: "2026-08-02", ShiftCount: 2}},
		shifts:      sundayShifts("rota-1", "2026-08-02", 2),
		getDraftErr: errors.New("connection refused"),
	}

	status, err := DraftRotaAllocationInFlight(context.Background(), store, &mockVolClient{}, &config.Config{}, zap.NewNop())

	require.Error(t, err)
	assert.Nil(t, status)
	assert.NotErrorIs(t, err, ErrNotFound)
}

// Solving is what makes a draft clean again, and this is the mechanism: the row
// a solve stores carries the Rotation's stamp as it stood when the solve began,
// so the two agree until something moves.
//
// The stamp is read before any input is (solveRotaInFlight reads the rota
// first), which is what makes a change landing mid-solve leave the draft dirty
// rather than being swallowed by it.
func TestASolveCarriesTheRotasInputsStamp(t *testing.T) {
	moved := time.Date(2026, 8, 5, 11, 0, 0, 0, time.UTC)
	solvedAt := time.Date(2026, 8, 5, 11, 0, 30, 0, time.UTC)
	solve := &rotaSolve{
		rota: &db.Rotation{ID: "rota-1", Start: "2026-08-02", InputsChangedAt: moved},
		shifts: []db.Shift{
			{ID: "shift-1", RotaID: "rota-1", Date: "2026-08-02"},
			{ID: "shift-2", RotaID: "rota-1", Date: "2026-08-09", Closed: true},
		},
		shapes: map[string]model.Shape{
			"shift-1": {{Role: model.Role{Name: "Team lead"}, Count: 1}, {Role: model.Role{Name: "Service volunteer"}, Count: 3}},
			// A closed Shift asks for nobody however it is shaped.
			"shift-2": {{Role: model.Role{Name: "Team lead"}, Count: 1}},
		},
		output: &allocator.CpsatOutput{Success: true, SolverStatus: "FEASIBLE", ObjectiveValue: 12},
		solvedShifts: []*allocator.Shift{
			{Assignments: []allocator.Assignment{{}, {}}},
			{Assignments: []allocator.Assignment{{}}},
		},
	}

	draft := solve.draft(solvedAt, []byte(`{}`))

	assert.Equal(t, "rota-1", draft.RotaID)
	assert.True(t, draft.InputsChangedAt.Equal(moved), "the stamp the solve began from, not the moment it ended")
	assert.True(t, draft.SolvedAt.Equal(solvedAt))
	assert.Equal(t, "FEASIBLE", draft.SolverStatus)
	assert.Equal(t, 4, draft.SeatsAsked, "the open Shift's four Seats; the closed one asks for nobody")
	assert.Equal(t, 3, draft.SeatsFilled)
}
