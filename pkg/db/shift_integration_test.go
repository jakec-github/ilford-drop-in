package db_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
	"github.com/jakechorley/ilford-drop-in/pkg/db/dbtest"
)

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
