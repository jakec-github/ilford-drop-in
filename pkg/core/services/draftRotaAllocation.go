package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/allocator"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// DraftRotaAllocationStore is what solving a draft needs: everything the solve
// reads, plus somewhere to put the answer that is not the `allocation` table
// (ADR 0008), plus the two reads that say whether solving is called for at all.
//
// It deliberately does not embed AllocateRotaStore. Holding one of these is
// permission to draft a rota, not to allocate it, and the HTTP API holds one.
type DraftRotaAllocationStore interface {
	SolveRotaStore
	GetRotaInFlight(ctx context.Context) (*db.RotaInFlight, error)
	GetDraftRotaAllocation(ctx context.Context, rotaID string) (*db.DraftRotaAllocation, error)
	ReplaceDraftRotaAllocation(ctx context.Context, draft db.DraftRotaAllocation, seats []db.DraftAllocation) error
}

// DraftRotaAllocationStatus is what a draft has to say for itself: not the rota
// it drafted — that is read back from the draft — but whether it found one, how
// much of it it managed to staff, and whether it still speaks for the inputs as
// they now stand.
//
// SeatsFilled against SeatsAsked is the number an admin acts on during the
// availability window. "Four Seats unfilled" is a nudge to chase somebody;
// INFEASIBLE is a conflict to go and resolve. Neither is legible from the draft
// rows, which is why the outcome is stored beside them.
//
// One type for both a fresh solve and a stored draft, because an admin asking
// "where is the rota up to" is asking one question, and two shapes for it would
// be two things for a screen to reconcile.
type DraftRotaAllocationStatus struct {
	RotaID    string
	RotaStart string
	// Solved is false for the rota nobody has drafted yet, where every rota
	// starts and where the fields below say nothing.
	Solved         bool
	SolvedAt       time.Time
	Success        bool
	SolverStatus   string
	ObjectiveValue int
	Diagnostics    allocator.CpsatDiagnostics
	SeatsAsked     int
	SeatsFilled    int
	// Dirty reports that an allocator input has moved since the draft was
	// solved, so it is a guess at a question nobody is asking any more. An
	// undrafted rota is dirty: there is nothing that speaks for it at all.
	Dirty bool
	// Solving reports that a solve is running for this rota in this process, so
	// what is above it is the previous answer and a fresher one is on its way.
	// Set by the caller that owns the solve, since that is where the fact lives
	// (api/draftsolves.go).
	Solving bool
}

// SolveDraftRotaAllocation solves the rota in flight and stores the answer as
// its Draft Rota Allocation, replacing whatever draft was there.
//
// The same solve allocating runs, written somewhere else — which is the whole
// of ADR 0008. Nothing here reaches the `allocation` table, the calendar feed or
// the public shift listing, and no reader of those has anything new to remember.
//
// An infeasible solve is stored like any other. It is the outcome an admin most
// needs to see early, while there is still availability window left to fix the
// input in, and a draft with no Seats and no explanation would look exactly like
// a rota nobody had solved yet.
func SolveDraftRotaAllocation(
	ctx context.Context,
	database DraftRotaAllocationStore,
	volunteerClient VolunteerClient,
	cfg *config.Config,
	logger *zap.Logger,
	pythonFlag string,
) (*DraftRotaAllocationStatus, error) {
	logger.Debug("Solving the draft rota allocation")

	solve, err := solveRotaInFlight(ctx, database, volunteerClient, cfg, logger, pythonFlag)
	if err != nil {
		return nil, err
	}

	// A draft Seat is exactly an allocation row — it becomes one when the rota
	// is allocated — so there is one converter and this lifts its answer across
	// rather than walking the solved shifts a second way. Two walks would be two
	// chances to disagree about what the solver said.
	allocations, err := convertToDBAllocations(solve.shiftIDByDate, solve.solvedShifts)
	if err != nil {
		return nil, fmt.Errorf("failed to convert the solved rota: %w", err)
	}
	seats := make([]db.DraftAllocation, 0, len(allocations))
	for _, a := range allocations {
		seats = append(seats, db.DraftAllocation{
			ID:          a.ID,
			ShiftID:     a.ShiftID,
			Role:        a.Role,
			VolunteerID: a.VolunteerID,
			CustomEntry: a.CustomEntry,
		})
	}

	// The diagnostics travel as the solver's own JSON. The store has no business
	// knowing their shape, and pyallocator is free to add to them.
	diagnostics, err := json.Marshal(solve.output.Diagnostics)
	if err != nil {
		return nil, fmt.Errorf("failed to encode solver diagnostics: %w", err)
	}

	draft := solve.draft(time.Now().UTC(), diagnostics)
	if err := database.ReplaceDraftRotaAllocation(ctx, draft, seats); err != nil {
		return nil, fmt.Errorf("failed to store the draft rota allocation: %w", err)
	}

	logger.Info("Stored the draft rota allocation",
		zap.String("rota_id", draft.RotaID),
		zap.String("solver_status", draft.SolverStatus),
		zap.Int("seats_filled", draft.SeatsFilled),
		zap.Int("seats_asked", draft.SeatsAsked))

	return &DraftRotaAllocationStatus{
		RotaID:         draft.RotaID,
		RotaStart:      solve.rota.Start,
		Solved:         true,
		SolvedAt:       draft.SolvedAt,
		Success:        draft.Success,
		SolverStatus:   draft.SolverStatus,
		ObjectiveValue: draft.ObjectiveValue,
		Diagnostics:    solve.output.Diagnostics,
		SeatsAsked:     draft.SeatsAsked,
		SeatsFilled:    draft.SeatsFilled,
		// Clean by construction: this draft was solved from the inputs as the
		// stamp it carries found them. Anything landing since has already moved
		// the Rotation's stamp past it, and the next read will say so.
		Dirty: false,
	}, nil
}

