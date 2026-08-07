package services

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/allocator"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// stubSolver stands in for pyallocator: a script that swallows the input and
// prints the answer the test wants, at a path the python flag can name.
//
// The tests that reach the solver are about what allocating does with an
// answer — commits it, refuses it, replaces the draft with it — and none of
// them are about CP-SAT. Running the real solver would make them slow, and
// would make the answer something the test reads rather than states, which is
// exactly the thing being asserted on. pyallocator's own suite covers the
// solving.
func stubSolver(t *testing.T, output allocator.CpsatOutput) string {
	t.Helper()

	payload, err := json.Marshal(output)
	require.NoError(t, err)

	// The runner invokes `<python> -m pyallocator` with the input on stdin, so
	// the stub ignores its arguments and drains stdin before answering — a
	// script that exited without reading would break the pipe under it.
	script := "#!/bin/sh\ncat > /dev/null\ncat <<'CPSAT_OUTPUT'\n" + string(payload) + "\nCPSAT_OUTPUT\n"
	path := filepath.Join(t.TempDir(), "stub-pyallocator")
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755))
	return path
}

// solvedRota is what the stub solver answers: a rota that staffs the given
// volunteers, in order, on the dates named.
func solvedRota(placements map[string][]allocator.CpsatAssignment) allocator.CpsatOutput {
	output := allocator.CpsatOutput{SolverStatus: "OPTIMAL", Success: true}
	index := 0
	for _, date := range sortedDates(placements) {
		output.Shifts = append(output.Shifts, allocator.CpsatOutputShift{
			Index:       index,
			Date:        date,
			Size:        4,
			Assignments: placements[date],
		})
		index++
	}
	return output
}

func sortedDates(placements map[string][]allocator.CpsatAssignment) []string {
	dates := make([]string, 0, len(placements))
	for date := range placements {
		dates = append(dates, date)
	}
	// The dates are ISO, so lexical order is date order.
	for i := 1; i < len(dates); i++ {
		for j := i; j > 0 && dates[j] < dates[j-1]; j-- {
			dates[j], dates[j-1] = dates[j-1], dates[j]
		}
	}
	return dates
}

// allocatableRota is a rota with everything allocating needs: two Shifts, a
// round two volunteers have answered, and the Roles and settings a configured
// drop-in has. Tests vary what the solver says about it, not what it is.
func allocatableRota() (*mockAllocateRotaStore, *mockVolClient) {
	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{{ID: "rota-1", Start: "2026-08-02", ShiftCount: 2}},
		shifts:    sundayShifts("rota-1", "2026-08-02", 2),
		availabilityRequests: []db.AvailabilityRequest{
			{ID: "req-1", RotaID: "rota-1", VolunteerID: "vol-1", Token: "tok-1"},
			{ID: "req-2", RotaID: "rota-1", VolunteerID: "vol-2", Token: "tok-2"},
		},
		generations: map[string]db.AvailabilityGeneration{
			"req-1": {RequestID: "req-1", ResponseID: "gen-1", Answers: []db.ShiftAnswer{
				{ShiftID: "2026-08-02", Answer: db.AnswerYes},
				{ShiftID: "2026-08-09", Answer: db.AnswerYes},
			}},
			"req-2": {RequestID: "req-2", ResponseID: "gen-2", Answers: []db.ShiftAnswer{
				{ShiftID: "2026-08-02", Answer: db.AnswerYes},
				{ShiftID: "2026-08-09", Answer: db.AnswerYes},
			}},
		},
	}
	volunteers := &mockVolClient{volunteers: []model.Volunteer{
		{ID: "vol-1", FirstName: "Ada", LastName: "Active", Roles: []string{"Team lead", "Service volunteer"}, Status: "Active"},
		{ID: "vol-2", FirstName: "Bo", LastName: "Busy", Roles: []string{"Service volunteer"}, Status: "Active"},
	}}
	return store, volunteers
}

