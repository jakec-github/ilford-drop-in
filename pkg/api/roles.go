package api

import (
	"net/http"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// roleResponse is one Role. Name is what every other endpoint calls it — a
// chip's role, a pin's role, a volunteer's held roles are all this string.
// Colour is a palette token (model.RoleColours), not a colour value: the
// frontend owns what each token looks like in each theme, and a Role created
// without one comes back as the default rather than as "".
//
// ID is the Role's identity, stable across a rename. Nothing on the read path
// needs it — the rota names Roles by string — but a Role is addressable now
// that Roles are rows, and a client that edits one addresses it by id.
type roleResponse struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Colour string `json:"colour"`
}

type listRolesResponse struct {
	Roles []roleResponse `json:"roles"`
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
		resp.Roles = append(resp.Roles, roleResponse{ID: role.ID, Name: role.Name, Colour: role.Colour})
	}

	h.writeJSON(w, http.StatusOK, resp)
}
