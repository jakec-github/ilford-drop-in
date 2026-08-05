package api

import "net/http"

// roleResponse is one configured Role. Name is what every other endpoint calls
// it — a chip's role, a pin's role, a volunteer's held roles are all this
// string. Colour is a palette token (model.RoleColours), not a colour value:
// the frontend owns what each token looks like in each theme, and a Role
// configured without one comes back as the default rather than as "".
type roleResponse struct {
	Name   string `json:"name"`
	Colour string `json:"colour"`
}

type listRolesResponse struct {
	Roles []roleResponse `json:"roles"`
}

// handleListRoles reports the configured Roles, in the order their Seats are
// filled. It is the frontend's only source for which Roles exist: everything
// else names Roles by string, so without this a client would have to enumerate
// them itself, which is exactly what config being authoritative rules out.
//
// Public, like the rota it colours: every Role name is already on a chip there,
// and gating this would leave a logged-out visitor's rota uncoloured.
func (h *Handler) handleListRoles(w http.ResponseWriter, r *http.Request) {
	roles := h.cfg.RoleTable().ByPriority()

	resp := listRolesResponse{Roles: make([]roleResponse, 0, len(roles))}
	for _, role := range roles {
		resp.Roles = append(resp.Roles, roleResponse{Name: role.Name, Colour: role.Colour})
	}

	h.writeJSON(w, http.StatusOK, resp)
}
