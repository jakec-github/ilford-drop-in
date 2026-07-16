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
	require.NoError(t, database.InsertRotationAndShifts(ctx, rota1, []db.Shift{
		{ID: uuid.New().String(), Date: "2026-08-02", RotaID: rota1.ID},
		{ID: uuid.New().String(), Date: "2026-08-09", RotaID: rota1.ID},
	}))
	require.NoError(t, database.InsertAllocationsAndSetAllocated(ctx,
		[]db.Allocation{{ID: uuid.New().String(), RotaID: rota1.ID, ShiftDate: "2026-08-02", Role: "team-lead", VolunteerID: "alice"}},
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
