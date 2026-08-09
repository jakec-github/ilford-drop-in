package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

type defineRotaResponse struct {
	Rotation struct {
		ID         string `json:"id"`
		Start      string `json:"start"`
		End        string `json:"end"`
		ShiftCount int    `json:"shiftCount"`
	} `json:"rotation"`
	Shifts []struct {
		ID   string `json:"id"`
		Date string `json:"date"`
	} `json:"shifts"`
}

// defineBody is the whole of what the endpoint takes: how many shifts, and from
// when. The hours and the Shape are the Rota Defaults' to say, and the endpoint
// spends them rather than taking them (issue #176).
func defineBody(shiftCount int, startDate string) string {
	return fmt.Sprintf(`{"shiftCount":%d,"startDate":%q}`, shiftCount, startDate)
}

func TestDefineRotaEndpoint(t *testing.T) {
	store := &mockStore{}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPost, "/api/rotations", defineBody(6, "2026-08-02"), adminCookie())
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var resp defineRotaResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp.Rotation.ID)
	assert.Equal(t, 6, resp.Rotation.ShiftCount)
	assert.Equal(t, "2026-08-02", resp.Rotation.Start, "the stated date, not one the server worked out")
	require.Len(t, resp.Shifts, 6)
	assert.Equal(t, resp.Rotation.Start, resp.Shifts[0].Date, "the rota starts on its first shift")
	assert.Equal(t, resp.Rotation.End, resp.Shifts[5].Date, "and ends on its last")

	// Every shift is identified, so a caller can act on one without a second read.
	for i, s := range resp.Shifts {
		assert.NotEmpty(t, s.ID, "shift %d has an id", i)
	}

	// Proves the rota persisted through the store rather than only being reported.
	require.Len(t, store.insertedRotations, 1)
	assert.Equal(t, resp.Rotation.ID, store.insertedRotations[0].ID)
	require.Len(t, store.insertedShifts, 6)
	assert.Equal(t, resp.Shifts[0].ID, store.insertedShifts[0].ID)
	assert.Equal(t, resp.Shifts[0].Date, store.insertedShifts[0].Date)
	assert.Equal(t, "2026-08-02T19:30:00", store.insertedShifts[0].StartAt, "minted with the default hours")
}

// The hours and the Shape come from the Rota Defaults, which the request cannot
// contradict — it cannot even mention them (issue #176). Spending them changes
// nothing: the settings the next rota starts from are the ones this one did.
func TestDefineRotaEndpoint_MintsFromTheRotaDefaults(t *testing.T) {
	store := &mockStore{
		rotaDefaults: &db.RotaDefaults{ShiftStartTime: "18:00", ShiftEndTime: "20:00", ShiftTimezone: "Europe/London"},
		defaultShape: []db.DefaultShapeSeat{{RoleID: "role-service-volunteer", Seats: 2}},
	}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPost, "/api/rotations", defineBody(2, "2026-08-02"), adminCookie())
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	require.Len(t, store.insertedShifts, 2)
	for i, s := range store.insertedShifts {
		assert.Equal(t, s.Date+"T18:00:00", s.StartAt, "shift %d runs the default hours", i)
		assert.Equal(t, s.Date+"T20:00:00", s.EndAt, "shift %d runs the default hours", i)
	}

	for _, s := range store.insertedShifts {
		assert.Equal(t,
			[]db.ShiftRequirement{{ShiftID: s.ID, RoleID: "role-service-volunteer", Seats: 2}},
			store.shiftRequirements[s.ID],
			"the shift asks for the default shape and nothing else")
	}

	assert.Empty(t, store.savedRotaDefaults, "defining spends the settings; it does not write them")
	assert.Empty(t, store.savedShapes, "nor the shape")
}

// A body naming the hours or the Shape is refused rather than obeyed or
// ignored: they are stated in the Rota Defaults and nowhere else, and a client
// still sending them is one that believes it is deciding something.
func TestDefineRotaEndpoint_RefusesAStatedShiftTimeOrShape(t *testing.T) {
	for _, body := range []string{
		`{"shiftCount":2,"startDate":"2026-08-02","shiftStartTime":"18:00","shiftEndTime":"20:00"}`,
		`{"shiftCount":2,"startDate":"2026-08-02","shape":[{"roleId":"role-team-lead","count":1}]}`,
	} {
		store := &mockStore{}

		rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPost, "/api/rotations", body, adminCookie())
		assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		assert.Empty(t, store.insertedRotations)
	}
}

