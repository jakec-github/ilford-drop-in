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