// draftThenAllocate is the whole journey in one helper: solve a draft, then
// allocate confirming the hash that draft came back with. Which is the point of
// the design — an admin allocates the rota they were shown — so most of these
// tests start by being shown one.
func draftThenAllocate(
	t *testing.T,
	store *mockAllocateRotaStore,
	volunteers *mockVolClient,
	drafted, allocating allocator.CpsatOutput,
) (*DraftRotaAllocationStatus, *AllocateRotaOutcome, error) {
	t.Helper()

	shown, err := SolveDraftRotaAllocation(context.Background(), store, volunteers, testCfg, zap.NewNop(), stubSolver(t, drafted))
	require.NoError(t, err)

	outcome, err := AllocateRotaInFlight(context.Background(), store, volunteers, testCfg, zap.NewNop(), shown.Hash, stubSolver(t, allocating))
	return shown, outcome, err
}

// The rota an admin was shown is the rota that gets allocated: re-solving
// answered the same thing, so the answer is committed and the Rotation stamped.
func TestAllocateRotaInFlightCommitsTheRotaItWasShown(t *testing.T) {
	store, volunteers := allocatableRota()
	solved := solvedRota(map[string][]allocator.CpsatAssignment{
		"2026-08-02": {{VolunteerID: "vol-1", Role: "Team lead"}, {VolunteerID: "vol-2", Role: "Service volunteer"}},
		"2026-08-09": {{VolunteerID: "vol-1", Role: "Service volunteer"}},
	})

	shown, outcome, err := draftThenAllocate(t, store, volunteers, solved, solved)

	require.NoError(t, err)
	require.NotNil(t, outcome)
	assert.True(t, outcome.Allocated)
	assert.False(t, outcome.AllocatedAt.IsZero(), "the moment it was allocated is reported")
	require.Len(t, store.insertedAllocations, 3, "one row per filled Seat")
	assert.Equal(t, shown.Hash, outcome.Solve.Hash, "what was committed is what was shown")

	// The rota that was committed, not merely a count of it: this is what the
	// screen shows once allocating has succeeded.
	byShift := map[string][]string{}
	for _, seat := range store.insertedAllocations {
		byShift[seat.ShiftID] = append(byShift[seat.ShiftID], seat.Role+":"+seat.VolunteerID)
	}
	assert.ElementsMatch(t, []string{"Team lead:vol-1", "Service volunteer:vol-2"}, byShift["2026-08-02"])
	assert.ElementsMatch(t, []string{"Service volunteer:vol-1"}, byShift["2026-08-09"])
}

// The whole point of confirming by output hash: if re-solving answers something
// else, the inputs have moved since the admin looked, and nothing is committed.
// The fresh solve becomes the draft, so what they are shown next is the rota as
// it now stands — and that is the one they can confirm.
func TestAllocateRotaInFlightRefusesARotaThatHasMoved(t *testing.T) {
	store, volunteers := allocatableRota()
	shown, outcome, err := draftThenAllocate(t, store, volunteers,
		solvedRota(map[string][]allocator.CpsatAssignment{
			"2026-08-02": {{VolunteerID: "vol-1", Role: "Team lead"}, {VolunteerID: "vol-2", Role: "Service volunteer"}},
		}),
		solvedRota(map[string][]allocator.CpsatAssignment{
			"2026-08-02": {{VolunteerID: "vol-1", Role: "Team lead"}},
			"2026-08-09": {{VolunteerID: "vol-2", Role: "Service volunteer"}},
		}),
	)

	require.NoError(t, err, "a rota that has moved is an answer, not a failure")
	require.NotNil(t, outcome)
	assert.False(t, outcome.Allocated)
	assert.Empty(t, store.insertedAllocations, "nothing is committed")

	require.Len(t, store.storedDrafts, 2, "the fresh solve replaced the draft")
	assert.NotEqual(t, shown.Hash, outcome.Solve.Hash, "and it is a different rota")
	assert.Equal(t, 2, len(outcome.Solve.Shifts), "which the admin is shown, to confirm instead")
}

