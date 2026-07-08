package db

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// GetAllocationsInRange retrieves allocation records with shift_date between
// from and to (inclusive). A zero time leaves that bound open.
func (d *DB) GetAllocationsInRange(ctx context.Context, from, to time.Time) ([]Allocation, error) {
	where, args := shiftDateWhere(from, to)
	rows, err := d.pool.Query(ctx, `
		SELECT id, rota_id, shift_date, role, volunteer_id, custom_entry
		FROM allocation
	`+where, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query allocations: %w", err)
	}
	return scanAllocations(rows)
}

// shiftDateWhere builds a WHERE clause bounding shift_date, with zero times
// leaving the corresponding bound open
func shiftDateWhere(from, to time.Time) (string, []any) {
	var conds []string
	var args []any
	if !from.IsZero() {
		args = append(args, from)
		conds = append(conds, fmt.Sprintf("shift_date >= $%d", len(args)))
	}
	if !to.IsZero() {
		args = append(args, to)
		conds = append(conds, fmt.Sprintf("shift_date <= $%d", len(args)))
	}
	if len(conds) == 0 {
		return "", nil
	}
	return "WHERE " + strings.Join(conds, " AND "), args
}

// GetAllocationsByRotaID retrieves the allocation records for a single rota
func (d *DB) GetAllocationsByRotaID(ctx context.Context, rotaID string) ([]Allocation, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, rota_id, shift_date, role, volunteer_id, custom_entry
		FROM allocation
		WHERE rota_id = $1
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

	for _, a := range allocations {
		var volunteerID, customEntry *string
		if a.VolunteerID != "" {
			volunteerID = &a.VolunteerID
		}
		if a.CustomEntry != "" {
			customEntry = &a.CustomEntry
		}

		_, err := tx.Exec(ctx, `
			INSERT INTO allocation (id, rota_id, shift_date, role, volunteer_id, custom_entry)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, a.ID, a.RotaID, a.ShiftDate, a.Role, volunteerID, customEntry)
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