// A deployment nobody has set the Rota Defaults on cannot define a rota, and is
// told which of them to go and set. Everything that only reads still reads — a
// narrowing of ADR 0006 that defining shares with allocating.
func TestDefineRotaEndpoint_RefusesUnsetRotaDefaults(t *testing.T) {
	for _, tt := range []struct {
		name  string
		store *mockStore
		says  string
	}{
		{
			name:  "no shift times",
			store: &mockStore{rotaDefaults: &db.RotaDefaults{ShiftTimezone: "Europe/London"}},
			says:  "the default shift start time and the default shift end time have not been set",
		},
		{
			name:  "no shape",
			store: &mockStore{noShape: true},
			says:  "the default shape has not been set",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := doRequest(t, newTestHandler(tt.store, testVolunteers()), http.MethodPost, "/api/rotations", defineBody(2, "2026-08-02"), adminCookie())
			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			assert.Contains(t, rec.Body.String(), tt.says)
			assert.Empty(t, tt.store.insertedRotations, "nothing is minted against settings nobody has stated")
		})
	}
}

// TestDefineRotaEndpoint_NotIdempotent pins the deliberate semantics: defining
// again makes a second rota rather than re-answering with the one that exists
// (issue #75). What has changed since is when a second define is allowed at
// all — the first rota has to have been allocated (issue #139).
func TestDefineRotaEndpoint_NotIdempotent(t *testing.T) {
	store := &mockStore{}
	handler := newTestHandler(store, testVolunteers())

	first := doRequest(t, handler, http.MethodPost, "/api/rotations", defineBody(2, "2026-08-02"), adminCookie())
	require.Equal(t, http.StatusCreated, first.Code, first.Body.String())

	var a, b defineRotaResponse
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &a))
	store.allocate(a.Rotation.ID)

	second := doRequest(t, handler, http.MethodPost, "/api/rotations", defineBody(2, "2026-08-16"), adminCookie())
	require.Equal(t, http.StatusCreated, second.Code, second.Body.String())
	require.NoError(t, json.Unmarshal(second.Body.Bytes(), &b))

	assert.NotEqual(t, a.Rotation.ID, b.Rotation.ID)
	assert.Greater(t, b.Rotation.Start, a.Rotation.End, "the second rota follows the first")
	assert.Len(t, store.insertedRotations, 2)
}

// One rota is in flight at a time, and defining a second is a 409 rather than a
// 400: the request is not malformed, it contradicts the state of the rota
// (issue #139).
func TestDefineRotaEndpoint_RefusedWhileARotaIsInFlight(t *testing.T) {
	store := &mockStore{}
	handler := newTestHandler(store, testVolunteers())

	first := doRequest(t, handler, http.MethodPost, "/api/rotations", defineBody(2, "2026-08-02"), adminCookie())
	require.Equal(t, http.StatusCreated, first.Code, first.Body.String())

	second := doRequest(t, handler, http.MethodPost, "/api/rotations", defineBody(2, "2026-08-16"), adminCookie())
	require.Equal(t, http.StatusConflict, second.Code, second.Body.String())
	assert.Contains(t, second.Body.String(), "already in flight")
	assert.Len(t, store.insertedRotations, 1, "the refused define minted nothing")
}

func TestDefineRotaEndpoint_Errors(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		store      *mockStore
		wantStatus int
	}{
		{
			name:       "zero shifts",
			body:       defineBody(0, "2026-08-02"),
			store:      &mockStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "negative shifts",
			body:       defineBody(-5, "2026-08-02"),
			store:      &mockStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing shift count",
			body:       `{}`,
			store:      &mockStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing start date",
			body:       `{"shiftCount":6}`,
			store:      &mockStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "malformed JSON",
			body:       `{"shiftCount":`,
			store:      &mockStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown field",
			body:       `{"shiftCount":6,"bogus":true}`,
			store:      &mockStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "non-numeric shift count",
			body:       `{"shiftCount":"six"}`,
			store:      &mockStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "store insert failure",
			body: defineBody(6, "2026-08-02"),
			store: func() *mockStore {
				s := &mockStore{}
				s.insertErr = errors.New("disk full")
				return s
			}(),
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "a date the drop-in already runs on",
			body: defineBody(6, "2026-08-02"),
			store: func() *mockStore {
				s := &mockStore{}
				s.insertErr = db.ErrShiftDateTaken
				return s
			}(),
			wantStatus: http.StatusConflict,
		},
		{
			name: "store read failure",
			body: defineBody(6, "2026-08-02"),
			store: func() *mockStore {
				s := &mockStore{}
				s.getRotationsErr = errors.New("connection refused")
				return s
			}(),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doRequest(t, newTestHandler(tt.store, testVolunteers()), http.MethodPost, "/api/rotations", tt.body, adminCookie())
			assert.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
			assert.Empty(t, tt.store.insertedRotations, "a rejected request must not define a rota")
		})
	}
}

