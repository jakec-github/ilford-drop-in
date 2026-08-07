package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// draftRotaAllocationResponse is what the rota in flight's draft has to say for
// itself: whether it found a rota, how much of one it managed to staff, whether
// it still speaks for the inputs as they now stand, and the rota it drafted.
//
// seatsFilled against seatsAsked is the number that changes what an admin does
// next: four Seats unfilled is somebody to chase, INFEASIBLE is a conflict to go
// and resolve, and there is still availability window left to do either in.
// shifts is what they read once they have decided to look (issue #143).
type draftRotaAllocationResponse struct {
	RotaID    string `json:"rotaId"`
	RotaStart string `json:"rotaStart"`
	// Solved is false for a rota nobody has drafted yet, where everything below
	// it says nothing. It is not an error state: it is where every rota starts,
	// and it lasts until the first solve finishes.
	Solved       bool   `json:"solved"`
	SolvedAt     string `json:"solvedAt"`
	Success      bool   `json:"success"`
	SolverStatus string `json:"solverStatus"`
	SeatsAsked   int    `json:"seatsAsked"`
	SeatsFilled  int    `json:"seatsFilled"`
	// Dirty says an allocator input has moved since the draft was solved. A
	// reader that finds this true has already caused a re-solve, unless solving
	// says one was underway.
	Dirty bool `json:"dirty"`
	// Solving says a solve is running now, so a fresher answer is on its way and
	// asking again shortly will get it.
	Solving bool `json:"solving"`
	// Hash fingerprints the rota below. It is what a client says back when it
	// allocates, which is how "allocate the rota you were shown" is enforced:
	// allocating re-solves and commits only if the answer fingerprints the same
	// (ADR 0008). Empty for a rota nobody has drafted — there is nothing to
	// confirm.
	Hash string `json:"hash"`
	// SolveTimeSeconds is the one diagnostic worth an admin's attention: the
	// solve sits on the allocate path too, so a rota creeping towards the
	// solver's thirty-second ceiling is worth seeing before it gets there. The
	// rest of the diagnostics are stored, not shown.
	SolveTimeSeconds float64 `json:"solveTimeSeconds"`
	// Shifts is the rota the draft drafted, carrying only the Shifts it placed
	// anybody on. Never null, so a rota nobody has solved for and one the solver
	// could staff nobody on both read as an empty list rather than an absence.
	Shifts []draftShiftResponse `json:"shifts"`
}

type draftShiftResponse struct {
	// ShiftID rather than a date: the client already holds the rota's Shifts and
	// merges these onto them by identity (ADR 0001).
	ShiftID   string             `json:"shiftId"`
	Assignees []assigneeResponse `json:"assignees"`
}

// handleGetDraftRotaAllocation reports where the rota in flight's draft has got
// to, and the rota it drafted — and re-solves it first when its inputs have
// moved.
//
// Solving on read is the whole design (issue #142). A draft that only refreshed
// when an admin asked it to would be stale exactly when it mattered, and the
// alternative — a timer re-solving the rota through the night — would spend
// solves on a rota nobody is looking at. Reading is the moment the answer is
// wanted, and the solve that produces it is quick enough to wait out.
//
// A solve already running is not waited for or duplicated: the draft as it
// stands comes back with solving set, which is a screen's cue to show what is
// there and ask again shortly.
//
// Admin-only, and that gate is the endpoint's whole reason to exist as its own
// resource rather than as a field of GET /api/shifts. That listing is public and
// stays that way: it never reads a draft table, so no future edit to it can leak
// a speculative rota to the volunteers who subscribe to the rota page and its
// calendar feed (ADR 0008).
func (h *Handler) handleGetDraftRotaAllocation(w http.ResponseWriter, r *http.Request) {
	status, err := services.DraftRotaAllocationInFlight(r.Context(), h.store, h.volunteers, h.cfg, h.logger)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	if status.Dirty {
		if h.drafts.begin() {
			defer h.drafts.end()
			solved, err := h.solveDraftRotaAllocation(r)
			if err != nil {
				h.writeServiceError(w, err)
				return
			}
			status = solved
		} else {
			status.Solving = true
		}
	}

	h.writeJSON(w, http.StatusOK, draftStatus(status))
}

// handleSolveDraftRotaAllocation re-solves the rota in flight and stores the
// answer as its Draft Rota Allocation, replacing whatever draft was there.
//
// A POST rather than a GET, and it is the whole draft that is created: a solve
// writes, and what it writes is the resource. Nothing about it is idempotent in
// the sense a GET promises — the inputs move underneath it all through the
// availability window, which is the entire reason to solve again.
//
// It solves whether or not the inputs have moved, which is what makes it worth
// having beside the GET above. The roster is a Google Sheet with no change
// notification, so a new volunteer or a newly held Role moves no stamp here —
// and that is precisely the change nobody could have predicted. This is the
// admin saying "look again anyway".
//
// It runs inline rather than as a job, unlike an availability send. pyallocator
// caps itself at thirty seconds, so this is a request an admin waits out with a
// spinner rather than one they come back to.
func (h *Handler) handleSolveDraftRotaAllocation(w http.ResponseWriter, r *http.Request) {
	if !h.drafts.begin() {
		// Refused rather than queued behind the running solve. The answer that
		// one produces is the answer this one would have produced, and an admin
		// told "already solving" can watch it land.
		h.writeServiceError(w, fmt.Errorf("a solve is already running for this rota - wait for it to finish%.0w", services.ErrConflict))
		return
	}
	defer h.drafts.end()

	status, err := h.solveDraftRotaAllocation(r)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, draftStatus(status))
}

// solveDraftRotaAllocation runs the solve both handlers share. The caller holds
// the solve slot.
//
// No python flag: the server has none to pass, and ResolvePythonInterpreter
// falls back to $ILFORD_CPSAT_PYTHON, then the venv, then python3. The flag
// belongs to the CLI, where a maintainer is choosing an interpreter by hand.
func (h *Handler) solveDraftRotaAllocation(r *http.Request) (*services.DraftRotaAllocationStatus, error) {
	return services.SolveDraftRotaAllocation(r.Context(), h.store, h.volunteers, h.cfg, h.logger, "")
}

// draftStatus is the wire form of a draft's state, from either handler. One
// shape for both: an admin asking "where is the rota up to" is asking one
// question, whether or not their request is what caused the solve.
func draftStatus(status *services.DraftRotaAllocationStatus) draftRotaAllocationResponse {
	response := draftRotaAllocationResponse{
		RotaID:           status.RotaID,
		RotaStart:        status.RotaStart,
		Solved:           status.Solved,
		Success:          status.Success,
		SolverStatus:     status.SolverStatus,
		SeatsAsked:       status.SeatsAsked,
		SeatsFilled:      status.SeatsFilled,
		Dirty:            status.Dirty,
		Solving:          status.Solving,
		Hash:             status.Hash,
		SolveTimeSeconds: status.Diagnostics.SolveTimeSeconds,
		Shifts:           draftShifts(status.Shifts),
	}
	// An unsolved rota carries no time, rather than the zero time formatted as
	// the year 1: there is no moment to report.
	if status.Solved {
		response.SolvedAt = status.SolvedAt.Format(time.RFC3339)
	}
	return response
}

// draftShifts is the wire form of the rota a draft drafted.
func draftShifts(shifts []services.DraftShift) []draftShiftResponse {
	out := make([]draftShiftResponse, 0, len(shifts))
	for _, shift := range shifts {
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
		out = append(out, draftShiftResponse{ShiftID: shift.ShiftID, Assignees: assignees})
	}
	return out
}
