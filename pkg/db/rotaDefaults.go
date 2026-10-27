package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// RotaDefaults is the Organisation's settings record as the database holds it: one
// set of answers about how the Organisation as a whole runs, edited on the Settings
// screen: the Shift pattern, and the Allocation Settings. The default Shape and
// the Overrides are tables of their own.
//
// Every field is a zero value until an Organiser fills it in. Nothing is seeded
// (ADR 0006), so unset is the ordinary first state of a deployment rather than
// a fault — the settings' emptiness blocks allocation and nothing else.
type RotaDefaults struct {
	// ShiftPattern is the Organiser's RRULE lines, as typed (ADR 0013). Empty
	// means no pattern has been set, and no rota can be defined.
	ShiftPattern string
	// PatternStartFrom is the date the pattern counts from, "2006-01-02".
	// Empty means unset.
	PatternStartFrom string
	// ShiftLengthMinutes is how long each generated Shift runs. Zero means
	// unset.
	ShiftLengthMinutes int
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
	// to_char keeps the date a date: scanned into a time.Time it would arrive
	// as a midnight in some zone, which is not what Start from means.
	var pattern, startFrom, timezone, allocation *string
	var length *int
	err := d.pool.QueryRow(ctx, `
		SELECT shift_pattern,
		       to_char(pattern_start_from, 'YYYY-MM-DD'),
		       shift_length_minutes,
		       shift_timezone,
		       allocation_settings::text
		FROM rota_defaults
		WHERE organisation_id = $1
	`, d.org()).Scan(&pattern, &startFrom, &length, &timezone, &allocation)
	if errors.Is(err, pgx.ErrNoRows) {
		return RotaDefaults{}, nil
	}
	if err != nil {
		return RotaDefaults{}, fmt.Errorf("failed to query rota defaults: %w", err)
	}

	return RotaDefaults{
		ShiftPattern:       deref(pattern),
		PatternStartFrom:   deref(startFrom),
		ShiftLengthMinutes: derefInt(length),
		ShiftTimezone:      deref(timezone),
		AllocationSettings: deref(allocation),
	}, nil
}

// SaveShiftPattern writes the Shift pattern section — the pattern, its Start
// from, the time zone and the length — creating the record if this is the
// first time anyone has saved it.
//
// It names the columns it sets rather than replacing the row, so a later
// section of the settings — the default Shape, the Allocation Rules — cannot
// be blanked by a save of this one.
//
// An empty field is written as NULL, so "not set" survives the round trip.
//
// The pattern is not an allocator input — it decides which Shifts the next
// rota is minted with, and a rota already defined keeps the ones it has — so
// saving it leaves the rota in flight's draft alone.
func (d *DB) SaveShiftPattern(ctx context.Context, defaults RotaDefaults) error {
	_, err := d.pool.Exec(ctx, `
		INSERT INTO rota_defaults (organisation_id, shift_pattern, pattern_start_from, shift_length_minutes, shift_timezone)
		VALUES ($5, NULLIF($1, ''), NULLIF($2, '')::date, NULLIF($3, 0), NULLIF($4, ''))
		ON CONFLICT (organisation_id) DO UPDATE SET
			shift_pattern = EXCLUDED.shift_pattern,
			pattern_start_from = EXCLUDED.pattern_start_from,
			shift_length_minutes = EXCLUDED.shift_length_minutes,
			shift_timezone = EXCLUDED.shift_timezone
	`, defaults.ShiftPattern, defaults.PatternStartFrom, defaults.ShiftLengthMinutes, defaults.ShiftTimezone, d.org())
	if err != nil {
		return fmt.Errorf("failed to save the shift pattern: %w", err)
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

// derefInt reads a nullable integer column as zero, for the same reason.
func derefInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
