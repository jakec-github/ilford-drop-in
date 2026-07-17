package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// GetManualPreallocationsByShiftIDs retrieves the manual preallocation records
// belonging to the given shifts. Like GetAllocationsByShiftIDs it scopes by the
// shift set the caller already holds rather than a re-derived date window (ADR
// 0001); each record carries only its shift_id, with rota and date living on the
// shift. An empty id set returns no rows without a query.
func (d *DB) GetManualPreallocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]ManualPreallocation, error) {
	return getManualPreallocationsByShiftIDs(ctx, d.pool, shiftIDs)
}

func getManualPreallocationsByShiftIDs(ctx context.Context, q querier, shiftIDs []string) ([]ManualPreallocation, error) {
	if len(shiftIDs) == 0 {
		return nil, nil
	}
	rows, err := q.Query(ctx, `
		SELECT id, shift_id, role, volunteer_id, custom_value
		FROM manual_preallocation
		WHERE shift_id = ANY($1)
	`, shiftIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to query manual preallocations by shift: %w", err)
	}
	return scanManualPreallocations(rows)
}

func scanManualPreallocations(rows pgx.Rows) ([]ManualPreallocation, error) {
	defer rows.Close()

	var preallocations []ManualPreallocation
	for rows.Next() {
		var mp ManualPreallocation
		var volunteerID, customValue *string
		if err := rows.Scan(&mp.ID, &mp.ShiftID, &mp.Role, &volunteerID, &customValue); err != nil {
			return nil, fmt.Errorf("failed to scan manual preallocation: %w", err)
		}
		if volunteerID != nil {
			mp.VolunteerID = *volunteerID
		}
		if customValue != nil {
			mp.CustomValue = *customValue
		}
		preallocations = append(preallocations, mp)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating manual preallocations: %w", err)
	}

	return preallocations, nil
}

// GetManualPreallocationByID retrieves a single manual preallocation together
// with its shift, or (nil, nil, nil) if no row matches. A DELETE resolves the
// pin to its shift's rota before locking, so the shift (carrying rota_id) is
// returned alongside the pin in one join rather than a second round trip.
func (d *DB) GetManualPreallocationByID(ctx context.Context, id string) (*ManualPreallocation, *Shift, error) {
	var mp ManualPreallocation
	var s Shift
	var volunteerID, customValue *string
	var date time.Time
	err := d.pool.QueryRow(ctx, `
		SELECT mp.id, mp.shift_id, mp.role, mp.volunteer_id, mp.custom_value,
		       s.date, s.rota_id
		FROM manual_preallocation mp
		JOIN shift s ON s.id = mp.shift_id
		WHERE mp.id = $1
	`, id).Scan(&mp.ID, &mp.ShiftID, &mp.Role, &volunteerID, &customValue, &date, &s.RotaID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query manual preallocation %s: %w", id, err)
	}
	if volunteerID != nil {
		mp.VolunteerID = *volunteerID
	}
	if customValue != nil {
		mp.CustomValue = *customValue
	}
	s.ID = mp.ShiftID
	s.Date = date.Format("2006-01-02")
	return &mp, &s, nil
}

// insertManualPreallocation writes a single manual preallocation row. The
// nullable volunteer_id / custom_value follow the allocation pattern: an empty
// string is stored as NULL. An unknown shift_id trips the FK and fails loudly.
func insertManualPreallocation(ctx context.Context, q querier, mp ManualPreallocation) error {
	var volunteerID, customValue *string
	if mp.VolunteerID != "" {
		volunteerID = &mp.VolunteerID
	}
	if mp.CustomValue != "" {
		customValue = &mp.CustomValue
	}
	_, err := q.Exec(ctx, `
		INSERT INTO manual_preallocation (id, shift_id, role, volunteer_id, custom_value)
		VALUES ($1, $2, $3, $4, $5)
	`, mp.ID, mp.ShiftID, mp.Role, volunteerID, customValue)
	if err != nil {
		return fmt.Errorf("failed to insert manual preallocation: %w", err)
	}
	return nil
}

// deleteManualPreallocationByID removes the row with the given id, reporting
// whether a row was actually deleted (false lets a caller distinguish a
// concurrent delete from success).
func deleteManualPreallocationByID(ctx context.Context, q querier, id string) (bool, error) {
	tag, err := q.Exec(ctx, `DELETE FROM manual_preallocation WHERE id = $1`, id)
	if err != nil {
		return false, fmt.Errorf("failed to delete manual preallocation %s: %w", id, err)
	}
	return tag.RowsAffected() > 0, nil
}

// rotaAllocated reports whether the rotation has been allocated (its
// allocated_datetime is set). Callers run this against a rotation row already
// locked FOR UPDATE so the answer cannot change under them.
func rotaAllocated(ctx context.Context, q querier, rotaID string) (bool, error) {
	var allocated bool
	err := q.QueryRow(ctx, `
		SELECT allocated_datetime IS NOT NULL FROM rotation WHERE id = $1
	`, rotaID).Scan(&allocated)
	if err != nil {
		return false, fmt.Errorf("failed to read allocation state for rotation %s: %w", rotaID, err)
	}
	return allocated, nil
}
