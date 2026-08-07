package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// mockUpdateShiftStore implements UpdateShiftStore. WithRotaShiftLock records
// the lock it was asked for and hands the mock itself to the callback as the
// transaction-bound store, mirroring the WithRotaLock mocks elsewhere.
//
// dateTaken is the one-Shift-per-date index standing in: a start landing on one
// of these dates is refused the way the real index refuses it.
type mockUpdateShiftStore struct {
	shifts    []db.ShiftInRange
	dateTaken map[string]bool

	lockedRotaIDs [][]string
	writes        []bool   // closed values written, in order
	timeWrites    []string // "start..end" pairs written, in order
}

func (m *mockUpdateShiftStore) GetShiftByID(_ context.Context, id string) (*db.ShiftInRange, error) {
	for i := range m.shifts {
		if m.shifts[i].ID == id {
			return &m.shifts[i], nil
		}
	}
	return nil, nil
}

func (m *mockUpdateShiftStore) WithRotaShiftLock(_ context.Context, rotaIDs []string, fn func(db.ShiftTxStore) error) error {
	m.lockedRotaIDs = append(m.lockedRotaIDs, rotaIDs)
	return fn(m)
}

func (m *mockUpdateShiftStore) RotaAllocated(_ context.Context, rotaID string) (bool, error) {
	for _, s := range m.shifts {
		if s.RotaID == rotaID {
			return s.Allocated, nil
		}
	}
	return false, nil
}

func (m *mockUpdateShiftStore) SetShiftClosed(_ context.Context, shiftID string, closed bool) (bool, error) {
	for i := range m.shifts {
		if m.shifts[i].ID == shiftID {
			m.shifts[i].Closed = closed
			m.writes = append(m.writes, closed)
			return true, nil
		}
	}
	return false, nil
}

func (m *mockUpdateShiftStore) SetShiftTimes(_ context.Context, shiftID, startAt, endAt string) (bool, error) {
	if m.dateTaken[startAt[:len("2006-01-02")]] {
		return false, db.ErrShiftDateTaken
	}
	for i := range m.shifts {
		if m.shifts[i].ID == shiftID {
			m.shifts[i].StartAt, m.shifts[i].EndAt = startAt, endAt
			m.timeWrites = append(m.timeWrites, startAt+".."+endAt)
			return true, nil
		}
	}
	return false, nil
}

func unallocatedShiftStore() *mockUpdateShiftStore {
	return &mockUpdateShiftStore{shifts: []db.ShiftInRange{{Shift: db.Shift{
		ID:      "shift-1",
		Date:    "2026-12-27",
		RotaID:  "rota-1",
		StartAt: "2026-12-27T19:30:00",
		EndAt:   "2026-12-27T21:30:00",
	}}}}
}

func closure(closed bool) UpdateShiftParams { return UpdateShiftParams{Closed: &closed} }

func TestUpdateShift_ClosesAnUnallocatedShift(t *testing.T) {
	store := unallocatedShiftStore()

	shift, err := UpdateShift(context.Background(), store, "shift-1", closure(true), zap.NewNop())
	require.NoError(t, err)
	assert.Equal(t, &ShiftState{
		ID:      "shift-1",
		Date:    "2026-12-27",
		StartAt: "2026-12-27T19:30:00",
		EndAt:   "2026-12-27T21:30:00",
		Closed:  true,
	}, shift)
	assert.Equal(t, []bool{true}, store.writes)
	// The write serialises against allocation of the rota it belongs to.
	assert.Equal(t, [][]string{{"rota-1"}}, store.lockedRotaIDs)
}

func TestUpdateShift_Reopens(t *testing.T) {
	store := unallocatedShiftStore()
	store.shifts[0].Closed = true

	shift, err := UpdateShift(context.Background(), store, "shift-1", closure(false), zap.NewNop())
	require.NoError(t, err)
	assert.False(t, shift.Closed)
	assert.Equal(t, []bool{false}, store.writes)
}

