package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// RotaDefaults is the Organisation's settings record as the database holds it: one
// set of answers about how the Organisation as a whole runs, edited on the Settings
// screen. It arrives holding the shift times; the default Shape, the allocation
// toggles and the Standing Preallocations join it in later tickets.
//
// Every field is a zero value until an Organiser fills it in. Nothing is seeded
// (ADR 0006), so unset is the ordinary first state of a deployment rather than
// a fault — the settings' emptiness blocks allocation and nothing else.
//
// The times are held as "15:04" strings rather than as time.Time, because a
// time of day with no date is not a moment: it becomes one only when read
// against a Shift's date in the Organisation's timezone, which is
// model.RotaDefaults' job. The column is a real TIME, so the round trip is
// through Postgres's own parser rather than the app's.
type RotaDefaults struct {
	// ShiftStartTime and ShiftEndTime are wall-clock times of day in
	// ShiftTimezone, e.g. "19:30". Empty means an Organiser has not set them.
	ShiftStartTime string
	ShiftEndTime   string
	// ShiftTimezone is an IANA zone name, e.g. "Europe/London". Empty means
	// unset; the domain falls back rather than guessing at read time.
	ShiftTimezone string
	// AllocationSettings is the Organiser's Allocation Rules, as the JSON
	// document the column holds. Empty means an Organiser has never saved the
	// section, which reads as no rules.
	//
	// Carried verbatim rather than decoded here: what the answers mean is the
	// domain's business (model.AllocationSettings), and the whole reason the
	// column is JSON is that this layer should not need changing when a
	// Rule Kind arrives or leaves (ADR 0006).
	AllocationSettings string
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
	var start, end, timezone, allocation *string
	err := d.pool.QueryRow(ctx, `
		SELECT to_char(shift_start_time, 'HH24:MI'),
		       to_char(shift_end_time, 'HH24:MI'),
		       shift_timezone,
		       allocation_settings::text
		FROM rota_defaults
		WHERE organisation_id = $1
	`, d.org()).Scan(&start, &end, &timezone, &allocation)
	if errors.Is(err, pgx.ErrNoRows) {
		return RotaDefaults{}, nil
	}
	if err != nil {
		return RotaDefaults{}, fmt.Errorf("failed to query rota defaults: %w", err)
	}

	return RotaDefaults{
		ShiftStartTime:     deref(start),
		ShiftEndTime:       deref(end),
		ShiftTimezone:      deref(timezone),
		AllocationSettings: deref(allocation),
	}, nil
}

// SaveRotaDefaults writes the shift-time settings, creating the record if this
// is the first time anyone has saved it.
//
// It names the columns it sets rather than replacing the row, so a later
// section of the settings — the default Shape, the Allocation Rules — cannot
// be blanked by a save of this one.
//
// An empty field is written as NULL. "Not set" has to survive the round trip:
// stored as a zero TIME it would read back as midnight, which is a time the
// Organisation could plausibly be told to start at.
func (d *DB) SaveRotaDefaults(ctx context.Context, defaults RotaDefaults) error {
	_, err := d.pool.Exec(ctx, `
		INSERT INTO rota_defaults (organisation_id, shift_start_time, shift_end_time, shift_timezone)
		VALUES ($4, NULLIF($1, '')::time, NULLIF($2, '')::time, NULLIF($3, ''))
		ON CONFLICT (organisation_id) DO UPDATE SET
			shift_start_time = EXCLUDED.shift_start_time,
			shift_end_time = EXCLUDED.shift_end_time,
			shift_timezone = EXCLUDED.shift_timezone
	`, defaults.ShiftStartTime, defaults.ShiftEndTime, defaults.ShiftTimezone, d.org())
	if err != nil {
		return fmt.Errorf("failed to save rota defaults: %w", err)
	}
	return nil
}

// UpdateAllocationSettings edits the Allocation Settings document, creating
// the settings record if this is the first time anyone has saved it.
//
// The edit is handed the document as it stands ("" when there is none) and
// returns the one to store, inside one transaction holding the record's row
// lock. So an edit to one Allocation Rule always starts from every other rule
// as it now is: two Organisers adding rules at once both land, and a stored
// rule of a kind this build does not know survives an edit to its neighbour
// (ADR 0012). An edit that returns an error writes nothing.
//
// Its own method rather than a field of SaveRotaDefaults, for the reason that
// one names the columns it sets: the settings screen is sections, and saving
// one must not blank another.
//
// The document is written as given. The database checks it is an object and
// nothing more — the rules inside it are the domain's, and a column that
// vetted them would have to be migrated every time a Rule Kind arrived, which
// is precisely what storing them as JSON avoids.
//
// The rules are an allocator input, so editing them makes the rota in
// flight's draft dirty (issue #142).
func (d *DB) UpdateAllocationSettings(ctx context.Context, edit func(current string) (string, error)) error {
	return d.inTx(ctx, func(tx pgx.Tx) error {
		// The row must exist to be locked, so the first edit creates it
		// before reading it back FOR UPDATE.
		if _, err := tx.Exec(ctx, `
			INSERT INTO rota_defaults (organisation_id) VALUES ($1)
			ON CONFLICT (organisation_id) DO NOTHING
		`, d.org()); err != nil {
			return fmt.Errorf("failed to create the settings record: %w", err)
		}

		var current *string
		if err := tx.QueryRow(ctx, `
			SELECT allocation_settings::text FROM rota_defaults
			WHERE organisation_id = $1
			FOR UPDATE
		`, d.org()).Scan(&current); err != nil {
			return fmt.Errorf("failed to read allocation settings: %w", err)
		}

		next, err := edit(deref(current))
		if err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `
			UPDATE rota_defaults SET allocation_settings = NULLIF($1, '')::jsonb
			WHERE organisation_id = $2
		`, next, d.org()); err != nil {
			return fmt.Errorf("failed to save allocation settings: %w", err)
		}
		return markAllRotaInputsChanged(ctx, tx, d.org())
	})
}

// deref reads a nullable text column as the empty string, which is how this
// package spells "the Organiser has not set this".
func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
