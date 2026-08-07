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

// rotaInFlightResponse is the rota being worked on, or nothing.
//
// The rotation is nested and nullable rather than the body being empty or the
// status being 404, because "no rota is in flight" is an answer this endpoint
// gives rather than a failure to answer: it is the state in which a rota may be
// defined, and the screen renders it.
type rotaInFlightResponse struct {
	Rotation *rotaInFlightBody `json:"rotation"`
}

type rotaInFlightBody struct {
	ID         string `json:"id"`
	Start      string `json:"start"`
	End        string `json:"end"`
	ShiftCount int    `json:"shiftCount"`
	// The round that has grown around this rota, in volunteers: how many hold a
	// link, how many have been emailed theirs, and how many have answered.
	// Replied is the number a discard confirmation quotes.
	Asked   int `json:"asked"`
	Sent    int `json:"sent"`
	Replied int `json:"replied"`
}

// handleGetRotaInFlight answers with the one unallocated rota, or null.
func (h *Handler) handleGetRotaInFlight(w http.ResponseWriter, r *http.Request) {
	inFlight, err := services.RotaInFlight(r.Context(), h.store)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	resp := rotaInFlightResponse{}
	if inFlight != nil {
		resp.Rotation = &rotaInFlightBody{
			ID:         inFlight.ID,
			Start:      inFlight.Start,
			End:        inFlight.End,
			ShiftCount: inFlight.ShiftCount,
			Asked:      inFlight.Asked,
			Sent:       inFlight.Sent,
			Replied:    inFlight.Replied,
		}
	}

	h.writeJSON(w, http.StatusOK, resp)
}

// handleDiscardRota destroys an unallocated rota and everything hanging off it.
//
// DELETE on the rotation itself, with no request body and nothing to confirm on
// the way in: the confirmation an admin gives is a decision made in front of the
// numbers, which the screen has already read from GET /rotations/in-flight.
// Repeating it here as a token or a count would make the API's guarantee depend
// on the caller's honesty, where the allocated check does not.
func (h *Handler) handleDiscardRota(w http.ResponseWriter, r *http.Request) {
	if err := services.DiscardRota(r.Context(), h.store, h.logger, r.PathValue("id")); err != nil {
		h.writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
