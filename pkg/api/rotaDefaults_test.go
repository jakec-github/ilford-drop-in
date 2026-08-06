package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// shiftTimesOf reads the shift-time part of a settings response. The record
// carries more than that now — the allocation settings and the registry of
// rules they answer — so a test about the times says which part it means
// rather than restating the whole document every time one grows.
func shiftTimesOf(t *testing.T, rec *httptest.ResponseRecorder) struct {
	ShiftStartTime string `json:"shiftStartTime"`
	ShiftEndTime   string `json:"shiftEndTime"`
	ShiftTimezone  string `json:"shiftTimezone"`
} {
	t.Helper()
	var body struct {
		ShiftStartTime string `json:"shiftStartTime"`
		ShiftEndTime   string `json:"shiftEndTime"`
		ShiftTimezone  string `json:"shiftTimezone"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body
}

func TestGetRotaDefaultsEndpoint(t *testing.T) {
	store := &mockStore{}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/rota-defaults", "", adminCookie())

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")

	body := shiftTimesOf(t, rec)
	assert.Equal(t, "19:30", body.ShiftStartTime)
	assert.Equal(t, "21:30", body.ShiftEndTime)
	assert.Equal(t, "Europe/London", body.ShiftTimezone)
}

// Settings nobody has filled in are a state to render, not an error: the times
// come back empty so the screen can say they are unset, and the zone falls back
// so the form has something to start on.
func TestGetRotaDefaultsEndpointUnset(t *testing.T) {
	store := &mockStore{rotaDefaults: &db.RotaDefaults{}}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/rota-defaults", "", adminCookie())

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	body := shiftTimesOf(t, rec)
	assert.Empty(t, body.ShiftStartTime)
	assert.Empty(t, body.ShiftEndTime)
	assert.Equal(t, "Europe/London", body.ShiftTimezone)
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

	body := shiftTimesOf(t, rec)
	assert.Equal(t, "09:00", body.ShiftStartTime)
	assert.Equal(t, "12:15", body.ShiftEndTime)
	assert.Equal(t, "UTC", body.ShiftTimezone)
}

// The answer carries the zone that was filled in for an admin who left the
// field blank, so the form shows what was actually stored rather than what was
// typed.
func TestSaveRotaDefaultsEndpointFillsInTheZone(t *testing.T) {
	store := &mockStore{rotaDefaults: &db.RotaDefaults{}}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPut, "/api/rota-defaults",
		`{"shiftStartTime":"19:30","shiftEndTime":"21:30","shiftTimezone":""}`, adminCookie())

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "Europe/London", shiftTimesOf(t, rec).ShiftTimezone)
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

// The settings record carries the rules there are as well as the answers to
// them, because the screen cannot draw a list of switches from the answers
// alone — a rule nobody has answered still has to appear, switched off.
func TestGetRotaDefaultsCarriesTheConstraintRegistry(t *testing.T) {
	store := &mockStore{rotaDefaults: &db.RotaDefaults{
		AllocationSettings: `{"enabled":{"no_back_to_back":true},"maxFrequency":0.34}`,
	}}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/rota-defaults", "", adminCookie())

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		AllocationSettings struct {
			Enabled      map[string]bool `json:"enabled"`
			MaxFrequency float64         `json:"maxFrequency"`
		} `json:"allocationSettings"`
		SwitchableConstraints []struct {
			Name       string `json:"name"`
			Label      string `json:"label"`
			ValueLabel string `json:"valueLabel"`
		} `json:"switchableConstraints"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	require.Len(t, body.SwitchableConstraints, 4)
	assert.Equal(t, "max_frequency", body.SwitchableConstraints[0].Name)
	assert.NotEmpty(t, body.SwitchableConstraints[0].Label)
	assert.NotEmpty(t, body.SwitchableConstraints[0].ValueLabel,
		"the one rule carrying a value says so, so the screen knows to ask for one")

	// Every rule has a definite answer, including the three nobody answered.
	assert.Equal(t, map[string]bool{
		"max_frequency":       false,
		"male_required":       false,
		"no_back_to_back":     true,
		"one_shift_per_month": false,
	}, body.AllocationSettings.Enabled)
	assert.Equal(t, 0.34, body.AllocationSettings.MaxFrequency)
}

func TestSaveAllocationSettingsEndpoint(t *testing.T) {
	store := &mockStore{rotaDefaults: &db.RotaDefaults{}}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPut,
		"/api/rota-defaults/allocation-settings",
		`{"enabled":{"male_required":true,"max_frequency":true},"maxFrequency":0.5}`, adminCookie())

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, store.savedAllocationSettings, 1)
	assert.JSONEq(t,
		`{"enabled":{"male_required":true,"max_frequency":true},"maxFrequency":0.5}`,
		store.savedAllocationSettings[0])

	// The reply is the whole section as it now stands, so the screen shows what
	// was stored rather than what was typed.
	assert.JSONEq(t, `{
		"enabled": {"max_frequency": true, "male_required": true,
		            "no_back_to_back": false, "one_shift_per_month": false},
		"maxFrequency": 0.5
	}`, rec.Body.String())
}

// Saving one section of the settings leaves the others alone — the store is
// told about this section only.
func TestSaveAllocationSettingsLeavesTheShiftTimesAlone(t *testing.T) {
	store := &mockStore{}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPut,
		"/api/rota-defaults/allocation-settings", `{"enabled":{"no_back_to_back":true}}`, adminCookie())

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Empty(t, store.savedRotaDefaults, "the shift-time section is not written")

	rec = doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/rota-defaults", "", adminCookie())
	assert.Equal(t, "19:30", shiftTimesOf(t, rec).ShiftStartTime)
}

// An admin's mistake is a 400 carrying the service's own message, not a 500.
func TestSaveAllocationSettingsRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"frequency on with no value": `{"enabled":{"max_frequency":true}}`,
		"frequency out of range":     `{"enabled":{"max_frequency":true},"maxFrequency":4}`,
		"unknown field":              `{"enabled":{},"maxAllocationFrequency":0.5}`,
		"not json":                   `nonsense`,
	}

	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			store := &mockStore{rotaDefaults: &db.RotaDefaults{}}

			rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPut,
				"/api/rota-defaults/allocation-settings", request, adminCookie())

			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			assert.Empty(t, store.savedAllocationSettings)
		})
	}
}

func TestSaveAllocationSettingsIsAdminOnly(t *testing.T) {
	rec := doRequest(t, newTestHandler(&mockStore{}, testVolunteers()), http.MethodPut,
		"/api/rota-defaults/allocation-settings", `{"enabled":{}}`)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
