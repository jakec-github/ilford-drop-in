package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// A draft names people against Shifts on a rota nobody has decided yet, so an
// anonymous caller cannot ask for one to be solved — and the refusal comes
// before the solve, since starting a thirty-second subprocess for a stranger
// would be worth having even if it published nothing.
func TestSolveDraftRotaAllocationRequiresAdmin(t *testing.T) {
	store := &mockStore{}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPost, "/api/draft-rota-allocation", "")

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, store.storedDrafts, "nothing was solved, let alone stored")
}

// Reading the draft is admin-only for the same reason as solving it: what comes
// back names people against Shifts nobody has decided yet, and the rota page and
// its calendar feed are read by the very volunteers it names (ADR 0008).
func TestGetDraftRotaAllocationRequiresAdmin(t *testing.T) {
	rec := doRequest(t, newTestHandler(draftedRotaStore(), testVolunteers()), http.MethodGet, "/api/draft-rota-allocation", "")

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.NotContains(t, rec.Body.String(), "Alice", "no drafted name reaches the body")
	assert.NotContains(t, rec.Body.String(), "shift-1")
}

// draftedRotaStore is one rota in flight of two Shifts, with a clean draft solved
// against it: Alice leading and Bob beside her on the first Shift, nobody on the
// second. Clean so that reading it reports rather than re-solves — the mock would
// take a solve as far as the CP-SAT subprocess, which no Go test runs.
func draftedRotaStore() *mockStore {
	moved := time.Date(2026, 8, 5, 6, 0, 0, 0, time.UTC)
	return &mockStore{
		rotations: []db.Rotation{{ID: "rota-1", Start: "2026-08-02", End: "2026-08-09", ShiftCount: 2, InputsChangedAt: moved}},
		shifts: []db.Shift{
			{ID: "shift-1", RotaID: "rota-1", Date: "2026-08-02", StartAt: "2026-08-02T19:30:00", EndAt: "2026-08-02T21:30:00"},
			{ID: "shift-2", RotaID: "rota-1", Date: "2026-08-09", StartAt: "2026-08-09T19:30:00", EndAt: "2026-08-09T21:30:00"},
		},
		storedDrafts: []db.DraftRotaAllocation{{
			RotaID:          "rota-1",
			SolvedAt:        time.Date(2026, 8, 5, 6, 0, 30, 0, time.UTC),
			Success:         true,
			SolverStatus:    "OPTIMAL",
			Diagnostics:     []byte(`{}`),
			InputsChangedAt: moved,
			SeatsAsked:      10,
			SeatsFilled:     2,
		}},
		draftSeats: []db.DraftAllocation{
			{ID: "seat-1", ShiftID: "shift-1", Role: "Service volunteer", VolunteerID: "bob"},
			{ID: "seat-2", ShiftID: "shift-1", Role: "Team lead", VolunteerID: "alice"},
		},
	}
}

// The rota an admin watches take shape: who the solver put where, keyed by Shift
// so the page can lay the draft over the rota it is already showing.
func TestGetDraftRotaAllocationReportsTheRotaItDrafted(t *testing.T) {
	rec := doRequest(t, newTestHandler(draftedRotaStore(), testVolunteers()), http.MethodGet, "/api/draft-rota-allocation", "", adminCookie())

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body draftRotaAllocationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	// Only the Shift the draft placed anybody on, its people in the order the
	// rota shows them — by Role priority, so the team lead leads.
	require.Len(t, body.Shifts, 1, "the Shift the solver staffed, not the one it left empty")
	assert.Equal(t, "shift-1", body.Shifts[0].ShiftID)
	require.Len(t, body.Shifts[0].Assignees, 2)
	assert.Equal(t, "Alice", body.Shifts[0].Assignees[0].Name)
	assert.Equal(t, "Team lead", body.Shifts[0].Assignees[0].Role)
	assert.Equal(t, "Bob", body.Shifts[0].Assignees[1].Name)
	assert.Equal(t, 2, body.SeatsFilled)
	assert.Equal(t, 10, body.SeatsAsked)
}

