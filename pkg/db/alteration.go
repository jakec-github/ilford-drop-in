package db

import (
	"context"
	"fmt"
	"time"
)

// GetAlterations retrieves all alteration records
func (d *DB) GetAlterations(ctx context.Context) ([]Alteration, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, shift_date, rota_id, direction, volunteer_id, custom_value, cover_id, set_time, role
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
		var volunteerID, customValue, role *string
		if err := rows.Scan(&a.ID, &shiftDate, &a.RotaID, &a.Direction, &volunteerID, &customValue, &a.CoverID, &setTime, &role); err != nil {
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
		if role != nil {
			a.Role = *role
		}
		alterations = append(alterations, a)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating alterations: %w", err)
	}

	return alterations, nil
}
