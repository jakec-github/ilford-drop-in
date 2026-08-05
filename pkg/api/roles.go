package api

import (
	"encoding/json"
	"net/http"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// roleResponse is one Role. Name is what every other endpoint calls it — a
// chip's role, a pin's role, a volunteer's held roles are all this string.
// Colour is a palette token (model.RoleColours), not a colour value: the
// frontend owns what each token looks like in each theme, and a Role created
// without one comes back as the default rather than as "".
//
// ID is the Role's identity, stable across a rename. Nothing on the read path
// needs it — the rota names Roles by string — but the settings screen edits a
// Role by addressing it.
//
// Max and Priority are here for that screen rather than for the rota. They ride
// on the public listing rather than a gated one of their own because they are
// facts about how the drop-in is run, not about anybody in it: a ceiling and a
// filling order say nothing a visitor could not count off the rota.
type roleResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Explicitly null when the Role is uncapped, never absent: the settings
	// screen has a field for the ceiling, and a client should not have to read
	// a missing key as "no limit".
	Max      *int   `json:"max"`
	Priority int    `json:"priority"`
	Colour   string `json:"colour"`
}

type listRolesResponse struct {
	Roles []roleResponse `json:"roles"`
}

// roleRequest is a Role as an admin states it, and is the body of both writes:
// an edit says everything a creation says, because a Role must not be able to
// reach through an edit a state it could not have been created in.
//
// Max is a pointer so that leaving it out, and sending null, both mean uncapped
// — which is a Role's ordinary state, not a missing answer.
type roleRequest struct {
	Name     string `json:"name"`
	Max      *int   `json:"max"`
	Priority int    `json:"priority"`
	Colour   string `json:"colour"`
}

// handleListRoles reports the Roles the drop-in offers, in the order their
// Seats are filled. It is the frontend's only source for which Roles exist:
// everything else names Roles by string, so without this a client would have to
// enumerate them itself, which is exactly what the app owning them rules out.
//
// Public, like the rota it colours: every Role name is already on a chip there,
// and gating this would leave a logged-out visitor's rota uncoloured.
func (h *Handler) handleListRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := services.RoleTable(r.Context(), h.store)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	ordered := roles.ByPriority()
	resp := listRolesResponse{Roles: make([]roleResponse, 0, len(ordered))}
	for _, role := range ordered {
		resp.Roles = append(resp.Roles, toRoleResponse(role))
	}

	h.writeJSON(w, http.StatusOK, resp)
}

// handleCreateRole adds a Role and answers with it, id included — the client
// has to address that id to edit it, and it was minted server-side.
//
// Admin-only: which Roles exist is a decision about how the drop-in runs.
func (h *Handler) handleCreateRole(w http.ResponseWriter, r *http.Request) {
	params, ok := h.decodeRoleRequest(w, r)
	if !ok {
		return
	}

	role, err := services.CreateRole(r.Context(), h.store, params, h.logger)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	h.writeJSON(w, http.StatusCreated, toRoleResponse(*role))
}

// handleUpdateRole rewrites one Role's name, ceiling, priority and colour. The
// id never moves, so a rename leaves every allocation, alteration and pin
// written against it intact.
//
// There is deliberately no DELETE beside this. Roles are permanent (ADR 0006),
// and the absence of the route is what makes that true of the API rather than
// only of the screen.
func (h *Handler) handleUpdateRole(w http.ResponseWriter, r *http.Request) {
	params, ok := h.decodeRoleRequest(w, r)
	if !ok {
		return
	}

	role, err := services.UpdateRole(r.Context(), h.store, r.PathValue("id"), params, h.logger)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, toRoleResponse(*role))
}

// decodeRoleRequest reads the body both writes share. It reports whether to
// carry on, having already answered the client when not.
func (h *Handler) decodeRoleRequest(w http.ResponseWriter, r *http.Request) (services.RoleParams, bool) {
	var req roleRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return services.RoleParams{}, false
	}

	return services.RoleParams{
		Name:     req.Name,
		Max:      req.Max,
		Priority: req.Priority,
		Colour:   req.Colour,
	}, true
}

func toRoleResponse(role model.Role) roleResponse {
	return roleResponse{
		ID:       role.ID,
		Name:     role.Name,
		Max:      role.Max,
		Priority: role.Priority,
		Colour:   role.Colour,
	}
}
