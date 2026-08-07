package api

import (
	"context"
	"encoding/json"
	"net/http"
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
	rec = doRequest(t, handler, http.MethodGet, "/api/draft-rota-allocation", "", adminCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var view draftRotaViewResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &view))
	require.NotNil(t, view.Rota)
	assert.Equal(t, rota.ID, view.Rota.ID)
	require.NotNil(t, view.Draft)
	assert.Equal(t, 2, view.Draft.SeatsFilled, "the draft is there; it simply is not published")
	require.Len(t, view.Draft.Shifts, 2)
	assert.Equal(t, first.ID, view.Draft.Shifts[0].ShiftID)
	assert.Equal(t, "Alice", view.Draft.Shifts[0].Assignees[0].Name)
	assert.Equal(t, second.ID, view.Draft.Shifts[1].ShiftID)
	assert.Equal(t, "External Org", view.Draft.Shifts[1].Assignees[0].Name)

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
