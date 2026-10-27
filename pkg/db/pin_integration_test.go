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

// TestPinInsertReadDelete exercises the pin lifecycle through
// the rota-pin lock: insert two pins on one shift, read them back
// scoped by shift id, resolve one by id to its shift and rota, then delete it
// (true), delete again (false), and confirm the empty-id read is a no-op.
func TestPinInsertReadDelete(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	dbtest.SeedRoles(t, database)
	roles, err := database.ListRoles(ctx)
	require.NoError(t, err)

	rota := &db.Rotation{ID: uuid.New().String()}
	shiftA := dbtest.Shift(rota.ID, "2026-08-02")
	shiftB := dbtest.Shift(rota.ID, "2026-08-09")
	require.NoError(t, database.InsertDefinedRota(ctx, rota, []db.Shift{shiftA, shiftB}, nil, nil))

	volPin := db.Pin{ID: uuid.New().String(), ShiftID: shiftA.ID, RoleID: roles[0].ID, VolunteerID: "alice"}
	customPin := db.Pin{ID: uuid.New().String(), ShiftID: shiftA.ID, RoleID: roles[1].ID, CustomValue: "External Org"}

	// Insert both pins under the lock.
	require.NoError(t, database.WithRotaPinLock(ctx, []string{rota.ID}, func(store db.PinTxStore) error {
		allocated, err := store.RotaAllocated(ctx, rota.ID)
		require.NoError(t, err)
		assert.False(t, allocated, "a freshly minted rota is not allocated")
		if err := store.InsertPin(ctx, volPin); err != nil {
			return err
		}
		return store.InsertPin(ctx, customPin)
	}))

	// Read back scoped by shift id: shiftA has both, shiftB has none.
	pins, err := database.GetPinsByShiftIDs(ctx, []string{shiftA.ID})
	require.NoError(t, err)
	require.Len(t, pins, 2)

	none, err := database.GetPinsByShiftIDs(ctx, []string{shiftB.ID})
	require.NoError(t, err)
	assert.Empty(t, none)

	// The nullable columns round-trip: the volunteer pin has no custom value and
	// vice versa.
	byID := map[string]db.Pin{}
	for _, p := range pins {
		byID[p.ID] = p
	}
	assert.Equal(t, "alice", byID[volPin.ID].VolunteerID)
	assert.Empty(t, byID[volPin.ID].CustomValue)
	assert.Equal(t, "External Org", byID[customPin.ID].CustomValue)
	assert.Empty(t, byID[customPin.ID].VolunteerID)

	// Resolve a pin by id to its shift and rota in one join.
	got, shift, err := database.GetPinByID(ctx, volPin.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, shift)
	assert.Equal(t, shiftA.ID, shift.ID)
	assert.Equal(t, rota.ID, shift.RotaID)
	assert.Equal(t, "2026-08-02", shift.Date)
	assert.Equal(t, "alice", got.VolunteerID)

	// An unknown id resolves to nil without error.
	missing, missingShift, err := database.GetPinByID(ctx, uuid.New().String())
	require.NoError(t, err)
	assert.Nil(t, missing)
	assert.Nil(t, missingShift)

	// Delete returns true first, false on the second attempt (concurrent-delete
	// signal).
	require.NoError(t, database.WithRotaPinLock(ctx, []string{rota.ID}, func(store db.PinTxStore) error {
		deleted, err := store.DeletePinByID(ctx, volPin.ID)
		require.NoError(t, err)
		assert.True(t, deleted)
		deleted, err = store.DeletePinByID(ctx, volPin.ID)
		require.NoError(t, err)
		assert.False(t, deleted, "deleting an already-gone pin reports false")
		return nil
	}))

	// Only the custom pin remains.
	pins, err = database.GetPinsByShiftIDs(ctx, []string{shiftA.ID})
	require.NoError(t, err)
	require.Len(t, pins, 1)
	assert.Equal(t, customPin.ID, pins[0].ID)

	// Empty id set is a no-op.
	pins, err = database.GetPinsByShiftIDs(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, pins)
}

// TestPinFrozenAfterAllocation pins the frozen guard: once
// InsertAllocationsAndSetAllocated marks the rota allocated, the lock's
// RotaAllocated read observes true, which the service layer turns into a
// rejection.
func TestPinFrozenAfterAllocation(t *testing.T) {
	database, _ := dbtest.New(t)
	roleIDs := dbtest.SeedRoles(t, database)
	ctx := context.Background()

	rota := &db.Rotation{ID: uuid.New().String()}
	shift := dbtest.Shift(rota.ID, "2026-08-02")
	require.NoError(t, database.InsertDefinedRota(ctx, rota, []db.Shift{shift}, nil, nil))

	require.NoError(t, database.WithRotaPinLock(ctx, []string{rota.ID}, func(store db.PinTxStore) error {
		allocated, err := store.RotaAllocated(ctx, rota.ID)
		require.NoError(t, err)
		assert.False(t, allocated)
		return nil
	}))

	require.NoError(t, database.InsertAllocationsAndSetAllocated(ctx,
		[]db.Allocation{{ID: uuid.New().String(), ShiftID: shift.ID, RoleID: roleIDs["Service volunteer"], VolunteerID: "alice"}},
		rota.ID, time.Now()))

	require.NoError(t, database.WithRotaPinLock(ctx, []string{rota.ID}, func(store db.PinTxStore) error {
		allocated, err := store.RotaAllocated(ctx, rota.ID)
		require.NoError(t, err)
		assert.True(t, allocated, "an allocated rota's pin set is frozen")
		return nil
	}))
}

// TestPinUnknownShiftIDFails checks the shift_id FK rejects a
// pin referencing a non-existent shift, rolling the locking transaction back.
func TestPinUnknownShiftIDFails(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	dbtest.SeedRoles(t, database)
	roles, err := database.ListRoles(ctx)
	require.NoError(t, err)

	rota := &db.Rotation{ID: uuid.New().String()}
	require.NoError(t, database.InsertDefinedRota(ctx, rota, []db.Shift{
		dbtest.Shift(rota.ID, "2026-08-02"),
	}, nil, nil))

	err = database.WithRotaPinLock(ctx, []string{rota.ID}, func(store db.PinTxStore) error {
		return store.InsertPin(ctx, db.Pin{
			ID: uuid.New().String(), ShiftID: uuid.New().String(), RoleID: roles[0].ID, VolunteerID: "alice",
		})
	})
	require.Error(t, err, "an unknown ShiftID must be rejected by the FK")
}
