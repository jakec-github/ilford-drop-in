package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// GetAllocationsByShiftIDs retrieves the allocation records belonging to the
// given shifts. Callers that have already resolved a set of shifts (e.g.
// ListShifts) scope allocations by that set rather than re-deriving a date
// window, so the two can never disagree. Each record carries only its shift_id;
// rota and date live on the shift (ADR 0001). An empty id set returns no rows
// without a query.
func (d *DB) GetAllocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]Allocation, error) {
	return getAllocationsByShiftIDs(ctx, d.pool, shiftIDs)
}

func getAllocationsByShiftIDs(ctx context.Context, q querier, shiftIDs []string) ([]Allocation, error) {
	if len(shiftIDs) == 0 {
		return nil, nil
	}
	rows, err := q.Query(ctx, `
		SELECT id, shift_id, role, volunteer_id, custom_entry
		FROM allocation
		WHERE shift_id = ANY($1)
	`, shiftIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to query allocations by shift: %w", err)
	}
	return scanAllocations(rows)
}

func scanAllocations(rows pgx.Rows) ([]Allocation, error) {
	defer rows.Close()

	var allocations []Allocation
	for rows.Next() {
		var a Allocation
		var volunteerID, customEntry *string
		if err := rows.Scan(&a.ID, &a.ShiftID, &a.Role, &volunteerID, &customEntry); err != nil {
			return nil, fmt.Errorf("failed to scan allocation: %w", err)
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
		return nil, fmt.Errorf("error iterating allocations: %w", err)
	}

	return allocations, nil
}

// InsertAllocationsAndSetAllocated inserts allocation records, clears the
// rota's Draft Rota Allocation and marks the rotation as allocated, in a single
// transaction.
//
// The draft goes with the rest because allocating is what consumes it: the
// speculative rota has become the rota, and a draft left beside an allocation
// would be a second answer for a rota nobody can draft again — ReplaceDraftRotaAllocation
// refuses an allocated one (ADR 0008).
func (d *DB) InsertAllocationsAndSetAllocated(ctx context.Context, allocations []Allocation, rotaID string, datetime time.Time) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Double-allocation guard (issue #8). Lock the rotation row and read its
	// allocated_datetime in one shot; a non-NULL value marks an allocation that
	// already completed, so refuse. The FOR UPDATE lock makes this
	// check-then-write atomic under READ COMMITTED: a racing run blocks on the
	// lock until this transaction commits, then its own read observes the
	// allocated_datetime this run set and fails. On failure the deferred
	// Rollback writes nothing.
	var allocatedAt *time.Time
	if err := tx.QueryRow(ctx, `SELECT allocated_datetime FROM rotation WHERE id = $1 FOR UPDATE`, rotaID).Scan(&allocatedAt); err != nil {
		return fmt.Errorf("failed to lock rotation %s: %w", rotaID, err)
	}
	if allocatedAt != nil {
		return fmt.Errorf("rota %s is already allocated (at %s) - refusing to allocate again", rotaID, allocatedAt.UTC().Format(time.RFC3339))
	}

	for _, a := range allocations {
		var volunteerID, customEntry *string
		if a.VolunteerID != "" {
			volunteerID = &a.VolunteerID
		}
		if a.CustomEntry != "" {
			customEntry = &a.CustomEntry
		}

		// The allocation references its shift directly; the shift is the sole
		// authority on rota and date (ADR 0001). An unknown ShiftID trips the
		// shift_id FK constraint and fails loudly.
		_, err := tx.Exec(ctx, `
			INSERT INTO allocation (id, role, volunteer_id, custom_entry, shift_id)
			VALUES ($1, $2, $3, $4, $5)
		`, a.ID, a.Role, volunteerID, customEntry, a.ShiftID)
		if err != nil {
			return fmt.Errorf("failed to insert allocation: %w", err)
		}
	}

	// The draft's Seats go by way of their shifts, which is the only route
	// there is: a draft Seat names its shift and nothing else (ADR 0001).
	if _, err := tx.Exec(ctx, `
		DELETE FROM draft_allocation
		WHERE shift_id IN (SELECT id FROM shift WHERE rota_id = $1)
	`, rotaID); err != nil {
		return fmt.Errorf("failed to clear the draft's seats: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM draft_rota_allocation WHERE rota_id = $1`, rotaID); err != nil {
		return fmt.Errorf("failed to clear the draft: %w", err)
	}

	_, err = tx.Exec(ctx, `
		UPDATE rotation SET allocated_datetime = $2 WHERE id = $1
	`, rotaID, datetime.UTC())
	if err != nil {
		return fmt.Errorf("failed to set rotation allocated_datetime: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
