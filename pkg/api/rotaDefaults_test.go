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

// defaultShapeOf reads the Shape part of a settings response, for the same
// reason shiftTimesOf reads the times: a test says which section it means.
func defaultShapeOf(t *testing.T, rec *httptest.ResponseRecorder) []seatResponse {
	t.Helper()
	var body struct {
		DefaultShape []seatResponse `json:"defaultShape"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body.DefaultShape
}

func TestGetRotaDefaultsEndpoint(t *testing.T) {
	store := &mockStore{}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/rota-defaults", "", organiserCookie())

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")

	body := shiftTimesOf(t, rec)
	assert.Equal(t, "19:30", body.ShiftStartTime)
	assert.Equal(t, "21:30", body.ShiftEndTime)
	assert.Equal(t, "Europe/London", body.ShiftTimezone)

	// Each Seat carries the Role's name as well as its id: the id is what an
	// edit names, the name is what an Organiser reads.
	assert.Equal(t, []seatResponse{
		{RoleID: "role-team-lead", Role: "Team lead", Count: 1},
		{RoleID: "role-service-volunteer", Role: "Service volunteer", Count: 4},
	}, defaultShapeOf(t, rec))
}

// Settings nobody has filled in are a state to render, not an error: the times
// come back empty so the screen can say they are unset, and the zone falls back
// so the form has something to start on.
func TestGetRotaDefaultsEndpointUnset(t *testing.T) {
	store := &mockStore{rotaDefaults: &db.RotaDefaults{}, noShape: true}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/rota-defaults", "", organiserCookie())

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	body := shiftTimesOf(t, rec)
	assert.Empty(t, body.ShiftStartTime)
	assert.Empty(t, body.ShiftEndTime)
	assert.Empty(t, defaultShapeOf(t, rec))
	assert.Equal(t, "Europe/London", body.ShiftTimezone)
}

// Both verbs are Organiser-only. Nothing a logged-out visitor sees needs the
// settings record, and the sections joining it are an Organiser's business.
func TestRotaDefaultsEndpointIsOrganiserOnly(t *testing.T) {
	handler := newTestHandler(&mockStore{}, testVolunteers())

	rec := doRequest(t, handler, http.MethodGet, "/api/rota-defaults", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = doRequest(t, handler, http.MethodPut, "/api/rota-defaults/shift-times",
		`{"shiftStartTime":"19:30","shiftEndTime":"21:30","shiftTimezone":"Europe/London"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = doRequest(t, handler, http.MethodPut, "/api/rota-defaults/shape",
		`{"seats":[{"roleId":"role-team-lead","count":1}]}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestSaveRotaDefaultsEndpoint(t *testing.T) {
	store := &mockStore{rotaDefaults: &db.RotaDefaults{}}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPut, "/api/rota-defaults/shift-times",
		`{"shiftStartTime":"09:00","shiftEndTime":"12:15","shiftTimezone":"UTC"}`, organiserCookie())

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

// The answer carries the zone that was filled in for an Organiser who left the
// field blank, so the form shows what was actually stored rather than what was
// typed.
func TestSaveRotaDefaultsEndpointFillsInTheZone(t *testing.T) {
	store := &mockStore{rotaDefaults: &db.RotaDefaults{}}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPut, "/api/rota-defaults/shift-times",
		`{"shiftStartTime":"19:30","shiftEndTime":"21:30","shiftTimezone":""}`, organiserCookie())

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, "Europe/London", shiftTimesOf(t, rec).ShiftTimezone)
}

// An Organiser's mistake is a 400 carrying the message the service wrote, not a
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

			rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPut, "/api/rota-defaults/shift-times", request, organiserCookie())

			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			assert.Empty(t, store.savedRotaDefaults)

			var body map[string]string
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.NotEmpty(t, body["error"], "the message is shown beside the field")
		})
	}
}

// rulesBody is the part of the settings record the rules section reads.
type rulesBody struct {
	AllocationRules []struct {
		ID    string `json:"id"`
		Kind  string `json:"kind"`
		Value int    `json:"value"`
		Scope struct {
			Include []string `json:"include"`
			Exclude []string `json:"exclude"`
		} `json:"scope"`
		Note    string `json:"note"`
		Ignored bool   `json:"ignored"`
	} `json:"allocationRules"`
	RuleLibrary []struct {
		ID         string `json:"id"`
		Family     string `json:"family"`
		Label      string `json:"label"`
		ValueLabel string `json:"valueLabel"`
		MinValue   int    `json:"minValue"`
		MaxValue   int    `json:"maxValue"`
	} `json:"ruleLibrary"`
}

func rulesOf(t *testing.T, rec *httptest.ResponseRecorder) rulesBody {
	t.Helper()
	var body rulesBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body
}

// The settings record carries the library as well as the rules, so the client
// holds no copy of what kinds exist: the list in Go is the only one. A stored
// rule this build cannot apply is still listed, marked ignored, so an Organiser
// can see it and take it off.
func TestGetRotaDefaultsCarriesTheRulesAndTheLibrary(t *testing.T) {
	store := &mockStore{rotaDefaults: &db.RotaDefaults{
		AllocationSettings: `{"rules":[` +
			`{"id":"a","kind":"shift_gap","value":2,"scope":{"exclude":["alice"]},"note":"rest"},` +
			`{"id":"b","kind":"phase_of_the_moon"}]}`,
	}}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/rota-defaults", "", organiserCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rulesOf(t, rec)

	require.Len(t, body.RuleLibrary, 4)
	assert.Equal(t, "max_frequency", body.RuleLibrary[0].ID)
	assert.Equal(t, "volunteer", body.RuleLibrary[0].Family)
	assert.NotEmpty(t, body.RuleLibrary[0].ValueLabel)
	assert.Equal(t, 100, body.RuleLibrary[0].MaxValue)
	assert.Equal(t, "shift", body.RuleLibrary[3].Family)

	require.Len(t, body.AllocationRules, 2)
	assert.Equal(t, "a", body.AllocationRules[0].ID)
	assert.Equal(t, 2, body.AllocationRules[0].Value)
	assert.Equal(t, []string{"alice"}, body.AllocationRules[0].Scope.Exclude)
	assert.NotNil(t, body.AllocationRules[0].Scope.Include, "an empty list is a list, not null")
	assert.Equal(t, "rest", body.AllocationRules[0].Note)
	assert.False(t, body.AllocationRules[0].Ignored)
	assert.True(t, body.AllocationRules[1].Ignored)
}

// Every change to a rule answers with the whole settings record, so the screen
// holds one thing after any save.
func TestAllocationRuleEndpoints(t *testing.T) {
	store := &mockStore{rotaDefaults: &db.RotaDefaults{ShiftStartTime: "19:30"}}
	handler := newTestHandler(store, testVolunteers())

	rec := doRequest(t, handler, http.MethodPost, "/api/rota-defaults/allocation-rules",
		`{"kind":"monthly_cap","value":1,"scope":{"include":["alice"],"exclude":[]},"note":"once a month"}`, organiserCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rules := rulesOf(t, rec).AllocationRules
	require.Len(t, rules, 1)
	id := rules[0].ID
	assert.NotEmpty(t, id)
	assert.Equal(t, "19:30", shiftTimesOf(t, rec).ShiftStartTime, "it is the whole record")

	rec = doRequest(t, handler, http.MethodPut, "/api/rota-defaults/allocation-rules/"+id,
		`{"value":2,"scope":{"include":[],"exclude":["bob"]},"note":""}`, organiserCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rules = rulesOf(t, rec).AllocationRules
	require.Len(t, rules, 1)
	assert.Equal(t, "monthly_cap", rules[0].Kind)
	assert.Equal(t, 2, rules[0].Value)
	assert.Equal(t, []string{"bob"}, rules[0].Scope.Exclude)

	rec = doRequest(t, handler, http.MethodDelete, "/api/rota-defaults/allocation-rules/"+id, "", organiserCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Empty(t, rulesOf(t, rec).AllocationRules)
}

// An Organiser's mistake is a 400 carrying the service's own message, not a 500.
func TestAddAllocationRuleRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"an unknown kind":         `{"kind":"phase_of_the_moon"}`,
		"a value out of range":    `{"kind":"max_frequency","value":400}`,
		"including and excluding": `{"kind":"shift_gap","value":1,"scope":{"include":["a"],"exclude":["b"]}}`,
		"an unknown field":        `{"kind":"male_required","colour":"blue"}`,
		"not json":                `nonsense`,
	}

	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			store := &mockStore{rotaDefaults: &db.RotaDefaults{}}

			rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPost,
				"/api/rota-defaults/allocation-rules", request, organiserCookie())

			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			assert.Empty(t, store.savedAllocationSettings)
		})
	}
}

func TestEditingAMissingAllocationRuleIsNotFound(t *testing.T) {
	store := &mockStore{rotaDefaults: &db.RotaDefaults{}}
	handler := newTestHandler(store, testVolunteers())

	rec := doRequest(t, handler, http.MethodPut, "/api/rota-defaults/allocation-rules/nope", `{"value":1}`, organiserCookie())
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

	rec = doRequest(t, handler, http.MethodDelete, "/api/rota-defaults/allocation-rules/nope", "", organiserCookie())
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

// Saving the Shape answers with the whole settings record, so the screen holds
// one thing after a save of any section rather than stitching answers together.
func TestSaveDefaultShapeEndpoint(t *testing.T) {
	store := &mockStore{noShape: true}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPut, "/api/rota-defaults/shape",
		`{"seats":[{"roleId":"role-team-lead","count":1},{"roleId":"role-service-volunteer","count":6}]}`,
		organiserCookie())

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, store.savedShapes, 1)
	assert.Equal(t, []db.DefaultShapeSeat{
		{RoleID: "role-team-lead", Seats: 1},
		{RoleID: "role-service-volunteer", Seats: 6},
	}, store.savedShapes[0])
	assert.Equal(t, []seatResponse{
		{RoleID: "role-team-lead", Role: "Team lead", Count: 1},
		{RoleID: "role-service-volunteer", Role: "Service volunteer", Count: 6},
	}, defaultShapeOf(t, rec))
	assert.Equal(t, "19:30", shiftTimesOf(t, rec).ShiftStartTime,
		"the times are in the answer too - it is the whole record")
}

// A Role dropped from the list is a Role the Shape no longer asks for, and an
// empty list is a Shape that asks for nothing — the only way to say either.
func TestSaveDefaultShapeEndpointEmpties(t *testing.T) {
	store := &mockStore{}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPut, "/api/rota-defaults/shape",
		`{"seats":[]}`, organiserCookie())

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, store.savedShapes, 1)
	assert.Empty(t, store.savedShapes[0])
	assert.Empty(t, defaultShapeOf(t, rec))
}

// Saving one section leaves the others alone — the store is told about this
// section only.
func TestSaveDefaultShapeLeavesTheShiftTimesAlone(t *testing.T) {
	store := &mockStore{}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPut, "/api/rota-defaults/shape",
		`{"seats":[{"roleId":"role-team-lead","count":1}]}`, organiserCookie())

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Empty(t, store.savedRotaDefaults, "the shift-time section is not written")
	assert.Equal(t, "19:30", shiftTimesOf(t, rec).ShiftStartTime)
}

// An Organiser's mistake is a 400 carrying the service's message.
func TestSaveDefaultShapeEndpointRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"no seats":            `{"seats":[{"roleId":"role-team-lead","count":0}]}`,
		"unknown role":        `{"seats":[{"roleId":"role-nobody","count":1}]}`,
		"the same role twice": `{"seats":[{"roleId":"role-team-lead","count":1},{"roleId":"role-team-lead","count":1}]}`,
		"unknown field":       `{"seats":[{"roleId":"role-team-lead","seats":1}]}`,
		"not json":            `nonsense`,
	}

	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			store := &mockStore{}

			rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPut, "/api/rota-defaults/shape", request, organiserCookie())

			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			assert.Empty(t, store.savedShapes)

			var body map[string]string
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.NotEmpty(t, body["error"], "the message is shown beside the field")
		})
	}
}
