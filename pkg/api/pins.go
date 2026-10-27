package api

import (
	"encoding/json"
	"net/http"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// createPinRequest names the Seat a pin fills by its Role id, as the
// standing-preallocation request does. A pin outlives any number of renames, so
// the id is the only reference to a Role that survives one (issue #195). It
// names its Shift by id for the same reason: a date stops naming one Shift once
// a day can hold two (issue #293).
type createPinRequest struct {
	ShiftID     string `json:"shiftId"`
	VolunteerID string `json:"volunteerId,omitempty"`
	Custom      string `json:"custom,omitempty"`
	RoleID      string `json:"roleId"`
}

// pinResponse is one pin. Role comes back twice for the reason a
// standing one does: roleId is what the row references, role is what an Organiser
// recognises.
//
// There is one kind of them (issue #131) — a pin somebody made by hand and a
// pin a Standing Preallocation seeded at definition are the same row — so it
// says nothing about where it came from and every one of them carries an id a
// DELETE can be addressed at.
type pinResponse struct {
	ID          string `json:"id"`
	ShiftID     string `json:"shiftId"`
	Date        string `json:"date"`
	RoleID      string `json:"roleId"`
	Role        string `json:"role"`
	VolunteerID string `json:"volunteerId,omitempty"`
	Custom      string `json:"custom,omitempty"`
	Name        string `json:"name"`
}

type listPinsResponse struct {
	Pins []pinResponse `json:"pins"`
}

// handleCreatePin pins one assignee to a shift. Set-time validation
// lives in the service; rejections map to 400/404/409 via writeServiceError.
func (h *Handler) handleCreatePin(w http.ResponseWriter, r *http.Request) {
	var req createPinRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	view, err := services.AddPin(r.Context(), h.store, h.volunteers, h.org, services.AddPinParams{
		ShiftID:     req.ShiftID,
		VolunteerID: req.VolunteerID,
		Custom:      req.Custom,
		RoleID:      req.RoleID,
	}, h.logger)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	h.writeJSON(w, http.StatusCreated, toPinResponse(*view))
}

// handleDeletePin removes one pin by id. 204 on success; a missing
// pin or a frozen rota surfaces via writeServiceError (404/409).
func (h *Handler) handleDeletePin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := services.DeletePin(r.Context(), h.store, id, h.logger); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleListPins returns every pin whose shift falls in the optional
// from/to date range. Behind a session: it names people against dates the rota has not
// published yet.
func (h *Handler) handleListPins(w http.ResponseWriter, r *http.Request) {
	views, err := services.ListPins(r.Context(), h.store, h.volunteers, h.org, services.ListPinsParams{
		From: r.URL.Query().Get("from"),
		To:   r.URL.Query().Get("to"),
	}, h.logger)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	resp := listPinsResponse{Pins: make([]pinResponse, 0, len(views))}
	for _, v := range views {
		resp.Pins = append(resp.Pins, toPinResponse(v))
	}
	h.writeJSON(w, http.StatusOK, resp)
}

func toPinResponse(v services.PinView) pinResponse {
	return pinResponse{
		ID:          v.ID,
		ShiftID:     v.ShiftID,
		Date:        v.Date,
		RoleID:      v.RoleID,
		Role:        v.Role,
		VolunteerID: v.VolunteerID,
		Custom:      v.Custom,
		Name:        v.Name,
	}
}
