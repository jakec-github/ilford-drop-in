package services

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
)

// AllocateRotaOutcome is what came of an attempt to allocate the rota in
// flight: either it was committed, or it had moved since the admin looked and
// the fresh solve is what they are now being shown.
//
// Not an error in the second case. "The rota changed" is a well-formed answer
// to "allocate the one I was shown" — the admin does the same thing next
// either way, which is read the rota in front of them — and the caller needs
// the fresh rota to show, which an error could not carry.
type AllocateRotaOutcome struct {
	// Allocated says whether anything was committed. False means nothing was:
	// no allocation rows, no stamp on the Rotation.
	Allocated bool
	// AllocatedAt is when, and is the stamp on the Rotation itself. Zero when
	// nothing was allocated.
	AllocatedAt time.Time
	// Solve is the rota this attempt solved, in the shape a draft is read in.
	// When it was allocated, this is the rota that was committed; when it was
	// not, this is the draft that replaced the one confirmed — the rota to
	// read, and to confirm instead.
	Solve *DraftRotaAllocationStatus
}

// AllocateRotaInFlight allocates the rota in flight — but only the one the
// admin was shown.
//
// It re-solves, hashes the answer, and commits it only if that hash is the one
// the admin confirmed (ADR 0008). The solver is deterministic, so an identical
// hash means nothing that could change the rota has moved since they looked at
// it. A different one means it has: nothing is committed, the fresh solve
// becomes the draft, and the admin reads that one and confirms it instead.
//
// The comparison is against the hash the caller states rather than against the
// stored draft's, and the difference matters exactly once: when another admin's
// read re-solved the draft while this admin had the rota open. Comparing
// against the store would then commit a rota nobody had looked at, which is the
// one outcome this whole mechanism exists to prevent.
//
// It writes through InsertAllocationsAndSetAllocated, so the allocation rows,
// the Rotation's stamp and the draft's removal are one transaction under the
// row lock that makes double allocation impossible (issue #8). Every check
// here is a fast refusal in front of that lock, never a substitute for it: two
// admins can perfectly well confirm the same draft at the same moment, and the
// store is what settles it.
func AllocateRotaInFlight(
	ctx context.Context,
	database AllocateRotaStore,
	volunteerClient VolunteerClient,
	cfg *config.Config,
	logger *zap.Logger,
	confirmedHash string,
	pythonFlag string,
) (*AllocateRotaOutcome, error) {
	if confirmedHash == "" {
		return nil, wrapf(ErrInvalidInput, "allocating states the draft being confirmed - reload the rota and allocate it again")
	}

	// Both of these are gates in front of a thirty-second solve, so they are
	// read first and cheaply. Neither is the authority on anything: the solve
	// refuses an allocated rota again, and the store refuses it again after
	// that.
	rota, err := database.GetRotaInFlight(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read the rota in flight: %w", err)
	}
	if rota == nil {
		// Said as the state rather than as the cause, because there are two
		// causes and this cannot tell them apart: the rota was allocated while
		// the page was open, or it was discarded.
		return nil, wrapf(ErrConflict, "there is no rota in flight - it may have been allocated or discarded already")
	}
	draft, err := database.GetDraftRotaAllocation(ctx, rota.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to read the draft rota allocation for rota %s: %w", rota.ID, err)
	}
	if draft == nil {
		return nil, wrapf(ErrConflict, "rota %s has not been drafted yet - solve a draft and read it before allocating", rota.ID)
	}

	solve, err := solveRotaInFlight(ctx, database, volunteerClient, cfg, logger, pythonFlag)
	if err != nil {
		return nil, err
	}

	allocations, err := convertToDBAllocations(solve.shiftIDByDate, solve.solvedShifts)
	if err != nil {
		return nil, fmt.Errorf("failed to convert allocations: %w", err)
	}

	// An infeasible solve is a well-formed answer and a fine thing to draft,
	// but it is not a rota, so there is nothing here to commit. It is stored so
	// that the screen refusing the allocation is showing the reason.
	if !solve.output.Success {
		if _, err := storeSolveAsDraft(ctx, database, solve, allocations, time.Now().UTC(), logger); err != nil {
			return nil, err
		}
		return nil, wrapf(ErrConflict,
			"no rota is possible from the availability, pins and shapes as they stand (the solver said %s) - nothing has been allocated",
			solve.output.SolverStatus)
	}

	solvedHash := hashAllocations(allocations)
	if solvedHash != confirmedHash {
		logger.Info("The rota moved under the draft being allocated - committing nothing",
			zap.String("rota_id", solve.rota.ID),
			zap.String("confirmed", confirmedHash),
			zap.String("solved", solvedHash))
		fresh, err := storeSolveAsDraft(ctx, database, solve, allocations, time.Now().UTC(), logger)
		if err != nil {
			return nil, err
		}
		return &AllocateRotaOutcome{Allocated: false, Solve: fresh}, nil
	}

	allocatedAt := time.Now().UTC()
	if err := database.InsertAllocationsAndSetAllocated(ctx, allocations, solve.rota.ID, allocatedAt); err != nil {
		return nil, fmt.Errorf("failed to save allocations: %w", err)
	}
	logger.Info("Allocated the rota in flight",
		zap.String("rota_id", solve.rota.ID),
		zap.Int("seats_filled", len(allocations)))

	return &AllocateRotaOutcome{
		Allocated:   true,
		AllocatedAt: allocatedAt,
		// The rota as committed, reported in the shape it was read in as a
		// draft — because that is what it was until this call.
		Solve: solve.status(allocatedAt, allocations, logger),
	}, nil
}
