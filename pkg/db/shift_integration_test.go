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

// TestGetShiftsInRange checks that every minted shift in range is returned,
// allocated or not, with the Allocated flag sourced from the rota's
// allocated_datetime, and that the from/to bounds are inclusive (issue #38).
func TestGetShiftsInRange(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	// rota1 is allocated; rota2 is minted but left unallocated.
	rota1 := &db.Rotation{ID: uuid.New().String()}
	shift1 := db.Shift{ID: uuid.New().String(), Date: "2026-08-02", RotaID: rota1.ID}
	require.NoError(t, database.InsertRotationAndShifts(ctx, rota1, []db.Shift{
		shift1,
		{ID: uuid.New().String(), Date: "2026-08-09", RotaID: rota1.ID},
	}))
	require.NoError(t, database.InsertAllocationsAndSetAllocated(ctx,
		[]db.Allocation{{ID: uuid.New().String(), ShiftID: shift1.ID, Role: "team-lead", VolunteerID: "alice"}},
		rota1.ID, time.Now()))

	rota2 := &db.Rotation{ID: uuid.New().String()}
	require.NoError(t, database.InsertRotationAndShifts(ctx, rota2, []db.Shift{
		{ID: uuid.New().String(), Date: "2026-08-16", RotaID: rota2.ID},
	}))

	// All three shifts, unbounded, ordered by date.
	shifts, err := database.GetShiftsInRange(ctx, time.Time{}, time.Time{})
	require.NoError(t, err)
	require.Len(t, shifts, 3)
	assert.Equal(t, "2026-08-02", shifts[0].Date)
	assert.True(t, shifts[0].Allocated, "allocated rota's shift")
	assert.True(t, shifts[1].Allocated)
	assert.Equal(t, "2026-08-16", shifts[2].Date)
	assert.False(t, shifts[2].Allocated, "unallocated rota's shift must still appear")

	// Inclusive bounds drop the out-of-range shift.
	bounded, err := database.GetShiftsInRange(ctx,
		time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Len(t, bounded, 2)
	assert.Equal(t, "2026-08-09", bounded[0].Date)
	assert.Equal(t, "2026-08-16", bounded[1].Date)
}

// TestGetAllocationsAndAlterationsByShiftIDs checks that allocations and
// alterations are fetched for exactly the given shifts (issue #38): the scope
// comes from the shift ids the caller holds, not a re-derived date window, and
// an empty id set is a no-op.
func TestGetAllocationsAndAlterationsByShiftIDs(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	rota := &db.Rotation{ID: uuid.New().String()}
	shiftA := db.Shift{ID: uuid.New().String(), Date: "2026-08-02", RotaID: rota.ID}
	shiftB := db.Shift{ID: uuid.New().String(), Date: "2026-08-09", RotaID: rota.ID}
	require.NoError(t, database.InsertRotationAndShifts(ctx, rota, []db.Shift{shiftA, shiftB}))
	require.NoError(t, database.InsertAllocationsAndSetAllocated(ctx, []db.Allocation{
		{ID: uuid.New().String(), ShiftID: shiftA.ID, Role: "team-lead", VolunteerID: "alice"},
		{ID: uuid.New().String(), ShiftID: shiftB.ID, Role: "volunteer", VolunteerID: "bob"},
	}, rota.ID, time.Now()))

	// An alteration on shiftB only; its cover_id must reference the cover row.
	coverID := uuid.New().String()
	require.NoError(t, database.WithRotaLock(ctx, []string{rota.ID}, func(store db.RotaChangeStore) error {
		return store.InsertCoverAndAlterations(ctx,
			&db.Cover{ID: coverID, Reason: "cover", UserEmail: "jane@example.com"},
			[]db.Alteration{{ID: uuid.New().String(), ShiftID: shiftB.ID, Direction: "remove", VolunteerID: "bob", CoverID: coverID}})
	}))

	// Scoping to shiftA returns only its allocation and no alterations.
	allocs, err := database.GetAllocationsByShiftIDs(ctx, []string{shiftA.ID})
	require.NoError(t, err)
	require.Len(t, allocs, 1)
	assert.Equal(t, "alice", allocs[0].VolunteerID)
	assert.Equal(t, shiftA.ID, allocs[0].ShiftID)

	alts, err := database.GetAlterationsByShiftIDs(ctx, []string{shiftA.ID})
	require.NoError(t, err)
	assert.Empty(t, alts)

	// Scoping to shiftB picks up its allocation and its alteration.
	alts, err = database.GetAlterationsByShiftIDs(ctx, []string{shiftB.ID})
	require.NoError(t, err)
	require.Len(t, alts, 1)
	assert.Equal(t, shiftB.ID, alts[0].ShiftID)

	// Both shifts returns both allocations.
	allocs, err = database.GetAllocationsByShiftIDs(ctx, []string{shiftA.ID, shiftB.ID})
	require.NoError(t, err)
	assert.Len(t, allocs, 2)

	// Empty id set is a no-op.
	allocs, err = database.GetAllocationsByShiftIDs(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, allocs)
	alts, err = database.GetAlterationsByShiftIDs(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, alts)
}

// TestInsertAllocationsUnknownShiftIDFails pins the new failure mode after the
// shift_id re-key (ADR 0001): an allocation carrying a ShiftID with no matching
// shift row is rejected by the shift_id FK, and the whole transaction rolls
// back (the rotation is not marked allocated). This replaced the old NOT NULL
// trip on the resolved-via-subselect shift_id.
func TestInsertAllocationsUnknownShiftIDFails(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	rota := &db.Rotation{ID: uuid.New().String()}
	require.NoError(t, database.InsertRotationAndShifts(ctx, rota, []db.Shift{
		{ID: uuid.New().String(), Date: "2026-08-02", RotaID: rota.ID},
	}))

	err := database.InsertAllocationsAndSetAllocated(ctx, []db.Allocation{
		{ID: uuid.New().String(), ShiftID: uuid.New().String(), Role: "volunteer", VolunteerID: "alice"},
	}, rota.ID, time.Now())
	require.Error(t, err, "an unknown ShiftID must be rejected by the FK")

	rotations, err := database.GetRotations(ctx)
	require.NoError(t, err)
	require.Len(t, rotations, 1)
	assert.Empty(t, rotations[0].AllocatedDatetime, "the failed insert must not mark the rota allocated")
}

// TestShiftDateUniqueRejectsOverlappingRotas pins the concurrency role of the
// shift.date UNIQUE constraint (issue #41, hazard B1): two rotas minting the
// same shift date cannot both commit. The constraint exists for ADR 0001
// reasons, but it is also the only thing making concurrent DefineRota runs
// safe — the losing insert fails wholesale, writing neither the rotation nor
// its non-overlapping shifts. If a schema change ever relaxes the constraint
// (e.g. multiple shifts per day), this test flags that the define-rota race
// needs a replacement guard.
func TestShiftDateUniqueRejectsOverlappingRotas(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	rota1 := &db.Rotation{ID: uuid.New().String()}
	require.NoError(t, database.InsertRotationAndShifts(ctx, rota1, []db.Shift{
		{ID: uuid.New().String(), Date: "2026-08-02", RotaID: rota1.ID},
		{ID: uuid.New().String(), Date: "2026-08-09", RotaID: rota1.ID},
	}))

	// The second rota overlaps rota1 on one date only; the shared date must
	// sink the whole insert, including the non-overlapping shift.
	rota2 := &db.Rotation{ID: uuid.New().String()}
	err := database.InsertRotationAndShifts(ctx, rota2, []db.Shift{
		{ID: uuid.New().String(), Date: "2026-08-09", RotaID: rota2.ID},
		{ID: uuid.New().String(), Date: "2026-08-16", RotaID: rota2.ID},
	})
	require.Error(t, err)

	rotations, err := database.GetRotations(ctx)
	require.NoError(t, err)
	require.Len(t, rotations, 1, "losing rota must not be committed")
	assert.Equal(t, rota1.ID, rotations[0].ID)
	assert.Equal(t, 2, rotations[0].ShiftCount, "winning rota's shifts must be untouched")
}
