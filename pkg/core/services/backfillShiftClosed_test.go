package services

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// mockBackfillClosedStore implements BackfillShiftClosedStore, recording every
// write so a run's effect can be read off the mock rather than inferred from
// the result.
type mockBackfillClosedStore struct {
	shifts []db.ShiftInRange
	closed []string // shift ids written, in order
}

func (m *mockBackfillClosedStore) GetShiftsInRange(ctx context.Context, from, to time.Time) ([]db.ShiftInRange, error) {
	return m.shifts, nil
}

func (m *mockBackfillClosedStore) SetShiftClosed(ctx context.Context, shiftID string, closed bool) (bool, error) {
	m.closed = append(m.closed, shiftID)
	for i := range m.shifts {
		if m.shifts[i].ID == shiftID {
			m.shifts[i].Closed = closed
			return true, nil
		}
	}
	return false, nil
}

func backfillShift(id, date string, allocated, closed bool) db.ShiftInRange {
	return db.ShiftInRange{
		Shift:     db.Shift{ID: id, Date: date, RotaID: "rota-1", Closed: closed},
		Allocated: allocated,
	}
}

// The rules close Christmas Day 2026, which falls on a Friday; the shifts
// either side of it are ordinary Sundays.
const christmasRRule = "FREQ=YEARLY;BYMONTH=12;BYMONTHDAY=25"

func TestBackfillShiftClosedStampsMatchingDates(t *testing.T) {
	store := &mockBackfillClosedStore{shifts: []db.ShiftInRange{
		backfillShift("s1", "2026-12-20", false, false),
		backfillShift("s2", "2026-12-25", false, false),
		backfillShift("s3", "2026-12-27", false, false),
	}}
	cfg := &config.Config{RotaOverrides: []config.RotaOverride{
		{RRule: christmasRRule, Closed: true},
	}}

	result, err := BackfillShiftClosed(context.Background(), store, cfg, false, zap.NewNop())
	require.NoError(t, err)

	assert.Equal(t, 3, result.Scanned)
	assert.Equal(t, []string{"2026-12-25"}, result.Closed)
	assert.Equal(t, []string{"s2"}, store.closed, "only the matching shift is written")
}

// An allocated rota is backfilled too: the freeze stops an admin changing an
// allocator input, but history has to keep saying the drop-in was shut.
func TestBackfillShiftClosedStampsAllocatedRotas(t *testing.T) {
	store := &mockBackfillClosedStore{shifts: []db.ShiftInRange{
		backfillShift("s1", "2026-12-25", true, false),
	}}
	cfg := &config.Config{RotaOverrides: []config.RotaOverride{
		{RRule: christmasRRule, Closed: true},
	}}

	result, err := BackfillShiftClosed(context.Background(), store, cfg, false, zap.NewNop())
	require.NoError(t, err)

	assert.Equal(t, []string{"2026-12-25"}, result.Closed)
	assert.Equal(t, []string{"s1"}, store.closed)
}

// Re-running is the expected way to use it, so a second run must be a no-op
// rather than a second round of writes.
func TestBackfillShiftClosedIsRerunnable(t *testing.T) {
	store := &mockBackfillClosedStore{shifts: []db.ShiftInRange{
		backfillShift("s1", "2026-12-25", false, false),
	}}
	cfg := &config.Config{RotaOverrides: []config.RotaOverride{
		{RRule: christmasRRule, Closed: true},
	}}

	_, err := BackfillShiftClosed(context.Background(), store, cfg, false, zap.NewNop())
	require.NoError(t, err)

	second, err := BackfillShiftClosed(context.Background(), store, cfg, false, zap.NewNop())
	require.NoError(t, err)
	assert.Empty(t, second.Closed)
	assert.Equal(t, 1, second.AlreadyClosed)
	assert.Len(t, store.closed, 1, "the second run writes nothing")
}

// It only ever closes. A shift closed by hand stays closed even though no rule
// matches it — the rules stop being the authority the moment this has run.
func TestBackfillShiftClosedNeverReopens(t *testing.T) {
	store := &mockBackfillClosedStore{shifts: []db.ShiftInRange{
		backfillShift("s1", "2026-12-27", false, true),
	}}
	cfg := &config.Config{RotaOverrides: []config.RotaOverride{
		{RRule: christmasRRule, Closed: true},
	}}

	result, err := BackfillShiftClosed(context.Background(), store, cfg, false, zap.NewNop())
	require.NoError(t, err)
	assert.Empty(t, result.Closed)
	assert.Empty(t, store.closed)
	assert.True(t, store.shifts[0].Closed)
}

// An override that pins someone but does not close is not a closure, however
// well its rrule matches.
func TestBackfillShiftClosedIgnoresOpenOverrides(t *testing.T) {
	store := &mockBackfillClosedStore{shifts: []db.ShiftInRange{
		backfillShift("s1", "2026-12-25", false, false),
	}}
	cfg := &config.Config{RotaOverrides: []config.RotaOverride{
		{RRule: christmasRRule, Preallocations: []config.Preallocation{{VolunteerID: "v1", Role: "Team Lead"}}},
	}}

	result, err := BackfillShiftClosed(context.Background(), store, cfg, false, zap.NewNop())
	require.NoError(t, err)
	assert.Empty(t, result.Closed)
	assert.Empty(t, store.closed)
}

func TestBackfillShiftClosedDryRunWritesNothing(t *testing.T) {
	store := &mockBackfillClosedStore{shifts: []db.ShiftInRange{
		backfillShift("s1", "2026-12-25", false, false),
	}}
	cfg := &config.Config{RotaOverrides: []config.RotaOverride{
		{RRule: christmasRRule, Closed: true},
	}}

	result, err := BackfillShiftClosed(context.Background(), store, cfg, true, zap.NewNop())
	require.NoError(t, err)
	assert.Equal(t, []string{"2026-12-25"}, result.Closed, "a dry run still reports what it would close")
	assert.Empty(t, store.closed)
}

// A closure that cannot be evaluated fails the run: silently skipping it would
// leave a shift open with nothing left afterwards to say it should not be.
func TestBackfillShiftClosedFailsOnBadRRule(t *testing.T) {
	store := &mockBackfillClosedStore{shifts: []db.ShiftInRange{
		backfillShift("s1", "2026-12-25", false, false),
	}}
	cfg := &config.Config{RotaOverrides: []config.RotaOverride{
		{RRule: "NOT AN RRULE", Closed: true},
	}}

	_, err := BackfillShiftClosed(context.Background(), store, cfg, false, zap.NewNop())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid rrule")
}
