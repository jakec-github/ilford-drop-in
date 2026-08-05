package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
)

type roleBody struct {
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
	cfg := &config.Config{
		ShiftStartTime: "19:30",
		ShiftEndTime:   "21:30",
		Roles: []model.Role{
			{Name: "Service volunteer", Priority: 3, Colour: model.ColourTeal},
			{Name: "Team lead", Max: intPtr(1), Priority: 1, Colour: model.ColourViolet},
			{Name: "Food collector", Max: intPtr(2), Priority: 2},
		},
	}

	rec := doRequest(t, newTestHandlerWithConfig(&mockStore{}, testVolunteers(), cfg), http.MethodGet, "/api/roles", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")

	assert.Equal(t, []roleBody{
		{Name: "Team lead", Colour: model.ColourViolet},
		{Name: "Food collector", Colour: model.DefaultRoleColour},
		{Name: "Service volunteer", Colour: model.ColourTeal},
	}, decodeRoles(t, rec.Body.Bytes()),
		"in priority order, and a Role configured without a colour still has one")
}

// The rota is public and already names Roles on every chip, so the set of Roles
// and their colours is not admin-gated: gating it would leave a logged-out
// visitor's rota uncoloured.
func TestListRolesEndpointIsPublic(t *testing.T) {
	rec := doRequest(t, newTestHandler(&mockStore{}, testVolunteers()), http.MethodGet, "/api/roles", "")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Len(t, decodeRoles(t, rec.Body.Bytes()), 2)
}

// A config with no Roles cannot be started against, but the endpoint should
// answer with an empty list rather than null: a client should not have to tell
// the two apart.
func TestListRolesEndpointWithNoRoles(t *testing.T) {
	cfg := &config.Config{ShiftStartTime: "19:30", ShiftEndTime: "21:30"}

	rec := doRequest(t, newTestHandlerWithConfig(&mockStore{}, testVolunteers(), cfg), http.MethodGet, "/api/roles", "")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"roles":[]}`, rec.Body.String())
}
