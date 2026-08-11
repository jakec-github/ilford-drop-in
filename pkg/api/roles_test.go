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
	ID       string `json:"id"`
	Name     string `json:"name"`
	Priority int    `json:"priority"`
	Colour   string `json:"colour"`
}

func decodeRoles(t *testing.T, body []byte) []roleBody {
	t.Helper()
	var resp struct {
		Roles []roleBody `json:"roles"`
	}
	require.NoError(t, json.Unmarshal(body, &resp))
	return resp.Roles
}

func decodeRole(t *testing.T, body []byte) roleBody {
	t.Helper()
	var role roleBody
	require.NoError(t, json.Unmarshal(body, &role))
	return role
}

// The endpoint is the frontend's only source for which Roles exist and what
// each is drawn in — the rota renders a chip per Role and cannot enumerate the
// set itself.
func TestListRolesEndpoint(t *testing.T) {
	store := &mockStore{roles: []db.Role{
		{ID: "r-service", Name: "Service volunteer", Priority: 3, Colour: model.ColourTeal},
		{ID: "r-lead", Name: "Team lead", Priority: 1, Colour: model.ColourViolet},
		{ID: "r-food", Name: "Food collector", Priority: 2},
	}}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/roles", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")

	assert.Equal(t, []roleBody{
		{ID: "r-lead", Name: "Team lead", Priority: 1, Colour: model.ColourViolet},
		{ID: "r-food", Name: "Food collector", Priority: 2, Colour: model.DefaultRoleColour},
		{ID: "r-service", Name: "Service volunteer", Priority: 3, Colour: model.ColourTeal},
	}, decodeRoles(t, rec.Body.Bytes()),
		"in priority order, and a Role stored without a colour still has one")
}

// An uncapped Role's ceiling is explicitly null rather than absent: the settings
// screen has a field for it, and a client should not have to read "the key is
// missing" as "no limit".
func TestListRolesEndpointStatesAnAbsentCeiling(t *testing.T) {
	store := &mockStore{roles: []db.Role{
		{ID: "r-service", Name: "Service volunteer", Priority: 1, Colour: model.ColourTeal},
	}}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/roles", "")

	require.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t,
		`{"roles":[{"id":"r-service","name":"Service volunteer","priority":1,"colour":"teal"}]}`,
		rec.Body.String())
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

// Creating a Role answers with the Role as it now stands, id included: the
// client has to address it to edit it, and the id was minted server-side.
func TestCreateRoleEndpoint(t *testing.T) {
	store := &mockStore{roles: []db.Role{}}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPost, "/api/roles",
		`{"name":"Food collector","priority":3,"colour":"amber"}`, adminCookie())
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	require.Len(t, store.insertedRoles, 1)
	assert.Equal(t, "Food collector", store.insertedRoles[0].Name)

	created := decodeRole(t, rec.Body.Bytes())
	assert.Equal(t, store.insertedRoles[0].ID, created.ID)
	assert.NotEmpty(t, created.ID)
	assert.Equal(t, "Food collector", created.Name)
	assert.Equal(t, 3, created.Priority)
	assert.Equal(t, model.ColourAmber, created.Colour)
}

// A Role has no ceiling to state, so a body naming one is a body naming a field
// nothing reads — refused, rather than quietly ignored, so an old client is
// told rather than silently having half its request honoured (issue #185).
func TestCreateRoleEndpointRefusesACeiling(t *testing.T) {
	store := &mockStore{roles: []db.Role{}}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPost, "/api/roles",
		`{"name":"Service volunteer","max":4,"priority":2,"colour":"teal"}`, adminCookie())

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Empty(t, store.insertedRoles)
}

// Which Roles exist is a decision about how the drop-in runs, so only an admin
// makes it. The read stays public; the writes do not.
func TestCreateRoleEndpointRequiresAdmin(t *testing.T) {
	store := &mockStore{roles: []db.Role{}}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPost, "/api/roles",
		`{"name":"Food collector","priority":3,"colour":"amber"}`)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, store.insertedRoles, "a rejected request writes nothing")
}

