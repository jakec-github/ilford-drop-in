package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

func TestGetRotaDefaultsEndpoint(t *testing.T) {
	store := &mockStore{}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/rota-defaults", "", adminCookie())

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
	assert.JSONEq(t,
		`{"shiftStartTime":"19:30","shiftEndTime":"21:30","shiftTimezone":"Europe/London"}`,
		rec.Body.String())
}

// Settings nobody has filled in are a state to render, not an error: the times
// come back empty so the screen can say they are unset, and the zone falls back
// so the form has something to start on.
func TestGetRotaDefaultsEndpointUnset(t *testing.T) {
	store := &mockStore{rotaDefaults: &db.RotaDefaults{}}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/rota-defaults", "", adminCookie())

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t,
		`{"shiftStartTime":"","shiftEndTime":"","shiftTimezone":"Europe/London"}`,
		rec.Body.String())
}

// Both verbs are admin-only. Nothing a logged-out visitor sees needs the
// settings record, and the sections joining it are an admin's business.
func TestRotaDefaultsEndpointIsAdminOnly(t *testing.T) {
	handler := newTestHandler(&mockStore{}, testVolunteers())

	rec := doRequest(t, handler, http.MethodGet, "/api/rota-defaults", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = doRequest(t, handler, http.MethodPut, "/api/rota-defaults",
		`{"shiftStartTime":"19:30","shiftEndTime":"21:30","shiftTimezone":"Europe/London"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestSaveRotaDefaultsEndpoint(t *testing.T) {
	store := &mockStore{rotaDefaults: &db.RotaDefaults{}}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPut, "/api/rota-defaults",
		`{"shiftStartTime":"09:00","shiftEndTime":"12:15","shiftTimezone":"UTC"}`, adminCookie())

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, store.savedRotaDefaults, 1)
	assert.Equal(t, db.RotaDefaults{
		ShiftStartTime: "09:00", ShiftEndTime: "12:15", ShiftTimezone: "UTC",
	}, store.savedRotaDefaults[0])
	assert.JSONEq(t,
		`{"shiftStartTime":"09:00","shiftEndTime":"12:15","shiftTimezone":"UTC"}`,
		rec.Body.String())
}

// The answer carries the zone that was filled in for an admin who left the
// field blank, so the form shows what was actually stored rather than what was
// typed.
func TestSaveRotaDefaultsEndpointFillsInTheZone(t *testing.T) {
	store := &mockStore{rotaDefaults: &db.RotaDefaults{}}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPut, "/api/rota-defaults",
		`{"shiftStartTime":"19:30","shiftEndTime":"21:30","shiftTimezone":""}`, adminCookie())

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t,
		`{"shiftStartTime":"19:30","shiftEndTime":"21:30","shiftTimezone":"Europe/London"}`,
		rec.Body.String())
}

// An admin's mistake is a 400 carrying the message the service wrote, not a
// 500: the screen shows it beside the field.
func TestSaveRotaDefaultsEndpointRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"end before start": `{"shiftStartTime":"21:30","shiftEndTime":"19:30","shiftTimezone":"Europe/London"}`,
		"not a time":       `{"shiftStartTime":"half seven","shiftEndTime":"21:30","shiftTimezone":"Europe/London"}`,
		"no start":         `{"shiftStartTime":"","shiftEndTime":"21:30","shiftTimezone":"Europe/London"}`,
		"unknown zone":     `{"shiftStartTime":"19:30","shiftEndTime":"21:30","shiftTimezone":"Not/AZone"}`,
		"unknown field":    `{"shiftStartTime":"19:30","shiftEndTime":"21:30","shiftSize":4}`,
		"not json":         `nonsense`,
	}

	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			store := &mockStore{rotaDefaults: &db.RotaDefaults{}}

			rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPut, "/api/rota-defaults", request, adminCookie())

			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			assert.Empty(t, store.savedRotaDefaults)

			var body map[string]string
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.NotEmpty(t, body["error"], "the message is shown beside the field")
		})
	}
}
