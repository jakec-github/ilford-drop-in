package api

import (
	"encoding/json"
	"net/http"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// defineRotaRequest is the rota being made: the two things about it that are
// nobody's setting.
//
// The hours every minted shift runs and what each of them asks for are not
// here. They are the Rota Defaults, which are the only place they are stated
// (issue #176), and defining spends whatever they say at the moment it runs.
// The screen shows the settings card itself, so what an admin is looking at
// when they press the button is the setting rather than a copy of it.
type defineRotaRequest struct {
	ShiftCount int `json:"shiftCount"`
	// StartDate is the first shift's date, "2026-08-02". The rest follow weekly.
	StartDate string `json:"startDate"`
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

// handleDefineRota defines the rota the request states, mints its weekly
// shifts, and opens its availability round.
//
// The round is not in the response: what it did is read back by GET
// /availability-rounds, which the screen an admin lands on after defining is
// showing anyway. Nothing is emailed — minting writes the links, sending them
// is its own action with its own deadline.
//
// Deliberately not idempotent: a second call defines a second rota. Nothing
// about the request identifies which rota it is asking for, and there is no
// token to make it so — what stops a double submission turning into two rotas
// is the one-rota-in-flight rule, which refuses the second with a conflict. The
// response names the dates just created, which is what makes what happened
// visible to the caller.
func (h *Handler) handleDefineRota(w http.ResponseWriter, r *http.Request) {
	var req defineRotaRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	result, err := services.DefineRota(r.Context(), h.store, h.volunteers, h.cfg, h.logger, services.DefineRotaParams{
		ShiftCount: req.ShiftCount,
		StartDate:  req.StartDate,
	})
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

// rotaProposalResponse is the define form before anybody has touched it: where
// the rota made by defining one right now would begin.
//
// The shift count is not here, and the form has no default for it either: no
// rota implies how long the next one should be, so it is the one field an admin
// has to state (issue #174). Neither are the hours or the Shape, which the form
// no longer states at all — they are the Rota Defaults, read by the settings
// card the define screen shows (issue #176).
type rotaProposalResponse struct {
	StartDate string `json:"startDate"`
}

// handleGetRotaProposal reports what the define form starts from.
//
// A read of its own rather than a field of GET /rotations/in-flight, though the
// screen shows one or the other: this is arithmetic over the rotas that exist
// plus the settings, where that is the state of one rota and the numbers a
// discard would destroy. Tying them together would make each re-read whenever
// the other changed.
func (h *Handler) handleGetRotaProposal(w http.ResponseWriter, r *http.Request) {
	proposal, err := services.ProposeRota(r.Context(), h.store)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, rotaProposalResponse{StartDate: proposal.StartDate})
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