// Being Closed is an allocator input, so it is frozen once the rota has been
// solved around it — unlike a shift's times, which stay editable.
func TestUpdateShift_ClosureRefusedOnAnAllocatedRota(t *testing.T) {
	store := unallocatedShiftStore()
	store.shifts[0].Allocated = true

	_, err := UpdateShift(context.Background(), store, "shift-1", closure(true), zap.NewNop())
	require.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "already been allocated")
	assert.Empty(t, store.writes, "nothing is written when the change is refused")
}

func TestUpdateShift_ReopeningAnAllocatedShiftIsRefusedToo(t *testing.T) {
	store := unallocatedShiftStore()
	store.shifts[0].Allocated = true
	store.shifts[0].Closed = true

	_, err := UpdateShift(context.Background(), store, "shift-1", closure(false), zap.NewNop())
	require.ErrorIs(t, err, ErrConflict)
	assert.Empty(t, store.writes)
}

func TestUpdateShift_UnknownShift(t *testing.T) {
	store := unallocatedShiftStore()

	_, err := UpdateShift(context.Background(), store, "ghost", closure(true), zap.NewNop())
	require.ErrorIs(t, err, ErrNotFound)
	assert.Empty(t, store.lockedRotaIDs, "an unknown shift resolves to no rota to lock")
}

// The caller asked for a state and the shift is in it, so there is nothing to
// disagree with — a client that sends the same PATCH twice has not gone wrong.
func TestUpdateShift_AlreadyInThatStateSucceeds(t *testing.T) {
	store := unallocatedShiftStore()
	store.shifts[0].Closed = true

	shift, err := UpdateShift(context.Background(), store, "shift-1", closure(true), zap.NewNop())
	require.NoError(t, err)
	assert.True(t, shift.Closed)
}

// An empty edit is refused rather than treated as a no-op: a client that sent
// one meant to change something and did not say what.
func TestUpdateShift_NothingToChange(t *testing.T) {
	store := unallocatedShiftStore()

	_, err := UpdateShift(context.Background(), store, "shift-1", UpdateShiftParams{}, zap.NewNop())
	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Empty(t, store.lockedRotaIDs)
}

func TestUpdateShift_MovesTheTimes(t *testing.T) {
	store := unallocatedShiftStore()

	shift, err := UpdateShift(context.Background(), store, "shift-1", UpdateShiftParams{
		StartAt: "2026-12-27T18:00:00",
		EndAt:   "2026-12-27T20:00:00",
	}, zap.NewNop())
	require.NoError(t, err)
	assert.Equal(t, "2026-12-27T18:00:00", shift.StartAt)
	assert.Equal(t, "2026-12-27T20:00:00", shift.EndAt)
	assert.Equal(t, []string{"2026-12-27T18:00:00..2026-12-27T20:00:00"}, store.timeWrites)
	assert.Empty(t, store.writes, "the closed flag is left alone")
}

// A Shift's date is the date of its start, so moving the start moves the Shift.
func TestUpdateShift_MovingTheStartMovesTheDate(t *testing.T) {
	store := unallocatedShiftStore()

	shift, err := UpdateShift(context.Background(), store, "shift-1", UpdateShiftParams{
		StartAt: "2026-12-30T19:30:00",
		EndAt:   "2026-12-30T21:30:00",
	}, zap.NewNop())
	require.NoError(t, err)
	assert.Equal(t, "2026-12-30", shift.Date)
}

// Times are descriptive rather than an allocator input, so unlike a closure
// they stay editable after the rota has been allocated (ADR 0007).
func TestUpdateShift_TimesStayEditableAfterAllocation(t *testing.T) {
	store := unallocatedShiftStore()
	store.shifts[0].Allocated = true

	_, err := UpdateShift(context.Background(), store, "shift-1", UpdateShiftParams{
		StartAt: "2026-12-27T18:00:00",
		EndAt:   "2026-12-27T20:00:00",
	}, zap.NewNop())
	require.NoError(t, err)
	assert.Len(t, store.timeWrites, 1)
}

