package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// shiftEditTestStore is one unallocated rota of two shifts, the second already
// shut, so both directions of a closure have somewhere to land and a time
// change has another shift to collide with.
func shiftEditTestStore() *mockStore {
	return &mockStore{
		shiftsInRange: []db.ShiftInRange{
			{Shift: db.Shift{
				ID: "s1", RotaID: "rota-1", Date: "2026-12-20",
				StartAt: "2026-12-20T19:30:00", EndAt: "2026-12-20T21:30:00",
			}},
			{Shift: db.Shift{
				ID: "s2", RotaID: "rota-1", Date: "2026-12-27", Closed: true,
				StartAt: "2026-12-27T19:30:00", EndAt: "2026-12-27T21:30:00",
			}},
		},
	}
}

func decodeShiftUpdate(t *testing.T, body []byte) shiftUpdateResponse {
	t.Helper()
	var resp shiftUpdateResponse
	require.NoError(t, json.Unmarshal(body, &resp))
	return resp
}

func TestUpdateShiftClosesAShift(t *testing.T) {
	store := shiftEditTestStore()

	rec := doRequest(t, newTestHandler(store, testVolunteers()),
		http.MethodPatch, "/api/shifts/s1", `{"closed":true}`, adminCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.Equal(t, shiftUpdateResponse{
		ID:     "s1",
		Date:   "2026-12-20",
		Start:  "2026-12-20T19:30:00",
		End:    "2026-12-20T21:30:00",
		Closed: true,
	}, decodeShiftUpdate(t, rec.Body.Bytes()))
	assert.True(t, store.shiftsInRange[0].Closed, "the change reached the store")
}

func TestUpdateShiftReopensAShift(t *testing.T) {
	store := shiftEditTestStore()

	rec := doRequest(t, newTestHandler(store, testVolunteers()),
		http.MethodPatch, "/api/shifts/s2", `{"closed":false}`, adminCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.False(t, decodeShiftUpdate(t, rec.Body.Bytes()).Closed)
	assert.False(t, store.shiftsInRange[1].Closed)
}

// The rota page reads the flag back through the listing, so a close has to be
// visible there rather than only in the PATCH's own answer.
func TestUpdateShiftIsVisibleInTheListing(t *testing.T) {
	store := shiftEditTestStore()
	handler := newTestHandler(store, testVolunteers())

	require.Equal(t, http.StatusOK, doRequest(t, handler,
		http.MethodPatch, "/api/shifts/s1", `{"closed":true}`, adminCookie()).Code)

	rec := doRequest(t, handler, http.MethodGet, "/api/shifts", "")
	require.Equal(t, http.StatusOK, rec.Code)

	var listing struct {
		Shifts []struct {
			ID     string `json:"id"`
			Date   string `json:"date"`
			Closed bool   `json:"closed"`
		} `json:"shifts"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listing))
	require.Len(t, listing.Shifts, 2)
	assert.Equal(t, "s1", listing.Shifts[0].ID, "the listing carries the id a PATCH is addressed at")
	assert.True(t, listing.Shifts[0].Closed)
}

// Being Closed is an allocator input, so it freezes at allocation.
func TestUpdateShiftRefusedOnAnAllocatedRota(t *testing.T) {
	store := shiftEditTestStore()
	store.shiftsInRange[0].Allocated = true
	store.allocatedRotas = map[string]bool{"rota-1": true}

	rec := doRequest(t, newTestHandler(store, testVolunteers()),
		http.MethodPatch, "/api/shifts/s1", `{"closed":true}`, adminCookie())
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.False(t, store.shiftsInRange[0].Closed)
}

// A shift's times are descriptive rather than an allocator input, so the same
// allocated rota that freezes the closure above leaves these editable
// (ADR 0007).
func TestUpdateShiftTimesOnAnAllocatedRota(t *testing.T) {
	store := shiftEditTestStore()
	store.shiftsInRange[0].Allocated = true
	store.allocatedRotas = map[string]bool{"rota-1": true}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPatch,
		"/api/shifts/s1", `{"start":"2026-12-20T18:00:00","end":"2026-12-20T20:00:00"}`, adminCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.Equal(t, "2026-12-20T18:00:00", decodeShiftUpdate(t, rec.Body.Bytes()).Start)
	assert.Equal(t, "2026-12-20T18:00:00", store.shiftsInRange[0].StartAt)
}

// Moving the start moves the shift: its date is the date it starts, so a shift
// moved onto a Wednesday reads as that Wednesday in the listing.
func TestUpdateShiftMovesTheDate(t *testing.T) {
	store := shiftEditTestStore()
	handler := newTestHandler(store, testVolunteers())

	rec := doRequest(t, handler, http.MethodPatch,
		"/api/shifts/s1", `{"start":"2026-12-23T19:30:00","end":"2026-12-23T21:30:00"}`, adminCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "2026-12-23", decodeShiftUpdate(t, rec.Body.Bytes()).Date)

	rec = doRequest(t, handler, http.MethodGet, "/api/shifts", "")
	var listing struct {
		Shifts []struct {
			ID    string `json:"id"`
			Date  string `json:"date"`
			Start string `json:"start"`
		} `json:"shifts"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listing))
	require.Len(t, listing.Shifts, 2)
	assert.Equal(t, "2026-12-23", listing.Shifts[0].Date)
	assert.Equal(t, "2026-12-23T19:30:00", listing.Shifts[0].Start)
}

// Two shifts cannot share a day, and the refusal names the day rather than
// reporting a broken index.
func TestUpdateShiftRefusesADateAnotherShiftHolds(t *testing.T) {
	store := shiftEditTestStore()

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPatch,
		"/api/shifts/s1", `{"start":"2026-12-27T19:30:00","end":"2026-12-27T21:30:00"}`, adminCookie())
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "27 December 2026")
	assert.Equal(t, "2026-12-20T19:30:00", store.shiftsInRange[0].StartAt, "the shift stays where it was")
}

