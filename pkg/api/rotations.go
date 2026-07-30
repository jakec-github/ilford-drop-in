package api

import (
	"encoding/json"
	"net/http"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

type defineRotaRequest struct {
	ShiftCount int `json:"shiftCount"`
}

type rotationResponse struct {
	ID         string `json:"id"`
	Start      string `json:"start"`
	End        string `json:"end"`
	ShiftCount int    `json:"shiftCount"`
}

type mintedShiftResponse struct {
	ID   string `json:"id"`
	Date string `json:"date"`
}

type defineRotaResponseBody struct {
	Rotation rotationResponse      `json:"rotation"`
	Shifts   []mintedShiftResponse `json:"shifts"`
}

// handleDefineRota defines the next rota and mints its weekly shifts. The start
// date follows the latest existing rota (or the next Sunday when there are
// none), so the endpoint is deliberately not idempotent: two calls define two
// consecutive rotas, exactly as two CLI invocations do. The response names the
// dates just created, which is what makes that visible to the caller.
func (h *Handler) handleDefineRota(w http.ResponseWriter, r *http.Request) {
	var req defineRotaRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	result, err := services.DefineRota(r.Context(), h.store, h.logger, req.ShiftCount)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	resp := defineRotaResponseBody{
		Rotation: rotationResponse{
			ID:         result.Rotation.ID,
			Start:      result.Rotation.Start,
			End:        result.Rotation.End,
			ShiftCount: result.Rotation.ShiftCount,
		},
		Shifts: make([]mintedShiftResponse, 0, len(result.Shifts)),
	}
	for _, s := range result.Shifts {
		resp.Shifts = append(resp.Shifts, mintedShiftResponse{ID: s.ID, Date: s.Date})
	}

	h.writeJSON(w, http.StatusCreated, resp)
}