// The browser's datetime-local field leaves the seconds off, so both spellings
// are read and what reaches the store is the one the column holds.
func TestUpdateShift_AcceptsTimesWithoutSeconds(t *testing.T) {
	store := unallocatedShiftStore()

	shift, err := UpdateShift(context.Background(), store, "shift-1", UpdateShiftParams{
		StartAt: "2026-12-27T18:00",
		EndAt:   "2026-12-27T20:00",
	}, zap.NewNop())
	require.NoError(t, err)
	assert.Equal(t, "2026-12-27T18:00:00", shift.StartAt)
	assert.Equal(t, []string{"2026-12-27T18:00:00..2026-12-27T20:00:00"}, store.timeWrites)
}

func TestUpdateShift_TimesMustComeInPairs(t *testing.T) {
	store := unallocatedShiftStore()

	_, err := UpdateShift(context.Background(), store, "shift-1",
		UpdateShiftParams{StartAt: "2026-12-27T18:00:00"}, zap.NewNop())
	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Contains(t, err.Error(), "end")
	assert.Empty(t, store.timeWrites)
}

func TestUpdateShift_TimesMustBeReadable(t *testing.T) {
	store := unallocatedShiftStore()

	_, err := UpdateShift(context.Background(), store, "shift-1",
		UpdateShiftParams{StartAt: "half seven", EndAt: "2026-12-27T20:00:00"}, zap.NewNop())
	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Contains(t, err.Error(), "half seven")

	// An offset is not a spelling of these: a Shift's times are wall-clock
	// facts about Ilford, and one carrying a zone means something else.
	_, err = UpdateShift(context.Background(), store, "shift-1",
		UpdateShiftParams{StartAt: "2026-12-27T18:00:00Z", EndAt: "2026-12-27T20:00:00Z"}, zap.NewNop())
	require.ErrorIs(t, err, ErrInvalidInput)
}

func TestUpdateShift_MustEndAfterItStarts(t *testing.T) {
	store := unallocatedShiftStore()

	_, err := UpdateShift(context.Background(), store, "shift-1", UpdateShiftParams{
		StartAt: "2026-12-27T21:30:00",
		EndAt:   "2026-12-27T19:30:00",
	}, zap.NewNop())
	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Contains(t, err.Error(), "end after it starts")
	assert.Empty(t, store.timeWrites)
}

// Moving a Shift onto a day the drop-in already runs is a conflict, and the
// message names the day rather than reporting a broken index.
func TestUpdateShift_RefusesADateAnotherShiftHolds(t *testing.T) {
	store := unallocatedShiftStore()
	store.dateTaken = map[string]bool{"2027-01-03": true}

	_, err := UpdateShift(context.Background(), store, "shift-1", UpdateShiftParams{
		StartAt: "2027-01-03T19:30:00",
		EndAt:   "2027-01-03T21:30:00",
	}, zap.NewNop())
	require.ErrorIs(t, err, ErrConflict)
	assert.Contains(t, err.Error(), "3 January 2027")
}

// One edit, one transaction: a time change the index refuses reports itself
// rather than being swallowed, and the transaction the real store runs this in
// is what takes the closure beside it back out.
func TestUpdateShift_ARefusedTimeSinksTheWholeEdit(t *testing.T) {
	store := unallocatedShiftStore()
	store.dateTaken = map[string]bool{"2027-01-03": true}
	closed := true

	_, err := UpdateShift(context.Background(), store, "shift-1", UpdateShiftParams{
		Closed:  &closed,
		StartAt: "2027-01-03T19:30:00",
		EndAt:   "2027-01-03T21:30:00",
	}, zap.NewNop())
	require.ErrorIs(t, err, ErrConflict)
	assert.Empty(t, store.timeWrites)
}
