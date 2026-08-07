package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
	"github.com/jakechorley/ilford-drop-in/pkg/db/dbtest"
)

// draftFixture mints an unallocated rota with two shifts, which is the only
// state a draft may be written for.
func draftFixture(t *testing.T, database *db.DB) (db.Rotation, db.Shift, db.Shift) {
	t.Helper()
	rota := db.Rotation{ID: uuid.New().String()}
	first := dbtest.Shift(rota.ID, "2026-08-02")
	second := dbtest.Shift(rota.ID, "2026-08-09")
	require.NoError(t, database.InsertDefinedRota(context.Background(), &rota, []db.Shift{first, second}, nil, nil))
	return rota, first, second
}

// A draft round-trips whole: the outcome an admin reads during the availability
// window, and the Seats it placed, scoped by shift the way allocations are.
func TestReplaceDraftRotaAllocation(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	rota, first, second := draftFixture(t, database)

	solvedAt := time.Date(2026, 8, 5, 9, 30, 0, 0, time.UTC)
	seats := []db.DraftAllocation{
		{ID: uuid.New().String(), ShiftID: first.ID, Role: "Team lead", VolunteerID: "alice"},
		{ID: uuid.New().String(), ShiftID: first.ID, Role: "Service volunteer", CustomEntry: "External Org"},
		{ID: uuid.New().String(), ShiftID: second.ID, Role: "Team lead", VolunteerID: "bob"},
	}
	require.NoError(t, database.ReplaceDraftRotaAllocation(ctx, db.DraftRotaAllocation{
		RotaID:         rota.ID,
		SolvedAt:       solvedAt,
		Success:        true,
		SolverStatus:   "OPTIMAL",
		ObjectiveValue: 42,
		Diagnostics:    []byte(`{"solve_time_seconds":0.5,"num_groups":3}`),
	}, seats))

	draft, err := database.GetDraftRotaAllocation(ctx, rota.ID)
	require.NoError(t, err)
	require.NotNil(t, draft)
	assert.Equal(t, rota.ID, draft.RotaID)
	assert.True(t, draft.SolvedAt.Equal(solvedAt))
	assert.True(t, draft.Success)
	assert.Equal(t, "OPTIMAL", draft.SolverStatus)
	assert.Equal(t, 42, draft.ObjectiveValue)
	assert.JSONEq(t, `{"solve_time_seconds":0.5,"num_groups":3}`, string(draft.Diagnostics))

	// Scoped by shift, like allocations: the caller has already resolved the
	// shifts it cares about and never re-derives a date window (ADR 0001).
	onFirst, err := database.GetDraftAllocationsByShiftIDs(ctx, []string{first.ID})
	require.NoError(t, err)
	require.Len(t, onFirst, 2)
	byID := map[string]db.DraftAllocation{}
	for _, seat := range onFirst {
		byID[seat.ID] = seat
	}
	// The nullable columns round-trip: a volunteer Seat carries no custom entry
	// and a custom one carries no volunteer.
	assert.Equal(t, "alice", byID[seats[0].ID].VolunteerID)
	assert.Empty(t, byID[seats[0].ID].CustomEntry)
	assert.Equal(t, "External Org", byID[seats[1].ID].CustomEntry)
	assert.Empty(t, byID[seats[1].ID].VolunteerID)

	both, err := database.GetDraftAllocationsByShiftIDs(ctx, []string{first.ID, second.ID})
	require.NoError(t, err)
	assert.Len(t, both, 3)

	// An empty id set answers without a query, as every by-shift reader does.
	none, err := database.GetDraftAllocationsByShiftIDs(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, none)
}

