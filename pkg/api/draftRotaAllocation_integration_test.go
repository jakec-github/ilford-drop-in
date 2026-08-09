package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
	"github.com/jakechorley/ilford-drop-in/pkg/db/dbtest"
)

// TestDraftRotaAllocationReachesNoPublicEndpoint is the test ADR 0008 is
// written around.
//
// Drafts live in tables of their own precisely so that no public reader can
// stumble into one. This proves it from the outside, against a real Postgres:
// with a draft placing Alice on a Shift, the two endpoints that carry a rota to
// people who are not admins carry nothing of it.
//
// Both matter, and the second matters more. GET /api/shifts has no admin gate at
// all. /calendars/{filename} pushes to calendar apps volunteers have already
// subscribed to — so a leak there does not wait to be looked at, it arrives on
// somebody's phone, and it arrives repeatedly, because drafts re-solve all
// through the availability window.
func TestDraftRotaAllocationReachesNoPublicEndpoint(t *testing.T) {
	database, _ := dbtest.New(t)
	dbtest.SeedRoles(t, database)
	dbtest.SeedRotaDefaults(t, database)
	ctx := context.Background()
	handler := NewHandler(database, testVolunteers(), apiTestCfg, newTestAuthenticator(), nil, nil, zap.NewNop()).Routes()

	// An unallocated rota — the only kind that has a draft.
	rota := db.Rotation{ID: uuid.New().String()}
	first := dbtest.Shift(rota.ID, "2026-08-02")
	second := dbtest.Shift(rota.ID, "2026-08-09")
	require.NoError(t, database.InsertDefinedRota(ctx, &rota, []db.Shift{first, second}, nil, nil))

	require.NoError(t, database.ReplaceDraftRotaAllocation(ctx, db.DraftRotaAllocation{
		RotaID:       rota.ID,
		SolvedAt:     time.Now().UTC(),
		Success:      true,
		SolverStatus: "OPTIMAL",
		Diagnostics:  []byte(`{}`),
	}, []db.DraftAllocation{
		{ID: uuid.New().String(), ShiftID: first.ID, Role: "Team lead", VolunteerID: "alice"},
		{ID: uuid.New().String(), ShiftID: second.ID, Role: "Service volunteer", CustomEntry: "External Org"},
	}))

	// The shift listing, anonymously. The Shifts are there — they exist whether
	// or not anybody is on them — but they read as the unallocated Shifts they
	// are, with nobody on them.
	rec := doRequest(t, handler, http.MethodGet, "/api/shifts", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var listed listShiftsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listed))
	require.Len(t, listed.Shifts, 2)
	for _, shift := range listed.Shifts {
		assert.False(t, shift.Allocated, "the rota has not been allocated")
		assert.Empty(t, shift.Assignees, "a draft is not an assignment")
	}
	// Belt and braces on the raw body: an assertion on the parsed struct would
	// pass if a future field carried the draft alongside `assignees`.
	assert.NotContains(t, rec.Body.String(), "alice")
	assert.NotContains(t, rec.Body.String(), "External Org")

	// The calendar feed, for the volunteer the draft placed. A valid, empty
	// calendar: she is subscribed, and there is nothing yet to tell her.
	rec = doRequest(t, handler, http.MethodGet, "/calendars/alice.ics", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "BEGIN:VCALENDAR")
	assert.NotContains(t, rec.Body.String(), "BEGIN:VEVENT", "a draft must never reach a subscribed calendar")
	assert.NotContains(t, rec.Body.String(), "2026-08-02")

	// The one path that does read drafts. Anonymously it is a 401 that names
	// nobody — not a thinner answer, no answer — which is what makes the silence
	// above a gate rather than an omission.
	rec = doRequest(t, handler, http.MethodGet, "/api/draft-rota-allocation", "")
	require.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "alice")
	assert.NotContains(t, rec.Body.String(), "External Org")

	// And with an admin session, the whole draft — which is the point of the gate
	// rather than an exception to it, and is what the rota view renders (#143).
	// Nothing has moved under this rota, so the read reports the stored draft
	// rather than solving it again.
	rec = doRequest(t, handler, http.MethodGet, "/api/draft-rota-allocation", "", adminCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var view draftRotaAllocationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	assert.Equal(t, rota.ID, view.RotaID)
	assert.True(t, view.Solved)
	require.Len(t, view.Shifts, 2, "the draft is there; it simply is not published")
	assert.Equal(t, first.ID, view.Shifts[0].ShiftID)
	assert.Equal(t, "Alice", view.Shifts[0].Assignees[0].Name)
	assert.Equal(t, second.ID, view.Shifts[1].ShiftID)
	assert.Equal(t, "External Org", view.Shifts[1].Assignees[0].Name)

	// The control, without which none of the above proves anything: allocate the
	// same person to the same Shift for real, and both endpoints say so. The
	// silence above is about drafts, not about a feed that never emits an event
	// or a listing that never names anybody.
	require.NoError(t, database.InsertAllocationsAndSetAllocated(ctx, []db.Allocation{
		{ID: uuid.New().String(), ShiftID: first.ID, Role: "Team lead", VolunteerID: "alice"},
	}, rota.ID, time.Now().UTC()))

	rec = doRequest(t, handler, http.MethodGet, "/api/shifts", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "alice")

	rec = doRequest(t, handler, http.MethodGet, "/calendars/alice.ics", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "BEGIN:VEVENT")
	assert.Contains(t, rec.Body.String(), "20260802")

	// And the second Shift's draft Seat is still nobody's business: it was never
	// allocated, so the custom entry the draft placed there stays unpublished.
	assert.NotContains(t, rec.Body.String(), "20260809")
}