// draft is the row that stores this solve, minus its Seats: what the solver
// answered, and what the answer can later be judged against.
//
// The inputs stamp is the Rotation's own as it stood when the solve began —
// solveRotaInFlight reads the rota before it reads a single input — rather than
// one taken now. That is what makes a draft read as dirty when something moved
// while the solver was running: erring that way costs a re-solve, and the other
// way loses the change entirely (issue #142).
func (s *rotaSolve) draft(solvedAt time.Time, diagnostics []byte) db.DraftRotaAllocation {
	return db.DraftRotaAllocation{
		RotaID:          s.rota.ID,
		SolvedAt:        solvedAt,
		Success:         s.output.Success,
		SolverStatus:    s.output.SolverStatus,
		ObjectiveValue:  s.output.ObjectiveValue,
		Diagnostics:     diagnostics,
		InputsChangedAt: s.rota.InputsChangedAt,
		SeatsAsked:      s.seatsAsked(),
		SeatsFilled:     s.seatsFilled(),
	}
}

// DraftRotaAllocationInFlight reports where the rota in flight's draft has got
// to: when it was last solved, what it found, and whether an allocator input has
// moved under it since.
//
// It writes nothing and never solves. Deciding to solve is the caller's, because
// only the caller knows whether a solve is already running in this process —
// and a solve is a subprocess, so two of them for one answer is a real cost
// rather than a tidiness point.
//
// A rota with no draft comes back Solved false and Dirty true, which is the
// honest reading of both: nothing has been solved, and nothing speaks for the
// rota as it stands.
func DraftRotaAllocationInFlight(ctx context.Context, database DraftRotaAllocationStore) (*DraftRotaAllocationStatus, error) {
	rota, err := database.GetRotaInFlight(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read the rota in flight: %w", err)
	}
	if rota == nil {
		// The ordinary state between one rota going out and the next being
		// defined. Said as the missing step rather than as an empty answer:
		// there is nothing to draft until a rota exists.
		return nil, wrapf(ErrNotFound, "there is no rota in flight - define a rota first")
	}

	status := &DraftRotaAllocationStatus{
		RotaID:    rota.ID,
		RotaStart: rota.Start,
		Dirty:     true,
	}

	draft, err := database.GetDraftRotaAllocation(ctx, rota.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to read the draft rota allocation for rota %s: %w", rota.ID, err)
	}
	if draft == nil {
		return status, nil
	}

	status.Solved = true
	status.SolvedAt = draft.SolvedAt.UTC()
	status.Success = draft.Success
	status.SolverStatus = draft.SolverStatus
	status.ObjectiveValue = draft.ObjectiveValue
	status.SeatsAsked = draft.SeatsAsked
	status.SeatsFilled = draft.SeatsFilled
	// Dirty is the two stamps disagreeing, and nothing else. Not "solved before
	// the last change", which would be a comparison against a clock and would
	// lose any change that landed while the solver was running.
	status.Dirty = !draft.InputsChangedAt.Equal(rota.InputsChangedAt)

	// The diagnostics were stored as the solver's own JSON and are read back the
	// same way. Only the solve time is spent, and a bag this layer cannot parse
	// is not worth failing a status read over — it means pyallocator changed
	// shape, which the next solve will settle.
	if err := json.Unmarshal(draft.Diagnostics, &status.Diagnostics); err != nil {
		status.Diagnostics = allocator.CpsatDiagnostics{}
	}

	return status, nil
}
