package api

import (
	"encoding/json"
	"errors"
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

func TestDefineRotaEndpoint(t *testing.T) {
	store := &mockStore{}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPost, "/rotations", `{"shiftCount":6}`, adminCookie())
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var resp defineRotaResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp.Rotation.ID)
	assert.Equal(t, 6, resp.Rotation.ShiftCount)
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
}

// TestDefineRotaEndpoint_NotIdempotent pins the deliberate semantics: two calls
// define two consecutive rotas, exactly as two CLI invocations do (issue #75).
func TestDefineRotaEndpoint_NotIdempotent(t *testing.T) {
	store := &mockStore{}
	handler := newTestHandler(store, testVolunteers())

	first := doRequest(t, handler, http.MethodPost, "/rotations", `{"shiftCount":2}`, adminCookie())
	require.Equal(t, http.StatusCreated, first.Code, first.Body.String())
	second := doRequest(t, handler, http.MethodPost, "/rotations", `{"shiftCount":2}`, adminCookie())
	require.Equal(t, http.StatusCreated, second.Code, second.Body.String())

	var a, b defineRotaResponse
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &a))
	require.NoError(t, json.Unmarshal(second.Body.Bytes(), &b))

	assert.NotEqual(t, a.Rotation.ID, b.Rotation.ID)
	assert.Greater(t, b.Rotation.Start, a.Rotation.End, "the second rota follows the first")
	assert.Len(t, store.insertedRotations, 2)
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
			body:       `{"shiftCount":0}`,
			store:      &mockStore{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "negative shifts",
			body:       `{"shiftCount":-5}`,
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
			body: `{"shiftCount":6}`,
			store: func() *mockStore {
				s := &mockStore{}
				s.insertErr = errors.New("disk full")
				return s
			}(),
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "store read failure",
			body: `{"shiftCount":6}`,
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
			rec := doRequest(t, newTestHandler(tt.store, testVolunteers()), http.MethodPost, "/rotations", tt.body, adminCookie())
			assert.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
			assert.Empty(t, tt.store.insertedRotations, "a rejected request must not define a rota")
		})
	}
}

// TestDefineRotaEndpoint_ExistingRota checks the start date is derived from the
// rota already in the store, not from today.
func TestDefineRotaEndpoint_ExistingRota(t *testing.T) {
	store := &mockStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2026-08-02", End: "2026-08-09", ShiftCount: 2},
		},
	}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPost, "/rotations", `{"shiftCount":2}`, adminCookie())
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var resp defineRotaResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "2026-08-16", resp.Rotation.Start, "the Sunday after the existing rota's last shift")
	assert.Equal(t, "2026-08-23", resp.Rotation.End)
}

// TestDefineRotaRequiresAdmin proves the route is gated: without a session the
// request is rejected and no rota is defined.
func TestDefineRotaRequiresAdmin(t *testing.T) {
	store := &mockStore{}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPost, "/rotations", `{"shiftCount":6}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, store.insertedRotations, "an unauthenticated request must not define a rota")
}

func TestRotationsMethodNotAllowed(t *testing.T) {
	rec := doRequest(t, newTestHandler(&mockStore{}, testVolunteers()), http.MethodGet, "/rotations", "")
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}