// An infeasible solve is not a rota, so there is nothing to allocate. It is
// still stored as the draft: it is the answer as things stand, and the screen
// that refused the allocation is the one that has to explain why.
func TestAllocateRotaInFlightRefusesAnInfeasibleSolve(t *testing.T) {
	store, volunteers := allocatableRota()
	_, outcome, err := draftThenAllocate(t, store, volunteers,
		solvedRota(map[string][]allocator.CpsatAssignment{
			"2026-08-02": {{VolunteerID: "vol-1", Role: "Team lead"}},
		}),
		allocator.CpsatOutput{SolverStatus: "INFEASIBLE", Success: false},
	)

	require.ErrorIs(t, err, ErrConflict)
	assert.Nil(t, outcome)
	assert.Contains(t, err.Error(), "INFEASIBLE")
	assert.Empty(t, store.insertedAllocations, "nothing is committed")
	require.Len(t, store.storedDrafts, 2, "the infeasible solve replaced the draft")
	assert.False(t, store.storedDrafts[1].Success)
}

// Allocating confirms a draft, so a rota nobody has drafted cannot be
// allocated. It fails before the solver rather than after: there is nothing the
// answer could be compared against, so solving would be thirty seconds spent to
// reach the same refusal.
func TestAllocateRotaInFlightRefusesWithNoDraft(t *testing.T) {
	store, volunteers := allocatableRota()

	outcome, err := AllocateRotaInFlight(
		context.Background(), store, volunteers, testCfg, zap.NewNop(),
		"a-hash-from-nowhere",
		// A solver that does not exist: reaching it would fail differently, so
		// this pins the refusal to the gate rather than to the solve.
		filepath.Join(t.TempDir(), "no-such-python"),
	)

	require.ErrorIs(t, err, ErrConflict)
	assert.Nil(t, outcome)
	assert.Contains(t, err.Error(), "drafted")
	assert.Empty(t, store.insertedAllocations)
}

// Allocating states which rota is being confirmed. A request that names none is
// refused rather than treated as "whatever the solver says now", which is the
// one thing the design exists to prevent.
func TestAllocateRotaInFlightRefusesAnUnstatedDraft(t *testing.T) {
	store, volunteers := allocatableRota()

	outcome, err := AllocateRotaInFlight(
		context.Background(), store, volunteers, testCfg, zap.NewNop(),
		"", filepath.Join(t.TempDir(), "no-such-python"),
	)

	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Nil(t, outcome)
	assert.Empty(t, store.insertedAllocations)
}

// The rota was allocated while this admin had the page open — by another admin,
// or by their own second tab. There is no rota in flight any more, and saying so
// is more use than a solve that would be thrown away.
func TestAllocateRotaInFlightRefusesAnAllocatedRota(t *testing.T) {
	store, volunteers := allocatableRota()
	store.rotations[0].AllocatedDatetime = "2026-08-01T10:00:00Z"

	outcome, err := AllocateRotaInFlight(
		context.Background(), store, volunteers, testCfg, zap.NewNop(),
		"a-hash-from-before", filepath.Join(t.TempDir(), "no-such-python"),
	)

	require.ErrorIs(t, err, ErrConflict)
	assert.Nil(t, outcome)
	assert.Contains(t, err.Error(), "no rota in flight")
	assert.Empty(t, store.insertedAllocations, "no allocations should be written")
}

// The last word belongs to the store, not to any check above it. Two admins can
// confirm the same draft at the same moment and both reach a solve that matches;
// the row lock in InsertAllocationsAndSetAllocated is what makes exactly one of
// them win, and the loser's refusal has to reach the screen.
func TestAllocateRotaInFlightSurfacesTheStoresRefusal(t *testing.T) {
	store, volunteers := allocatableRota()
	solved := solvedRota(map[string][]allocator.CpsatAssignment{
		"2026-08-02": {{VolunteerID: "vol-1", Role: "Team lead"}},
	})
	store.insertAllocationsErr = assert.AnError

	_, outcome, err := draftThenAllocate(t, store, volunteers, solved, solved)

	require.Error(t, err)
	assert.Nil(t, outcome)
	assert.Contains(t, err.Error(), "failed to save allocations")
}

