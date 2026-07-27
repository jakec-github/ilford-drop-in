package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
)

// rosterVolunteers is a roster with a mix of statuses and a group, to prove the
// endpoint renders every field and hides nobody.
func rosterVolunteers() *mockVolunteerClient {
	return &mockVolunteerClient{
		volunteers: []model.Volunteer{
			{ID: "bob", DisplayName: "Bob", Role: model.RoleVolunteer, Status: "Active", GroupKey: "smith-family"},
			{ID: "alice", DisplayName: "Alice", Role: model.RoleTeamLead, Status: "active"},
			{ID: "charlie", DisplayName: "Charlie", Role: model.RoleVolunteer, Status: "Left"},
		},
	}
}

type volunteerBody struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Role   string `json:"role"`
	Group  string `json:"group"`
	Active bool   `json:"active"`
}

func decodeVolunteers(t *testing.T, body []byte) []volunteerBody {
	t.Helper()
	var resp struct {
		Volunteers []volunteerBody `json:"volunteers"`
	}
	require.NoError(t, json.Unmarshal(body, &resp))
	return resp.Volunteers
}

func TestListVolunteersEndpoint(t *testing.T) {
	rec := doRequest(t, newTestHandler(&mockStore{}, rosterVolunteers()), http.MethodGet, "/volunteers", "", adminCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")

	volunteers := decodeVolunteers(t, rec.Body.Bytes())
	// Sorted by name, so the picker's ordering does not depend on sheet order
	require.Len(t, volunteers, 3)
	assert.Equal(t, []string{"alice", "bob", "charlie"}, []string{volunteers[0].ID, volunteers[1].ID, volunteers[2].ID})

	assert.Equal(t, volunteerBody{ID: "alice", Name: "Alice", Role: string(model.RoleTeamLead), Active: true}, volunteers[0])
	assert.Equal(t, volunteerBody{
		ID: "bob", Name: "Bob", Role: string(model.RoleVolunteer), Group: "smith-family", Active: true,
	}, volunteers[1])
}

// TestListVolunteersIncludesInactive proves left volunteers are still listed —
// the roster is the full one, flagged rather than filtered, so an admin can see
// who has stopped without the endpoint deciding for them.
func TestListVolunteersIncludesInactive(t *testing.T) {
	rec := doRequest(t, newTestHandler(&mockStore{}, rosterVolunteers()), http.MethodGet, "/volunteers", "", adminCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	volunteers := decodeVolunteers(t, rec.Body.Bytes())
	require.Len(t, volunteers, 3)
	assert.Equal(t, "charlie", volunteers[2].ID)
	assert.False(t, volunteers[2].Active, "a volunteer who has left must be listed but flagged inactive")
}

func TestListVolunteersEmptyRoster(t *testing.T) {
	rec := doRequest(t, newTestHandler(&mockStore{}, &mockVolunteerClient{}), http.MethodGet, "/volunteers", "", adminCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"volunteers":[]}`, rec.Body.String())
}

// TestListVolunteersRequiresAdmin proves the roster is admin-only: it exposes
// volunteer ids, groups and everyone not currently on a shift, which the public
// rota does not.
func TestListVolunteersRequiresAdmin(t *testing.T) {
	volunteers := rosterVolunteers()
	rec := doRequest(t, newTestHandler(&mockStore{}, volunteers), http.MethodGet, "/volunteers", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Zero(t, volunteers.calls, "an unauthenticated request must not reach the roster")
}

func TestListVolunteersRosterError(t *testing.T) {
	client := &mockVolunteerClient{err: errors.New("sheet unavailable")}
	rec := doRequest(t, newTestHandler(&mockStore{}, client), http.MethodGet, "/volunteers", "", adminCookie())
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
