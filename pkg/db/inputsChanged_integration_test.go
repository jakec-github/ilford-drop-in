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

// A Draft Rota Allocation is dirty when an allocator input has moved under the
// Rotation since it was solved, and this is the half of that the database owns:
// every write of an input stamps the Rotation it belongs to (issue #142).
//
// The tests below are one per input, against a real Postgres, because the stamp
// lives inside the write's own statement or transaction. A mock could only
// prove that the Go code called something.

// inputsFixture mints an unallocated rota with two shifts and the Roles a Shape
// or a pin needs, and hands back the rota and its shifts.
func inputsFixture(t *testing.T, database *db.DB) (db.Rotation, db.Shift, db.Shift) {
	t.Helper()
	dbtest.SeedRoles(t, database)
	rota := db.Rotation{ID: uuid.New().String()}
	first := dbtest.Shift(rota.ID, "2026-08-02")
	second := dbtest.Shift(rota.ID, "2026-08-09")
	require.NoError(t, database.InsertDefinedRota(context.Background(), &rota, []db.Shift{first, second}, nil, nil))
	return rota, first, second
}

// inputsChangedAt reads the stamp back the way the draft's dirty check does.
func inputsChangedAt(t *testing.T, database *db.DB) time.Time {
	t.Helper()
	inFlight, err := database.GetRotaInFlight(context.Background())
	require.NoError(t, err)
	require.NotNil(t, inFlight, "the fixture's rota is unallocated, so it is the rota in flight")
	return inFlight.InputsChangedAt
}

// A freshly defined rota has had nothing move under it yet, and says so.
func TestARotaStartsWithNothingMovedUnderIt(t *testing.T) {
	database, _ := dbtest.New(t)
	inputsFixture(t, database)

	assert.True(t, inputsChangedAt(t, database).IsZero(), "nothing has moved under a rota nobody has touched")
}

// Every input an admin or a volunteer can move, one at a time. Each starts from
// the stamp the one before it left, so the test also proves the stamp moves
// forwards rather than merely becoming non-zero once.
func TestEveryAllocatorInputStampsTheRota(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	rota, first, _ := inputsFixture(t, database)
	roles, err := database.ListRoles(ctx)
	require.NoError(t, err)

	for _, input := range []struct {
		name string
		move func(t *testing.T)
	}{
		{
			// The biggest input there is: who is free.
			name: "an availability response",
			move: func(t *testing.T) {
				request := db.AvailabilityRequest{
					ID:          uuid.New().String(),
					RotaID:      rota.ID,
					VolunteerID: "alice",
					Token:       uuid.New().String(),
				}
				minted, err := database.MintAvailabilityRequests(ctx, []db.AvailabilityRequest{request})
				require.NoError(t, err)
				require.Equal(t, 1, minted)

				_, err = database.InsertAvailabilityResponse(ctx, request.ID, []db.ShiftAnswer{
					{ShiftID: first.ID, Answer: db.AnswerYes},
				})
				require.NoError(t, err)
			},
		},
		{
			name: "a Shift being closed",
			move: func(t *testing.T) {
				require.NoError(t, database.WithRotaShiftLock(ctx, []string{rota.ID}, func(tx db.ShiftTxStore) error {
					written, err := tx.SetShiftClosed(ctx, first.ID, true)
					require.NoError(t, err)
					require.True(t, written)
					return nil
				}))
			},
		},
		{
			name: "a Shape edit",
			move: func(t *testing.T) {
				require.NoError(t, database.WithRotaShapeLock(ctx, []string{rota.ID}, func(tx db.ShapeTxStore) error {
					written, err := tx.SetShiftShape(ctx, first.ID, []db.ShiftRequirement{
						{ShiftID: first.ID, RoleID: roles[0].ID, Seats: 2},
					})
					require.NoError(t, err)
					require.True(t, written)
					return nil
				}))
			},
		},
		{
			name: "a Preallocation",
			move: func(t *testing.T) {
				require.NoError(t, database.WithRotaPreallocationLock(ctx, []string{rota.ID}, func(tx db.PreallocationTxStore) error {
					return tx.InsertPreallocation(ctx, db.Preallocation{
						ID:          pinID,
						ShiftID:     first.ID,
						Role:        "Team lead",
						VolunteerID: "alice",
					})
				}))
			},
		},
		{
			// Unpinning changes the problem exactly as pinning does.
			name: "a Preallocation being removed",
			move: func(t *testing.T) {
				require.NoError(t, database.WithRotaPreallocationLock(ctx, []string{rota.ID}, func(tx db.PreallocationTxStore) error {
					deleted, err := tx.DeletePreallocationByID(ctx, pinID)
					require.NoError(t, err)
					require.True(t, deleted)
					return nil
				}))
			},
		},
		{
			// Not a fact about one rota, which is why it stamps every rota in
			// flight rather than one named rota.
			name: "an Allocation Settings change",
			move: func(t *testing.T) {
				require.NoError(t, database.SaveAllocationSettings(ctx, `{"maxFrequency":0.5}`))
			},
		},
		{
			// The Roles are what the solver fills Seats from: a cap or a
			// priority moving changes the rota it would produce.
			name: "a Role being edited",
			move: func(t *testing.T) {
				edited := roles[0]
				edited.Priority = 5
				written, err := database.UpdateRole(ctx, edited)
				require.NoError(t, err)
				require.True(t, written)
			},
		},
		{
			name: "a Role being added",
			move: func(t *testing.T) {
				require.NoError(t, database.InsertRole(ctx, db.Role{
					ID: uuid.New().String(), Name: "Food collector", Priority: 9, Colour: "amber",
				}))
			},
		},
	} {
		t.Run(input.name, func(t *testing.T) {
			before := inputsChangedAt(t, database)
			input.move(t)
			after := inputsChangedAt(t, database)
			assert.True(t, after.After(before), "%s stamps the rota (was %s, now %s)", input.name, before, after)
		})
	}
}