// TestDefineRotaRequiresAdmin proves the route is gated: without a session the
// request is rejected and no rota is defined.
func TestDefineRotaRequiresAdmin(t *testing.T) {
	store := &mockStore{}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPost, "/api/rotations", defineBody(6, "2026-08-02"))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, store.insertedRotations, "an unauthenticated request must not define a rota")
}

func TestRotationsMethodNotAllowed(t *testing.T) {
	rec := doRequest(t, newTestHandler(&mockStore{}, testVolunteers()), http.MethodGet, "/api/rotations", "")
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

type rotaProposalBody struct {
	StartDate string `json:"startDate"`
}

// The proposal is the one thing the define form cannot work out for itself and
// the settings do not hold: where the next rota would begin.
func TestRotaProposalEndpoint(t *testing.T) {
	store := &mockStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2026-08-02", End: "2026-08-09", ShiftCount: 2, AllocatedDatetime: "2026-07-26T09:00:00Z"},
		},
	}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/rotations/proposed", "", adminCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp rotaProposalBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "2026-08-16", resp.StartDate, "the Sunday after the existing rota's last shift")
}

// A deployment nobody has configured still gets a date: the proposal is
// arithmetic over the rotas that exist and reads no settings at all. Defining is
// where unstated ones are turned away.
func TestRotaProposalEndpoint_NeedsNoSettings(t *testing.T) {
	store := &mockStore{rotaDefaults: &db.RotaDefaults{}, noShape: true}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/rotations/proposed", "", adminCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp rotaProposalBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp.StartDate)
}

func TestRotaProposalRequiresAdmin(t *testing.T) {
	rec := doRequest(t, newTestHandler(&mockStore{}, testVolunteers()), http.MethodGet, "/api/rotations/proposed", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

type rotaInFlightBodyResponse struct {
	Rotation *struct {
		ID         string `json:"id"`
		Start      string `json:"start"`
		End        string `json:"end"`
		ShiftCount int    `json:"shiftCount"`
		Asked      int    `json:"asked"`
		Sent       int    `json:"sent"`
		Replied    int    `json:"replied"`
	} `json:"rotation"`
}

// No rota in flight is an answer rather than a 404: it is the state in which a
// rota may be defined, and the screen renders it as the define form.
func TestRotaInFlightEndpoint_Nothing(t *testing.T) {
	store := &mockStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2026-08-02", End: "2026-08-09", ShiftCount: 2, AllocatedDatetime: "2026-07-26T09:00:00Z"},
		},
	}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/rotations/in-flight", "", adminCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp rotaInFlightBodyResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Nil(t, resp.Rotation)
}

// The rota in flight comes back with its round, in volunteers: how many hold a
// link, how many were emailed one, and how many have answered — the last being
// the number a discard confirmation quotes.
func TestRotaInFlightEndpoint_WithRound(t *testing.T) {
	store := &mockStore{
		rotations: []db.Rotation{
			{ID: "old", Start: "2026-07-05", End: "2026-07-12", ShiftCount: 2, AllocatedDatetime: "2026-06-28T09:00:00Z"},
			{ID: "live", Start: "2026-08-02", End: "2026-08-23", ShiftCount: 4},
		},
		availabilityRequests: []db.AvailabilityRequest{
			{ID: "req-1", RotaID: "live", VolunteerID: "alice", SentAt: "2026-07-20T09:00:00Z"},
			{ID: "req-2", RotaID: "live", VolunteerID: "bob", SentAt: "2026-07-20T09:00:00Z"},
			{ID: "req-3", RotaID: "live", VolunteerID: "carol"},
			{ID: "req-4", RotaID: "old", VolunteerID: "alice", SentAt: "2026-06-20T09:00:00Z"},
		},
		repliedRequestIDs: map[string]bool{"req-1": true, "req-4": true},
	}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/rotations/in-flight", "", adminCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp rotaInFlightBodyResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotNil(t, resp.Rotation)
	assert.Equal(t, "live", resp.Rotation.ID)
	assert.Equal(t, "2026-08-02", resp.Rotation.Start)
	assert.Equal(t, "2026-08-23", resp.Rotation.End)
	assert.Equal(t, 4, resp.Rotation.ShiftCount)
	assert.Equal(t, 3, resp.Rotation.Asked)
	assert.Equal(t, 2, resp.Rotation.Sent)
	assert.Equal(t, 1, resp.Rotation.Replied, "the allocated rota's answer is not this rota's")
}

