package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// GetAllocationsInRange retrieves allocation records whose shift falls between
// from and to (inclusive). A zero time leaves that bound open. The date is
// hydrated from the joined shift, not the legacy shift_date column (ADR 0001).
func (d *DB) GetAllocationsInRange(ctx context.Context, from, to time.Time) ([]Allocation, error) {
	return getAllocationsInRange(ctx, d.pool, from, to)
}

func getAllocationsInRange(ctx context.Context, q querier, from, to time.Time) ([]Allocation, error) {
	where, args := shiftDateWhere(from, to)
	rows, err := q.Query(ctx, `
		SELECT a.id, s.rota_id, s.date, a.role, a.volunteer_id, a.custom_entry
		FROM allocation a
		JOIN shift s ON s.id = a.shift_id
	`+where, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query allocations: %w", err)
	}
	return scanAllocations(rows)
}

// shiftDateWhere builds a WHERE clause bounding the joined shift's date (aliased
// s), with zero times leaving the corresponding bound open
func shiftDateWhere(from, to time.Time) (string, []any) {
	var conds []string
	var args []any
	if !from.IsZero() {
		args = append(args, from)
		conds = append(conds, fmt.Sprintf("s.date >= $%d", len(args)))
	}
	if !to.IsZero() {
		args = append(args, to)
		conds = append(conds, fmt.Sprintf("s.date <= $%d", len(args)))
	}
	if len(conds) == 0 {
		return "", nil
	}
	return "WHERE " + strings.Join(conds, " AND "), args
}

// GetAllocationsByShiftIDs retrieves the allocation records belonging to the
// given shifts. Callers that have already resolved a set of shifts (e.g.
// ListShifts) scope allocations by that set rather than re-deriving a date
// window, so the two can never disagree. The rota and date are hydrated from the
// joined shift (ADR 0001); an empty id set returns no rows without a query.
func (d *DB) GetAllocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]Allocation, error) {
	if len(shiftIDs) == 0 {
		return nil, nil
	}
	rows, err := d.pool.Query(ctx, `
		SELECT a.id, s.rota_id, s.date, a.role, a.volunteer_id, a.custom_entry
		FROM allocation a
		JOIN shift s ON s.id = a.shift_id
		WHERE a.shift_id = ANY($1)
	`, shiftIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to query allocations by shift: %w", err)
	}
	return scanAllocations(rows)
}

// GetAllocationsByRotaID retrieves the allocation records for a single rota.
// The rota and date are read from the joined shift, not legacy columns (ADR 0001).
func (d *DB) GetAllocationsByRotaID(ctx context.Context, rotaID string) ([]Allocation, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT a.id, s.rota_id, s.date, a.role, a.volunteer_id, a.custom_entry
		FROM allocation a
		JOIN shift s ON s.id = a.shift_id
		WHERE s.rota_id = $1
	`, rotaID)
	if err != nil {
		return nil, fmt.Errorf("failed to query allocations for rota %s: %w", rotaID, err)
	}
	return scanAllocations(rows)
}

func scanAllocations(rows pgx.Rows) ([]Allocation, error) {
	defer rows.Close()

	var allocations []Allocation
	for rows.Next() {
		var a Allocation
		var shiftDate time.Time
		var volunteerID, customEntry *string
		if err := rows.Scan(&a.ID, &a.RotaID, &shiftDate, &a.Role, &volunteerID, &customEntry); err != nil {
			return nil, fmt.Errorf("failed to scan allocation: %w", err)
		}
		a.ShiftDate = shiftDate.Format("2006-01-02")
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

// InsertAllocationsAndSetAllocated inserts allocation records and marks the
// rotation as allocated in a single transaction.
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

		// Resolve the minted shift for this rota and date and store only its
		// reference; the shift is the sole authority on rota and date (ADR 0001).
		// A missing shift trips the NOT NULL constraint and fails loudly.
		_, err := tx.Exec(ctx, `
			INSERT INTO allocation (id, role, volunteer_id, custom_entry, shift_id)
			VALUES ($1, $2, $3, $4,
				(SELECT id FROM shift WHERE rota_id = $5 AND date = $6))
		`, a.ID, a.Role, volunteerID, customEntry, a.RotaID, a.ShiftDate)
		if err != nil {
			return fmt.Errorf("failed to insert allocation: %w", err)
		}
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