// A draft is solved for the rota in flight, so an admin with no rota in flight
// is told which step is missing. Drafting is the first thing to put these
// refusals in front of a browser, and "internal server error" would be the
// wrong answer to "you have not defined a rota yet".
func TestSolveDraftRotaAllocationSaysWhichStepIsMissing(t *testing.T) {
	database, _ := dbtest.New(t)
	dbtest.SeedRoles(t, database)
	dbtest.SeedRotaDefaults(t, database)
	handler := NewHandler(database, testVolunteers(), apiTestCfg, newTestAuthenticator(), nil, nil, zap.NewNop()).Routes()

	rec := doRequest(t, handler, http.MethodPost, "/api/draft-rota-allocation", "", adminCookie())

	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "define a rota first")
}

// The same, one step further on: a rota that has been allocated is the rota, and
// a draft beside it could only contradict it. A conflict, not a fault.
func TestSolveDraftRotaAllocationRefusesAnAllocatedRota(t *testing.T) {
	database, _ := dbtest.New(t)
	dbtest.SeedRoles(t, database)
	dbtest.SeedRotaDefaults(t, database)
	ctx := context.Background()
	handler := NewHandler(database, testVolunteers(), apiTestCfg, newTestAuthenticator(), nil, nil, zap.NewNop()).Routes()

	rota := db.Rotation{ID: uuid.New().String()}
	shift := dbtest.Shift(rota.ID, "2026-08-02")
	require.NoError(t, database.InsertDefinedRota(ctx, &rota, []db.Shift{shift}, nil, nil))
	require.NoError(t, database.InsertAllocationsAndSetAllocated(ctx, []db.Allocation{
		{ID: uuid.New().String(), ShiftID: shift.ID, Role: "Team lead", VolunteerID: "alice"},
	}, rota.ID, time.Now().UTC()))

	rec := doRequest(t, handler, http.MethodPost, "/api/draft-rota-allocation", "", adminCookie())

	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "already allocated")

	draft, err := database.GetDraftRotaAllocation(ctx, rota.ID)
	require.NoError(t, err)
	assert.Nil(t, draft, "no draft was written for an allocated rota")
}

