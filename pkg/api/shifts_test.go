package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// closureTestStore is one unallocated rota of two shifts, the second already
// shut, so both directions of the change have somewhere to land.
func closureTestStore() *mockStore {
	return &mockStore{
		shiftsInRange: []db.ShiftInRange{
			{Shift: db.Shift{ID: "s1", RotaID: "rota-1", Date: "2026-12-20"}},
			{Shift: db.Shift{ID: "s2", RotaID: "rota-1", Date: "2026-12-27", Closed: true}},
		},
	}
}

func decodeClosure(t *testing.T, body []byte) shiftClosureResponse {
	t.Helper()
	var resp shiftClosureResponse
	require.NoError(t, json.Unmarshal(body, &resp))
	return resp
}

func TestUpdateShiftClosesAShift(t *testing.T) {
	store := closureTestStore()

	rec := doRequest(t, newTestHandler(store, testVolunteers()),
		http.MethodPatch, "/api/shifts/s1", `{"closed":true}`, adminCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	resp := decodeClosure(t, rec.Body.Bytes())
	assert.Equal(t, shiftClosureResponse{ID: "s1", Date: "2026-12-20", Closed: true}, resp)
	assert.True(t, store.shiftsInRange[0].Closed, "the change reached the store")
}

func TestUpdateShiftReopensAShift(t *testing.T) {
	store := closureTestStore()

	rec := doRequest(t, newTestHandler(store, testVolunteers()),
		http.MethodPatch, "/api/shifts/s2", `{"closed":false}`, adminCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.False(t, decodeClosure(t, rec.Body.Bytes()).Closed)
	assert.False(t, store.shiftsInRange[1].Closed)
}

// The rota page reads the flag back through the listing, so a close has to be
// visible there rather than only in the PATCH's own answer.
func TestUpdateShiftIsVisibleInTheListing(t *testing.T) {
	store := closureTestStore()
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
	store := closureTestStore()
	store.shiftsInRange[0].Allocated = true
	store.allocatedRotas = map[string]bool{"rota-1": true}

	rec := doRequest(t, newTestHandler(store, testVolunteers()),
		http.MethodPatch, "/api/shifts/s1", `{"closed":true}`, adminCookie())
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.False(t, store.shiftsInRange[0].Closed)
}

func TestUpdateShiftUnknownShift(t *testing.T) {
	rec := doRequest(t, newTestHandler(closureTestStore(), testVolunteers()),
		http.MethodPatch, "/api/shifts/ghost", `{"closed":true}`, adminCookie())
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

// An omitted closed is not "open it": a body that says nothing is a client
// error, not an instruction to reopen a shift somebody deliberately shut.
func TestUpdateShiftRequiresClosed(t *testing.T) {
	store := closureTestStore()

	rec := doRequest(t, newTestHandler(store, testVolunteers()),
		http.MethodPatch, "/api/shifts/s2", `{}`, adminCookie())
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.True(t, store.shiftsInRange[1].Closed, "the shut shift stays shut")
}

func TestUpdateShiftRejectsUnknownFields(t *testing.T) {
	rec := doRequest(t, newTestHandler(closureTestStore(), testVolunteers()),
		http.MethodPatch, "/api/shifts/s1", `{"closed":true,"date":"2026-12-21"}`, adminCookie())
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}

// Anyone may read the rota; only an admin may change what allocation will do
// with it.
func TestUpdateShiftRequiresAdmin(t *testing.T) {
	store := closureTestStore()

	rec := doRequest(t, newTestHandler(store, testVolunteers()),
		http.MethodPatch, "/api/shifts/s1", `{"closed":true}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
	assert.False(t, store.shiftsInRange[0].Closed)
}
