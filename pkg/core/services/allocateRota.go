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
// (pyallocator). It fetches availability data, builds the solver input,
// runs the subprocess and persists the result when !dryRun and the solve
// succeeded (or forceCommit).
func AllocateRota(
	ctx context.Context,
	database AllocateRotaStore,
	volunteerClient VolunteerClient,
	formsClient FormsClientWithResponses,
	cfg *config.Config,
	logger *zap.Logger,
	dryRun bool,
	forceCommit bool,
	pythonFlag string,
) (*AllocateRotaResult, error) {
	logger.Debug("Starting allocateRota",
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

	// Refuse to re-allocate a rota that has already been allocated (issue #8).
	// A set allocated_datetime is the mark of a completed allocation (it is
	// written in the same transaction as the allocation rows). This is a
	// fast-fail that stops before the expensive solve; the authoritative,
	// race-safe guard lives in the shared persistence path
	// (InsertAllocationsAndSetAllocated), where it cannot be bypassed.
	if targetRota.AllocatedDatetime != "" {
		return nil, fmt.Errorf("rota %s is already allocated (at %s) - refusing to allocate again", targetRota.ID, targetRota.AllocatedDatetime)
	}

	// Read the rota's shifts once: the allocator works in dates, but persistence
	// keys allocations by shift id (ADR 0001). shiftIDByDate carries the solver's
	// date-keyed output back to the minted shift ids.
	shifts, err := database.GetShiftsByRotaID(ctx, targetRota.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch shifts: %w", err)
	}
	if len(shifts) == 0 {
		return nil, fmt.Errorf("rota %s has no shifts", targetRota.ID)
	}
	shiftDates, err := utils.ShiftDatesFromShifts(shifts)
	if err != nil {
		return nil, err
	}
	shiftIDByDate := make(map[string]string, len(shifts))
	dateByShiftID := make(map[string]string, len(shifts))
	shiftIDs := make([]string, len(shifts))
	for i, s := range shifts {
		shiftIDByDate[s.Date] = s.ID
		dateByShiftID[s.ID] = s.Date
		shiftIDs[i] = s.ID
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

	// Manual preallocations (issue #39): union operator-set pins with the config
	// preallocations. Each pin becomes a synthetic exact-date override appended
	// to the config-derived overrides so InitShifts unions them with no new merge
	// logic; add-only, deduped against identical config contributions.
	manualPins, err := database.GetManualPreallocationsByShiftIDs(ctx, shiftIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch manual preallocations: %w", err)
	}
	activeIDs := make(map[string]bool, len(activeVolunteers))
	for _, v := range activeVolunteers {
		activeIDs[v.ID] = true
	}
	// Pre-solve stale-pin check: fail loudly, naming the pin, rather than letting
	// an inactive/deleted preallocated volunteer surface as the solver's opaque
	// ProblemError (covers config pins too, per ADR 0003).
	if err := checkPreallocationsResolve(manualPins, dateByShiftID, allocatorOverrides, shiftDates, activeIDs); err != nil {
		return nil, err
	}
	manualOverrides, err := buildManualPreallocationOverrides(manualPins, dateByShiftID, allocatorOverrides)
	if err != nil {
		return nil, err
	}
	allocatorOverrides = append(allocatorOverrides, manualOverrides...)

	// Build the solver input and run the Python subprocess.
	input, err := allocator.BuildCpsatInput(
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

	pythonPath := allocator.ResolvePythonInterpreter(pythonFlag)
	logger.Info("Running CP-SAT allocator", zap.String("python", pythonPath))
	output, err := allocator.RunCpsatAllocator(ctx, pythonPath, input, logger)
	if err != nil {
		return nil, err
	}

	logger.Info("CP-SAT allocation completed",
		zap.String("solver_status", output.SolverStatus),
		zap.Bool("success", output.Success),
		zap.Int("objective_value", output.ObjectiveValue),
		zap.Float64("solve_time_seconds", output.Diagnostics.SolveTimeSeconds))

	allocatedShifts, err := allocator.CpsatOutputToShifts(output, allocatorVolunteers)
	if err != nil {
		return nil, fmt.Errorf("failed to convert cpsat output: %w", err)
	}

	// Persistence — same semantics as AllocateRota.
	shouldSave := !dryRun && (output.Success || forceCommit)
	if shouldSave {
		logger.Info("Saving allocations to database",
			zap.Bool("success", output.Success),
			zap.Bool("forced", forceCommit && !output.Success))
		dbAllocations, err := convertToDBAllocations(shiftIDByDate, allocatedShifts)
		if err != nil {
			return nil, fmt.Errorf("failed to convert allocations: %w", err)
		}
		if err := database.InsertAllocationsAndSetAllocated(ctx, dbAllocations, targetRota.ID, time.Now().UTC()); err != nil {
			return nil, fmt.Errorf("failed to save allocations: %w", err)
		}
		logger.Info("Allocations saved and rotation marked as allocated", zap.Int("count", len(dbAllocations)))
	} else if dryRun {
		logger.Info("Dry run mode - allocations not saved")
	} else {
		logger.Warn("Solver did not find a feasible rota - not saving to database (use forceCommit to save anyway)")
	}

	return &AllocateRotaResult{
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
