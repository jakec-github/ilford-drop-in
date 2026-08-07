package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// GetPreallocationsByShiftIDs retrieves the Preallocation records belonging to
// the given shifts. Like GetAllocationsByShiftIDs it scopes by the
// shift set the caller already holds rather than a re-derived date window (ADR
// 0001); each record carries only its shift_id, with rota and date living on the
// shift. An empty id set returns no rows without a query.
func (d *DB) GetPreallocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]Preallocation, error) {
	return getPreallocationsByShiftIDs(ctx, d.pool, shiftIDs)
}

func getPreallocationsByShiftIDs(ctx context.Context, q querier, shiftIDs []string) ([]Preallocation, error) {
	if len(shiftIDs) == 0 {
		return nil, nil
	}
	rows, err := q.Query(ctx, `
		SELECT id, shift_id, role, volunteer_id, custom_value
		FROM preallocation
		WHERE shift_id = ANY($1)
	`, shiftIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to query preallocations by shift: %w", err)
	}
	return scanPreallocations(rows)
}

func scanPreallocations(rows pgx.Rows) ([]Preallocation, error) {
	defer rows.Close()

	var preallocations []Preallocation
	for rows.Next() {
		var mp Preallocation
		var volunteerID, customValue *string
		if err := rows.Scan(&mp.ID, &mp.ShiftID, &mp.Role, &volunteerID, &customValue); err != nil {
			return nil, fmt.Errorf("failed to scan preallocation: %w", err)
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
		return nil, fmt.Errorf("error iterating preallocations: %w", err)
	}

	return preallocations, nil
}

// GetPreallocationByID retrieves a single preallocation together with its
// shift, or (nil, nil, nil) if no row matches. A DELETE resolves the
// pin to its shift's rota before locking, so the shift (carrying rota_id) is
// returned alongside the pin in one join rather than a second round trip.
func (d *DB) GetPreallocationByID(ctx context.Context, id string) (*Preallocation, *Shift, error) {
	var mp Preallocation
	var s Shift
	var volunteerID, customValue *string
	var date time.Time
	err := d.pool.QueryRow(ctx, `
		SELECT mp.id, mp.shift_id, mp.role, mp.volunteer_id, mp.custom_value,
		       `+shiftDateExpr+`, s.rota_id
		FROM preallocation mp
		JOIN shift s ON s.id = mp.shift_id
		WHERE mp.id = $1
	`, id).Scan(&mp.ID, &mp.ShiftID, &mp.Role, &volunteerID, &customValue, &date, &s.RotaID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query preallocation %s: %w", id, err)
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

// insertPreallocation writes a single preallocation row. The
// nullable volunteer_id / custom_value follow the allocation pattern: an empty
// string is stored as NULL. An unknown shift_id trips the FK and fails loudly.
func insertPreallocation(ctx context.Context, q querier, mp Preallocation) error {
	var volunteerID, customValue *string
	if mp.VolunteerID != "" {
		volunteerID = &mp.VolunteerID
	}
	if mp.CustomValue != "" {
		customValue = &mp.CustomValue
	}
	_, err := q.Exec(ctx, `
		INSERT INTO preallocation (id, shift_id, role, volunteer_id, custom_value)
		VALUES ($1, $2, $3, $4, $5)
	`, mp.ID, mp.ShiftID, mp.Role, volunteerID, customValue)
	if err != nil {
		return fmt.Errorf("failed to insert preallocation: %w", err)
	}
	// A pin is an allocator input twice over — it fills a Seat and it grants the
	// Role it names (issue #109) — so the rota's draft is stale for it.
	return markRotaInputsChangedForShift(ctx, q, mp.ShiftID)
}

// deletePreallocationByID removes the row with the given id, reporting
// whether a row was actually deleted (false lets a caller distinguish a
// concurrent delete from success).
//
// The shift comes back with it so the rota's draft can be stamped stale:
// unpinning changes the problem exactly as pinning does, and after the delete
// there is no row left to ask which Shift it was on.
func deletePreallocationByID(ctx context.Context, q querier, id string) (bool, error) {
	var shiftID string
	err := q.QueryRow(ctx, `DELETE FROM preallocation WHERE id = $1 RETURNING shift_id`, id).Scan(&shiftID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to delete preallocation %s: %w", id, err)
	}
	if err := markRotaInputsChangedForShift(ctx, q, shiftID); err != nil {
		return false, err
	}
	return true, nil
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
