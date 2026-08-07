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
// (ADR 0008).
//
// It deliberately does not embed AllocateRotaStore. Holding one of these is
// permission to draft a rota, not to allocate it, and the HTTP API holds one.
type DraftRotaAllocationStore interface {
	SolveRotaStore
	ReplaceDraftRotaAllocation(ctx context.Context, draft db.DraftRotaAllocation, seats []db.DraftAllocation) error
}

// DraftRotaAllocationResult is what a solve has to say for itself: not the rota
// it drafted — that is read back from the draft — but whether it found one, and
// how much of it it managed to staff.
//
// SeatsFilled against SeatsAsked is the number an admin acts on during the
// availability window. "Four Seats unfilled" is a nudge to chase somebody;
// INFEASIBLE is a conflict to go and resolve. Neither is legible from the draft
// rows, which is why the outcome is stored beside them.
type DraftRotaAllocationResult struct {
	RotaID         string
	RotaStart      string
	SolvedAt       time.Time
	Success        bool
	SolverStatus   string
	ObjectiveValue int
	Diagnostics    allocator.CpsatDiagnostics
	SeatsAsked     int
	SeatsFilled    int
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
) (*DraftRotaAllocationResult, error) {
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

	solvedAt := time.Now().UTC()
	draft := db.DraftRotaAllocation{
		RotaID:         solve.rota.ID,
		SolvedAt:       solvedAt,
		Success:        solve.output.Success,
		SolverStatus:   solve.output.SolverStatus,
		ObjectiveValue: solve.output.ObjectiveValue,
		Diagnostics:    diagnostics,
	}
	if err := database.ReplaceDraftRotaAllocation(ctx, draft, seats); err != nil {
		return nil, fmt.Errorf("failed to store the draft rota allocation: %w", err)
	}

	asked, filled := solve.seatsAsked(), solve.seatsFilled()
	logger.Info("Stored the draft rota allocation",
		zap.String("rota_id", solve.rota.ID),
		zap.String("solver_status", solve.output.SolverStatus),
		zap.Int("seats_filled", filled),
		zap.Int("seats_asked", asked))

	return &DraftRotaAllocationResult{
		RotaID:         solve.rota.ID,
		RotaStart:      solve.rota.Start,
		SolvedAt:       solvedAt,
		Success:        solve.output.Success,
		SolverStatus:   solve.output.SolverStatus,
		ObjectiveValue: solve.output.ObjectiveValue,
		Diagnostics:    solve.output.Diagnostics,
		SeatsAsked:     asked,
		SeatsFilled:    filled,
	}, nil
}