// What a draft records about its own freshness round-trips: the Rotation's
// inputs stamp as it stood when the solve began, and what the solve was asked
// for against what it managed (issue #142).
//
// The stamp is stored as the Rotation holds it, NULL and all, so that a draft of
// a rota nothing has moved under reads as equal to it rather than as dirty
// forever.
func TestReplaceDraftRotaAllocationRecordsFreshness(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	rota, first, _ := draftFixture(t, database)

	require.NoError(t, database.ReplaceDraftRotaAllocation(ctx, db.DraftRotaAllocation{
		RotaID:       rota.ID,
		SolvedAt:     time.Now().UTC(),
		Success:      true,
		SolverStatus: "OPTIMAL",
		Diagnostics:  []byte(`{}`),
		SeatsAsked:   10,
		SeatsFilled:  7,
	}, []db.DraftAllocation{
		{ID: uuid.New().String(), ShiftID: first.ID, Role: "Team lead", VolunteerID: "alice"},
	}))

	draft, err := database.GetDraftRotaAllocation(ctx, rota.ID)
	require.NoError(t, err)
	require.NotNil(t, draft)
	assert.True(t, draft.InputsChangedAt.IsZero(), "nothing had moved under the rota when it was solved")
	assert.Equal(t, 10, draft.SeatsAsked)
	assert.Equal(t, 7, draft.SeatsFilled, "three Seats short, and the draft says so on its own")

	// Now something moves, and the draft is solved again against the stamp it
	// read on the way in.
	require.NoError(t, database.WithRotaShiftLock(ctx, []string{rota.ID}, func(tx db.ShiftTxStore) error {
		_, err := tx.SetShiftClosed(ctx, first.ID, true)
		return err
	}))
	inFlight, err := database.GetRotaInFlight(ctx)
	require.NoError(t, err)
	require.False(t, inFlight.InputsChangedAt.IsZero())

	require.NoError(t, database.ReplaceDraftRotaAllocation(ctx, db.DraftRotaAllocation{
		RotaID:          rota.ID,
		SolvedAt:        time.Now().UTC(),
		Success:         true,
		SolverStatus:    "OPTIMAL",
		Diagnostics:     []byte(`{}`),
		InputsChangedAt: inFlight.InputsChangedAt,
		SeatsAsked:      5,
		SeatsFilled:     5,
	}, nil))

	draft, err = database.GetDraftRotaAllocation(ctx, rota.ID)
	require.NoError(t, err)
	require.NotNil(t, draft)
	assert.True(t, draft.InputsChangedAt.Equal(inFlight.InputsChangedAt),
		"the draft caught up with the Rotation, which is what makes it clean again")
}

// A Rotation nobody has solved for has no draft, which is an ordinary answer
// rather than an error — it is where every rota starts.
func TestGetDraftRotaAllocationUnsolved(t *testing.T) {
	database, _ := dbtest.New(t)
	rota, _, _ := draftFixture(t, database)

	draft, err := database.GetDraftRotaAllocation(context.Background(), rota.ID)
	require.NoError(t, err)
	assert.Nil(t, draft)
}

// A draft is replaced entire, never merged: the second solve is the draft, and
// a Seat the first one placed is gone rather than kept alongside. That is what
// makes a draft a whole rota rather than an accumulation of guesses.
func TestReplaceDraftRotaAllocationReplacesTheWholeDraft(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	rota, first, second := draftFixture(t, database)

	require.NoError(t, database.ReplaceDraftRotaAllocation(ctx, db.DraftRotaAllocation{
		RotaID:       rota.ID,
		SolvedAt:     time.Now().UTC(),
		Success:      true,
		SolverStatus: "OPTIMAL",
		Diagnostics:  []byte(`{}`),
	}, []db.DraftAllocation{
		{ID: uuid.New().String(), ShiftID: first.ID, Role: "Team lead", VolunteerID: "alice"},
		{ID: uuid.New().String(), ShiftID: second.ID, Role: "Team lead", VolunteerID: "bob"},
	}))

	// The rota turns out infeasible on the next solve, so it staffs nobody.
	require.NoError(t, database.ReplaceDraftRotaAllocation(ctx, db.DraftRotaAllocation{
		RotaID:       rota.ID,
		SolvedAt:     time.Now().UTC(),
		Success:      false,
		SolverStatus: "INFEASIBLE",
		Diagnostics:  []byte(`{}`),
	}, nil))

	draft, err := database.GetDraftRotaAllocation(ctx, rota.ID)
	require.NoError(t, err)
	require.NotNil(t, draft)
	assert.False(t, draft.Success)
	assert.Equal(t, "INFEASIBLE", draft.SolverStatus)

	seats, err := database.GetDraftAllocationsByShiftIDs(ctx, []string{first.ID, second.ID})
	require.NoError(t, err)
	assert.Empty(t, seats, "the previous draft's Seats went with it")
}

