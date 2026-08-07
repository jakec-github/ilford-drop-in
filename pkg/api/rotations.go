package api

import (
	"encoding/json"
	"net/http"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// defineRotaRequest is the rota being made, stated whole.
//
// Nothing is optional and nothing falls back to the Rota Defaults. The screen
// reads those from GET /rotations/proposed, shows them, and sends back what it
// showed — so the rota that appears is the rota the admin was looking at, even
// if somebody edited the settings in between (issue #140).
type defineRotaRequest struct {
	ShiftCount int `json:"shiftCount"`
	// StartDate is the first shift's date, "2026-08-02". The rest follow weekly.
	StartDate string `json:"startDate"`
	// The hours every minted shift runs, as 24-hour times of day: "19:30".
	ShiftStartTime string `json:"shiftStartTime"`
	ShiftEndTime   string `json:"shiftEndTime"`
	// Shape is what every minted shift asks for. A Role left out is one no shift
	// of this rota asks for; a Seat of nought is refused rather than read as
	// that, so the stated Shape and the minted one are the same thing.
	Shape []seatRequest `json:"shape"`
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

// handleDefineRota defines the rota the request states and mints its weekly
// shifts.
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

	shape := make([]services.SeatParams, 0, len(req.Shape))
	for _, seat := range req.Shape {
		shape = append(shape, services.SeatParams{RoleID: seat.RoleID, Count: seat.Count})
	}

	result, err := services.DefineRota(r.Context(), h.store, h.logger, services.DefineRotaParams{
		ShiftCount:     req.ShiftCount,
		StartDate:      req.StartDate,
		ShiftStartTime: req.ShiftStartTime,
		ShiftEndTime:   req.ShiftEndTime,
		Shape:          shape,
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

// rotaProposalResponse is the define form before anybody has touched it: the
// rota that would be made by defining one right now.
//
// It mirrors the define request field for field, which is the point — the
// screen shows what comes back, the admin edits what they like, and what goes
// out is the same shape of thing. The shift count is not here: no rota implies
// how long the next one should be, so the form starts on its own default.
//
// The times and the Shape may be empty, which means the Rota Defaults have not
// been stated. The form renders that as empty boxes rather than refusing: an
// admin can state the hours for this rota without visiting the settings screen
// first, and stating them here does not save them there.
type rotaProposalResponse struct {
	StartDate      string         `json:"startDate"`
	ShiftStartTime string         `json:"shiftStartTime"`
	ShiftEndTime   string         `json:"shiftEndTime"`
	Shape          []seatResponse `json:"shape"`
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

	h.writeJSON(w, http.StatusOK, rotaProposalResponse{
		StartDate:      proposal.StartDate,
		ShiftStartTime: proposal.ShiftStartTime,
		ShiftEndTime:   proposal.ShiftEndTime,
		Shape:          toSeatResponses(proposal.Shape),
	})
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
