package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// RotaDefaults is the drop-in's settings record as the database holds it: one
// set of answers about how the drop-in as a whole runs, edited on the Settings
// screen. It arrives holding the shift times; the default Shape, the allocation
// toggles and the Standing Preallocations join it in later tickets.
//
// Every field is a zero value until an admin fills it in. Nothing is seeded
// (ADR 0006), so unset is the ordinary first state of a deployment rather than
// a fault — the settings' emptiness blocks allocation and nothing else.
//
// The times are held as "15:04" strings rather than as time.Time, because a
// time of day with no date is not a moment: it becomes one only when read
// against a Shift's date in the drop-in's timezone, which is
// model.RotaDefaults' job. The column is a real TIME, so the round trip is
// through Postgres's own parser rather than the app's.
type RotaDefaults struct {
	// ShiftStartTime and ShiftEndTime are wall-clock times of day in
	// ShiftTimezone, e.g. "19:30". Empty means an admin has not set them.
	ShiftStartTime string
	ShiftEndTime   string
	// ShiftTimezone is an IANA zone name, e.g. "Europe/London". Empty means
	// unset; the domain falls back rather than guessing at read time.
	ShiftTimezone string
}

// GetRotaDefaults reads the settings record.
//
// No row is the same answer as a row of nulls — nothing has been set — so both
// come back as the zero RotaDefaults rather than one of them being an error.
// That matters because the table is deliberately unseeded: the first save
// creates the row, and every read before it lands here.
func (d *DB) GetRotaDefaults(ctx context.Context) (RotaDefaults, error) {
	// to_char renders the TIME the way the app states it. Doing the formatting
	// in SQL keeps a time of day a string on this side of the boundary, where
	// scanning into a time.Time would attach a meaningless date to it.
	var start, end, timezone *string
	err := d.pool.QueryRow(ctx, `
		SELECT to_char(shift_start_time, 'HH24:MI'),
		       to_char(shift_end_time, 'HH24:MI'),
		       shift_timezone
		FROM rota_defaults
	`).Scan(&start, &end, &timezone)
	if errors.Is(err, pgx.ErrNoRows) {
		return RotaDefaults{}, nil
	}
	if err != nil {
		return RotaDefaults{}, fmt.Errorf("failed to query rota defaults: %w", err)
	}

	return RotaDefaults{
		ShiftStartTime: deref(start),
		ShiftEndTime:   deref(end),
		ShiftTimezone:  deref(timezone),
	}, nil
}

// SaveRotaDefaults writes the shift-time settings, creating the record if this
// is the first time anyone has saved it.
//
// It names the columns it sets rather than replacing the row, so a later
// section of the settings — the default Shape, the allocation toggles — cannot
// be blanked by a save of this one.
//
// An empty field is written as NULL. "Not set" has to survive the round trip:
// stored as a zero TIME it would read back as midnight, which is a time the
// drop-in could plausibly be told to start at.
func (d *DB) SaveRotaDefaults(ctx context.Context, defaults RotaDefaults) error {
	_, err := d.pool.Exec(ctx, `
		INSERT INTO rota_defaults (id, shift_start_time, shift_end_time, shift_timezone)
		VALUES (TRUE, NULLIF($1, '')::time, NULLIF($2, '')::time, NULLIF($3, ''))
		ON CONFLICT (id) DO UPDATE SET
			shift_start_time = EXCLUDED.shift_start_time,
			shift_end_time = EXCLUDED.shift_end_time,
			shift_timezone = EXCLUDED.shift_timezone
	`, defaults.ShiftStartTime, defaults.ShiftEndTime, defaults.ShiftTimezone)
	if err != nil {
		return fmt.Errorf("failed to save rota defaults: %w", err)
	}
	return nil
}

// deref reads a nullable text column as the empty string, which is how this
// package spells "the admin has not set this".
func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
