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
	shift1 := dbtest.Shift(rota1.ID, "2026-08-02")
	require.NoError(t, database.InsertDefinedRota(ctx, rota1, []db.Shift{
		shift1,
		dbtest.Shift(rota1.ID, "2026-08-09"),
	}, nil))
	require.NoError(t, database.InsertAllocationsAndSetAllocated(ctx,
		[]db.Allocation{{ID: uuid.New().String(), ShiftID: shift1.ID, Role: "team-lead", VolunteerID: "alice"}},
		rota1.ID, time.Now()))

	rota2 := &db.Rotation{ID: uuid.New().String()}
	require.NoError(t, database.InsertDefinedRota(ctx, rota2, []db.Shift{
		dbtest.Shift(rota2.ID, "2026-08-16"),
	}, nil))

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
	shiftA := dbtest.Shift(rota.ID, "2026-08-02")
	shiftB := dbtest.Shift(rota.ID, "2026-08-09")
	require.NoError(t, database.InsertDefinedRota(ctx, rota, []db.Shift{shiftA, shiftB}, nil))
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
	require.NoError(t, database.InsertDefinedRota(ctx, rota, []db.Shift{
		dbtest.Shift(rota.ID, "2026-08-02"),
	}, nil))

	err := database.InsertAllocationsAndSetAllocated(ctx, []db.Allocation{
		{ID: uuid.New().String(), ShiftID: uuid.New().String(), Role: "volunteer", VolunteerID: "alice"},
	}, rota.ID, time.Now())
	require.Error(t, err, "an unknown ShiftID must be rejected by the FK")

	rotations, err := database.GetRotations(ctx)
	require.NoError(t, err)
	require.Len(t, rotations, 1)
	assert.Empty(t, rotations[0].AllocatedDatetime, "the failed insert must not mark the rota allocated")
}

