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

// shiftPatternBody is the Shift pattern part of a settings response.
type shiftPatternBody struct {
	ShiftPattern   string                 `json:"shiftPattern"`
	PatternEntries []patternEntryResponse `json:"patternEntries"`
	ShiftTimezone  string                 `json:"shiftTimezone"`
}

// shiftPatternOf reads the Shift pattern part of a settings response. The
// record carries more than that — the Shape, the allocation settings and the
// registry of rules they answer — so a test about the pattern says which part
// it means rather than restating the whole document every time one grows.
func shiftPatternOf(t *testing.T, rec *httptest.ResponseRecorder) shiftPatternBody {
	t.Helper()
	var body shiftPatternBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body
}

// defaultShapeOf reads the Shape part of a settings response, for the same
// reason shiftPatternOf reads the pattern: a test says which section it means.
func defaultShapeOf(t *testing.T, rec *httptest.ResponseRecorder) []seatResponse {
	t.Helper()
	var body struct {
		DefaultShape []seatResponse `json:"defaultShape"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body.DefaultShape
}

// weeklySunday is every Sunday at 19:30 for two hours, the first on Sunday 4
// January 2026. jsonString puts it in a request body.
const weeklySunday = "BEGIN:VEVENT\nDTSTART:20260104T193000\nDURATION:PT2H\nRRULE:FREQ=WEEKLY;BYDAY=SU\nEND:VEVENT"

// jsonString is s as a JSON string literal, quotes and all.
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestGetRotaDefaultsEndpoint(t *testing.T) {
	store := &mockStore{}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/rota-defaults", "", organiserCookie())

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")

	// Each entry comes back described, its first date and length included.
	assert.Equal(t, shiftPatternBody{
		ShiftPattern: weeklySunday,
		PatternEntries: []patternEntryResponse{{
			Number: 1, Line: 1, Description: "Every Sunday at 19:30 for 2 hours, from 4 Jan 2026",
			Problems: []string{}, Warnings: []string{},
		}},
		ShiftTimezone: "Europe/London",
	}, shiftPatternOf(t, rec))

	// Each Seat carries the Role's name as well as its id: the id is what an
	// edit names, the name is what an Organiser reads.
	assert.Equal(t, []seatResponse{
		{RoleID: "role-team-lead", Role: "Team lead", Count: 1},
		{RoleID: "role-service-volunteer", Role: "Service volunteer", Count: 4},
	}, defaultShapeOf(t, rec))
}

// Settings nobody has filled in are a state to render, not an error. The zone
// comes back empty rather than as the fallback, so the config page can tell
// nobody has chosen one and save the browser's.
func TestGetRotaDefaultsEndpointUnset(t *testing.T) {
	store := &mockStore{rotaDefaults: &db.RotaDefaults{}, noShape: true}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/rota-defaults", "", organiserCookie())

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.Equal(t, shiftPatternBody{PatternEntries: []patternEntryResponse{}}, shiftPatternOf(t, rec))
	assert.Empty(t, defaultShapeOf(t, rec))
}

// Every verb is Organiser-only. Nothing a logged-out visitor sees needs the
// settings record, and the sections joining it are an Organiser's business.
func TestRotaDefaultsEndpointIsOrganiserOnly(t *testing.T) {
	handler := newTestHandler(&mockStore{}, testVolunteers())

	rec := doRequest(t, handler, http.MethodGet, "/api/rota-defaults", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = doRequest(t, handler, http.MethodPut, "/api/rota-defaults/shift-pattern",
		`{"shiftPattern":`+jsonString(weeklySunday)+`}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = doRequest(t, handler, http.MethodPut, "/api/rota-defaults/timezone", `{"shiftTimezone":"UTC"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = doRequest(t, handler, http.MethodPost, "/api/rota-defaults/shift-pattern/preview",
		`{"shiftPattern":`+jsonString(weeklySunday)+`,"from":"2026-11-01","to":"2026-12-01"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	rec = doRequest(t, handler, http.MethodPut, "/api/rota-defaults/shape",
		`{"seats":[{"roleId":"role-team-lead","count":1}]}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestSaveShiftPatternEndpoint(t *testing.T) {
	store := &mockStore{rotaDefaults: &db.RotaDefaults{ShiftTimezone: "UTC"}}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPut, "/api/rota-defaults/shift-pattern",
		`{"shiftPattern":`+jsonString(weeklySunday)+`}`, organiserCookie())

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, []string{weeklySunday}, store.savedPatterns)
	assert.Empty(t, store.savedZones, "the zone is its own section")

	body := shiftPatternOf(t, rec)
	assert.Equal(t, weeklySunday, body.ShiftPattern)
	assert.Equal(t, "UTC", body.ShiftTimezone)
	require.Len(t, body.PatternEntries, 1)
}

// Saving the pattern answers with the whole record, rules and all, so the
// screen does not lose the rules it is showing by holding the answer.
func TestSaveShiftPatternAnswersWithTheRules(t *testing.T) {
	store := &mockStore{rotaDefaults: &db.RotaDefaults{
		AllocationSettings: `{"rules":[{"id":"a","kind":"shift_gap","value":1,"scope":{}}]}`,
	}}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPut, "/api/rota-defaults/shift-pattern",
		`{"shiftPattern":`+jsonString(weeklySunday)+`}`, organiserCookie())

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	body := rulesOf(t, rec)
	require.Len(t, body.AllocationRules, 1)
	assert.Equal(t, "a", body.AllocationRules[0].ID)
}

// An Organiser's mistake is a 400 carrying the message the service wrote, not a
// 500: the screen shows it beside the field. The fields the pattern used to
// carry beside it are refused as any unknown field is.
func TestSaveShiftPatternEndpointRejectsBadInput(t *testing.T) {
	cases := map[string]string{
		"no pattern":       `{"shiftPattern":""}`,
		"a bare RRULE":     `{"shiftPattern":"RRULE:FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=30"}`,
		"a Start from":     `{"shiftPattern":` + jsonString(weeklySunday) + `,"patternStartFrom":"2026-11-01"}`,
		"a length":         `{"shiftPattern":` + jsonString(weeklySunday) + `,"shiftLengthMinutes":120}`,
		"a zone alongside": `{"shiftPattern":` + jsonString(weeklySunday) + `,"shiftTimezone":"UTC"}`,
		"not json":         `nonsense`,
	}

	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			store := &mockStore{rotaDefaults: &db.RotaDefaults{}}

			rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPut, "/api/rota-defaults/shift-pattern", request, organiserCookie())

			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			assert.Empty(t, store.savedPatterns)

			var body map[string]string
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.NotEmpty(t, body["error"], "the message is shown beside the field")
		})
	}
}

// The time zone is its own section: saved alone, and answered with the whole
// record.
func TestSaveTimezoneEndpoint(t *testing.T) {
	store := &mockStore{rotaDefaults: &db.RotaDefaults{ShiftPattern: weeklySunday}}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPut, "/api/rota-defaults/timezone",
		`{"shiftTimezone":"America/New_York"}`, organiserCookie())

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, []string{"America/New_York"}, store.savedZones)
	assert.Empty(t, store.savedPatterns)
	body := shiftPatternOf(t, rec)
	assert.Equal(t, "America/New_York", body.ShiftTimezone)
	assert.Equal(t, weeklySunday, body.ShiftPattern)
}

// A zone the server cannot load is refused with a message, and nothing is
// written.
func TestSaveTimezoneEndpointRefusesAZoneItCannotLoad(t *testing.T) {
	for _, request := range []string{`{"shiftTimezone":"Not/AZone"}`, `{"shiftTimezone":""}`, `{"shiftTimezone":"Local"}`, `{"zone":"UTC"}`} {
		t.Run(request, func(t *testing.T) {
			store := &mockStore{rotaDefaults: &db.RotaDefaults{}}
			rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPut, "/api/rota-defaults/timezone", request, organiserCookie())
			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			assert.Empty(t, store.savedZones)
		})
	}
}

// The preview answers a pattern still being written entry by entry, and with
// where its Shifts start and for how long, so the screen can describe it and
// draw its calendar from the same arithmetic that mints Shifts. It writes
// nothing.
func TestShiftPatternPreviewEndpoint(t *testing.T) {
	store := &mockStore{rotaDefaults: &db.RotaDefaults{ShiftTimezone: "Europe/London"}}
	broken := "BEGIN:VEVENT\nDTSTART:20261101T100000\nDURATION:PT1H\nRRULE:FREQ=DAILY\nRDATE:20261101T100000\nEND:VEVENT"

	request := `{"shiftPattern":` + jsonString(weeklySunday+"\n\n"+broken) + `,"from":"2026-11-01","to":"2026-11-15"}`
	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPost, "/api/rota-defaults/shift-pattern/preview",
		request, organiserCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body shiftPatternPreviewResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Entries, 2)
	assert.Equal(t, 1, body.Entries[0].Line)
	assert.Equal(t, "Every Sunday at 19:30 for 2 hours, from 4 Jan 2026", body.Entries[0].Description)
	assert.Equal(t, 7, body.Entries[1].Line, "a blank line still counts")
	assert.NotEmpty(t, body.Entries[1].Problems)
	assert.Empty(t, body.Starts, "a pattern with a problem generates nothing")
	assert.Empty(t, body.StartsFrom)
	assert.Empty(t, store.savedPatterns)

	rec = doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPost, "/api/rota-defaults/shift-pattern/preview",
		`{"shiftPattern":`+jsonString(weeklySunday)+`,"from":"2026-11-01","to":"2026-11-15"}`, organiserCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "2026-01-04", body.StartsFrom)
	assert.Equal(t, []patternStartResponse{
		{Start: "2026-11-01T19:30:00", LengthMinutes: 120},
		{Start: "2026-11-08T19:30:00", LengthMinutes: 120},
	}, body.Starts)
}

// A pasted calendar event reads, with warnings for what it carries that a
// pattern does not use.
func TestShiftPatternPreviewEndpointWarnsAboutAPastedEvent(t *testing.T) {
	pasted := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:abc@example.com\r\nDTSTAMP:20261001T120000Z\r\n" +
		"SUMMARY:Drop-in\r\nDTSTART:20261101T193000\r\nDURATION:PT3H\r\nRRULE:FREQ=WEEKLY;\r\n BYDAY=SU\r\nEND:VEVENT\r\nEND:VCALENDAR"
	rec := doRequest(t, newTestHandler(&mockStore{}, testVolunteers()), http.MethodPost, "/api/rota-defaults/shift-pattern/preview",
		`{"shiftPattern":`+jsonString(pasted)+`,"from":"2026-11-01","to":"2026-11-15"}`, organiserCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body shiftPatternPreviewResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Entries, 1)
	assert.Empty(t, body.Entries[0].Problems)
	assert.NotEmpty(t, body.Entries[0].Warnings)
	assert.NotEmpty(t, body.Warnings, "the VCALENDAR around it is read past")
	assert.Len(t, body.Starts, 2)
}

func TestShiftPatternPreviewEndpointRejectsARangeItCannotRead(t *testing.T) {
	rec := doRequest(t, newTestHandler(&mockStore{}, testVolunteers()), http.MethodPost, "/api/rota-defaults/shift-pattern/preview",
		`{"shiftPattern":`+jsonString(weeklySunday)+`,"from":"2026-12-01","to":"2026-11-01"}`, organiserCookie())
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
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
	store := &mockStore{rotaDefaults: &db.RotaDefaults{ShiftPattern: weeklySunday}}
	handler := newTestHandler(store, testVolunteers())

	rec := doRequest(t, handler, http.MethodPost, "/api/rota-defaults/allocation-rules",
		`{"kind":"monthly_cap","value":1,"scope":{"include":["alice"],"exclude":[]},"note":"once a month"}`, organiserCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rules := rulesOf(t, rec).AllocationRules
	require.Len(t, rules, 1)
	id := rules[0].ID
	assert.NotEmpty(t, id)
	assert.Equal(t, weeklySunday, shiftPatternOf(t, rec).ShiftPattern, "it is the whole record")

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
		"an unknown field":        `{"kind":"shift_requires","option":"Male","colour":"blue"}`,
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
	assert.Equal(t, weeklySunday, shiftPatternOf(t, rec).ShiftPattern,
		"the pattern is in the answer too - it is the whole record")
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
func TestSaveDefaultShapeLeavesTheShiftPatternAlone(t *testing.T) {
	store := &mockStore{}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPut, "/api/rota-defaults/shape",
		`{"seats":[{"roleId":"role-team-lead","count":1}]}`, organiserCookie())

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Empty(t, store.savedPatterns, "the shift pattern section is not written")
	assert.Equal(t, weeklySunday, shiftPatternOf(t, rec).ShiftPattern)
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