func TestRotaInFlightRequiresAdmin(t *testing.T) {
	rec := doRequest(t, newTestHandler(&mockStore{}, testVolunteers()), http.MethodGet, "/api/rotations/in-flight", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// Discard is the release valve the one-rota-in-flight rule requires: it destroys
// the rota and everything hanging off it, and a define is possible again after.
func TestDiscardRotaEndpoint(t *testing.T) {
	store := &mockStore{
		rotations: []db.Rotation{
			{ID: "live", Start: "2026-08-02", End: "2026-08-23", ShiftCount: 4},
		},
		shifts: []db.Shift{
			{ID: "shift-1", RotaID: "live", Date: "2026-08-02"},
		},
		manualPreallocations: []db.Preallocation{
			{ID: "pin-1", ShiftID: "shift-1", Role: "Team lead", VolunteerID: "alice"},
		},
		availabilityRequests: []db.AvailabilityRequest{
			{ID: "req-1", RotaID: "live", VolunteerID: "alice", SentAt: "2026-07-20T09:00:00Z"},
		},
	}
	handler := newTestHandler(store, testVolunteers())

	rec := doRequest(t, handler, http.MethodDelete, "/api/rotations/live", "", adminCookie())
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, []string{"live"}, store.discardedRotaIDs)
	assert.Empty(t, store.rotations)
	assert.Empty(t, store.shifts)
	assert.Empty(t, store.manualPreallocations, "the pins go with the shifts they were on")
	assert.Empty(t, store.availabilityRequests, "and the round goes with the rota it asked about")

	// Nothing is in flight any more, so the next rota can be defined.
	rec = doRequest(t, handler, http.MethodGet, "/api/rotations/in-flight", "", adminCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp rotaInFlightBodyResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Nil(t, resp.Rotation)
}

// An allocated rota is never discarded: it is the rota people are turning up to,
// and the tool for changing one is an Alteration.
func TestDiscardRotaEndpoint_RefusesAnAllocatedRota(t *testing.T) {
	store := &mockStore{
		rotations: []db.Rotation{
			{ID: "done", Start: "2026-08-02", End: "2026-08-23", ShiftCount: 4, AllocatedDatetime: "2026-07-26T09:00:00Z"},
		},
	}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodDelete, "/api/rotations/done", "", adminCookie())
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "never discarded")
	assert.Len(t, store.rotations, 1, "the rota is still there")
	assert.Empty(t, store.discardedRotaIDs)
}

func TestDiscardRotaEndpoint_UnknownRota(t *testing.T) {
	rec := doRequest(t, newTestHandler(&mockStore{}, testVolunteers()), http.MethodDelete, "/api/rotations/nope", "", adminCookie())
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

func TestDiscardRotaEndpoint_StoreFailure(t *testing.T) {
	store := &mockStore{
		rotations:  []db.Rotation{{ID: "live", Start: "2026-08-02", End: "2026-08-23", ShiftCount: 4}},
		discardErr: errors.New("connection refused"),
	}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodDelete, "/api/rotations/live", "", adminCookie())
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Len(t, store.rotations, 1)
}

func TestDiscardRotaRequiresAdmin(t *testing.T) {
	store := &mockStore{
		rotations: []db.Rotation{{ID: "live", Start: "2026-08-02", End: "2026-08-23", ShiftCount: 4}},
	}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodDelete, "/api/rotations/live", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, store.discardedRotaIDs, "an unauthenticated request must not discard a rota")
}
