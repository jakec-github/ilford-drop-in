package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// GetAlterationsByShiftIDs retrieves the alteration records belonging to the
// given shifts. Like GetAllocationsByShiftIDs, it scopes by the shift set the
// caller already holds rather than a second date-range scan (ADR 0001). Each
// record carries only its shift_id; rota and date live on the shift. An empty
// id set returns no rows without a query.
func (d *DB) GetAlterationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]Alteration, error) {
	return getAlterationsByShiftIDs(ctx, d.pool, shiftIDs)
}

func getAlterationsByShiftIDs(ctx context.Context, q querier, shiftIDs []string) ([]Alteration, error) {
	if len(shiftIDs) == 0 {
		return nil, nil
	}
	rows, err := q.Query(ctx, `
		SELECT id, shift_id, direction, volunteer_id, custom_value, cover_id, set_time, role
		FROM alteration
		WHERE shift_id = ANY($1)
		ORDER BY set_time ASC
	`, shiftIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to query alterations by shift: %w", err)
	}
	return scanAlterations(rows)
}

func scanAlterations(rows pgx.Rows) ([]Alteration, error) {
	defer rows.Close()

	var alterations []Alteration
	for rows.Next() {
		var a Alteration
		var setTime time.Time
		var volunteerID, customValue, role *string
		if err := rows.Scan(&a.ID, &a.ShiftID, &a.Direction, &volunteerID, &customValue, &a.CoverID, &setTime, &role); err != nil {
			return nil, fmt.Errorf("failed to scan alteration: %w", err)
		}
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
