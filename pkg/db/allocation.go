package db

import (
	"context"
	"fmt"
	"time"
)

// GetAllocations retrieves all allocation records
func (d *DB) GetAllocations(ctx context.Context) ([]Allocation, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, rota_id, shift_date, role, volunteer_id, custom_entry
		FROM allocation
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query allocations: %w", err)
	}
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
