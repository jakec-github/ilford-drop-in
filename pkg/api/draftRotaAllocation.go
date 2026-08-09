package api

import (
	"context"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// draftRotaAllocationResponse is what the rota in flight's draft has to say for
// itself: whether it found a rota, how much of one it managed to staff, and the
// rota it drafted.
//
// It never says whether the draft is stale or whether a solve is running,
// because by the time it is written neither can be true: a read waits for the
// solve slot and re-solves if it needs to, so what comes back always speaks for
// the inputs as they stood (issue #179).
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
	// SolveError is why the re-solve this read would have run did not happen —
	// most often a step nobody has taken yet, like an availability round nobody
	// has minted. Empty when there was nothing to re-solve or the re-solve
	// worked.
	//
	// It reports rather than fails, because it is about the solve and not about
	// the read: what comes back is the draft as it stands, which is exactly what
	// was asked for. A rota between being defined and its round being minted is
	// permanently in this state, and it is a state with something to say rather
	// than an error to show instead of the panel (issue #145).
	SolveError string `json:"solveError"`
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
// A solve already running is waited for rather than reported (issue #179). What
// comes back is never a stale draft, so a reader has no retry policy to hold:
// the request takes as long as it takes, and the answer speaks for the inputs.
//
// Waiting alone would not be enough to make that true. The solve queued ahead
// may have started before the edit that sent this reader here, so its answer
// need not account for that edit — which is why the status is read again once
// this request holds the slot, and solved only if it is *still* dirty. A reader
// whose edit the running solve already covered returns at once with that
// answer; one whose edit it predates gets its own solve. It terminates because
// every solve captures the inputs stamp as it starts, so the queue drains.
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
	if !status.Dirty {
		h.writeJSON(w, http.StatusOK, draftStatus(status))
		return
	}

	if err := h.drafts.acquire(r.Context()); err != nil {
		// The client gave up while queueing, so there is nobody to answer.
		// Nothing is written: the connection this would be written to is the one
		// that went away, and the solve this reader was waiting for carries on
		// regardless.
		h.logger.Debug("A draft read left the solve queue", zap.Error(err))
		return
	}
	defer h.drafts.release()

	// Read again, now that this request is the one solving. What was dirty when
	// this reader arrived may have been answered by the solve it queued behind.
	status, err = services.DraftRotaAllocationInFlight(r.Context(), h.store, h.volunteers, h.cfg, h.logger)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}
	if !status.Dirty {
		h.writeJSON(w, http.StatusOK, draftStatus(status))
		return
	}

	response := draftStatus(status)
	solved, err := h.solveDraftRotaAllocation(r)
	if err != nil {
		// Reported, not returned. The draft as it stands is what was asked for
		// and it is still worth showing; what failed is the courtesy this
		// endpoint performs on the way. Every rota is in exactly this state
		// between being defined and its availability round being minted, and
		// answering that with an error would take the draft panel off the one
		// screen that says what to do about it.
		response.SolveError = err.Error()
	} else {
		response = draftStatus(solved)
	}

	h.writeJSON(w, http.StatusOK, response)
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
//
// A solve already running is waited for and then solved past, rather than
// refused (issue #179): unlike a read, this cannot be satisfied by the answer
// the running solve produces, because the input that prompted it — a new
// volunteer on the Sheet — moves no stamp in this database and so the running
// solve may never have read it.
func (h *Handler) handleSolveDraftRotaAllocation(w http.ResponseWriter, r *http.Request) {
	if err := h.drafts.acquire(r.Context()); err != nil {
		h.logger.Debug("A solve left the queue before it ran", zap.Error(err))
		return
	}
	defer h.drafts.release()

	status, err := h.solveDraftRotaAllocation(r)
	if err != nil {
		h.writeServiceError(w, err)
		return
	}

	h.writeJSON(w, http.StatusOK, draftStatus(status))
}

// solveCeiling is how long a solve gets before it is taken to be wedged.
//
// pyallocator caps CP-SAT's *search* at thirty seconds (solver.py) but nothing
// caps process start, model building or the IO either side of it. That was one
// caller's problem while a solve nobody could get behind was refused; now that
// callers queue, a wedged subprocess holds up every reader of the rota. So it
// fails loudly and gives the slot back, at double the search cap — long enough
// that a legitimately slow solve still lands.
const solveCeiling = 60 * time.Second

// solveDraftRotaAllocation runs the solve both handlers share, under the ceiling
// and under the request's own context, so the subprocess dies with either. The
// caller holds the solve slot.
//
// No python flag: the server has none to pass, and ResolvePythonInterpreter
// falls back to $ILFORD_CPSAT_PYTHON, then the venv, then python3. The flag
// belongs to the CLI, where a maintainer is choosing an interpreter by hand.
func (h *Handler) solveDraftRotaAllocation(r *http.Request) (*services.DraftRotaAllocationStatus, error) {
	ctx, cancel := context.WithTimeout(r.Context(), solveCeiling)
	defer cancel()
	return services.SolveDraftRotaAllocation(ctx, h.store, h.volunteers, h.cfg, h.logger, "")
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
