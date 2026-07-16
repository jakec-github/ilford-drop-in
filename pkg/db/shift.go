package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// GetShiftsByRotaID retrieves a rotation's shifts, ordered by date ascending.
// Consumers that once recomputed a rota's dates by arithmetic read them here
// instead (ADR 0001).
func (d *DB) GetShiftsByRotaID(ctx context.Context, rotaID string) ([]Shift, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, date, rota_id
		FROM shift
		WHERE rota_id = $1
		ORDER BY date
	`, rotaID)
	if err != nil {
		return nil, fmt.Errorf("failed to query shifts for rota %s: %w", rotaID, err)
	}
	defer rows.Close()

	var shifts []Shift
	for rows.Next() {
		var s Shift
		var date time.Time
		if err := rows.Scan(&s.ID, &date, &s.RotaID); err != nil {
			return nil, fmt.Errorf("failed to scan shift: %w", err)
		}
		s.Date = date.Format("2006-01-02")
		shifts = append(shifts, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating shifts: %w", err)
	}

	return shifts, nil
}

// ShiftInRange is a minted shift within a queried date range, carrying whether
// its rota has been allocated. Allocation is whole-rota today (derived from the
// rota's allocated_datetime), but the flag is exposed per shift to leave room
// for per-shift allocation later (ADR 0001).
type ShiftInRange struct {
	Shift
	Allocated bool
}

// GetShiftsInRange retrieves the minted shifts whose date falls between from and
// to (inclusive), allocated or not, ordered by date ascending. A zero time
// leaves that bound open, mirroring GetAllocationsInRange. Each shift carries
// its rota's allocated state, joined from rotation.allocated_datetime.
func (d *DB) GetShiftsInRange(ctx context.Context, from, to time.Time) ([]ShiftInRange, error) {
	where, args := shiftDateWhere(from, to)
	rows, err := d.pool.Query(ctx, `
		SELECT s.id, s.date, s.rota_id, r.allocated_datetime IS NOT NULL
		FROM shift s
		JOIN rotation r ON r.id = s.rota_id
	`+where+`
		ORDER BY s.date
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query shifts in range: %w", err)
	}
	defer rows.Close()

	var shifts []ShiftInRange
	for rows.Next() {
		var s ShiftInRange
		var date time.Time
		if err := rows.Scan(&s.ID, &date, &s.RotaID, &s.Allocated); err != nil {
			return nil, fmt.Errorf("failed to scan shift: %w", err)
		}
		s.Date = date.Format("2006-01-02")
		shifts = append(shifts, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating shifts: %w", err)
	}

	return shifts, nil
}

// GetShiftByDate retrieves the single shift on the given date, or nil if no
// shift exists for that date. Dates are unique, so at most one row matches;
// this is the lookup that resolves a date to its shift and rota.
func (d *DB) GetShiftByDate(ctx context.Context, date time.Time) (*Shift, error) {
	var s Shift
	var d0 time.Time
	err := d.pool.QueryRow(ctx, `
		SELECT id, date, rota_id
		FROM shift
		WHERE date = $1
	`, date).Scan(&s.ID, &d0, &s.RotaID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query shift for date %s: %w", date.Format("2006-01-02"), err)
	}
	s.Date = d0.Format("2006-01-02")
	return &s, nil
}

// InsertRotationAndShifts inserts a rotation and all of its minted shifts in a
// single transaction, so a rotation can never exist without its shifts.
//
// Concurrency (issue #41, hazard B1): the shift.date UNIQUE constraint is what
// makes concurrent runs safe — two rotas minting the same date cannot both
// commit, and the losing transaction writes nothing. Any change that relaxes
// that constraint must introduce a replacement guard here.
func (d *DB) InsertRotationAndShifts(ctx context.Context, rotation *Rotation, shifts []Shift) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// The rotation row is now identity plus allocated_datetime; start and shift
	// count are derived from its shifts by GetRotations (ADR 0001).
	_, err = tx.Exec(ctx, `
		INSERT INTO rotation (id)
		VALUES ($1)
	`, rotation.ID)
	if err != nil {
		return fmt.Errorf("failed to insert rotation: %w", err)
	}

	batch := &pgx.Batch{}
	for _, s := range shifts {
		batch.Queue(`
			INSERT INTO shift (id, date, rota_id)
			VALUES ($1, $2, $3)
		`, s.ID, s.Date, s.RotaID)
	}
	results := tx.SendBatch(ctx, batch)
	for range shifts {
		if _, err := results.Exec(); err != nil {
			results.Close()
			return fmt.Errorf("failed to insert shift: %w", err)
		}
	}
	if err := results.Close(); err != nil {
		return fmt.Errorf("failed to close shift batch: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
