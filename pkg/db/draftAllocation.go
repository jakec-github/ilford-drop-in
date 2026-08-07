package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// GetDraftRotaAllocation reads a Rotation's Draft Rota Allocation: the outcome
// of the solve that produced it, without its Seats — GetDraftAllocationsByShiftIDs
// reads those, scoped the way a caller already holds its shifts.
//
// A nil draft with no error is a Rotation nobody has solved for yet, which is
// where every rota starts and is not a failure.
func (d *DB) GetDraftRotaAllocation(ctx context.Context, rotaID string) (*DraftRotaAllocation, error) {
	var draft DraftRotaAllocation
	var objectiveValue int64
	err := d.pool.QueryRow(ctx, `
		SELECT rota_id, solved_at, success, solver_status, objective_value, diagnostics
		FROM draft_rota_allocation
		WHERE rota_id = $1
	`, rotaID).Scan(
		&draft.RotaID,
		&draft.SolvedAt,
		&draft.Success,
		&draft.SolverStatus,
		&objectiveValue,
		&draft.Diagnostics,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query draft rota allocation for rota %s: %w", rotaID, err)
	}
	draft.ObjectiveValue = int(objectiveValue)
	return &draft, nil
}

// GetDraftAllocationsByShiftIDs retrieves the draft Seats belonging to the given
// shifts, mirroring GetAllocationsByShiftIDs: the caller has already resolved
// the shifts it cares about, so the two can never disagree about which they
// mean. An empty id set returns no rows without a query.
//
// Every caller of this is admin-gated, and must stay so. The whole reason
// drafts are a table of their own is that no public reader can reach one by
// forgetting a join (ADR 0008).
func (d *DB) GetDraftAllocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]DraftAllocation, error) {
	if len(shiftIDs) == 0 {
		return nil, nil
	}
	rows, err := d.pool.Query(ctx, `
		SELECT id, shift_id, role, volunteer_id, custom_entry
		FROM draft_allocation
		WHERE shift_id = ANY($1)
	`, shiftIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to query draft allocations by shift: %w", err)
	}
	defer rows.Close()

	var allocations []DraftAllocation
	for rows.Next() {
		var a DraftAllocation
		var volunteerID, customEntry *string
		if err := rows.Scan(&a.ID, &a.ShiftID, &a.Role, &volunteerID, &customEntry); err != nil {
			return nil, fmt.Errorf("failed to scan draft allocation: %w", err)
		}
		if volunteerID != nil {
			a.VolunteerID = *volunteerID
		}
		if customEntry != nil {
			a.CustomEntry = *customEntry
		}
		allocations = append(allocations, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating draft allocations: %w", err)
	}

	return allocations, nil
}

// ReplaceDraftRotaAllocation stores a solve as the Rotation's Draft Rota
// Allocation, replacing whatever draft was there.
//
// Whole rather than incremental, because that is what a draft is: it is solved
// entire from the inputs of the moment, and a Seat the previous solve placed is
// not evidence of anything once the next one has run (ADR 0008). Passing no
// seats stores an empty draft, which is what an infeasible solve produces — and
// reads differently from no draft at all, because the outcome row says why.
//
// The draft and its Seats share one transaction, so the rows and the outcome
// always describe the same solve.
func (d *DB) ReplaceDraftRotaAllocation(ctx context.Context, draft DraftRotaAllocation, seats []DraftAllocation) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// A draft speaks for a rota that has not been allocated; beside an
	// allocation it could only contradict it. This is the same FOR UPDATE guard
	// InsertAllocationsAndSetAllocated takes (issue #8), for the same reason: a
	// solve takes tens of seconds, so a rota can perfectly well be allocated
	// while one is running, and the lock makes this check-then-write atomic
	// against that. The loser blocks until the allocation commits, then reads
	// the stamp it set and refuses.
	var allocatedAt *time.Time
	if err := tx.QueryRow(ctx, `SELECT allocated_datetime FROM rotation WHERE id = $1 FOR UPDATE`, draft.RotaID).Scan(&allocatedAt); err != nil {
		return fmt.Errorf("failed to lock rotation %s: %w", draft.RotaID, err)
	}
	if allocatedAt != nil {
		return fmt.Errorf("rota %s is already allocated (at %s) - refusing to store a draft for it", draft.RotaID, allocatedAt.UTC().Format(time.RFC3339))
	}

	// The Seats go by way of their shifts, which is the only route there is: a
	// draft Seat names its shift and nothing else, and the shift is the sole
	// authority on which rota it belongs to (ADR 0001).
	if _, err := tx.Exec(ctx, `
		DELETE FROM draft_allocation
		WHERE shift_id IN (SELECT id FROM shift WHERE rota_id = $1)
	`, draft.RotaID); err != nil {
		return fmt.Errorf("failed to clear the previous draft's seats: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM draft_rota_allocation WHERE rota_id = $1`, draft.RotaID); err != nil {
		return fmt.Errorf("failed to clear the previous draft: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO draft_rota_allocation (rota_id, solved_at, success, solver_status, objective_value, diagnostics)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, draft.RotaID, draft.SolvedAt.UTC(), draft.Success, draft.SolverStatus, int64(draft.ObjectiveValue), draft.Diagnostics); err != nil {
		return fmt.Errorf("failed to write the draft for rota %s: %w", draft.RotaID, err)
	}

	for _, seat := range seats {
		var volunteerID, customEntry *string
		if seat.VolunteerID != "" {
			volunteerID = &seat.VolunteerID
		}
		if seat.CustomEntry != "" {
			customEntry = &seat.CustomEntry
		}
		// A Seat naming a shift outside this rota trips no constraint here —
		// the shift_id FK only says the shift exists. It cannot happen: the
		// solve reads the rota's own shifts and writes back against them.
		if _, err := tx.Exec(ctx, `
			INSERT INTO draft_allocation (id, shift_id, role, volunteer_id, custom_entry)
			VALUES ($1, $2, $3, $4, $5)
		`, seat.ID, seat.ShiftID, seat.Role, volunteerID, customEntry); err != nil {
			return fmt.Errorf("failed to write a draft seat: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit the draft: %w", err)
	}
	return nil
}