// A solve that staffed nobody — an infeasible one — is a draft with no Shifts
// rather than an absent list. It reads differently from a rota nobody has solved
// for, which is the distinction the stored outcome exists to make (ADR 0008).
func TestGetDraftRotaAllocationWithAnInfeasibleDraft(t *testing.T) {
	store := draftedRotaStore()
	store.storedDrafts[0].Success = false
	store.storedDrafts[0].SolverStatus = "INFEASIBLE"
	store.storedDrafts[0].SeatsFilled = 0
	store.draftSeats = nil

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/draft-rota-allocation", "", adminCookie())

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body draftRotaAllocationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.True(t, body.Solved, "solved, and the answer was that there is no rota")
	assert.False(t, body.Success)
	assert.Equal(t, "INFEASIBLE", body.SolverStatus)
	assert.NotNil(t, body.Shifts)
	assert.Empty(t, body.Shifts)
}

// A draft solved from the inputs as they stand is reported, not re-solved. This
// is the test that nothing solves needlessly: the mock store would take a solve
// as far as the CP-SAT subprocess, which no Go test runs, so a request that
// returns the stored draft is a request that did not try.
func TestGetDraftRotaAllocationReportsACleanDraft(t *testing.T) {
	moved := time.Date(2026, 8, 5, 11, 0, 0, 0, time.UTC)
	solvedAt := time.Date(2026, 8, 5, 11, 0, 30, 0, time.UTC)
	store := &mockStore{
		rotations: []db.Rotation{{ID: "rota-1", Start: "2026-08-02", ShiftCount: 2, InputsChangedAt: moved}},
		storedDrafts: []db.DraftRotaAllocation{{
			RotaID:          "rota-1",
			SolvedAt:        solvedAt,
			Success:         true,
			SolverStatus:    "OPTIMAL",
			Diagnostics:     []byte(`{"solve_time_seconds":2.5}`),
			InputsChangedAt: moved,
			SeatsAsked:      10,
			SeatsFilled:     8,
		}},
	}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/draft-rota-allocation", "", adminCookie())

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body draftRotaAllocationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "rota-1", body.RotaID)
	assert.Equal(t, "2026-08-02", body.RotaStart)
	assert.True(t, body.Solved)
	assert.False(t, body.Dirty)
	assert.False(t, body.Solving)
	assert.Equal(t, solvedAt.Format(time.RFC3339), body.SolvedAt)
	assert.Equal(t, "OPTIMAL", body.SolverStatus)
	assert.Equal(t, 10, body.SeatsAsked)
	assert.Equal(t, 8, body.SeatsFilled)
	assert.Equal(t, 2.5, body.SolveTimeSeconds)
	assert.Len(t, store.storedDrafts, 1, "the draft that was there, and no second one")
}

// A rota nobody has solved for yet is dirty by definition, so reading its draft
// tries to solve it — and on a rota with no availability round yet, that solve
// is refused. The read still answers: the draft as it stands is exactly what was
// asked for, and what stopped the solve is reported beside it.
//
// This is the state every rota is in between being defined and its round being
// minted, which is the whole first day of one. A 400 there would make the
// Allocation tab's draft panel unreachable on a screen whose job is to say what
// to do next (issue #145).
func TestGetDraftRotaAllocationWhenTheSolveIsRefused(t *testing.T) {
	store := &mockStore{
		rotations: []db.Rotation{{
			ID: "rota-1", Start: "2026-08-02", ShiftCount: 2,
			InputsChangedAt: time.Date(2026, 8, 5, 11, 0, 0, 0, time.UTC),
		}},
		shifts: []db.Shift{
			{ID: "shift-1", RotaID: "rota-1", Date: "2026-08-02", StartAt: "2026-08-02T19:30:00", EndAt: "2026-08-02T21:30:00"},
		},
	}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/draft-rota-allocation", "", adminCookie())

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body draftRotaAllocationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.False(t, body.Solved, "nothing has been solved for this rota")
	assert.True(t, body.Dirty, "and the inputs still stand ahead of the draft")
	assert.False(t, body.Solving, "no solve is running — one was refused")
	assert.Contains(t, body.SolveError, "availability round",
		"the step that has not been taken, named")
	assert.Empty(t, store.storedDrafts, "a refused solve stores nothing")
}

