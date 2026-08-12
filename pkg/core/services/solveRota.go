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
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// rotaSolve is one run of the CP-SAT solver over the rota in flight, plus the
// pieces of its input that persisting the answer needs.
//
// It exists because there are two things to do with a solve and only one way to
// produce one: allocating commits it as the rota, and drafting stores it as a
// Draft Rota Allocation for an admin to watch (ADR 0008). The two must solve the
// same problem from the same inputs — the ADR's confirm-by-output-hash rests on
// it — so the assembly and the solve live here and the callers differ only in
// what they write.
type rotaSolve struct {
	// rota is the rota in flight: the latest, and necessarily unallocated,
	// since solveRotaInFlight refuses an allocated one.
	rota *db.Rotation
	// shifts are that rota's Shifts, and shiftIDByDate maps the solver's
	// date-keyed output back onto their ids (ADR 0001).
	shifts        []db.Shift
	shiftDates    []time.Time
	shiftIDByDate map[string]string
	// shapes is what each Shift asked for, by Shift id — read from the Shift
	// rather than the settings (#137), and already checked for a Shift asking
	// for nobody.
	shapes map[string]model.Shape
	// output is the solver's verbatim answer; solvedShifts is it lifted into
	// the allocator's own types.
	output       *allocator.CpsatOutput
	solvedShifts []*allocator.Shift
	// roles and volunteersByID are what turn the answer back into names, for the
	// caller that reports the rota it drafted rather than only storing it. Kept
	// from the assembly rather than re-read: the roster is a Google Sheet, and a
	// second read of it could name the Seats after somebody the solve never saw.
	roles          model.Roles
	volunteersByID map[string]model.Volunteer
}