// pinID is shared by the pin-and-unpin pair above, which run in order.
var pinID = uuid.New().String()

// A Shift's times are descriptive rather than an allocator input (ADR 0007), so
// moving a session within its day leaves the draft speaking for the rota. Moving
// its start onto another day does not: the solver is told dates, and which day a
// session runs on decides who is free for it.
func TestOnlyTheDayOfAShiftIsAnInput(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	rota, first, _ := inputsFixture(t, database)

	// Half an hour later on the same day.
	require.NoError(t, database.WithRotaShiftLock(ctx, []string{rota.ID}, func(tx db.ShiftTxStore) error {
		written, err := tx.SetShiftTimes(ctx, first.ID, "2026-08-02T20:00:00", "2026-08-02T22:00:00")
		require.NoError(t, err)
		require.True(t, written)
		return nil
	}))
	assert.True(t, inputsChangedAt(t, database).IsZero(), "the drop-in still runs on the second of August")

	// The following Sunday — a different question for every volunteer.
	require.NoError(t, database.WithRotaShiftLock(ctx, []string{rota.ID}, func(tx db.ShiftTxStore) error {
		written, err := tx.SetShiftTimes(ctx, first.ID, "2026-08-16T19:30:00", "2026-08-16T21:30:00")
		require.NoError(t, err)
		require.True(t, written)
		return nil
	}))
	assert.False(t, inputsChangedAt(t, database).IsZero(), "the shift moved to another day")
}

// An allocated Rotation is never stamped. It has no draft to make stale — the
// allocation is the rota — and a stamp on one would be a claim about something
// that has been decided.
func TestAnAllocatedRotaIsNeverStamped(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	rota, first, _ := inputsFixture(t, database)
	require.NoError(t, database.InsertAllocationsAndSetAllocated(ctx, []db.Allocation{
		{ID: uuid.New().String(), ShiftID: first.ID, Role: "Team lead", VolunteerID: "alice"},
	}, rota.ID, time.Now().UTC()))

	// A time change is all that is still allowed on an allocated rota's Shift,
	// and the Settings are editable at any point.
	require.NoError(t, database.WithRotaShiftLock(ctx, []string{rota.ID}, func(tx db.ShiftTxStore) error {
		_, err := tx.SetShiftTimes(ctx, first.ID, "2026-08-23T19:30:00", "2026-08-23T21:30:00")
		return err
	}))
	require.NoError(t, database.SaveAllocationSettings(ctx, `{"maxFrequency":0.5}`))

	rotations, err := database.GetRotations(ctx)
	require.NoError(t, err)
	require.Len(t, rotations, 1)
	assert.True(t, rotations[0].InputsChangedAt.IsZero(), "an allocated rota is left alone")
}