// A draft speaks for a rota that has not been allocated. Once it has, the
// allocation is the rota and a draft beside it could only contradict it — so
// the write refuses, under the same rotation row lock allocation itself takes.
func TestReplaceDraftRotaAllocationRefusesAnAllocatedRota(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	rota, first, _ := draftFixture(t, database)

	require.NoError(t, database.InsertAllocationsAndSetAllocated(ctx, []db.Allocation{
		{ID: uuid.New().String(), ShiftID: first.ID, Role: "Team lead", VolunteerID: "alice"},
	}, rota.ID, time.Now().UTC()))

	err := database.ReplaceDraftRotaAllocation(ctx, db.DraftRotaAllocation{
		RotaID:       rota.ID,
		SolvedAt:     time.Now().UTC(),
		Success:      true,
		SolverStatus: "OPTIMAL",
		Diagnostics:  []byte(`{}`),
	}, []db.DraftAllocation{
		{ID: uuid.New().String(), ShiftID: first.ID, Role: "Team lead", VolunteerID: "bob"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), rota.ID, "the refusal names the rota")
	assert.Contains(t, err.Error(), "already allocated")

	draft, err := database.GetDraftRotaAllocation(ctx, rota.ID)
	require.NoError(t, err)
	assert.Nil(t, draft, "nothing was written")
	seats, err := database.GetDraftAllocationsByShiftIDs(ctx, []string{first.ID})
	require.NoError(t, err)
	assert.Empty(t, seats)
}

// Allocating consumes the draft. The rows it leaves behind are the allocation,
// and the draft that became it goes in the same transaction: a draft outliving
// its rota would be a second, speculative answer sitting beside the real one,
// for a rota nobody can draft again.
func TestInsertAllocationsAndSetAllocatedClearsTheDraft(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	rota, first, second := draftFixture(t, database)

	require.NoError(t, database.ReplaceDraftRotaAllocation(ctx, db.DraftRotaAllocation{
		RotaID:       rota.ID,
		SolvedAt:     time.Now().UTC(),
		Success:      true,
		SolverStatus: "OPTIMAL",
		Diagnostics:  []byte(`{}`),
	}, []db.DraftAllocation{
		{ID: uuid.New().String(), ShiftID: first.ID, Role: "Team lead", VolunteerID: "alice"},
		{ID: uuid.New().String(), ShiftID: second.ID, Role: "Team lead", VolunteerID: "bob"},
	}))

	require.NoError(t, database.InsertAllocationsAndSetAllocated(ctx, []db.Allocation{
		{ID: uuid.New().String(), ShiftID: first.ID, Role: "Team lead", VolunteerID: "alice"},
		{ID: uuid.New().String(), ShiftID: second.ID, Role: "Team lead", VolunteerID: "bob"},
	}, rota.ID, time.Now().UTC()))

	draft, err := database.GetDraftRotaAllocation(ctx, rota.ID)
	require.NoError(t, err)
	assert.Nil(t, draft, "the draft was consumed by the allocation")
	seats, err := database.GetDraftAllocationsByShiftIDs(ctx, []string{first.ID, second.ID})
	require.NoError(t, err)
	assert.Empty(t, seats, "and so were its Seats")

	allocated, err := database.GetAllocationsByShiftIDs(ctx, []string{first.ID, second.ID})
	require.NoError(t, err)
	assert.Len(t, allocated, 2, "while the allocation itself is there")
}

// The double-allocation guard (issue #8) under the row lock that enforces it: a
// second attempt on a rota that has already been allocated writes nothing and
// says why. This is the last word on allocating the rota you were shown — every
// check in front of it is a fast refusal, and two admins confirming the same
// draft at the same moment meet here.
func TestInsertAllocationsAndSetAllocatedRefusesASecondAllocation(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	rota, first, _ := draftFixture(t, database)

	firstAllocated := time.Now().UTC()
	require.NoError(t, database.InsertAllocationsAndSetAllocated(ctx, []db.Allocation{
		{ID: uuid.New().String(), ShiftID: first.ID, Role: "Team lead", VolunteerID: "alice"},
	}, rota.ID, firstAllocated))

	err := database.InsertAllocationsAndSetAllocated(ctx, []db.Allocation{
		{ID: uuid.New().String(), ShiftID: first.ID, Role: "Team lead", VolunteerID: "bob"},
	}, rota.ID, time.Now().UTC())

	require.Error(t, err)
	assert.Contains(t, err.Error(), rota.ID, "the refusal names the rota")
	assert.Contains(t, err.Error(), "already allocated")

	allocated, err := database.GetAllocationsByShiftIDs(ctx, []string{first.ID})
	require.NoError(t, err)
	require.Len(t, allocated, 1, "the second attempt wrote nothing")
	assert.Equal(t, "alice", allocated[0].VolunteerID, "and did not overwrite the first")
}
