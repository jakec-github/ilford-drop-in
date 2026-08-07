package services

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/allocator"
)

// AllocateRotaResult contains the CP-SAT allocation results.
type AllocateRotaResult struct {
	RotaID          string
	RotaStart       string
	ShiftCount      int
	ShiftDates      []time.Time
	Success         bool
	SolverStatus    string
	ObjectiveValue  int
	Diagnostics     allocator.CpsatDiagnostics
	AllocatedShifts []*allocator.Shift
	Saved           bool
}

// AllocateRota allocates the latest rota using the Python CP-SAT solver
// (pyallocator). The solve itself is solveRotaInFlight, shared with the Draft
// Rota Allocation so both answer the same problem from the same inputs; this is
// the half that commits the answer as the rota, when !dryRun and the solve
// succeeded (or forceCommit).
func AllocateRota(
	ctx context.Context,
	database AllocateRotaStore,
	volunteerClient VolunteerClient,
	cfg *config.Config,
	logger *zap.Logger,
	dryRun bool,
	forceCommit bool,
	pythonFlag string,
) (*AllocateRotaResult, error) {
	logger.Debug("Starting allocateRota",
		zap.Bool("dry_run", dryRun),
		zap.Bool("force_commit", forceCommit))

	solve, err := solveRotaInFlight(ctx, database, volunteerClient, cfg, logger, pythonFlag)
	if err != nil {
		return nil, err
	}

	shouldSave := !dryRun && (solve.output.Success || forceCommit)
	if shouldSave {
		logger.Info("Saving allocations to database",
			zap.Bool("success", solve.output.Success),
			zap.Bool("forced", forceCommit && !solve.output.Success))
		dbAllocations, err := convertToDBAllocations(solve.shiftIDByDate, solve.solvedShifts)
		if err != nil {
			return nil, fmt.Errorf("failed to convert allocations: %w", err)
		}
		if err := database.InsertAllocationsAndSetAllocated(ctx, dbAllocations, solve.rota.ID, time.Now().UTC()); err != nil {
			return nil, fmt.Errorf("failed to save allocations: %w", err)
		}
		logger.Info("Allocations saved and rotation marked as allocated", zap.Int("count", len(dbAllocations)))
	} else if dryRun {
		logger.Info("Dry run mode - allocations not saved")
	} else {
		logger.Warn("Solver did not find a feasible rota - not saving to database (use forceCommit to save anyway)")
	}

	return &AllocateRotaResult{
		RotaID:          solve.rota.ID,
		RotaStart:       solve.rota.Start,
		ShiftCount:      solve.rota.ShiftCount,
		ShiftDates:      solve.shiftDates,
		Success:         solve.output.Success,
		SolverStatus:    solve.output.SolverStatus,
		ObjectiveValue:  solve.output.ObjectiveValue,
		Diagnostics:     solve.output.Diagnostics,
		AllocatedShifts: solve.solvedShifts,
		Saved:           shouldSave,
	}, nil
}