// Incomplete settings block allocation and nothing else (ADR 0006). The refusal
// has to name what is missing: an admin who has not filled the settings in has
// nothing else telling them which box is empty, and this fires before the
// solver so nothing is written.
func TestAllocateRotaInFlightRefusesWhenTheShiftTimesAreNotSet(t *testing.T) {
	cases := map[string]struct {
		defaults db.RotaDefaults
		names    []string
	}{
		"nothing set": {
			defaults: db.RotaDefaults{},
			names:    []string{"start time", "end time"},
		},
		"only the start set": {
			defaults: db.RotaDefaults{ShiftStartTime: "19:30"},
			names:    []string{"end time"},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			store, volunteers := allocatableRota()
			store.rotaDefaults = &tc.defaults
			store.storedDrafts = []db.DraftRotaAllocation{{RotaID: "rota-1"}}

			outcome, err := AllocateRotaInFlight(
				context.Background(), store, volunteers, testCfg, zap.NewNop(),
				"a-hash", "",
			)

			require.ErrorIs(t, err, ErrInvalidInput)
			assert.Nil(t, outcome)
			assert.Contains(t, err.Error(), "settings screen", "the refusal says where to go")
			for _, named := range tc.names {
				assert.Contains(t, err.Error(), named)
			}
			assert.Empty(t, store.insertedAllocations, "nothing is written")
		})
	}
}

// A Shift asking for nobody is the one gap that would not fail loudly: the
// solve succeeds and staffs nobody. Since Shifts own their Shapes the question
// is asked of the rota's Shifts rather than of the settings (issue #137), and
// the refusal names the dates so an admin can see which rota it means.
func TestAllocateRotaInFlightRefusesWhenAShiftAsksForNobody(t *testing.T) {
	store, volunteers := allocatableRota()
	store.noShape = true
	store.storedDrafts = []db.DraftRotaAllocation{{RotaID: "rota-1"}}

	outcome, err := AllocateRotaInFlight(
		context.Background(), store, volunteers, testCfg, zap.NewNop(),
		"a-hash", "",
	)

	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Nil(t, outcome)
	assert.Contains(t, err.Error(), "2026-08-02")
	assert.Contains(t, err.Error(), "2026-08-09")
	assert.Contains(t, err.Error(), "ask for nobody")
	assert.Empty(t, store.insertedAllocations, "nothing is written")
}

// Nobody works a day the drop-in is shut, so a closed Shift with no Seats is
// not a gap in anything and must not stop the rota being allocated.
func TestAllocateRotaInFlightIgnoresAClosedShiftWithNoShape(t *testing.T) {
	store, volunteers := allocatableRota()
	store.shifts[1].Closed = true
	// The open Shift has its Shape; the closed one was never given any Seats.
	store.shiftShapes = map[string][]db.DefaultShapeSeat{"2026-08-09": {}}
	store.storedDrafts = []db.DraftRotaAllocation{{RotaID: "rota-1"}}

	solved := solvedRota(map[string][]allocator.CpsatAssignment{
		"2026-08-02": {{VolunteerID: "vol-1", Role: "Team lead"}},
	})

	outcome, err := AllocateRotaInFlight(
		context.Background(), store, volunteers, testCfg, zap.NewNop(),
		hashAllocations([]db.Allocation{{ShiftID: "2026-08-02", Role: "Team lead", VolunteerID: "vol-1"}}),
		stubSolver(t, solved),
	)

	require.NoError(t, err)
	require.NotNil(t, outcome)
	assert.True(t, outcome.Allocated, "a closed shift asking for nobody is not a gap")
}
