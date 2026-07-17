package services

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
	"github.com/jakechorley/ilford-drop-in/pkg/db/dbtest"
)

// seedAllocatedRota creates a one-shift rota on 2026-08-02 with alice
// allocated, returning the rota ID.
func seedAllocatedRota(t *testing.T, database *db.DB) string {
	t.Helper()
	ctx := context.Background()

	rotaID := uuid.New().String()
	shiftID := uuid.New().String()
	require.NoError(t, database.InsertRotationAndShifts(ctx, &db.Rotation{ID: rotaID}, []db.Shift{
		{ID: shiftID, Date: "2026-08-02", RotaID: rotaID},
	}))
	require.NoError(t, database.InsertAllocationsAndSetAllocated(ctx, []db.Allocation{
		{ID: uuid.New().String(), ShiftID: shiftID, Role: string(model.RoleVolunteer), VolunteerID: "alice"},
	}, rotaID, time.Now()))

	return rotaID
}

// alterationsForRota reads a rota's alterations via its shifts, the shift_id-keyed
// path that replaced GetAlterationsByRotaID (ADR 0001).
func alterationsForRota(t *testing.T, database *db.DB, rotaID string) []db.Alteration {
	t.Helper()
	ctx := context.Background()

	shifts, err := database.GetShiftsByRotaID(ctx, rotaID)
	require.NoError(t, err)
	shiftIDs := make([]string, len(shifts))
	for i, s := range shifts {
		shiftIDs[i] = s.ID
	}
	alterations, err := database.GetAlterationsByShiftIDs(ctx, shiftIDs)
	require.NoError(t, err)
	return alterations
}

// TestChangeRotaConcurrentIdenticalAdds covers hazard H1 (issue #41): two
// concurrent identical "add" changes to the same shift must serialise on the
// rotation row, so the loser revalidates against the winner's committed
// alteration and fails with ErrConflict instead of adding the volunteer twice.
func TestChangeRotaConcurrentIdenticalAdds(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	rotaID := seedAllocatedRota(t, database)

	params := ChangeRotaParams{
		Date:      "2026-08-02",
		In:        "dave",
		Reason:    "Extra cover",
		UserEmail: "test@example.com",
	}

	start := make(chan struct{})
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = ChangeRota(ctx, database, defaultVolunteers(), testCfg, params, zap.NewNop())
		}(i)
	}
	close(start)
	wg.Wait()

	var successes, conflicts int
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case assert.ErrorIs(t, err, ErrConflict):
			conflicts++
		}
	}
	assert.Equal(t, 1, successes, "exactly one change must commit")
	assert.Equal(t, 1, conflicts, "the losing change must fail with ErrConflict")

	alterations := alterationsForRota(t, database, rotaID)
	require.Len(t, alterations, 1, "the shift must gain dave exactly once")
	assert.Equal(t, "dave", alterations[0].VolunteerID)
	assert.Equal(t, "add", alterations[0].Direction)
}

// TestChangeRotaSerialisesWithAllocation covers hazard H2 (issue #41): a rota
// change racing an allocation of the same rota must serialise on the rotation
// row (the issue #8 lock), so the change validates against the allocation's
// committed state rather than the pre-allocation emptiness it would otherwise
// have read.
func TestChangeRotaSerialisesWithAllocation(t *testing.T) {
	database, dbURL := dbtest.New(t)
	ctx := context.Background()

	// An unallocated rota with one shift
	rotaID := uuid.New().String()
	shiftID := uuid.New().String()
	require.NoError(t, database.InsertRotationAndShifts(ctx, &db.Rotation{ID: rotaID}, []db.Shift{
		{ID: shiftID, Date: "2026-08-02", RotaID: rotaID},
	}))

	// Simulate an in-flight allocation: a raw transaction that has taken the
	// issue #8 rotation-row lock and written dave's allocation but not yet
	// committed, exactly as InsertAllocationsAndSetAllocated does mid-flight.
	conn, err := pgx.Connect(ctx, dbURL)
	require.NoError(t, err)
	defer conn.Close(ctx)
	allocTx, err := conn.Begin(ctx)
	require.NoError(t, err)
	_, err = allocTx.Exec(ctx, `SELECT id FROM rotation WHERE id = $1 FOR UPDATE`, rotaID)
	require.NoError(t, err)
	_, err = allocTx.Exec(ctx, `INSERT INTO allocation (id, role, volunteer_id, shift_id) VALUES ($1, $2, $3, $4)`,
		uuid.New().String(), string(model.RoleVolunteer), "dave", shiftID)
	require.NoError(t, err)
	_, err = allocTx.Exec(ctx, `UPDATE rotation SET allocated_datetime = NOW() WHERE id = $1`, rotaID)
	require.NoError(t, err)

	// Start a change adding dave to the same shift while the allocation is in
	// flight. It must block on the rotation lock rather than validate against
	// the pre-allocation state.
	changeErr := make(chan error, 1)
	go func() {
		_, err := ChangeRota(ctx, database, defaultVolunteers(), testCfg, ChangeRotaParams{
			Date:      "2026-08-02",
			In:        "dave",
			Reason:    "Extra cover",
			UserEmail: "test@example.com",
		}, zap.NewNop())
		changeErr <- err
	}()

	// Let the change reach the lock, then commit the allocation. Whether the
	// change was already blocked or arrives after, it must observe dave's
	// committed allocation and refuse to add him twice.
	time.Sleep(100 * time.Millisecond)
	require.NoError(t, allocTx.Commit(ctx))

	err = <-changeErr
	require.ErrorIs(t, err, ErrConflict, "change must see the committed allocation of dave")

	alterations := alterationsForRota(t, database, rotaID)
	assert.Empty(t, alterations, "the conflicting change must write nothing")
}
