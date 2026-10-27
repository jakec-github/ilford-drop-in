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

func overridesOf(t *testing.T, rec *httptest.ResponseRecorder) []overrideResponse {
	t.Helper()
	var body listOverridesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body.Overrides
}

func overrideIDsOf(overrides []overrideResponse) []string {
	ids := make([]string, len(overrides))
	for i, o := range overrides {
		ids[i] = o.ID
	}
	return ids
}

// Every write answers with the whole list, each Override numbered, described
// by the server's evaluator and with its people named.
func TestOverrideEndpoints(t *testing.T) {
	store := &mockStore{}
	handler := newTestHandler(store, testVolunteers())

	rec := doRequest(t, handler, http.MethodPost, "/api/rota-defaults/overrides",
		`{"rrule":"FREQ=MONTHLY;BYDAY=1SU","startFrom":"2026-11-01","pins":[
			{"roleId":"role-service-volunteer","custom":"St John's team"},
			{"roleId":"role-team-lead","volunteerId":"alice"}]}`, organiserCookie())
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	overrides := overridesOf(t, rec)
	require.Len(t, overrides, 1)
	first := overrides[0]
	assert.Equal(t, 1, first.Number)
	assert.Equal(t, "2026-11-01", first.StartFrom)
	assert.Equal(t, "date", first.Granularity)
	assert.NotEmpty(t, first.Description)
	assert.Empty(t, first.Problems)
	assert.Nil(t, first.Shape, "no Shape stated leaves the default")
	require.Len(t, first.Pins, 2)
	assert.Equal(t, "Team lead", first.Pins[0].Role, "Pins read in the order their Seats are filled")
	assert.Equal(t, "Alice", first.Pins[0].Name)
	assert.Equal(t, "St John's team", first.Pins[1].Name)

	rec = doRequest(t, handler, http.MethodPost, "/api/rota-defaults/overrides",
		`{"rrule":"FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=30","startFrom":"2026-11-01","shape":[]}`, organiserCookie())
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	overrides = overridesOf(t, rec)
	require.Len(t, overrides, 2)
	second := overrides[1]
	assert.Equal(t, 2, second.Number, "a new Override goes last")
	assert.Equal(t, "time", second.Granularity)
	require.NotNil(t, second.Shape)
	assert.Empty(t, *second.Shape, "an empty Shape asks for nobody")

	rec = doRequest(t, handler, http.MethodPut, "/api/rota-defaults/overrides/"+first.ID,
		`{"rrule":"FREQ=MONTHLY;BYDAY=1SU","startFrom":"2026-12-06","shape":[{"roleId":"role-service-volunteer","count":2}],"pins":[]}`,
		organiserCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	overrides = overridesOf(t, rec)
	assert.Equal(t, []string{first.ID, second.ID}, overrideIDsOf(overrides), "an edit keeps its place")
	assert.Equal(t, "2026-12-06", overrides[0].StartFrom)
	require.NotNil(t, overrides[0].Shape)
	assert.Equal(t, []seatResponse{{RoleID: "role-service-volunteer", Role: "Service volunteer", Count: 2}}, *overrides[0].Shape)
	assert.Empty(t, overrides[0].Pins)
	assert.Nil(t, overrides[0].StartTime, "null leaves the pattern's start")
	assert.Nil(t, overrides[0].LengthMinutes, "and its length")

	rec = doRequest(t, handler, http.MethodPut, "/api/rota-defaults/overrides/"+first.ID,
		`{"rrule":"FREQ=MONTHLY;BYDAY=1SU","startFrom":"2026-12-06","startTime":"18:00","lengthMinutes":150}`,
		organiserCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	overrides = overridesOf(t, rec)
	require.NotNil(t, overrides[0].StartTime)
	assert.Equal(t, "18:00", *overrides[0].StartTime)
	require.NotNil(t, overrides[0].LengthMinutes)
	assert.Equal(t, 150, *overrides[0].LengthMinutes)

	rec = doRequest(t, handler, http.MethodPut, "/api/rota-defaults/overrides/order",
		`{"ids":["`+second.ID+`","`+first.ID+`"]}`, organiserCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	overrides = overridesOf(t, rec)
	assert.Equal(t, []string{second.ID, first.ID}, overrideIDsOf(overrides))
	assert.Equal(t, 1, overrides[0].Number)

	rec = doRequest(t, handler, http.MethodDelete, "/api/rota-defaults/overrides/"+second.ID, "", organiserCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, []string{first.ID}, overrideIDsOf(overridesOf(t, rec)))

	rec = doRequest(t, handler, http.MethodGet, "/api/rota-defaults/overrides", "", organiserCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Len(t, overridesOf(t, rec), 1)
}

// A blank Start from is today, as for the pattern.
func TestAddOverrideStartsTodayByDefault(t *testing.T) {
	store := &mockStore{}
	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPost, "/api/rota-defaults/overrides",
		`{"rrule":"FREQ=WEEKLY;BYDAY=SU"}`, organiserCookie())
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.Regexp(t, `^\d{4}-\d{2}-\d{2}$`, overridesOf(t, rec)[0].StartFrom)
}

// An Override is refused only for what cannot be represented or read. Pinning
// somebody into a Role they do not hold is the Organiser's call (ADR 0010).
func TestAddOverrideRejectsBadInput(t *testing.T) {
	cases := map[string]struct {
		body   string
		status int
	}{
		"an unreadable rule":       {`{"rrule":"FREQ=SOMETIMES","startFrom":"2026-11-01"}`, http.StatusBadRequest},
		"an empty rule":            {`{"rrule":"  ","startFrom":"2026-11-01"}`, http.StatusBadRequest},
		"an hour without a minute": {`{"rrule":"FREQ=WEEKLY;BYDAY=SU;BYHOUR=10","startFrom":"2026-11-01"}`, http.StatusBadRequest},
		"a start from not a date":  {`{"rrule":"FREQ=WEEKLY;BYDAY=SU","startFrom":"1 November"}`, http.StatusBadRequest},
		"an unknown role":          {`{"rrule":"FREQ=WEEKLY;BYDAY=SU","pins":[{"roleId":"nope","custom":"x"}]}`, http.StatusBadRequest},
		"a pin naming nobody":      {`{"rrule":"FREQ=WEEKLY;BYDAY=SU","pins":[{"roleId":"role-team-lead"}]}`, http.StatusBadRequest},
		"a pin naming two": {`{"rrule":"FREQ=WEEKLY;BYDAY=SU","pins":[{"roleId":"role-team-lead","volunteerId":"alice","custom":"x"}]}`,
			http.StatusBadRequest},
		"one person pinned twice": {`{"rrule":"FREQ=WEEKLY;BYDAY=SU","pins":[
			{"roleId":"role-team-lead","volunteerId":"alice"},{"roleId":"role-service-volunteer","volunteerId":"alice"}]}`,
			http.StatusBadRequest},
		"a seat count of zero": {`{"rrule":"FREQ=WEEKLY;BYDAY=SU","shape":[{"roleId":"role-team-lead","count":0}]}`,
			http.StatusBadRequest},
		"a volunteer not on the roster": {`{"rrule":"FREQ=WEEKLY;BYDAY=SU","pins":[{"roleId":"role-team-lead","volunteerId":"zed"}]}`,
			http.StatusNotFound},
		"an unknown field": {`{"rrule":"FREQ=WEEKLY;BYDAY=SU","colour":"blue"}`, http.StatusBadRequest},
		"a start time not a time": {`{"rrule":"FREQ=WEEKLY;BYDAY=SU","startTime":"25:00"}`, http.StatusBadRequest},
		"a length of nothing":     {`{"rrule":"FREQ=WEEKLY;BYDAY=SU","lengthMinutes":0}`, http.StatusBadRequest},
		"a length past a day":     {`{"rrule":"FREQ=WEEKLY;BYDAY=SU","lengthMinutes":1441}`, http.StatusBadRequest},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			store := &mockStore{}
			rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPost,
				"/api/rota-defaults/overrides", c.body, organiserCookie())
			assert.Equal(t, c.status, rec.Code, rec.Body.String())
			assert.Empty(t, store.overrides)
		})
	}

	// Bob is not a Team lead, and that is no reason to refuse.
	store := &mockStore{}
	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPost, "/api/rota-defaults/overrides",
		`{"rrule":"FREQ=WEEKLY;BYDAY=SU","pins":[{"roleId":"role-team-lead","volunteerId":"bob"}]}`, organiserCookie())
	assert.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
}

