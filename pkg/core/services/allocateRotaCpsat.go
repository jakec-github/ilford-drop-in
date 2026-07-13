package services

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/allocator"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services/utils"
)

// AllocateRotaCpsatResult contains the CP-SAT allocation results.
type AllocateRotaCpsatResult struct {
	RotaID          string
	RotaStart       string
	ShiftCount      int
	ShiftDates      []time.Time
	Success         bool
	SolverStatus    string
	ObjectiveValue  int
	Diagnostics     CpsatDiagnostics
	AllocatedShifts []*allocator.Shift
	Saved           bool
}

// AllocateRotaCpsat allocates the latest rota using the Python CP-SAT
// solver (pyallocator) instead of the greedy allocator.
//
// The data-fetching orchestration mirrors AllocateRota step for step —
// duplicated deliberately so the experimental CP-SAT path adds new files
// only; consolidate if/when this becomes the primary allocator.
// Persistence semantics match AllocateRota: save when !dryRun and the
// solve succeeded (or forceCommit).
func AllocateRotaCpsat(
	ctx context.Context,
	database AllocateRotaStore,
	volunteerClient VolunteerClient,
	formsClient FormsClientWithResponses,
	cfg *config.Config,
	logger *zap.Logger,
	dryRun bool,
	forceCommit bool,
	pythonFlag string,
) (*AllocateRotaCpsatResult, error) {
	logger.Debug("Starting allocateRotaCpsat",
		zap.Bool("dry_run", dryRun),
		zap.Bool("force_commit", forceCommit))

	// Steps 1-7 mirror AllocateRota (see allocateRota.go).
	rotations, err := database.GetRotations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch rotations: %w", err)
	}
	if len(rotations) == 0 {
		return nil, fmt.Errorf("no rotations found - please define a rota first")
	}

	targetRota := utils.FindLatestRotation(rotations)
	logger.Debug("Using latest rota", zap.String("id", targetRota.ID))

	shiftDates, err := rotaShiftDates(ctx, database, targetRota.ID)
	if err != nil {
		return nil, err
	}

	rotaRequests, err := database.GetAvailabilityRequestsByRotaID(ctx, targetRota.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch availability requests: %w", err)
	}
	requestsForRota := utils.FilterSentRequests(rotaRequests)
	if len(requestsForRota) == 0 {
		return nil, fmt.Errorf("no availability requests found for rota %s - please run requestAvailability first", targetRota.ID)
	}

	allVolunteers, err := volunteerClient.ListVolunteers(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch volunteers: %w", err)
	}
	volunteersByID := make(map[string]model.Volunteer)
	for _, vol := range allVolunteers {
		volunteersByID[vol.ID] = vol
	}
	activeVolunteers := utils.FilterActiveVolunteers(allVolunteers)
	logger.Debug("Active volunteers", zap.Int("count", len(activeVolunteers)))

	availability, err := fetchAvailabilityResponses(
		ctx,
		requestsForRota,
		volunteersByID,
		shiftDates,
		formsClient,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch availability responses: %w", err)
	}

	allocatorVolunteers := convertToAllocatorVolunteers(activeVolunteers)

	shiftDateStrings := make([]string, len(shiftDates))
	for i, date := range shiftDates {
		shiftDateStrings[i] = date.Format("2006-01-02")
	}

	// History gets ALL volunteers (inactive included) so past shifts
	// keep their groups; allocation itself only sees active volunteers.
	historicalShifts, err := buildHistoricalShifts(
		ctx,
		database,
		rotations,
		targetRota,
		convertToAllocatorVolunteers(allVolunteers),
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build historical shifts: %w", err)
	}

	allocatorOverrides, err := convertRotaOverrides(cfg.RotaOverrides, shiftDates, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to convert rota overrides: %w", err)
	}

	// Build the solver input and run the Python subprocess.
	input, err := buildCpsatInput(
		allocatorVolunteers,
		availability,
		shiftDateStrings,
		cfg.DefaultShiftSize,
		allocatorOverrides,
		historicalShifts,
		cfg.MaxAllocationFrequency,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build cpsat input: %w", err)
	}
	logger.Debug("Built cpsat input",
		zap.Int("groups", len(input.Groups)),
		zap.Int("shifts", len(input.Shifts)),
		zap.Int("max_allocation_count", input.MaxAllocationCount))

	pythonPath := resolvePythonInterpreter(pythonFlag)
	logger.Info("Running CP-SAT allocator", zap.String("python", pythonPath))
	output, err := runCpsatAllocator(ctx, pythonPath, input, logger)
	if err != nil {
		return nil, err
	}

	logger.Info("CP-SAT allocation completed",
		zap.String("solver_status", output.SolverStatus),
		zap.Bool("success", output.Success),
		zap.Int("objective_value", output.ObjectiveValue),
		zap.Float64("solve_time_seconds", output.Diagnostics.SolveTimeSeconds))

	allocatedShifts, err := cpsatOutputToAllocatorShifts(output, allocatorVolunteers)
	if err != nil {
		return nil, fmt.Errorf("failed to convert cpsat output: %w", err)
	}

	// Persistence — same semantics as AllocateRota.
	shouldSave := !dryRun && (output.Success || forceCommit)
	if shouldSave {
		logger.Info("Saving allocations to database",
			zap.Bool("success", output.Success),
			zap.Bool("forced", forceCommit && !output.Success))
		dbAllocations := convertToDBAllocations(targetRota.ID, allocatedShifts)
		if err := database.InsertAllocationsAndSetAllocated(ctx, dbAllocations, targetRota.ID, time.Now().UTC()); err != nil {
			return nil, fmt.Errorf("failed to save allocations: %w", err)
		}
		logger.Info("Allocations saved and rotation marked as allocated", zap.Int("count", len(dbAllocations)))
	} else if dryRun {
		logger.Info("Dry run mode - allocations not saved")
	} else {
		logger.Warn("Solver did not find a feasible rota - not saving to database (use forceCommit to save anyway)")
	}

	return &AllocateRotaCpsatResult{
		RotaID:          targetRota.ID,
		RotaStart:       targetRota.Start,
		ShiftCount:      targetRota.ShiftCount,
		ShiftDates:      shiftDates,
		Success:         output.Success,
		SolverStatus:    output.SolverStatus,
		ObjectiveValue:  output.ObjectiveValue,
		Diagnostics:     output.Diagnostics,
		AllocatedShifts: allocatedShifts,
		Saved:           shouldSave,
	}, nil
}
