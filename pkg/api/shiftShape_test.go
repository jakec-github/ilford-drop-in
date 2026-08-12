package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

func decodeShiftShape(t *testing.T, body []byte) shiftShapeResponse {
	t.Helper()
	var resp shiftShapeResponse
	require.NoError(t, json.Unmarshal(body, &resp))
	return resp
}

// The Seats come back in the order they are filled, whatever order they were
// stated in, so the screen that saved a Shape reads the same list back.
func TestSaveShiftShape(t *testing.T) {
	store := shiftEditTestStore()

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPut,
		"/api/shifts/s1/shape",
		`{"seats":[{"roleId":"role-service-volunteer","count":6},{"roleId":"role-team-lead","count":1}]}`,
		adminCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.Equal(t, []seatResponse{
		{RoleID: "role-team-lead", Role: "Team lead", Count: 1},
		{RoleID: "role-service-volunteer", Role: "Service volunteer", Count: 6},
	}, decodeShiftShape(t, rec.Body.Bytes()).Shape)
}

// The rota page reads a Shape back through the listing, so an edit has to be
// visible there rather than only in the PUT's own answer — and only on the
// shift it was addressed at, which is the whole point of a per-Shift Shape.
func TestSaveShiftShapeIsVisibleInTheListing(t *testing.T) {
	store := shiftEditTestStore()
	handler := newTestHandler(store, testVolunteers())

	require.Equal(t, http.StatusOK, doRequest(t, handler, http.MethodPut,
		"/api/shifts/s1/shape",
		`{"seats":[{"roleId":"role-service-volunteer","count":2}]}`, adminCookie()).Code)

	rec := doRequest(t, handler, http.MethodGet, "/api/shifts", "")
	require.Equal(t, http.StatusOK, rec.Code)

	var listing struct {
		Shifts []shiftResponse `json:"shifts"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listing))
	require.Len(t, listing.Shifts, 2)
	assert.Equal(t, []seatResponse{
		{RoleID: "role-service-volunteer", Role: "Service volunteer", Count: 2},
	}, listing.Shifts[0].Shape)
	assert.Len(t, listing.Shifts[1].Shape, 2, "the other shift still asks for what it was minted with")
}

// Sending no Seats is how a Shift stops asking for anybody. It is allowed, and
// what it costs is said where allocation refuses over it.
func TestSaveShiftShapeAcceptsNothing(t *testing.T) {
	store := shiftEditTestStore()

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPut,
		"/api/shifts/s1/shape", `{"seats":[]}`, adminCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Empty(t, decodeShiftShape(t, rec.Body.Bytes()).Shape)
}

// A Shape is an allocator input: the solver filled Seats against it, so the
// allocated rota that leaves the shift times editable freezes this.
func TestSaveShiftShapeRefusedOnAnAllocatedRota(t *testing.T) {
	store := shiftEditTestStore()
	store.shiftsInRange[0].Allocated = true
	store.allocatedRotas = map[string]bool{"rota-1": true}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPut,
		"/api/shifts/s1/shape",
		`{"seats":[{"roleId":"role-service-volunteer","count":2}]}`, adminCookie())
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
}

// What a Shift asks for is its Shape and nothing above it, so an evening that
// wants two Team leads says so (issue #185).
func TestSaveShiftShapeTakesAnyCountOfAnyRole(t *testing.T) {
	store := shiftEditTestStore()

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPut,
		"/api/shifts/s1/shape",
		`{"seats":[{"roleId":"role-team-lead","count":2}]}`, adminCookie())
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// A pin promises somebody a named job on this shift, and the solver refuses a
// pin naming a Role the shift has no Seat for — so the Seat cannot be taken
// away underneath one, and the refusal says whose pin to remove.
func TestSaveShiftShapeRefusedWhenAPinWouldLoseItsSeat(t *testing.T) {
	store := shiftEditTestStore()
	store.manualPreallocations = []db.Preallocation{
		{ID: "pin-1", ShiftID: "s1", RoleID: "role-team-lead", VolunteerID: "vol-alice"},
	}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPut,
		"/api/shifts/s1/shape",
		`{"seats":[{"roleId":"role-service-volunteer","count":4}]}`, adminCookie())
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "Team lead")
}

func TestSaveShiftShapeUnknownShift(t *testing.T) {
	rec := doRequest(t, newTestHandler(shiftEditTestStore(), testVolunteers()), http.MethodPut,
		"/api/shifts/ghost/shape",
		`{"seats":[{"roleId":"role-service-volunteer","count":2}]}`, adminCookie())
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

func TestSaveShiftShapeRejectsUnknownFields(t *testing.T) {
	rec := doRequest(t, newTestHandler(shiftEditTestStore(), testVolunteers()), http.MethodPut,
		"/api/shifts/s1/shape",
		`{"seats":[{"roleId":"role-team-lead","count":1}],"date":"2026-12-20"}`, adminCookie())
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}

// Anyone may read what a shift asks for; only an admin may change it.
func TestSaveShiftShapeRequiresAdmin(t *testing.T) {
	store := shiftEditTestStore()

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPut,
		"/api/shifts/s1/shape",
		`{"seats":[{"roleId":"role-service-volunteer","count":2}]}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
}