// The service's refusals reach the client as its own reasons: an admin who
// typed a name that is taken has made an ordinary mistake and is told which.
func TestCreateRoleEndpointReportsRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		// insertErr stands in for what the database says rather than what the
		// request looks like.
		insertErr error
		status    int
	}{
		{"a blank name", `{"name":" ","priority":1,"colour":"teal"}`, nil, http.StatusBadRequest},
		{"a colour outside the palette", `{"name":"Team lead","priority":1,"colour":"puce"}`, nil, http.StatusBadRequest},
		{"a field nothing reads", `{"name":"Team lead","retired":true}`, nil, http.StatusBadRequest},
		{"malformed JSON", `{`, nil, http.StatusBadRequest},
		{"a name already taken", `{"name":"Team lead","priority":1,"colour":"teal"}`, db.ErrDuplicateRoleName, http.StatusConflict},
		{"an unreachable database", `{"name":"Team lead","priority":1,"colour":"teal"}`, errors.New("connection refused"), http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &mockStore{roles: []db.Role{}, roleWriteErr: tc.insertErr}

			rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPost, "/api/roles", tc.body, adminCookie())

			assert.Equal(t, tc.status, rec.Code, rec.Body.String())
		})
	}
}

// Editing addresses the Role by the id it was created with, and moves every
// editable field at once — the screen saves the Role as it now stands.
func TestUpdateRoleEndpoint(t *testing.T) {
	id := "4c1e2f8a-0b3d-4a5e-8c9f-1a2b3c4d5e6f"
	store := &mockStore{roles: []db.Role{
		{ID: id, Name: "Team lead", Priority: 1, Colour: model.ColourViolet},
	}}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPut, "/api/roles/"+id,
		`{"name":"Shift lead","priority":4,"colour":"rose"}`, adminCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	require.Len(t, store.updatedRoles, 1)
	assert.Equal(t, db.Role{
		ID: id, Name: "Shift lead", Priority: 4, Colour: model.ColourRose,
	}, store.updatedRoles[0], "the id is what a rename must not move")

	assert.Equal(t, id, decodeRole(t, rec.Body.Bytes()).ID)
}

func TestUpdateRoleEndpointRequiresAdmin(t *testing.T) {
	id := "4c1e2f8a-0b3d-4a5e-8c9f-1a2b3c4d5e6f"
	store := &mockStore{roles: []db.Role{{ID: id, Name: "Team lead", Priority: 1, Colour: model.ColourViolet}}}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPut, "/api/roles/"+id,
		`{"name":"Shift lead","priority":1,"colour":"violet"}`)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, store.updatedRoles)
}

// Roles are never deleted, so an id nothing matches is a wrong id — a 404, and
// not a driver error about UUID syntax when the id is not even a UUID.
func TestUpdateRoleEndpointReportsAnUnknownRole(t *testing.T) {
	for _, id := range []string{"4c1e2f8a-0b3d-4a5e-8c9f-1a2b3c4d5e6f", "Team%20lead"} {
		t.Run(id, func(t *testing.T) {
			store := &mockStore{roles: []db.Role{}, roleMissing: true}

			rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPut, "/api/roles/"+id,
				`{"name":"Shift lead","priority":1,"colour":"violet"}`, adminCookie())

			assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
		})
	}
}

// Renaming onto a name another Role holds is the same clash a creation meets,
// and reads the same way to the screen showing it.
func TestUpdateRoleEndpointReportsADuplicateName(t *testing.T) {
	id := "4c1e2f8a-0b3d-4a5e-8c9f-1a2b3c4d5e6f"
	store := &mockStore{roles: []db.Role{}, roleWriteErr: db.ErrDuplicateRoleName}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPut, "/api/roles/"+id,
		`{"name":"Food collector","priority":1,"colour":"violet"}`, adminCookie())

	assert.Equal(t, http.StatusConflict, rec.Code)
}

// There is no delete and no retire, anywhere: a Role is permanent so that
// nothing referencing one can dangle (ADR 0006). The route not existing is what
// makes that true of the API and not only of the screen.
func TestRolesCannotBeDeleted(t *testing.T) {
	id := "4c1e2f8a-0b3d-4a5e-8c9f-1a2b3c4d5e6f"
	store := &mockStore{roles: []db.Role{{ID: id, Name: "Team lead", Priority: 1, Colour: model.ColourViolet}}}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodDelete, "/api/roles/"+id, "", adminCookie())

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}
