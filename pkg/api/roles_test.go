package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

type roleBody struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Colour string `json:"colour"`
}

func decodeRoles(t *testing.T, body []byte) []roleBody {
	t.Helper()
	var resp struct {
		Roles []roleBody `json:"roles"`
	}
	require.NoError(t, json.Unmarshal(body, &resp))
	return resp.Roles
}

// The endpoint is the frontend's only source for which Roles exist and what
// each is drawn in — the rota renders a chip per Role and cannot enumerate the
// set itself.
func TestListRolesEndpoint(t *testing.T) {
	store := &mockStore{roles: []db.Role{
		{ID: "r-service", Name: "Service volunteer", Priority: 3, Colour: model.ColourTeal},
		{ID: "r-lead", Name: "Team lead", Max: intPtr(1), Priority: 1, Colour: model.ColourViolet},
		{ID: "r-food", Name: "Food collector", Max: intPtr(2), Priority: 2},
	}}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/roles", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")

	assert.Equal(t, []roleBody{
		{ID: "r-lead", Name: "Team lead", Colour: model.ColourViolet},
		{ID: "r-food", Name: "Food collector", Colour: model.DefaultRoleColour},
		{ID: "r-service", Name: "Service volunteer", Colour: model.ColourTeal},
	}, decodeRoles(t, rec.Body.Bytes()),
		"in priority order, and a Role stored without a colour still has one")
}

// The rota is public and already names Roles on every chip, so the set of Roles
// and their colours is not admin-gated: gating it would leave a logged-out
// visitor's rota uncoloured.
func TestListRolesEndpointIsPublic(t *testing.T) {
	rec := doRequest(t, newTestHandler(&mockStore{}, testVolunteers()), http.MethodGet, "/api/roles", "")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, decodeRoles(t, rec.Body.Bytes()), 2)
}

// A database nobody has created Roles in is what a fresh deployment looks like,
// and the endpoint answers with an empty list rather than null: a client should
// not have to tell the two apart.
func TestListRolesEndpointWithNoRoles(t *testing.T) {
	rec := doRequest(t, newTestHandler(&mockStore{roles: []db.Role{}}, testVolunteers()), http.MethodGet, "/api/roles", "")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"roles":[]}`, rec.Body.String())
}

// Roles come off the database now, so the read can fail — and a failure is a
// server error, not an empty palette that would silently uncolour the rota.
func TestListRolesEndpointReportsAReadFailure(t *testing.T) {
	store := &mockStore{rolesErr: errors.New("connection refused")}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/roles", "")

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
