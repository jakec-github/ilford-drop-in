package postgres

import (
	"context"
	"fmt"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// GetAllocations retrieves all allocation records
func (d *DB) GetAllocations(ctx context.Context) ([]db.Allocation, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, rota_id, shift_date, role, volunteer_id, custom_entry
		FROM allocation
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query allocations: %w", err)
	}
	defer rows.Close()

	var allocations []db.Allocation
	for rows.Next() {
		var a db.Allocation
		var volunteerID, customEntry *string
		if err := rows.Scan(&a.ID, &a.RotaID, &a.ShiftDate, &a.Role, &volunteerID, &customEntry); err != nil {
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

// InsertAllocations inserts allocation records into the database
func (d *DB) InsertAllocations(allocations []db.Allocation) error {
	if len(allocations) == 0 {
		return nil
	}

	ctx := context.Background()
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

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