func TestEditingAMissingOverrideIsNotFound(t *testing.T) {
	handler := newTestHandler(&mockStore{}, testVolunteers())

	rec := doRequest(t, handler, http.MethodPut, "/api/rota-defaults/overrides/nope",
		`{"rrule":"FREQ=WEEKLY;BYDAY=SU","startFrom":"2026-11-01"}`, organiserCookie())
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())

	rec = doRequest(t, handler, http.MethodDelete, "/api/rota-defaults/overrides/nope", "", organiserCookie())
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

// A reorder written from a stale list — missing an Override somebody else
// added, or naming one since removed — is a conflict, and moves nothing.
func TestReorderOverridesRefusesAStaleList(t *testing.T) {
	store := &mockStore{overrides: []db.Override{
		{ID: "a", RRule: "FREQ=WEEKLY;BYDAY=SU", StartFrom: "2026-11-01", Granularity: db.OverrideMatchesDate},
		{ID: "b", RRule: "FREQ=WEEKLY;BYDAY=SA", StartFrom: "2026-11-01", Granularity: db.OverrideMatchesDate},
	}}
	handler := newTestHandler(store, testVolunteers())

	for _, ids := range []string{`["b"]`, `["b","a","c"]`, `["b","c"]`} {
		rec := doRequest(t, handler, http.MethodPut, "/api/rota-defaults/overrides/order", `{"ids":`+ids+`}`, organiserCookie())
		assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	}
	assert.Equal(t, "a", store.overrides[0].ID)
}