// TestShiftClosedRoundTrips checks the closed flag through every read that
// carries it: a Shift is minted open, WithRotaShiftLock writes the flag, and
// each of the three shift reads reports what was written (issue #132).
func TestShiftClosedRoundTrips(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	rota := &db.Rotation{ID: uuid.New().String()}
	shift := dbtest.Shift(rota.ID, "2026-12-27")
	require.NoError(t, database.InsertDefinedRota(ctx, rota, []db.Shift{shift}, nil))

	minted, err := database.GetShiftByID(ctx, shift.ID)
	require.NoError(t, err)
	require.NotNil(t, minted)
	assert.False(t, minted.Closed, "a shift is minted open")
	assert.False(t, minted.Allocated)

	require.NoError(t, database.WithRotaShiftLock(ctx, []string{rota.ID}, func(tx db.ShiftTxStore) error {
		updated, err := tx.SetShiftClosed(ctx, shift.ID, true)
		require.NoError(t, err)
		assert.True(t, updated)
		return nil
	}))

	byID, err := database.GetShiftByID(ctx, shift.ID)
	require.NoError(t, err)
	assert.True(t, byID.Closed)

	byRota, err := database.GetShiftsByRotaID(ctx, rota.ID)
	require.NoError(t, err)
	require.Len(t, byRota, 1)
	assert.True(t, byRota[0].Closed)

	inRange, err := database.GetShiftsInRange(ctx, time.Time{}, time.Time{})
	require.NoError(t, err)
	require.Len(t, inRange, 1)
	assert.True(t, inRange[0].Closed)

	byDate, err := database.GetShiftByDate(ctx, time.Date(2026, 12, 27, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.NotNil(t, byDate)
	assert.True(t, byDate.Closed)
}

// TestGetShiftByIDUnknownReturnsNil keeps "no such shift" distinguishable from
// a failure, which is what lets the close/reopen flow answer 404 rather than
// 500 for an id that never existed.
func TestGetShiftByIDUnknownReturnsNil(t *testing.T) {
	database, _ := dbtest.New(t)

	shift, err := database.GetShiftByID(context.Background(), uuid.New().String())
	require.NoError(t, err)
	assert.Nil(t, shift)
}

// TestShiftDateUniqueRejectsOverlappingRotas pins the concurrency role of the
// one-Shift-per-date unique index (issue #41, hazard B1): two rotas minting the
// same shift date cannot both commit. The index exists for ADR 0001 reasons,
// but it is also the only thing making concurrent DefineRota runs safe — the
// losing insert fails wholesale, writing neither the rotation nor its
// non-overlapping shifts. If a schema change ever relaxes it (e.g. multiple
// shifts per day), this test flags that the define-rota race needs a
// replacement guard.
//
// The index moved from the stored date column onto `start_at::date` in the
// contract phase (issue #135, ADR 0007); the guarantee it gives here did not.
func TestShiftDateUniqueRejectsOverlappingRotas(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	rota1 := &db.Rotation{ID: uuid.New().String()}
	require.NoError(t, database.InsertDefinedRota(ctx, rota1, []db.Shift{
		dbtest.Shift(rota1.ID, "2026-08-02"),
		dbtest.Shift(rota1.ID, "2026-08-09"),
	}, nil))

	// The second rota overlaps rota1 on one date only; the shared date must
	// sink the whole insert, including the non-overlapping shift.
	rota2 := &db.Rotation{ID: uuid.New().String()}
	err := database.InsertDefinedRota(ctx, rota2, []db.Shift{
		dbtest.Shift(rota2.ID, "2026-08-09"),
		dbtest.Shift(rota2.ID, "2026-08-16"),
	}, nil)
	require.Error(t, err)

	rotations, err := database.GetRotations(ctx)
	require.NoError(t, err)
	require.Len(t, rotations, 1, "losing rota must not be committed")
	assert.Equal(t, rota1.ID, rotations[0].ID)
	assert.Equal(t, 2, rotations[0].ShiftCount, "winning rota's shifts must be untouched")
}

// TestShiftDateComesFromStartAt pins the contract phase (issue #135, ADR 0007):
// a Shift's date is the date of its start, and there is nothing else it could
// be — the stored column is gone. Setting Date on the way in is ignored, which
// is what these two shifts prove: each carries a Date naming a different day
// from its start, and every read answers with the start's.
//
// They are also inserted in the reverse of their date order, so ordering alone
// catches a read taking the rows as the planner hands them back.
func TestShiftDateComesFromStartAt(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	rota := &db.Rotation{ID: uuid.New().String()}
	later := db.Shift{
		ID:      uuid.New().String(),
		Date:    "2026-08-02",
		RotaID:  rota.ID,
		StartAt: "2026-08-16T19:30:00",
		EndAt:   "2026-08-16T21:30:00",
	}
	earlier := db.Shift{
		ID:      uuid.New().String(),
		Date:    "2026-08-30",
		RotaID:  rota.ID,
		StartAt: "2026-08-09T19:30:00",
		EndAt:   "2026-08-09T21:30:00",
	}
	require.NoError(t, database.InsertDefinedRota(ctx, rota, []db.Shift{later, earlier}, nil))

	byRota, err := database.GetShiftsByRotaID(ctx, rota.ID)
	require.NoError(t, err)
	require.Len(t, byRota, 2)
	assert.Equal(t, []string{"2026-08-09", "2026-08-16"},
		[]string{byRota[0].Date, byRota[1].Date}, "dates and order both follow start_at")

	inRange, err := database.GetShiftsInRange(ctx, time.Time{}, time.Time{})
	require.NoError(t, err)
	require.Len(t, inRange, 2)
	assert.Equal(t, []string{"2026-08-09", "2026-08-16"},
		[]string{inRange[0].Date, inRange[1].Date})

	// Bounding the range covers only the derived date: the later shift's own
	// Date said 2 August, which is outside this window.
	bounded, err := database.GetShiftsInRange(ctx,
		time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Len(t, bounded, 1)
	assert.Equal(t, later.ID, bounded[0].ID)

	byID, err := database.GetShiftByID(ctx, later.ID)
	require.NoError(t, err)
	require.NotNil(t, byID)
	assert.Equal(t, "2026-08-16", byID.Date)

	// The lookup that resolves a date to its shift answers on the start's date
	// and is silent on the one that was handed in.
	found, err := database.GetShiftByDate(ctx, time.Date(2026, 8, 16, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, later.ID, found.ID)

	missing, err := database.GetShiftByDate(ctx, time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	assert.Nil(t, missing, "a Date handed in is not what a date resolves against")

	// A rotation's span is derived from its shifts, so it moves with them.
	rotations, err := database.GetRotations(ctx)
	require.NoError(t, err)
	require.Len(t, rotations, 1)
	assert.Equal(t, "2026-08-09", rotations[0].Start)
	assert.Equal(t, "2026-08-16", rotations[0].End)
}

// TestShiftTimesRoundTrip checks a Shift's own start and end through every read
// that carries them (issue #133). They are wall-clock local times, so what goes
// in is what comes back with no zone applied to it anywhere — the test runs
// against a summer date, where a timestamptz round trip through UTC would show
// up as an hour's drift.
func TestShiftTimesRoundTrip(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	rota := &db.Rotation{ID: uuid.New().String()}
	shift := db.Shift{
		ID:      uuid.New().String(),
		RotaID:  rota.ID,
		StartAt: "2026-07-12T19:30:00",
		EndAt:   "2026-07-12T21:30:00",
	}
	require.NoError(t, database.InsertDefinedRota(ctx, rota, []db.Shift{shift}, nil))

	byID, err := database.GetShiftByID(ctx, shift.ID)
	require.NoError(t, err)
	require.NotNil(t, byID)
	assert.Equal(t, "2026-07-12T19:30:00", byID.StartAt)
	assert.Equal(t, "2026-07-12T21:30:00", byID.EndAt)

	byRota, err := database.GetShiftsByRotaID(ctx, rota.ID)
	require.NoError(t, err)
	require.Len(t, byRota, 1)
	assert.Equal(t, "2026-07-12T19:30:00", byRota[0].StartAt)
	assert.Equal(t, "2026-07-12T21:30:00", byRota[0].EndAt)

	byDate, err := database.GetShiftByDate(ctx, time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.NotNil(t, byDate)
	assert.Equal(t, "2026-07-12T19:30:00", byDate.StartAt)

	inRange, err := database.GetShiftsInRange(ctx, time.Time{}, time.Time{})
	require.NoError(t, err)
	require.Len(t, inRange, 1)
	assert.Equal(t, "2026-07-12T19:30:00", inRange[0].StartAt)
	assert.Equal(t, "2026-07-12T21:30:00", inRange[0].EndAt)
}

// The database refuses the three states a Shift's times have no meaning in: not
// set at all, half set, and ending before it starts. All three are constraints
// rather than checks in the app, so no writer can reintroduce them.
//
// "Not set at all" was an ordinary state through the expand phase — a Shift
// minted while the settings were empty — and stopped being one here: a Shift
// with no start has no date either (issue #135).
func TestShiftTimesConstraints(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	untimed := &db.Rotation{ID: uuid.New().String()}
	err := database.InsertDefinedRota(ctx, untimed, []db.Shift{
		{ID: uuid.New().String(), Date: "2026-07-12", RotaID: untimed.ID},
	}, nil)
	require.Error(t, err, "a shift with no times must be rejected")

	halfSet := &db.Rotation{ID: uuid.New().String()}
	err = database.InsertDefinedRota(ctx, halfSet, []db.Shift{
		{ID: uuid.New().String(), RotaID: halfSet.ID, StartAt: "2026-07-12T19:30:00"},
	}, nil)
	require.Error(t, err, "a start with no end must be rejected")

	backwards := &db.Rotation{ID: uuid.New().String()}
	err = database.InsertDefinedRota(ctx, backwards, []db.Shift{
		{
			ID:      uuid.New().String(),
			RotaID:  backwards.ID,
			StartAt: "2026-07-12T21:30:00",
			EndAt:   "2026-07-12T19:30:00",
		},
	}, nil)
	require.Error(t, err, "an end before the start must be rejected")
}

// TestSetShiftTimes moves a Shift onto another evening and onto another day
// entirely, and checks the date moves with it. Times are descriptive rather
// than an allocator input (ADR 0007), so the rota being allocated is no bar —
// which is what the allocation here is for.
func TestSetShiftTimes(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	rota := &db.Rotation{ID: uuid.New().String()}
	shift := dbtest.Shift(rota.ID, "2026-08-02")
	require.NoError(t, database.InsertDefinedRota(ctx, rota, []db.Shift{shift}, nil))
	require.NoError(t, database.InsertAllocationsAndSetAllocated(ctx,
		[]db.Allocation{{ID: uuid.New().String(), ShiftID: shift.ID, Role: "Team lead", VolunteerID: "alice"}},
		rota.ID, time.Now()))

	require.NoError(t, database.WithRotaShiftLock(ctx, []string{rota.ID}, func(tx db.ShiftTxStore) error {
		updated, err := tx.SetShiftTimes(ctx, shift.ID, "2026-08-05T18:00:00", "2026-08-05T20:00:00")
		require.NoError(t, err)
		assert.True(t, updated)
		return nil
	}))

	moved, err := database.GetShiftByID(ctx, shift.ID)
	require.NoError(t, err)
	require.NotNil(t, moved)
	assert.Equal(t, "2026-08-05T18:00:00", moved.StartAt)
	assert.Equal(t, "2026-08-05T20:00:00", moved.EndAt)
	assert.Equal(t, "2026-08-05", moved.Date, "the date follows the start")

	// The rotation's span is derived from its shifts, so it moved too.
	rotations, err := database.GetRotations(ctx)
	require.NoError(t, err)
	require.Len(t, rotations, 1)
	assert.Equal(t, "2026-08-05", rotations[0].Start)

	// An id nothing matches is reported as such rather than as a failure, which
	// is what lets the caller answer 404.
	require.NoError(t, database.WithRotaShiftLock(ctx, []string{rota.ID}, func(tx db.ShiftTxStore) error {
		updated, err := tx.SetShiftTimes(ctx, uuid.New().String(), "2026-09-06T19:30:00", "2026-09-06T21:30:00")
		require.NoError(t, err)
		assert.False(t, updated)
		return nil
	}))
}

// Moving a Shift onto a day another Shift already starts on is refused by the
// one-Shift-per-date index, and comes back named rather than as a driver error
// code — including when the Shift in the way belongs to another rota, which no
// read taken under this rota's lock could have seen.
func TestSetShiftTimesRejectsTakenDate(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	rota := &db.Rotation{ID: uuid.New().String()}
	first := dbtest.Shift(rota.ID, "2026-08-02")
	second := dbtest.Shift(rota.ID, "2026-08-09")
	require.NoError(t, database.InsertDefinedRota(ctx, rota, []db.Shift{first, second}, nil))

	other := &db.Rotation{ID: uuid.New().String()}
	require.NoError(t, database.InsertDefinedRota(ctx, other, []db.Shift{
		dbtest.Shift(other.ID, "2026-08-16"),
	}, nil))

	err := database.WithRotaShiftLock(ctx, []string{rota.ID}, func(tx db.ShiftTxStore) error {
		_, err := tx.SetShiftTimes(ctx, second.ID, "2026-08-02T18:00:00", "2026-08-02T20:00:00")
		return err
	})
	require.ErrorIs(t, err, db.ErrShiftDateTaken)

	err = database.WithRotaShiftLock(ctx, []string{rota.ID}, func(tx db.ShiftTxStore) error {
		_, err := tx.SetShiftTimes(ctx, second.ID, "2026-08-16T18:00:00", "2026-08-16T20:00:00")
		return err
	})
	require.ErrorIs(t, err, db.ErrShiftDateTaken, "a clash in another rota is still a clash")

	// The refusal left the shift where it was.
	unmoved, err := database.GetShiftByID(ctx, second.ID)
	require.NoError(t, err)
	assert.Equal(t, "2026-08-09", unmoved.Date)

	// Moving it onto its own date is not a clash: the index sees one row.
	require.NoError(t, database.WithRotaShiftLock(ctx, []string{rota.ID}, func(tx db.ShiftTxStore) error {
		_, err := tx.SetShiftTimes(ctx, second.ID, "2026-08-09T18:00:00", "2026-08-09T20:00:00")
		return err
	}))
}