// solveRotaInFlight assembles the allocator's input for the latest rota and runs
// the solver over it. It writes nothing.
//
// Everything it refuses over is a gate on the input rather than on the outcome —
// an already-allocated rota, settings nobody has filled in, a Shift asking for
// nobody, an availability round nobody has minted, a pin naming someone who has
// left. An INFEASIBLE solve is not one of those: it is a well-formed answer, and
// both callers have something to say about it.
//
// Every one of those refusals carries ErrInvalidInput or ErrConflict, because
// they describe a step the admin has not taken yet rather than a fault. They
// reach a browser now that drafting is an endpoint, and "internal server error"
// would be the wrong thing to tell somebody who has simply not minted the
// availability round.
func solveRotaInFlight(
	ctx context.Context,
	database SolveRotaStore,
	volunteerClient VolunteerClient,
	cfg *config.Config,
	logger *zap.Logger,
	pythonFlag string,
) (*rotaSolve, error) {
	rotations, err := database.GetRotations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch rotations: %w", err)
	}
	if len(rotations) == 0 {
		return nil, wrapf(ErrInvalidInput, "no rotations found - please define a rota first")
	}

	targetRota := utils.FindLatestRotation(rotations)
	logger.Debug("Using latest rota", zap.String("id", targetRota.ID))

	// Refuse a rota that has already been allocated (issue #8). A set
	// allocated_datetime is the mark of a completed allocation (it is written in
	// the same transaction as the allocation rows). This is a fast-fail that
	// stops before the expensive solve; the authoritative, race-safe guard lives
	// in each caller's persistence path — InsertAllocationsAndSetAllocated and
	// ReplaceDraftRotaAllocation — where it cannot be bypassed.
	if targetRota.AllocatedDatetime != "" {
		return nil, wrapf(ErrConflict, "rota %s is already allocated (at %s) - refusing to allocate again", targetRota.ID, targetRota.AllocatedDatetime)
	}

	// Read the rota's shifts once: the allocator works in dates, but persistence
	// keys allocations by shift id (ADR 0001). shiftIDByDate carries the solver's
	// date-keyed output back to the minted shift ids.
	shifts, err := database.GetShiftsByRotaID(ctx, targetRota.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch shifts: %w", err)
	}
	if len(shifts) == 0 {
		return nil, wrapf(ErrInvalidInput, "rota %s has no shifts", targetRota.ID)
	}
	shiftDates, err := utils.ShiftDatesFromShifts(shifts)
	if err != nil {
		return nil, err
	}
	shiftIDByDate := make(map[string]string, len(shifts))
	dateByShiftID := make(map[string]string, len(shifts))
	closedByDate := make(map[string]bool, len(shifts))
	shiftIDs := make([]string, len(shifts))
	for i, s := range shifts {
		shiftIDByDate[s.Date] = s.ID
		dateByShiftID[s.ID] = s.Date
		closedByDate[s.Date] = s.Closed
		shiftIDs[i] = s.ID
	}

	roles, err := RoleTable(ctx, database)
	if err != nil {
		return nil, err
	}
	// Allocation is the one path Roles are not optional on: every Seat the
	// solver fills belongs to a Role, so with none there is nothing to solve.
	// Incomplete settings block allocation and nothing else (ADR 0006).
	if len(roles.ByPriority()) == 0 {
		return nil, wrapf(ErrInvalidInput, "no roles are configured - add them on the settings screen before allocating")
	}

	settings, err := settingsForAllocation(ctx, database, logger)
	if err != nil {
		return nil, err
	}

	allVolunteers, err := volunteerClient.ListVolunteers(cfg, roles)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch volunteers: %w", err)
	}
	activeVolunteers := utils.FilterActiveVolunteers(allVolunteers)
	logger.Debug("Active volunteers", zap.Int("count", len(activeVolunteers)))

	allocatorVolunteers := convertToAllocatorVolunteers(activeVolunteers)

	// What each Shift asks for, read from the Shift itself rather than
	// recomputed from the settings (#137): a rota is allocated against the Shape
	// it was defined with, whatever the settings have been edited to since. The
	// gate refuses a rota with an open Shift asking for nobody.
	shapes, err := shapesForAllocation(ctx, database, shifts)
	if err != nil {
		return nil, err
	}

	// Shift indices are the solver's vocabulary, and index i is the i-th date in
	// order, so the shift ids availability is stored against are lined up the
	// same way. Each spec carries the Shift's own Shape and Closed: the solver
	// is told what each day asks for and which days the drop-in does not run,
	// rather than working either out (#132, #137).
	shiftSpecs := make([]allocator.ShiftSpec, len(shiftDates))
	orderedShiftIDs := make([]string, len(shiftDates))
	for i, date := range shiftDates {
		dateStr := date.Format("2006-01-02")
		shiftID := shiftIDByDate[dateStr]
		shiftSpecs[i] = allocator.ShiftSpec{
			Date:   dateStr,
			Shape:  convertShape(shapes[shiftID]),
			Closed: closedByDate[dateStr],
		}
		orderedShiftIDs[i] = shiftID
	}

	groupAvailability, err := fetchGroupAvailability(
		ctx,
		database,
		targetRota.ID,
		allocatorVolunteers,
		orderedShiftIDs,
		logger,
	)
	if err != nil {
		return nil, err
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

	// Preallocations (issue #39): each pin becomes a synthetic exact-date
	// override, so InitShifts applies them with no new merge logic. The
	// `preallocation` table is the whole set — pins an admin made by hand and
	// pins a Standing Preallocation seeded when the rota was defined are the
	// same rows (issue #131).
	pins, err := database.GetPreallocationsByShiftIDs(ctx, shiftIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch preallocations: %w", err)
	}
	activeIDs := make(map[string]bool, len(activeVolunteers))
	for _, v := range activeVolunteers {
		activeIDs[v.ID] = true
	}
	// Pre-solve stale-pin check: fail loudly, naming the pin, rather than letting
	// an inactive/deleted preallocated volunteer surface as the solver's opaque
	// ProblemError.
	if err := checkPreallocationsResolve(pins, shifts, activeIDs); err != nil {
		return nil, err
	}
	allocatorOverrides, err := buildPreallocationOverrides(pins, dateByShiftID, roles)
	if err != nil {
		return nil, err
	}

	// Build the solver input and run the Python subprocess.
	input, err := allocator.BuildCpsatInput(
		allocatorVolunteers,
		groupAvailability,
		shiftSpecs,
		allocatorOverrides,
		historicalShifts,
		settings.AllocationSettings,
		convertRoles(roles),
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

	logger.Info("CP-SAT solve completed",
		zap.String("solver_status", output.SolverStatus),
		zap.Bool("success", output.Success),
		zap.Int("objective_value", output.ObjectiveValue),
		zap.Float64("solve_time_seconds", output.Diagnostics.SolveTimeSeconds))

	solvedShifts, err := allocator.CpsatOutputToShifts(output, allocatorVolunteers)
	if err != nil {
		return nil, fmt.Errorf("failed to convert cpsat output: %w", err)
	}

	volunteersByID := make(map[string]model.Volunteer, len(allVolunteers))
	for _, v := range allVolunteers {
		volunteersByID[v.ID] = v
	}

	return &rotaSolve{
		rota:           targetRota,
		shifts:         shifts,
		shiftDates:     shiftDates,
		shiftIDByDate:  shiftIDByDate,
		shapes:         shapes,
		output:         output,
		solvedShifts:   solvedShifts,
		roles:          roles,
		volunteersByID: volunteersByID,
	}, nil
}

// seatsAsked is how many Seats the solve was asked to fill: every Seat of every
// open Shift's Shape. A closed Shift asks for nobody however it is shaped, so it
// counts for nothing.
//
// It is the denominator of "four Seats unfilled" — the thing an admin most wants
// from a draft — and it is derived rather than stored, since the Shapes it comes
// from are right there on the Shifts.
func (s *rotaSolve) seatsAsked() int {
	total := 0
	for _, shift := range s.shifts {
		if shift.Closed {
			continue
		}
		for _, seat := range s.shapes[shift.ID] {
			total += seat.Count
		}
	}
	return total
}

// seatsFilled is how many of those Seats the solve actually put somebody in.
func (s *rotaSolve) seatsFilled() int {
	filled := 0
	for _, shift := range s.solvedShifts {
		filled += len(shift.Assignments)
	}
	return filled
}
