package db

import (
	"context"
	"fmt"
	"time"
)

// InsertAlterations inserts alteration records into the database
func (d *DB) InsertAlterations(ctx context.Context, alterations []Alteration) error {
	if len(alterations) == 0 {
		return nil
	}

	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	for _, a := range alterations {
		var volunteerID, customValue *string
		if a.VolunteerID != "" {
			volunteerID = &a.VolunteerID
		}
		if a.CustomValue != "" {
			customValue = &a.CustomValue
		}

		_, err := tx.Exec(ctx, `
			INSERT INTO alteration (id, shift_date, rota_id, direction, volunteer_id, custom_value, cover_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, a.ID, a.ShiftDate, a.RotaID, a.Direction, volunteerID, customValue, a.CoverID)
		if err != nil {
			return fmt.Errorf("failed to insert alteration: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// GetAlterations retrieves all alteration records
func (d *DB) GetAlterations(ctx context.Context) ([]Alteration, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, shift_date, rota_id, direction, volunteer_id, custom_value, cover_id, set_time
		FROM alteration
		ORDER BY set_time ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query alterations: %w", err)
	}
	defer rows.Close()

	var alterations []Alteration
	for rows.Next() {
		var a Alteration
		var shiftDate time.Time
		var setTime time.Time
		var volunteerID, customValue *string
		if err := rows.Scan(&a.ID, &shiftDate, &a.RotaID, &a.Direction, &volunteerID, &customValue, &a.CoverID, &setTime); err != nil {
			return nil, fmt.Errorf("failed to scan alteration: %w", err)
		}
		a.ShiftDate = shiftDate.Format("2006-01-02")
		a.SetTime = setTime.UTC().Format(time.RFC3339)
		if volunteerID != nil {
			a.VolunteerID = *volunteerID
		}
		if customValue != nil {
			a.CustomValue = *customValue
		}
		alterations = append(alterations, a)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating alterations: %w", err)
	}

	return alterations, nil
}
