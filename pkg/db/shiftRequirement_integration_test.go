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

// A Shift's Shape is written with the Shift and read back in the order its
// Seats are filled, whatever order it was stated in.
func TestInsertDefinedRotaWritesShiftShapes(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	dbtest.SeedRoles(t, database)
	ids := roleIDsByName(t, database)

	rota := &db.Rotation{ID: uuid.New().String()}
	shift := dbtest.Shift(rota.ID, "2026-09-06")

	// Lowest priority first, so the ordering under test is the read's.
	require.NoError(t, database.InsertDefinedRota(ctx, rota, []db.Shift{shift}, nil, []db.ShiftRequirement{
		{ShiftID: shift.ID, RoleID: ids["Service volunteer"], Seats: 4},
		{ShiftID: shift.ID, RoleID: ids["Team lead"], Seats: 1},
	}))

	shapes, err := database.GetShiftShapes(ctx, []string{shift.ID})
	require.NoError(t, err)
	assert.Equal(t, map[string][]db.ShiftRequirement{
		shift.ID: {
			{ShiftID: shift.ID, RoleID: ids["Team lead"], Seats: 1},
			{ShiftID: shift.ID, RoleID: ids["Service volunteer"], Seats: 4},
		},
	}, shapes)
}

// The bug this table exists to close: editing the default Shape used to rewrite
// what every past Shift had asked for, because a Shift's Seats were resolved
// from the settings on every read. A Shift's Shape is now a copy taken when it
// was minted, and the settings cannot reach it.
func TestEditingTheDefaultShapeLeavesAnExistingShiftAlone(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	dbtest.SeedRoles(t, database)
	ids := roleIDsByName(t, database)

	rota := &db.Rotation{ID: uuid.New().String()}
	shift := dbtest.Shift(rota.ID, "2026-09-06")
	minted := []db.ShiftRequirement{
		{ShiftID: shift.ID, RoleID: ids["Team lead"], Seats: 1},
		{ShiftID: shift.ID, RoleID: ids["Service volunteer"], Seats: 4},
	}
	require.NoError(t, database.InsertDefinedRota(ctx, rota, []db.Shift{shift}, nil, minted))

	// An admin rethinks the settings entirely: fewer places, and no lead.
	require.NoError(t, database.SaveDefaultShape(ctx, []db.DefaultShapeSeat{
		{RoleID: ids["Service volunteer"], Seats: 2},
	}))

	shapes, err := database.GetShiftShapes(ctx, []string{shift.ID})
	require.NoError(t, err)
	assert.Equal(t, minted, shapes[shift.ID], "the shift still asks for what it was minted asking for")
}

// Each Shift's Shape is its own. Nothing here shares a row between two Shifts,
// which is the whole point of the table.
func TestGetShiftShapesKeepsShiftsApart(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	dbtest.SeedRoles(t, database)
	ids := roleIDsByName(t, database)

	rota := &db.Rotation{ID: uuid.New().String()}
	first := dbtest.Shift(rota.ID, "2026-09-06")
	second := dbtest.Shift(rota.ID, "2026-09-13")

	require.NoError(t, database.InsertDefinedRota(ctx, rota, []db.Shift{first, second}, nil, []db.ShiftRequirement{
		{ShiftID: first.ID, RoleID: ids["Service volunteer"], Seats: 4},
		{ShiftID: second.ID, RoleID: ids["Service volunteer"], Seats: 6},
		{ShiftID: second.ID, RoleID: ids["Team lead"], Seats: 1},
	}))

	shapes, err := database.GetShiftShapes(ctx, []string{first.ID, second.ID})
	require.NoError(t, err)
	assert.Len(t, shapes[first.ID], 1)
	assert.Equal(t, 4, shapes[first.ID][0].Seats)
	assert.Len(t, shapes[second.ID], 2)
}

// A Shift with no stored Seats is absent from the map rather than present and
// empty, so a caller asking "what does this Shift ask for" gets one answer for
// "nothing" rather than two.
func TestGetShiftShapesOmitsShiftsWithNoSeats(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	rota := &db.Rotation{ID: uuid.New().String()}
	shift := dbtest.Shift(rota.ID, "2026-09-06")
	require.NoError(t, database.InsertDefinedRota(ctx, rota, []db.Shift{shift}, nil, nil))

	shapes, err := database.GetShiftShapes(ctx, []string{shift.ID})
	require.NoError(t, err)
	assert.Empty(t, shapes)
}

// Asking about no Shifts is what a caller with no Shifts does, and it is not a
// query.
func TestGetShiftShapesWithNoShifts(t *testing.T) {
	database, _ := dbtest.New(t)

	shapes, err := database.GetShiftShapes(context.Background(), nil)
	require.NoError(t, err)
	assert.Empty(t, shapes)
}

