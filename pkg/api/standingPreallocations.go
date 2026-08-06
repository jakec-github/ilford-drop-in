package api

import (
	"encoding/json"
	"net/http"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// createStandingPreallocationRequest is one promise an admin is making for every
// rota from now on: this person, in this Role, on the Shifts this rule names.
//
// The Role is named by id, unlike an ordinary pin, which names it by name. These
// outlive any number of rotas and a Role may be renamed at any time, so the id
// is the only reference that survives it.
type createStandingPreallocationRequest struct {
	RRule       string `json:"rrule"`
	RoleID      string `json:"roleId"`
	VolunteerID string `json:"volunteerId,omitempty"`
	Custom      string `json:"custom,omitempty"`
}

// standingPreallocationResponse carries the Role both ways — the id, which is
// what the row references, and the name, which is what an admin recognises — so
// a client can render one without holding the Roles listing beside it.
type standingPreallocationResponse struct {
	ID          string `json:"id"`
	RRule       string `json:"rrule"`
	RoleID      string `json:"roleId"`
	Role        string `json:"role"`
	VolunteerID string `json:"volunteerId,omitempty"`
	Custom      string `json:"custom,omitempty"`
	Name        string `json:"name"`
}

type listStandingPreallocationsResponse struct {
	StandingPreallocations []standingPreallocationResponse `json:"standingPreallocations"`
}

// handleListStandingPreallocations returns the Standing Preallocations, in the
// order the settings screen lists them.
func (h *Handler) handleListStandingPreallocations(w http.ResponseWriter, r *http.Request) {
	views, err := services.ListStandingPreallocations(r.Context(), h.store, h.volunteers, h.cfg, h.logger)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	resp := listStandingPreallocationsResponse{
		StandingPreallocations: make([]standingPreallocationResponse, 0, len(views)),
	}
	for _, v := range views {
		resp.StandingPreallocations = append(resp.StandingPreallocations, toStandingPreallocationResponse(v))
	}
	h.writeJSON(w, http.StatusOK, resp)
}

// handleCreateStandingPreallocation adds one. Validation lives in the service;
// rejections map to 400/404/409 via writeServiceError.
func (h *Handler) handleCreateStandingPreallocation(w http.ResponseWriter, r *http.Request) {
	var req createStandingPreallocationRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	view, err := services.AddStandingPreallocation(r.Context(), h.store, h.volunteers, h.cfg, services.AddStandingPreallocationParams{
		RRule:       req.RRule,
		RoleID:      req.RoleID,
		VolunteerID: req.VolunteerID,
		Custom:      req.Custom,
	}, h.logger)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	h.writeJSON(w, http.StatusCreated, toStandingPreallocationResponse(*view))
}

// handleDeleteStandingPreallocation removes one by id. 204 on success, 404 when
// it has already gone.
//
// Nothing else changes: the Preallocations it has already seeded belong to the
// rotas that minted them and are left exactly as they are, which is what makes a
// Standing Preallocation a convenience at definition rather than a standing
// fact.
func (h *Handler) handleDeleteStandingPreallocation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := services.DeleteStandingPreallocation(r.Context(), h.store, id, h.logger); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func toStandingPreallocationResponse(v services.StandingPreallocationView) standingPreallocationResponse {
	return standingPreallocationResponse{
		ID:          v.ID,
		RRule:       v.RRule,
		RoleID:      v.RoleID,
		Role:        v.Role,
		VolunteerID: v.VolunteerID,
		Custom:      v.Custom,
		Name:        v.Name,
	}
}
