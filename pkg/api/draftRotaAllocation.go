package api

import (
	"net/http"
	"time"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// draftRotaAllocationResponse is what a solve has to say for itself: whether it
// found a rota, and how much of one it managed to staff. Not the rota it drafted
// — that is read back from the draft, and only ever by an admin (ADR 0008).
//
// seatsFilled against seatsAsked is the number that changes what an admin does
// next: four Seats unfilled is somebody to chase, INFEASIBLE is a conflict to go
// and resolve, and there is still availability window left to do either in.
type draftRotaAllocationResponse struct {
	RotaID       string `json:"rotaId"`
	RotaStart    string `json:"rotaStart"`
	SolvedAt     string `json:"solvedAt"`
	Success      bool   `json:"success"`
	SolverStatus string `json:"solverStatus"`
	SeatsAsked   int    `json:"seatsAsked"`
	SeatsFilled  int    `json:"seatsFilled"`
	// SolveTimeSeconds is the one diagnostic worth an admin's attention: the
	// solve sits on the allocate path too, so a rota creeping towards the
	// solver's thirty-second ceiling is worth seeing before it gets there. The
	// rest of the diagnostics are stored, not shown.
	SolveTimeSeconds float64 `json:"solveTimeSeconds"`
}

// handleSolveDraftRotaAllocation solves the rota in flight and stores the answer
// as its Draft Rota Allocation, replacing whatever draft was there.
//
// A POST rather than a GET, and it is the whole draft that is created: a solve
// writes, and what it writes is the resource. Nothing about it is idempotent in
// the sense a GET promises — the inputs move underneath it all through the
// availability window, which is the entire reason to solve again.
//
// It runs inline rather than as a job, unlike an availability send. pyallocator
// caps itself at thirty seconds, so this is a request an admin waits out with a
// spinner rather than one they come back to.
func (h *Handler) handleSolveDraftRotaAllocation(w http.ResponseWriter, r *http.Request) {
	// No python flag: the server has none to pass, and ResolvePythonInterpreter
	// falls back to $ILFORD_CPSAT_PYTHON, then the venv, then python3. The flag
	// belongs to the CLI, where a maintainer is choosing an interpreter by hand.
	result, err := services.SolveDraftRotaAllocation(r.Context(), h.store, h.volunteers, h.cfg, h.logger, "")
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, draftRotaAllocationResponse{
		RotaID:           result.RotaID,
		RotaStart:        result.RotaStart,
		SolvedAt:         result.SolvedAt.Format(time.RFC3339),
		Success:          result.Success,
		SolverStatus:     result.SolverStatus,
		SeatsAsked:       result.SeatsAsked,
		SeatsFilled:      result.SeatsFilled,
		SolveTimeSeconds: result.Diagnostics.SolveTimeSeconds,
	})
}

// draftRotaViewResponse is the rota in flight and whatever has been drafted for
// it — the read behind the draft chips and the panel above them (issue #143).
//
// Both halves are nullable and they mean different things. A null rota is
// nothing in flight, which is the state between one rota going out and the next
// being defined. A null draft beside a rota is one nobody has solved for yet,
// which is where every rota starts. An infeasible solve is neither: it comes
// back as a draft whose success is false, because "no rota is possible from
// these inputs" is the answer an admin most needs while there is still time to
// change them.
type draftRotaViewResponse struct {
	Rota  *draftRotaBody `json:"rota"`
	Draft *draftBody     `json:"draft"`
}

type draftRotaBody struct {
	ID string `json:"id"`
	// SeatsAsked is what the rota asks for now, not what it asked when the draft
	// was solved — the honest denominator of "two Seats unfilled".
	SeatsAsked int `json:"seatsAsked"`
}

type draftBody struct {
	SolvedAt     string `json:"solvedAt"`
	Success      bool   `json:"success"`
	SolverStatus string `json:"solverStatus"`
	SeatsFilled  int    `json:"seatsFilled"`
	// Shifts carries only the Shifts this solve placed anybody on. Never null, so
	// a draft that staffed nothing is an empty list rather than an absence.
	Shifts []draftShiftBody `json:"shifts"`
}

type draftShiftBody struct {
	// ShiftID rather than a date: the client already holds the rota's Shifts and
	// merges these onto them by identity (ADR 0001).
	ShiftID   string             `json:"shiftId"`
	Assignees []assigneeResponse `json:"assignees"`
}

// handleGetDraftRotaAllocation answers with the rota in flight's Draft Rota
// Allocation.
//
// Admin-only, and that gate is the endpoint's whole reason to exist as its own
// resource. GET /api/shifts is public and stays that way: it never reads a draft
// table, so no future edit to it can leak a speculative rota to the volunteers
// who subscribe to the rota page and its calendar feed (ADR 0008).
func (h *Handler) handleGetDraftRotaAllocation(w http.ResponseWriter, r *http.Request) {
	view, err := services.ReadDraftRotaAllocation(r.Context(), h.store, h.volunteers, h.cfg, h.logger)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	resp := draftRotaViewResponse{}
	if view == nil {
		h.writeJSON(w, http.StatusOK, resp)
		return
	}

	resp.Rota = &draftRotaBody{ID: view.RotaID, SeatsAsked: view.SeatsAsked}
	if view.Draft != nil {
		draft := &draftBody{
			SolvedAt:     view.Draft.SolvedAt.Format(time.RFC3339),
			Success:      view.Draft.Success,
			SolverStatus: view.Draft.SolverStatus,
			SeatsFilled:  view.Draft.SeatsFilled,
			Shifts:       make([]draftShiftBody, 0, len(view.Draft.Shifts)),
		}
		for _, shift := range view.Draft.Shifts {
			assignees := make([]assigneeResponse, 0, len(shift.Assignees))
			for _, a := range shift.Assignees {
				assignees = append(assignees, assigneeResponse{
					VolunteerID: a.VolunteerID,
					CustomEntry: a.CustomEntry,
					Name:        a.Name,
					Role:        a.Role,
					Group:       a.Group,
				})
			}
			draft.Shifts = append(draft.Shifts, draftShiftBody{ShiftID: shift.ShiftID, Assignees: assignees})
		}
		resp.Draft = draft
	}

	h.writeJSON(w, http.StatusOK, resp)
}