// Editing one Shift's Shape replaces it whole — a Role left out is a Role that
// Shift no longer asks for — and reaches no other Shift of the same rota.
func TestSetShiftShapeReplacesOneShiftsShape(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	dbtest.SeedRoles(t, database)
	ids := roleIDsByName(t, database)

	rota := &db.Rotation{ID: uuid.New().String()}
	first := dbtest.Shift(rota.ID, "2026-09-06")
	second := dbtest.Shift(rota.ID, "2026-09-13")
	minted := func(shiftID string) []db.ShiftRequirement {
		return []db.ShiftRequirement{
			{ShiftID: shiftID, RoleID: ids["Team lead"], Seats: 1},
			{ShiftID: shiftID, RoleID: ids["Service volunteer"], Seats: 4},
		}
	}
	require.NoError(t, database.InsertDefinedRota(ctx, rota, []db.Shift{first, second}, nil,
		append(minted(first.ID), minted(second.ID)...)))

	require.NoError(t, database.WithRotaShapeLock(ctx, []string{rota.ID}, func(tx db.ShapeTxStore) error {
		written, err := tx.SetShiftShape(ctx, first.ID, []db.ShiftRequirement{
			{ShiftID: first.ID, RoleID: ids["Service volunteer"], Seats: 6},
		})
		assert.True(t, written)
		return err
	}))

	shapes, err := database.GetShiftShapes(ctx, []string{first.ID, second.ID})
	require.NoError(t, err)
	assert.Equal(t, []db.ShiftRequirement{
		{ShiftID: first.ID, RoleID: ids["Service volunteer"], Seats: 6},
	}, shapes[first.ID], "the lead's seat is gone and the count is the new one")
	assert.Equal(t, minted(second.ID), shapes[second.ID], "the other shift is untouched")
}

// Emptying a Shape leaves the Shift asking for nobody, which is the same state
// as a Shift minted before Shapes existed: absent from the map rather than
// present and empty.
func TestSetShiftShapeCanEmptyIt(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	dbtest.SeedRoles(t, database)
	ids := roleIDsByName(t, database)

	rota := &db.Rotation{ID: uuid.New().String()}
	shift := dbtest.Shift(rota.ID, "2026-09-06")
	require.NoError(t, database.InsertDefinedRota(ctx, rota, []db.Shift{shift}, nil, []db.ShiftRequirement{
		{ShiftID: shift.ID, RoleID: ids["Team lead"], Seats: 1},
	}))

	require.NoError(t, database.WithRotaShapeLock(ctx, []string{rota.ID}, func(tx db.ShapeTxStore) error {
		_, err := tx.SetShiftShape(ctx, shift.ID, nil)
		return err
	}))

	shapes, err := database.GetShiftShapes(ctx, []string{shift.ID})
	require.NoError(t, err)
	assert.Empty(t, shapes)
}

// A Shift that does not exist is reported rather than silently written nothing
// for: a Shape with no Shift is the state this table must never be in, and a
// delete of nought rows cannot tell the two apart on its own.
func TestSetShiftShapeReportsAMissingShift(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	dbtest.SeedRoles(t, database)

	rota := &db.Rotation{ID: uuid.New().String()}
	require.NoError(t, database.InsertDefinedRota(ctx, rota, nil, nil, nil))

	require.NoError(t, database.WithRotaShapeLock(ctx, []string{rota.ID}, func(tx db.ShapeTxStore) error {
		written, err := tx.SetShiftShape(ctx, uuid.New().String(), nil)
		assert.False(t, written)
		return err
	}))
}

// The delete and the inserts share the caller's transaction, so a Seat naming a
// Role that does not exist leaves the Shape as it was rather than emptied.
func TestSetShiftShapeRollsBackOnABadSeat(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	dbtest.SeedRoles(t, database)
	ids := roleIDsByName(t, database)

	rota := &db.Rotation{ID: uuid.New().String()}
	shift := dbtest.Shift(rota.ID, "2026-09-06")
	minted := []db.ShiftRequirement{{ShiftID: shift.ID, RoleID: ids["Team lead"], Seats: 1}}
	require.NoError(t, database.InsertDefinedRota(ctx, rota, []db.Shift{shift}, nil, minted))

	require.Error(t, database.WithRotaShapeLock(ctx, []string{rota.ID}, func(tx db.ShapeTxStore) error {
		_, err := tx.SetShiftShape(ctx, shift.ID, []db.ShiftRequirement{
			{ShiftID: shift.ID, RoleID: uuid.New().String(), Seats: 2},
		})
		return err
	}))

	shapes, err := database.GetShiftShapes(ctx, []string{shift.ID})
	require.NoError(t, err)
	assert.Equal(t, minted, shapes[shift.ID])
}

// The Shape and the Shift are written in one transaction: a Seat naming a Role
// that does not exist takes the whole definition down rather than leaving a
// rota whose Shifts ask for the wrong thing.
func TestInsertDefinedRotaRollsBackOnABadSeat(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	dbtest.SeedRoles(t, database)
	ids := roleIDsByName(t, database)

	rota := &db.Rotation{ID: uuid.New().String()}
	shift := dbtest.Shift(rota.ID, "2026-09-06")

	require.Error(t, database.InsertDefinedRota(ctx, rota, []db.Shift{shift}, nil, []db.ShiftRequirement{
		{ShiftID: shift.ID, RoleID: ids["Team lead"], Seats: 1},
		{ShiftID: shift.ID, RoleID: uuid.New().String(), Seats: 2},
	}))

	shifts, err := database.GetShiftsByRotaID(ctx, rota.ID)
	require.NoError(t, err)
	assert.Empty(t, shifts)
}