// The live reading says what a rule being written means, or what is wrong
// with it, without saving anything.
func TestReadOverrideRuleEndpoint(t *testing.T) {
	store := &mockStore{}
	handler := newTestHandler(store, testVolunteers())

	var reading overrideRuleResponse
	rec := doRequest(t, handler, http.MethodPost, "/api/rota-defaults/overrides/reading",
		`{"rrule":"FREQ=WEEKLY;BYDAY=SU;BYHOUR=10;BYMINUTE=0","startFrom":"2026-11-01"}`, organiserCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &reading))
	assert.Equal(t, "time", reading.Granularity)
	assert.NotEmpty(t, reading.Description)
	assert.Empty(t, reading.Problems)

	rec = doRequest(t, handler, http.MethodPost, "/api/rota-defaults/overrides/reading",
		`{"rrule":"FREQ=WEEKLY;BYDAY=SU;BYMINUTE=0","startFrom":"2026-11-01"}`, organiserCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	reading = overrideRuleResponse{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &reading))
	assert.Empty(t, reading.Granularity)
	assert.NotEmpty(t, reading.Problems, "a rule that cannot be read is an answer, not an error")

	assert.Empty(t, store.overrides)
}

type overriddenPlanBody struct {
	Shifts []struct {
		Start     string                  `json:"start"`
		End       string                  `json:"end"`
		MovedFrom string                  `json:"movedFrom"`
		Clash     bool                    `json:"clash"`
		Overrides []overrideTouchResponse `json:"overrides"`
	} `json:"shifts"`
	Overrides []overrideReachResponse `json:"overrides"`
}

// The plan tags each Shift with the Overrides that touched it, and reports how
// far each Override reached, so the screen can warn about one matching nothing.
func TestRotaPlanEndpointCarriesTheOverrides(t *testing.T) {
	store := &mockStore{overrides: []db.Override{
		{ID: "first-sunday", RRule: "FREQ=MONTHLY;BYDAY=1SU", StartFrom: "2026-01-04", Granularity: db.OverrideMatchesDate,
			Pins: []db.OverridePin{{ID: "p", RoleID: "role-team-lead", VolunteerID: "alice"}}},
		{ID: "old-time", RRule: "FREQ=WEEKLY;BYDAY=SU;BYHOUR=18;BYMINUTE=0", StartFrom: "2026-01-04", Granularity: db.OverrideMatchesTime,
			SetsShape: true},
	}}
	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/rotations/next?shiftCount=2", "", organiserCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var plan overriddenPlanBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &plan))

	require.Len(t, plan.Shifts, 2)
	assert.Equal(t, []overrideTouchResponse{{Number: 1, Pins: 1}}, plan.Shifts[0].Overrides)
	assert.Equal(t, []overrideTouchResponse{}, plan.Shifts[1].Overrides)

	require.Len(t, plan.Overrides, 2)
	assert.Equal(t, 1, plan.Overrides[0].Matched)
	assert.Equal(t, "date", plan.Overrides[0].Granularity)
	assert.NotEmpty(t, plan.Overrides[0].Description)
	assert.Equal(t, 0, plan.Overrides[1].Matched)
	assert.True(t, plan.Overrides[1].MissedByTime)
}

// A Shift an Override moved says where the pattern put it, and one that would
// run into another is marked, beside the refusal that names it.
func TestRotaPlanEndpointSaysWhatMovedAndWhatClashes(t *testing.T) {
	store := &mockStore{overrides: []db.Override{
		{ID: "early", RRule: "FREQ=MONTHLY;BYDAY=1SU", StartFrom: "2026-01-04", Granularity: db.OverrideMatchesDate,
			StartTime: "17:00", LengthMinutes: 24 * 60},
	}}
	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/rotations/next?shiftCount=2", "", organiserCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var plan struct {
		overriddenPlanBody
		Refusal string `json:"refusal"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &plan))

	require.Len(t, plan.Shifts, 2)
	first := plan.Shifts[0]
	assert.NotEmpty(t, first.MovedFrom)
	assert.NotEqual(t, first.MovedFrom, first.Start)
	assert.Contains(t, first.Start, "T17:00:00")
	assert.Equal(t, []overrideTouchResponse{{Number: 1, StartTime: "17:00", LengthMinutes: 24 * 60}}, first.Overrides)
	assert.Empty(t, plan.Shifts[1].MovedFrom, "a Shift nothing moved")
	assert.False(t, first.Clash, "a day long is not a week long")
	assert.Empty(t, plan.Refusal)
}
