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

// TestPreallocationInsertReadDelete exercises the pin lifecycle through
// the rota-preallocation lock: insert two pins on one shift, read them back
// scoped by shift id, resolve one by id to its shift and rota, then delete it
// (true), delete again (false), and confirm the empty-id read is a no-op.
func TestPreallocationInsertReadDelete(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	rota := &db.Rotation{ID: uuid.New().String()}
	shiftA := dbtest.Shift(rota.ID, "2026-08-02")
	shiftB := dbtest.Shift(rota.ID, "2026-08-09")
	require.NoError(t, database.InsertDefinedRota(ctx, rota, []db.Shift{shiftA, shiftB}, nil))

	volPin := db.Preallocation{ID: uuid.New().String(), ShiftID: shiftA.ID, Role: "Team lead", VolunteerID: "alice"}
	customPin := db.Preallocation{ID: uuid.New().String(), ShiftID: shiftA.ID, Role: "Service volunteer", CustomValue: "External Org"}

	// Insert both pins under the lock.
	require.NoError(t, database.WithRotaPreallocationLock(ctx, []string{rota.ID}, func(store db.PreallocationTxStore) error {
		allocated, err := store.RotaAllocated(ctx, rota.ID)
		require.NoError(t, err)
		assert.False(t, allocated, "a freshly minted rota is not allocated")
		if err := store.InsertPreallocation(ctx, volPin); err != nil {
			return err
		}
		return store.InsertPreallocation(ctx, customPin)
	}))

	// Read back scoped by shift id: shiftA has both, shiftB has none.
	pins, err := database.GetPreallocationsByShiftIDs(ctx, []string{shiftA.ID})
	require.NoError(t, err)
	require.Len(t, pins, 2)

	none, err := database.GetPreallocationsByShiftIDs(ctx, []string{shiftB.ID})
	require.NoError(t, err)
	assert.Empty(t, none)

	// The nullable columns round-trip: the volunteer pin has no custom value and
	// vice versa.
	byID := map[string]db.Preallocation{}
	for _, p := range pins {
		byID[p.ID] = p
	}
	assert.Equal(t, "alice", byID[volPin.ID].VolunteerID)
	assert.Empty(t, byID[volPin.ID].CustomValue)
	assert.Equal(t, "External Org", byID[customPin.ID].CustomValue)
	assert.Empty(t, byID[customPin.ID].VolunteerID)

	// Resolve a pin by id to its shift and rota in one join.
	got, shift, err := database.GetPreallocationByID(ctx, volPin.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, shift)
	assert.Equal(t, shiftA.ID, shift.ID)
	assert.Equal(t, rota.ID, shift.RotaID)
	assert.Equal(t, "2026-08-02", shift.Date)
	assert.Equal(t, "alice", got.VolunteerID)

	// An unknown id resolves to nil without error.
	missing, missingShift, err := database.GetPreallocationByID(ctx, uuid.New().String())
	require.NoError(t, err)
	assert.Nil(t, missing)
	assert.Nil(t, missingShift)

	// Delete returns true first, false on the second attempt (concurrent-delete
	// signal).
	require.NoError(t, database.WithRotaPreallocationLock(ctx, []string{rota.ID}, func(store db.PreallocationTxStore) error {
		deleted, err := store.DeletePreallocationByID(ctx, volPin.ID)
		require.NoError(t, err)
		assert.True(t, deleted)
		deleted, err = store.DeletePreallocationByID(ctx, volPin.ID)
		require.NoError(t, err)
		assert.False(t, deleted, "deleting an already-gone pin reports false")
		return nil
	}))

	// Only the custom pin remains.
	pins, err = database.GetPreallocationsByShiftIDs(ctx, []string{shiftA.ID})
	require.NoError(t, err)
	require.Len(t, pins, 1)
	assert.Equal(t, customPin.ID, pins[0].ID)

	// Empty id set is a no-op.
	pins, err = database.GetPreallocationsByShiftIDs(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, pins)
}

// TestPreallocationFrozenAfterAllocation pins the frozen guard: once
// InsertAllocationsAndSetAllocated marks the rota allocated, the lock's
// RotaAllocated read observes true, which the service layer turns into a
// rejection.
func TestPreallocationFrozenAfterAllocation(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	rota := &db.Rotation{ID: uuid.New().String()}
	shift := dbtest.Shift(rota.ID, "2026-08-02")
	require.NoError(t, database.InsertDefinedRota(ctx, rota, []db.Shift{shift}, nil))

	require.NoError(t, database.WithRotaPreallocationLock(ctx, []string{rota.ID}, func(store db.PreallocationTxStore) error {
		allocated, err := store.RotaAllocated(ctx, rota.ID)
		require.NoError(t, err)
		assert.False(t, allocated)
		return nil
	}))

	require.NoError(t, database.InsertAllocationsAndSetAllocated(ctx,
		[]db.Allocation{{ID: uuid.New().String(), ShiftID: shift.ID, Role: "Service volunteer", VolunteerID: "alice"}},
		rota.ID, time.Now()))

	require.NoError(t, database.WithRotaPreallocationLock(ctx, []string{rota.ID}, func(store db.PreallocationTxStore) error {
		allocated, err := store.RotaAllocated(ctx, rota.ID)
		require.NoError(t, err)
		assert.True(t, allocated, "an allocated rota's preallocation set is frozen")
		return nil
	}))
}

// TestPreallocationUnknownShiftIDFails checks the shift_id FK rejects a
// pin referencing a non-existent shift, rolling the locking transaction back.
func TestPreallocationUnknownShiftIDFails(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	rota := &db.Rotation{ID: uuid.New().String()}
	require.NoError(t, database.InsertDefinedRota(ctx, rota, []db.Shift{
		dbtest.Shift(rota.ID, "2026-08-02"),
	}, nil))

	err := database.WithRotaPreallocationLock(ctx, []string{rota.ID}, func(store db.PreallocationTxStore) error {
		return store.InsertPreallocation(ctx, db.Preallocation{
			ID: uuid.New().String(), ShiftID: uuid.New().String(), Role: "Service volunteer", VolunteerID: "alice",
		})
	})
	require.Error(t, err, "an unknown ShiftID must be rejected by the FK")
}