// With no unallocated Rotation there is nothing to draft, and nothing solves.
// The endpoint says which step is missing rather than answering with an empty
// draft, which would read as "solved, and it staffed nobody".
func TestGetDraftRotaAllocationWithNoRotaInFlight(t *testing.T) {
	store := &mockStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2026-08-02", ShiftCount: 2, AllocatedDatetime: "2026-08-01T10:00:00Z"},
		},
	}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/draft-rota-allocation", "", adminCookie())

	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "no rota in flight")
	assert.Empty(t, store.storedDrafts)
}

// A dirty draft with a solve already running comes back as it stands, marked
// solving. Not queued behind the running solve, and not solved a second time:
// the answer already on its way is the answer this reader wants, and a screen
// has something to show in the meantime.
func TestGetDraftRotaAllocationWhileASolveIsRunning(t *testing.T) {
	moved := time.Date(2026, 8, 5, 11, 0, 0, 0, time.UTC)
	store := &mockStore{
		rotations: []db.Rotation{{ID: "rota-1", Start: "2026-08-02", ShiftCount: 2, InputsChangedAt: moved}},
		storedDrafts: []db.DraftRotaAllocation{{
			RotaID:       "rota-1",
			SolvedAt:     time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC),
			Success:      true,
			SolverStatus: "OPTIMAL",
			Diagnostics:  []byte(`{}`),
			SeatsAsked:   10,
			SeatsFilled:  8,
			// Solved before the change above landed.
		}},
	}
	handler := NewHandler(store, testVolunteers(), apiTestCfg, newTestAuthenticator(), nil, nil, zap.NewNop())
	require.True(t, handler.drafts.begin(), "a solve is now running in this process")
	defer handler.drafts.end()

	rec := doRequest(t, handler.Routes(), http.MethodGet, "/api/draft-rota-allocation", "", adminCookie())

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body draftRotaAllocationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.True(t, body.Dirty, "the inputs have moved, and the reader is told so")
	assert.True(t, body.Solving, "a fresher answer is on its way")
	assert.True(t, body.Solved, "with the previous one to show until it lands")
	assert.Equal(t, 8, body.SeatsFilled)
	assert.Len(t, store.storedDrafts, 1, "no second solve was started")
}

// Asking for a re-solve while one is running is refused rather than queued. The
// solve already going produces the same answer from the same inputs, so a second
// subprocess would be work done twice to replace the first one's result.
func TestSolveDraftRotaAllocationWhileASolveIsRunning(t *testing.T) {
	store := &mockStore{
		rotations: []db.Rotation{{ID: "rota-1", Start: "2026-08-02", ShiftCount: 2}},
	}
	handler := NewHandler(store, testVolunteers(), apiTestCfg, newTestAuthenticator(), nil, nil, zap.NewNop())
	require.True(t, handler.drafts.begin())
	defer handler.drafts.end()

	rec := doRequest(t, handler.Routes(), http.MethodPost, "/api/draft-rota-allocation", "", adminCookie())

	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "already running")
	assert.Empty(t, store.storedDrafts)
}

// The slot is given back when a solve finishes, however it finished: a solve
// that failed must not wedge every later one out of the process.
func TestTheSolveSlotIsReleased(t *testing.T) {
	slot := newDraftSolves()

	require.True(t, slot.begin())
	assert.False(t, slot.begin(), "one at a time")
	slot.end()
	assert.True(t, slot.begin(), "and free again afterwards")
}
