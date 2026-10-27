package api

import (
	"net/http"
	"strconv"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// defineRotaRequest is the rota being made: how many Shifts, and optionally a
// date none of them may start before.
//
// Which Shifts they are is not here. They are the next the Shift pattern
// generates after where the previous rota stopped (ADR 0013), and what each
// asks for is the default Shape: both are the Rota Defaults, and defining
// spends whatever they say at the moment it runs.
type defineRotaRequest struct {
	ShiftCount int `json:"shiftCount"`
	// NotBefore is a date, "2026-08-02", leaving a gap before the rota. Empty
	// or absent means none.
	NotBefore string `json:"notBefore"`
}

type rotationResponse struct {
	ID         string `json:"id"`
	Start      string `json:"start"`
	End        string `json:"end"`
	ShiftCount int    `json:"shiftCount"`
}

type mintedShiftResponse struct {
	ID    string `json:"id"`
	Date  string `json:"date"`
	Start string `json:"start"`
	End   string `json:"end"`
}

type defineRotaResponseBody struct {
	Rotation rotationResponse      `json:"rotation"`
	Shifts   []mintedShiftResponse `json:"shifts"`
}

// handleDefineRota defines the rota the request asks for, mints the Shifts the
// pattern generates next, and opens its availability round.
//
// The round is not in the response: what it did is read back by GET
// /availability-rounds, which the screen an Organiser lands on after defining is
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
	if err := decodeStrict(r, &req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	result, err := services.DefineRota(r.Context(), h.store, h.volunteers, h.org, h.logger, services.DefineRotaParams{
		ShiftCount: req.ShiftCount,
		NotBefore:  req.NotBefore,
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
		resp.Shifts = append(resp.Shifts, mintedShiftResponse{ID: s.ID, Date: s.Date, Start: s.StartAt, End: s.EndAt})
	}

	h.writeJSON(w, http.StatusCreated, resp)
}

// rotaPlanResponse is the rota defining one now would make: the Define Rota
// screen's preview, from the same arithmetic as the definition.
//
// Refusal is why defining it would be refused, or empty when it would not.
// It is part of the answer rather than an error status, because a plan that
// cannot be defined is still a plan the screen shows — the Shifts that would
// clash, beside the sentence saying so.
//
// Overrides is how far each Override reaches across the planned Shifts, in
// list order, so the screen can say which match nothing.
type rotaPlanResponse struct {
	PatternSet bool                    `json:"patternSet"`
	Previous   *previousRotaResponse   `json:"previous"`
	Shifts     []plannedShiftResponse  `json:"shifts"`
	Overrides  []overrideReachResponse `json:"overrides"`
	Refusal    string                  `json:"refusal"`
}

// overrideReachResponse is one Override and how many of the planned Shifts it
// matched. MissedByTime marks an exact-time Override that matched none though
// it falls on a date one of them is on: the pattern's time has likely changed
// under it.
type overrideReachResponse struct {
	Number       int    `json:"number"`
	Description  string `json:"description"`
	Granularity  string `json:"granularity"`
	Matched      int    `json:"matched"`
	MissedByTime bool   `json:"missedByTime"`
}

// overrideTouchResponse is one Override that matched a planned Shift, and
// what it set there: StartTime is "" and LengthMinutes 0 where it set neither.
type overrideTouchResponse struct {
	Number        int    `json:"number"`
	StartTime     string `json:"startTime"`
	LengthMinutes int    `json:"lengthMinutes"`
	Shape         bool   `json:"shape"`
	Pins          int    `json:"pins"`
}

// previousRotaResponse is where the rota before stopped: the generated start
// of its last Shift, and its last few Shifts as they now stand.
type previousRotaResponse struct {
	ShiftCount   int                    `json:"shiftCount"`
	StoppedAfter string                 `json:"stoppedAfter"`
	Tail         []plannedShiftResponse `json:"tail"`
}

// plannedShiftResponse is a Shift by its wall-clock times. A planned one has
// no id yet; a tail Shift's id is left off too, since nothing here acts on it.
//
// Overrides are the Overrides that matched it, in the order they applied;
// always empty on a tail Shift, which was defined already. MovedFrom is the
// start the pattern generated where an Override moved it, and "" where none
// did. Clash marks one that would run at the same moment as another Shift,
// planned or already defined.
type plannedShiftResponse struct {
	Start     string                  `json:"start"`
	End       string                  `json:"end"`
	MovedFrom string                  `json:"movedFrom"`
	Clash     bool                    `json:"clash"`
	Overrides []overrideTouchResponse `json:"overrides"`
}

// handleGetRotaPlan answers what defining a rota of shiftCount Shifts, none
// before notBefore, would make right now.
//
// It answers whether or not a rota is in flight. That rule is DefineRota's to
// enforce and GET /rotations/in-flight's to report; this is the arithmetic.
func (h *Handler) handleGetRotaPlan(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	count, err := strconv.Atoi(query.Get("shiftCount"))
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "shiftCount must be a whole number")
		return
	}

	plan, err := services.PlanRota(r.Context(), h.store, services.DefineRotaParams{
		ShiftCount: count,
		NotBefore:  query.Get("notBefore"),
	})
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	resp := rotaPlanResponse{
		PatternSet: plan.PatternSet,
		Shifts:     toPlannedShifts(plan.Shifts),
		Overrides:  make([]overrideReachResponse, 0, len(plan.Overrides)),
	}
	for i, overridden := range plan.Overridden {
		for _, touch := range overridden.Touches {
			resp.Shifts[i].Overrides = append(resp.Shifts[i].Overrides, overrideTouchResponse{
				Number:        touch.Number,
				StartTime:     touch.StartTime,
				LengthMinutes: touch.LengthMinutes,
				Shape:         touch.SetsShape,
				Pins:          touch.Pins,
			})
		}
	}
	for i, generated := range plan.Generated {
		if generated != plan.Shifts[i].StartAt {
			resp.Shifts[i].MovedFrom = generated
		}
	}
	for i, clashing := range plan.Clashing {
		resp.Shifts[i].Clash = clashing
	}
	for _, reach := range plan.Overrides {
		resp.Overrides = append(resp.Overrides, overrideReachResponse{
			Number:       reach.Number,
			Description:  reach.Description,
			Granularity:  reach.Granularity,
			Matched:      reach.Matched,
			MissedByTime: reach.MissedByTime,
		})
	}
	if plan.Previous != nil {
		resp.Previous = &previousRotaResponse{
			ShiftCount:   plan.Previous.ShiftCount,
			StoppedAfter: plan.Previous.StoppedAfter,
			Tail:         toPlannedShifts(plan.Previous.Tail),
		}
	}
	if plan.Refusal != nil {
		resp.Refusal = plan.Refusal.Error()
	}
	h.writeJSON(w, http.StatusOK, resp)
}

func toPlannedShifts(shifts []db.Shift) []plannedShiftResponse {
	out := make([]plannedShiftResponse, 0, len(shifts))
	for _, s := range shifts {
		out = append(out, plannedShiftResponse{Start: s.StartAt, End: s.EndAt, Overrides: []overrideTouchResponse{}})
	}
	return out
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
// the way in: the confirmation an Organiser gives is a decision made in front of the
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
