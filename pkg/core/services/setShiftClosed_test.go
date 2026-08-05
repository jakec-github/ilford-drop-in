package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// mockSetShiftClosedStore implements SetShiftClosedStore. WithRotaShiftLock
// records the lock it was asked for and hands the mock itself to the callback
// as the transaction-bound store, mirroring the WithRotaLock mocks elsewhere.
type mockSetShiftClosedStore struct {
	shifts []db.ShiftInRange

	lockedRotaIDs [][]string
	writes        []bool // closed values written, in order
}

func (m *mockSetShiftClosedStore) GetShiftByID(_ context.Context, id string) (*db.ShiftInRange, error) {
	for i := range m.shifts {
		if m.shifts[i].ID == id {
			return &m.shifts[i], nil
		}
	}
	return nil, nil
}

func (m *mockSetShiftClosedStore) WithRotaShiftLock(_ context.Context, rotaIDs []string, fn func(db.ShiftTxStore) error) error {
	m.lockedRotaIDs = append(m.lockedRotaIDs, rotaIDs)
	return fn(m)
}

func (m *mockSetShiftClosedStore) RotaAllocated(_ context.Context, rotaID string) (bool, error) {
	for _, s := range m.shifts {
		if s.RotaID == rotaID {
			return s.Allocated, nil
		}
	}
	return false, nil
}

func (m *mockSetShiftClosedStore) SetShiftClosed(_ context.Context, shiftID string, closed bool) (bool, error) {
	for i := range m.shifts {
		if m.shifts[i].ID == shiftID {
			m.shifts[i].Closed = closed
			m.writes = append(m.writes, closed)
			return true, nil
		}
	}
	return false, nil
}

func unallocatedShiftStore() *mockSetShiftClosedStore {
	return &mockSetShiftClosedStore{shifts: []db.ShiftInRange{
		{Shift: db.Shift{ID: "shift-1", Date: "2026-12-27", RotaID: "rota-1"}},
	}}
}

func TestSetShiftClosed_ClosesAnUnallocatedShift(t *testing.T) {
	store := unallocatedShiftStore()

	closure, err := SetShiftClosed(context.Background(), store, "shift-1", true, zap.NewNop())
	require.NoError(t, err)
	assert.Equal(t, &ShiftClosure{ID: "shift-1", Date: "2026-12-27", Closed: true}, closure)
	assert.Equal(t, []bool{true}, store.writes)
	// The write serialises against allocation of the rota it belongs to.
	assert.Equal(t, [][]string{{"rota-1"}}, store.lockedRotaIDs)
}

func TestSetShiftClosed_Reopens(t *testing.T) {
	store := unallocatedShiftStore()
	store.shifts[0].Closed = true

	closure, err := SetShiftClosed(context.Background(), store, "shift-1", false, zap.NewNop())
	require.NoError(t, err)
	assert.False(t, closure.Closed)
	assert.Equal(t, []bool{false}, store.writes)
}

// Being Closed is an allocator input, so it is frozen once the rota has been
// solved around it — unlike a shift's times, which stay editable.
func TestSetShiftClosed_RefusedOnAnAllocatedRota(t *testing.T) {
	store := unallocatedShiftStore()
	store.shifts[0].Allocated = true

	_, err := SetShiftClosed(context.Background(), store, "shift-1", true, zap.NewNop())
	require.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "already been allocated")
	assert.Empty(t, store.writes, "nothing is written when the change is refused")
}

func TestSetShiftClosed_ReopeningAnAllocatedShiftIsRefusedToo(t *testing.T) {
	store := unallocatedShiftStore()
	store.shifts[0].Allocated = true
	store.shifts[0].Closed = true

	_, err := SetShiftClosed(context.Background(), store, "shift-1", false, zap.NewNop())
	require.ErrorIs(t, err, ErrConflict)
	assert.Empty(t, store.writes)
}

func TestSetShiftClosed_UnknownShift(t *testing.T) {
	store := unallocatedShiftStore()

	_, err := SetShiftClosed(context.Background(), store, "ghost", true, zap.NewNop())
	require.ErrorIs(t, err, ErrNotFound)
	assert.Empty(t, store.lockedRotaIDs, "an unknown shift resolves to no rota to lock")
}

// The caller asked for a state and the shift is in it, so there is nothing to
// disagree with — a client that sends the same PATCH twice has not gone wrong.
func TestSetShiftClosed_AlreadyInThatStateSucceeds(t *testing.T) {
	store := unallocatedShiftStore()
	store.shifts[0].Closed = true

	closure, err := SetShiftClosed(context.Background(), store, "shift-1", true, zap.NewNop())
	require.NoError(t, err)
	assert.True(t, closure.Closed)
}
