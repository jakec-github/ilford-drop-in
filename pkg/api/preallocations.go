package api

import (
	"encoding/json"
	"net/http"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

type createPreallocationRequest struct {
	Date        string `json:"date"`
	VolunteerID string `json:"volunteerId,omitempty"`
	Custom      string `json:"custom,omitempty"`
	TeamLead    bool   `json:"teamLead,omitempty"`
}

type preallocationResponse struct {
	ID          string `json:"id"`
	Date        string `json:"date"`
	Role        string `json:"role"`
	VolunteerID string `json:"volunteerId,omitempty"`
	Custom      string `json:"custom,omitempty"`
}

type listPreallocationsResponse struct {
	Preallocations []preallocationResponse `json:"preallocations"`
}

// handleCreatePreallocation pins one assignee to a shift. Set-time validation
// lives in the service; rejections map to 400/404/409 via writeServiceError.
func (h *Handler) handleCreatePreallocation(w http.ResponseWriter, r *http.Request) {
	var req createPreallocationRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	view, err := services.AddPreallocation(r.Context(), h.store, h.volunteers, h.cfg, services.AddPreallocationParams{
		Date:        req.Date,
		VolunteerID: req.VolunteerID,
		Custom:      req.Custom,
		TeamLead:    req.TeamLead,
	}, h.logger)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	h.writeJSON(w, http.StatusCreated, toPreallocationResponse(*view))
}

// handleDeletePreallocation removes one pin by id. 204 on success; a missing
// pin or a frozen rota surfaces via writeServiceError (404/409).
func (h *Handler) handleDeletePreallocation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := services.DeletePreallocation(r.Context(), h.store, id, h.logger); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleListPreallocations returns the manual pins whose shift falls in the
// optional from/to date range.
func (h *Handler) handleListPreallocations(w http.ResponseWriter, r *http.Request) {
	views, err := services.ListPreallocations(r.Context(), h.store, services.ListPreallocationsParams{
		From: r.URL.Query().Get("from"),
		To:   r.URL.Query().Get("to"),
	})
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	resp := listPreallocationsResponse{Preallocations: make([]preallocationResponse, 0, len(views))}
	for _, v := range views {
		resp.Preallocations = append(resp.Preallocations, toPreallocationResponse(v))
	}
	h.writeJSON(w, http.StatusOK, resp)
}

func toPreallocationResponse(v services.PreallocationView) preallocationResponse {
	return preallocationResponse{
		ID:          v.ID,
		Date:        v.Date,
		Role:        v.Role,
		VolunteerID: v.VolunteerID,
		Custom:      v.Custom,
	}
}
