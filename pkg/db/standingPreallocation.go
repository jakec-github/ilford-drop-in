package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

// ErrDuplicateStandingPreallocation reports that the same subject is already
// pinned on the same recurrence. Named for the same reason
// ErrDuplicateRoleName is: an admin adding a promise they have already made has
// made an ordinary mistake and is told so, rather than being shown a driver
// error code.
var ErrDuplicateStandingPreallocation = errors.New("that person is already pinned on those shifts")

// isDuplicateStanding reports whether an error is one of the two partial unique
// indexes on standing_preallocation refusing a write — the volunteer one or the
// custom-entry one. Named constraints rather than any unique violation, so a
// later index on the table cannot quietly start reporting itself as a repeat.
func isDuplicateStanding(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != uniqueViolation {
		return false
	}
	return pgErr.ConstraintName == "idx_standing_preallocation_volunteer" ||
		pgErr.ConstraintName == "idx_standing_preallocation_custom"
}

// GetStandingPreallocations reads every Standing Preallocation. There is no
// range or filter to pass: they are part of the Rota Defaults, a settings record
// an admin reads whole, and the two callers — the settings screen and rota
// definition — both want all of them.
func (d *DB) GetStandingPreallocations(ctx context.Context) ([]StandingPreallocation, error) {
	return getStandingPreallocations(ctx, d.pool)
}

func getStandingPreallocations(ctx context.Context, q querier) ([]StandingPreallocation, error) {
	rows, err := q.Query(ctx, `
		SELECT id, rrule, role_id, volunteer_id, custom_value
		FROM standing_preallocation
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query standing preallocations: %w", err)
	}
	defer rows.Close()

	var standing []StandingPreallocation
	for rows.Next() {
		var s StandingPreallocation
		var volunteerID, customValue *string
		if err := rows.Scan(&s.ID, &s.RRule, &s.RoleID, &volunteerID, &customValue); err != nil {
			return nil, fmt.Errorf("failed to scan standing preallocation: %w", err)
		}
		if volunteerID != nil {
			s.VolunteerID = *volunteerID
		}
		if customValue != nil {
			s.CustomValue = *customValue
		}
		standing = append(standing, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating standing preallocations: %w", err)
	}

	return standing, nil
}

// InsertStandingPreallocation writes one Standing Preallocation, reporting a
// repeat of one that already exists as ErrDuplicateStandingPreallocation rather
// than as an opaque constraint violation. A repeat is the same subject on the
// same recurrence,
// whatever Role either names — a person fills at most one Seat on a Shift, so
// the second is a slip rather than a second promise.
//
// The nullable volunteer_id / custom_value follow the preallocation pattern: an
// empty string is stored as NULL, and the table's own CHECK refuses a row that
// names both or neither. An unknown role_id trips the foreign key.
func (d *DB) InsertStandingPreallocation(ctx context.Context, s StandingPreallocation) error {
	var volunteerID, customValue *string
	if s.VolunteerID != "" {
		volunteerID = &s.VolunteerID
	}
	if s.CustomValue != "" {
		customValue = &s.CustomValue
	}
	_, err := d.pool.Exec(ctx, `
		INSERT INTO standing_preallocation (id, rrule, role_id, volunteer_id, custom_value)
		VALUES ($1, $2, $3, $4, $5)
	`, s.ID, s.RRule, s.RoleID, volunteerID, customValue)
	if err != nil {
		if isDuplicateStanding(err) {
			return ErrDuplicateStandingPreallocation
		}
		return fmt.Errorf("failed to insert standing preallocation: %w", err)
	}
	return nil
}

// DeleteStandingPreallocationByID removes one, reporting whether a row was
// actually deleted so a caller can tell a missing one from a successful delete.
//
// Nothing cascades. The Preallocations a Standing Preallocation has already
// seeded are ordinary Preallocations belonging to the rotas that minted them,
// and they outlive it: it is a convenience at definition, not a standing fact.
func (d *DB) DeleteStandingPreallocationByID(ctx context.Context, id string) (bool, error) {
	tag, err := d.pool.Exec(ctx, `DELETE FROM standing_preallocation WHERE id = $1`, id)
	if err != nil {
		return false, fmt.Errorf("failed to delete standing preallocation %s: %w", id, err)
	}
	return tag.RowsAffected() > 0, nil
}
