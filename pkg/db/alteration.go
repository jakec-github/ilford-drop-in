package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// GetAlterationsInRange retrieves alteration records with shift_date between
// from and to (inclusive). A zero time leaves that bound open.
func (d *DB) GetAlterationsInRange(ctx context.Context, from, to time.Time) ([]Alteration, error) {
	where, args := shiftDateWhere(from, to)
	rows, err := d.pool.Query(ctx, `
		SELECT id, shift_date, rota_id, direction, volunteer_id, custom_value, cover_id, set_time, role
		FROM alteration
		`+where+`
		ORDER BY set_time ASC
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query alterations: %w", err)
	}
	return scanAlterations(rows)
}

// GetAlterationsByRotaID retrieves the alteration records for a single rota
func (d *DB) GetAlterationsByRotaID(ctx context.Context, rotaID string) ([]Alteration, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, shift_date, rota_id, direction, volunteer_id, custom_value, cover_id, set_time, role
		FROM alteration
		WHERE rota_id = $1
		ORDER BY set_time ASC
	`, rotaID)
	if err != nil {
		return nil, fmt.Errorf("failed to query alterations for rota %s: %w", rotaID, err)
	}
	return scanAlterations(rows)
}

func scanAlterations(rows pgx.Rows) ([]Alteration, error) {
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