// The browser's datetime-local field leaves the seconds off; what comes back is
// the spelling the shift is stored in.
func TestUpdateShiftAcceptsTimesWithoutSeconds(t *testing.T) {
	store := shiftEditTestStore()

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPatch,
		"/api/shifts/s1", `{"start":"2026-12-20T18:00","end":"2026-12-20T20:00"}`, adminCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "2026-12-20T18:00:00", decodeShiftUpdate(t, rec.Body.Bytes()).Start)
}

func TestUpdateShiftRejectsAnEndBeforeItsStart(t *testing.T) {
	store := shiftEditTestStore()

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPatch,
		"/api/shifts/s1", `{"start":"2026-12-20T21:30:00","end":"2026-12-20T19:30:00"}`, adminCookie())
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Equal(t, "2026-12-20T19:30:00", store.shiftsInRange[0].StartAt)
}

func TestUpdateShiftUnknownShift(t *testing.T) {
	rec := doRequest(t, newTestHandler(shiftEditTestStore(), testVolunteers()),
		http.MethodPatch, "/api/shifts/ghost", `{"closed":true}`, adminCookie())
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

// A body that says nothing is a client error, not an instruction to reopen a
// shift somebody deliberately shut.
func TestUpdateShiftRequiresSomethingToChange(t *testing.T) {
	store := shiftEditTestStore()

	rec := doRequest(t, newTestHandler(store, testVolunteers()),
		http.MethodPatch, "/api/shifts/s2", `{}`, adminCookie())
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.True(t, store.shiftsInRange[1].Closed, "the shut shift stays shut")
}

func TestUpdateShiftRejectsUnknownFields(t *testing.T) {
	rec := doRequest(t, newTestHandler(shiftEditTestStore(), testVolunteers()),
		http.MethodPatch, "/api/shifts/s1", `{"closed":true,"date":"2026-12-21"}`, adminCookie())
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}

// Anyone may read the rota; only an admin may change what allocation will do
// with it.
func TestUpdateShiftRequiresAdmin(t *testing.T) {
	store := shiftEditTestStore()

	rec := doRequest(t, newTestHandler(store, testVolunteers()),
		http.MethodPatch, "/api/shifts/s1", `{"closed":true}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	assert.False(t, store.shiftsInRange[0].Closed)
}
