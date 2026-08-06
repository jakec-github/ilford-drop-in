package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// standingPreallocationJSON is the wire shape of one Standing Preallocation, so
// tests read the same fields a client does.
type standingPreallocationJSON struct {
	ID          string `json:"id"`
	RRule       string `json:"rrule"`
	RoleID      string `json:"roleId"`
	Role        string `json:"role"`
	VolunteerID string `json:"volunteerId"`
	Custom      string `json:"custom"`
	Name        string `json:"name"`
}

func decodeStandingPreallocations(t *testing.T, body []byte) []standingPreallocationJSON {
	t.Helper()
	var resp struct {
		StandingPreallocations []standingPreallocationJSON `json:"standingPreallocations"`
	}
	require.NoError(t, json.Unmarshal(body, &resp))
	return resp.StandingPreallocations
}

func TestCreateStandingPreallocationEndpoint(t *testing.T) {
	store := &mockStore{}
	body := `{"rrule":"FREQ=MONTHLY;BYDAY=1SU","roleId":"role-team-lead","volunteerId":"alice"}`

	rec := doRequest(t, newTestHandler(store, activeVolunteers()), http.MethodPost, "/api/standing-preallocations", body, adminCookie())
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	require.Len(t, store.insertedStanding, 1)
	assert.Equal(t, "FREQ=MONTHLY;BYDAY=1SU", store.insertedStanding[0].RRule)
	assert.Equal(t, "role-team-lead", store.insertedStanding[0].RoleID)
	assert.Equal(t, "alice", store.insertedStanding[0].VolunteerID)

	var created standingPreallocationJSON
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	assert.Equal(t, "Team lead", created.Role)
	assert.Equal(t, "Alice", created.Name)
}

func TestCreateStandingPreallocationEndpoint_Errors(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		setup    func(*mockStore)
		wantCode int
	}{
		{name: "malformed json", body: `{`, wantCode: http.StatusBadRequest},
		{name: "unknown field", body: `{"rrule":"FREQ=WEEKLY;BYDAY=SU","roleId":"role-team-lead","volunteerId":"alice","shifts":2}`, wantCode: http.StatusBadRequest},
		{name: "no subject", body: `{"rrule":"FREQ=WEEKLY;BYDAY=SU","roleId":"role-team-lead"}`, wantCode: http.StatusBadRequest},
		{name: "unparseable rule", body: `{"rrule":"EVERY OTHER TUESDAY","roleId":"role-team-lead","volunteerId":"alice"}`, wantCode: http.StatusBadRequest},
		{name: "unknown role", body: `{"rrule":"FREQ=WEEKLY;BYDAY=SU","roleId":"role-imaginary","volunteerId":"alice"}`, wantCode: http.StatusBadRequest},
		{name: "unknown volunteer", body: `{"rrule":"FREQ=WEEKLY;BYDAY=SU","roleId":"role-service-volunteer","volunteerId":"ghost"}`, wantCode: http.StatusNotFound},
		{
			name: "already promised",
			body: `{"rrule":"FREQ=WEEKLY;BYDAY=SU","roleId":"role-team-lead","volunteerId":"alice"}`,
			setup: func(s *mockStore) {
				s.standingWriteErr = db.ErrDuplicateStandingPreallocation
			},
			wantCode: http.StatusConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &mockStore{}
			if tt.setup != nil {
				tt.setup(store)
			}
			rec := doRequest(t, newTestHandler(store, activeVolunteers()), http.MethodPost, "/api/standing-preallocations", tt.body, adminCookie())
			assert.Equal(t, tt.wantCode, rec.Code, rec.Body.String())
		})
	}
}

func TestListStandingPreallocationsEndpoint(t *testing.T) {
	store := &mockStore{standingPreallocations: []db.StandingPreallocation{
		{ID: "sp-1", RRule: "FREQ=WEEKLY;BYDAY=SU", RoleID: "role-service-volunteer", CustomValue: "Scouts"},
		{ID: "sp-2", RRule: "FREQ=MONTHLY;BYDAY=1SU", RoleID: "role-team-lead", VolunteerID: "alice"},
	}}

	rec := doRequest(t, newTestHandler(store, activeVolunteers()), http.MethodGet, "/api/standing-preallocations", "", adminCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	standing := decodeStandingPreallocations(t, rec.Body.Bytes())
	require.Len(t, standing, 2)
	assert.Equal(t, standingPreallocationJSON{
		ID: "sp-2", RRule: "FREQ=MONTHLY;BYDAY=1SU", RoleID: "role-team-lead",
		Role: "Team lead", VolunteerID: "alice", Name: "Alice",
	}, standing[0], "the lead sorts first, as it does everywhere a shift is read")
	assert.Equal(t, "Scouts", standing[1].Name)
}

// Empty is the ordinary state of a deployment nobody has configured, and it
// answers with a list rather than a null so a client has nothing to special-case.
func TestListStandingPreallocationsEndpoint_Empty(t *testing.T) {
	rec := doRequest(t, newTestHandler(&mockStore{}, activeVolunteers()), http.MethodGet, "/api/standing-preallocations", "", adminCookie())
	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"standingPreallocations":[]}`, rec.Body.String())
}

func TestDeleteStandingPreallocationEndpoint(t *testing.T) {
	store := &mockStore{standingPreallocations: []db.StandingPreallocation{
		{ID: "sp-1", RRule: "FREQ=WEEKLY;BYDAY=SU", RoleID: "role-team-lead", VolunteerID: "alice"},
	}}

	handler := newTestHandler(store, activeVolunteers())
	rec := doRequest(t, handler, http.MethodDelete, "/api/standing-preallocations/sp-1", "", adminCookie())
	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, []string{"sp-1"}, store.deletedStandingIDs)

	rec = doRequest(t, handler, http.MethodDelete, "/api/standing-preallocations/sp-1", "", adminCookie())
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// Every verb is admin-only: these name people against every rota to come.
func TestStandingPreallocationsRequireAdmin(t *testing.T) {
	handler := newTestHandler(&mockStore{}, activeVolunteers())

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/standing-preallocations", ""},
		{http.MethodPost, "/api/standing-preallocations", `{"rrule":"FREQ=WEEKLY;BYDAY=SU","roleId":"role-team-lead","volunteerId":"alice"}`},
		{http.MethodDelete, "/api/standing-preallocations/sp-1", ""},
	} {
		rec := doRequest(t, handler, tc.method, tc.path, tc.body)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, "%s %s", tc.method, tc.path)
	}
}