// The loop this ticket exists for, against a real Postgres: an input moves, and
// the next admin to read the draft causes it to be solved again (issue #142).
//
// The solve itself is a CP-SAT subprocess, which no Go test runs — so what is
// proved here is the decision to solve, by way of the input refusal that only
// the solve path produces. The clean read that opens the test is the control:
// the same request, the same store, no refusal, because nothing had moved.
func TestGetDraftRotaAllocationResolvesWhenTheInputsHaveMoved(t *testing.T) {
	database, _ := dbtest.New(t)
	dbtest.SeedRoles(t, database)
	dbtest.SeedRotaDefaults(t, database)
	ctx := context.Background()
	handler := NewHandler(database, testVolunteers(), apiTestCfg, newTestAuthenticator(), nil, nil, zap.NewNop()).Routes()

	rota := db.Rotation{ID: uuid.New().String()}
	first := dbtest.Shift(rota.ID, "2026-08-02")
	second := dbtest.Shift(rota.ID, "2026-08-09")
	require.NoError(t, database.InsertDefinedRota(ctx, &rota, []db.Shift{first, second}, nil, nil))

	// A draft solved from the inputs as they stand — nothing has moved under
	// this rota, so it carries no stamp, exactly as a solve of it would.
	require.NoError(t, database.ReplaceDraftRotaAllocation(ctx, db.DraftRotaAllocation{
		RotaID:       rota.ID,
		SolvedAt:     time.Now().UTC(),
		Success:      true,
		SolverStatus: "OPTIMAL",
		Diagnostics:  []byte(`{"solve_time_seconds":1.5}`),
		SeatsAsked:   10,
		SeatsFilled:  10,
	}, nil))

	rec := doRequest(t, handler, http.MethodGet, "/api/draft-rota-allocation", "", adminCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body draftRotaAllocationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Empty(t, body.SolveError, "nothing has moved, so the stored draft is the answer")
	assert.Equal(t, 10, body.SeatsFilled)

	// Close a Shift: the drop-in does not run that day, which is a different
	// rota to solve.
	require.NoError(t, database.WithRotaShiftLock(ctx, []string{rota.ID}, func(tx db.ShiftTxStore) error {
		_, err := tx.SetShiftClosed(ctx, first.ID, true)
		return err
	}))

	// The same read now solves — and runs headlong into the gate that stops a
	// rota whose Shifts ask for nobody, which is reachable from nowhere but the
	// solve. That refusal is what proves the read tried: a read that had merely
	// reported the stored draft would have come back clean.
	//
	// It comes back beside the draft rather than instead of it. The read
	// succeeded; the solve it attempted on the way did not, and both of those
	// are worth saying.
	rec = doRequest(t, handler, http.MethodGet, "/api/draft-rota-allocation", "", adminCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Contains(t, body.SolveError, "for nobody")
}

// dirtyDraftedRota is a rota in flight of two Shifts with a draft against it,
// and an edit landed since: the draft no longer speaks for the inputs, so
// reading it is a read that wants to solve. Returns the handler and the rota.
//
// The draft is clean when it is stored and made dirty by closing a Shift, so
// that the stamps are the ones the app itself writes rather than ones a test
// invented.
func dirtyDraftedRota(t *testing.T, database *db.DB) (*Handler, db.Rotation) {
	t.Helper()
	ctx := context.Background()
	dbtest.SeedRoles(t, database)
	dbtest.SeedRotaDefaults(t, database)

	rota := db.Rotation{ID: uuid.New().String()}
	first := dbtest.Shift(rota.ID, "2026-08-02")
	second := dbtest.Shift(rota.ID, "2026-08-09")
	require.NoError(t, database.InsertDefinedRota(ctx, &rota, []db.Shift{first, second}, nil, nil))
	require.NoError(t, database.ReplaceDraftRotaAllocation(ctx, db.DraftRotaAllocation{
		RotaID:       rota.ID,
		SolvedAt:     time.Now().UTC(),
		Success:      true,
		SolverStatus: "OPTIMAL",
		Diagnostics:  []byte(`{}`),
		SeatsAsked:   10,
		SeatsFilled:  10,
	}, nil))

	require.NoError(t, database.WithRotaShiftLock(ctx, []string{rota.ID}, func(tx db.ShiftTxStore) error {
		_, err := tx.SetShiftClosed(ctx, first.ID, true)
		return err
	}))

	return NewHandler(database, testVolunteers(), apiTestCfg, newTestAuthenticator(), nil, nil, zap.NewNop()), rota
}

// readDraftWhileTheSlotIsHeld starts a draft read against a handler whose solve
// slot is already taken, proves it is waiting rather than answering, and hands
// back the answer it eventually gives.
//
// The wait is the assertion. A read that came back at once would be a read that
// had reported a stale draft, which is the whole thing this endpoint stopped
// doing (issue #179).
func readDraftWhileTheSlotIsHeld(t *testing.T, handler *Handler, release func()) draftRotaAllocationResponse {
	t.Helper()
	answered := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		answered <- doRequest(t, handler.Routes(), http.MethodGet, "/api/draft-rota-allocation", "", adminCookie())
	}()

	select {
	case rec := <-answered:
		t.Fatalf("the read answered while a solve held the slot: %s", rec.Body.String())
	case <-time.After(100 * time.Millisecond):
	}

	release()

	select {
	case rec := <-answered:
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var body draftRotaAllocationResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		return body
	case <-time.After(10 * time.Second):
		t.Fatal("the read never took the slot it was waiting for")
		return draftRotaAllocationResponse{}
	}
}

// A read that arrives while a solve is running waits for it — and when that
// solve turns out to have accounted for the inputs this reader came in with,
// that is the answer, and nothing solves a second time (issue #179).
//
// This is the case that makes two tabs on one rota cheap: one solve, and one
// instant return once it lands.
//
// What proves nothing solved is that nothing was refused. A solve on this rota
// runs headlong into the gate on a rota whose Shifts ask for nobody — as the
// test below it shows — so a clean answer with no solveError is an answer that
// never went near the solver.
func TestGetDraftRotaAllocationWaitsForASolveThatAlreadyCoversIt(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	handler, rota := dirtyDraftedRota(t, database)

	require.NoError(t, handler.drafts.acquire(ctx), "a solve is now running for this rota")

	body := readDraftWhileTheSlotIsHeld(t, handler, func() {
		// The running solve finishes, having read the inputs as they now stand —
		// including the edit that sent the reader here. Its answer is the
		// reader's answer.
		inFlight, err := database.GetRotaInFlight(ctx)
		require.NoError(t, err)
		require.NoError(t, database.ReplaceDraftRotaAllocation(ctx, db.DraftRotaAllocation{
			RotaID:          rota.ID,
			SolvedAt:        time.Now().UTC(),
			Success:         true,
			SolverStatus:    "OPTIMAL",
			Diagnostics:     []byte(`{}`),
			InputsChangedAt: inFlight.InputsChangedAt,
			SeatsAsked:      10,
			SeatsFilled:     7,
		}, nil))
		handler.drafts.release()
	})

	assert.Empty(t, body.SolveError, "the running solve's answer was this reader's answer")
	assert.Equal(t, 7, body.SeatsFilled, "and it is that solve's answer, not the one it replaced")
}

// The same wait, and the other outcome: the solve queued ahead of this reader
// started before the edit that sent it here, so its answer says nothing about
// that edit. The reader re-reads the status inside the slot, finds it still
// dirty, and solves for itself.
//
// Without the re-read, "wait for the running solve and return that" would hand
// back an answer to a question nobody asked — which is the stale draft this
// endpoint stopped returning, arriving by a slower route.
func TestGetDraftRotaAllocationWaitsAndThenSolvesForItself(t *testing.T) {
	database, _ := dbtest.New(t)
	handler, _ := dirtyDraftedRota(t, database)

	require.NoError(t, handler.drafts.acquire(context.Background()))

	// The running solve finishes and stores nothing this reader can use: the
	// draft is still the one the edit moved past.
	body := readDraftWhileTheSlotIsHeld(t, handler, handler.drafts.release)

	assert.Contains(t, body.SolveError, "for nobody",
		"it solved for itself, and its own solve met the gate that proves it ran")
}
