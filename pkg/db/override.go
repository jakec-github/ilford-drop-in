package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrDuplicateOverridePin reports that one Override pins the same volunteer
// twice. A person fills at most one Seat on a Shift, so the second is a slip
// rather than a second promise, and an Organiser is told so in words.
var ErrDuplicateOverridePin = errors.New("that person is pinned twice by one override")

// ErrOverrideNotFound reports that an Override named by id does not exist in
// this Organisation.
var ErrOverrideNotFound = errors.New("override not found")

func isDuplicateOverridePin(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation &&
		pgErr.ConstraintName == "idx_override_pin_volunteer"
}

// GetOverrides reads the Organisation's Overrides in the order they apply, each
// with its Shape and its Pins. Like the rest of the Rota Defaults they are read
// whole: the settings screen lists every one, and defining a rota applies every
// one.
func (d *DB) GetOverrides(ctx context.Context) ([]Override, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT id, position, rrule, to_char(start_from, 'YYYY-MM-DD'), granularity, sets_shape
		FROM override
		WHERE organisation_id = $1
		ORDER BY position
	`, d.org())
	if err != nil {
		return nil, fmt.Errorf("failed to query overrides: %w", err)
	}
	var overrides []Override
	index := map[string]int{}
	for rows.Next() {
		var o Override
		if err := rows.Scan(&o.ID, &o.Position, &o.RRule, &o.StartFrom, &o.Granularity, &o.SetsShape); err != nil {
			rows.Close()
			return nil, fmt.Errorf("failed to scan override: %w", err)
		}
		index[o.ID] = len(overrides)
		overrides = append(overrides, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating overrides: %w", err)
	}
	if len(overrides) == 0 {
		return nil, nil
	}

	seats, err := d.pool.Query(ctx, `
		SELECT s.override_id, s.role_id, s.seats
		FROM override_seat s
		JOIN override o ON o.id = s.override_id
		JOIN role r ON r.id = s.role_id
		WHERE o.organisation_id = $1
		ORDER BY r.priority, r.name
	`, d.org())
	if err != nil {
		return nil, fmt.Errorf("failed to query override seats: %w", err)
	}
	for seats.Next() {
		var overrideID string
		var seat OverrideSeat
		if err := seats.Scan(&overrideID, &seat.RoleID, &seat.Seats); err != nil {
			seats.Close()
			return nil, fmt.Errorf("failed to scan override seat: %w", err)
		}
		i := index[overrideID]
		overrides[i].Seats = append(overrides[i].Seats, seat)
	}
	seats.Close()
	if err := seats.Err(); err != nil {
		return nil, fmt.Errorf("error iterating override seats: %w", err)
	}

	pins, err := d.pool.Query(ctx, `
		SELECT p.override_id, p.id, p.role_id, p.volunteer_id, p.custom_value
		FROM override_pin p
		JOIN override o ON o.id = p.override_id
		WHERE o.organisation_id = $1
		ORDER BY p.id
	`, d.org())
	if err != nil {
		return nil, fmt.Errorf("failed to query override pins: %w", err)
	}
	defer pins.Close()
	for pins.Next() {
		var overrideID string
		var pin OverridePin
		var volunteerID, customValue *string
		if err := pins.Scan(&overrideID, &pin.ID, &pin.RoleID, &volunteerID, &customValue); err != nil {
			return nil, fmt.Errorf("failed to scan override pin: %w", err)
		}
		if volunteerID != nil {
			pin.VolunteerID = *volunteerID
		}
		if customValue != nil {
			pin.CustomValue = *customValue
		}
		i := index[overrideID]
		overrides[i].Pins = append(overrides[i].Pins, pin)
	}
	if err := pins.Err(); err != nil {
		return nil, fmt.Errorf("error iterating override pins: %w", err)
	}

	return overrides, nil
}

// InsertOverride adds an Override at the end of the list, where it wins over
// every one before it. Its Position is chosen here and the one passed in is
// ignored.
func (d *DB) InsertOverride(ctx context.Context, o Override) error {
	return d.inTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO override (id, organisation_id, position, rrule, start_from, granularity, sets_shape)
			SELECT $1, $2, COALESCE(MAX(position), 0) + 1, $3, $4::date, $5, $6
			FROM override
			WHERE organisation_id = $2
		`, o.ID, d.org(), o.RRule, o.StartFrom, o.Granularity, o.SetsShape)
		if err != nil {
			return fmt.Errorf("failed to insert override: %w", err)
		}
		return d.writeOverrideParts(ctx, tx, o)
	})
}

// UpdateOverride rewrites an Override in place — its rule, Start from, Shape
// and Pins — keeping where it stands in the list. ErrOverrideNotFound when
// there is no such Override.
func (d *DB) UpdateOverride(ctx context.Context, o Override) error {
	return d.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE override
			SET rrule = $3, start_from = $4::date, granularity = $5, sets_shape = $6
			WHERE id = $1 AND organisation_id = $2
		`, o.ID, d.org(), o.RRule, o.StartFrom, o.Granularity, o.SetsShape)
		if err != nil {
			return fmt.Errorf("failed to update override: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrOverrideNotFound
		}
		if _, err := tx.Exec(ctx, `DELETE FROM override_seat WHERE override_id = $1`, o.ID); err != nil {
			return fmt.Errorf("failed to clear override seats: %w", err)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM override_pin WHERE override_id = $1`, o.ID); err != nil {
			return fmt.Errorf("failed to clear override pins: %w", err)
		}
		return d.writeOverrideParts(ctx, tx, o)
	})
}

// writeOverrideParts writes an Override's Seats and Pins. Each selects its
// Role from this Organisation's, so a Role of another inserts nothing and is
// reported rather than written.
func (d *DB) writeOverrideParts(ctx context.Context, tx pgx.Tx, o Override) error {
	for _, seat := range o.Seats {
		tag, err := tx.Exec(ctx, `
			INSERT INTO override_seat (override_id, role_id, seats)
			SELECT $1, r.id, $3 FROM role r WHERE r.id = $2 AND r.organisation_id = $4
		`, o.ID, seat.RoleID, seat.Seats, d.org())
		if err != nil {
			return fmt.Errorf("failed to insert override seat: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("failed to insert override seat: no role %s", seat.RoleID)
		}
	}
	for _, pin := range o.Pins {
		var volunteerID, customValue *string
		if pin.VolunteerID != "" {
			volunteerID = &pin.VolunteerID
		}
		if pin.CustomValue != "" {
			customValue = &pin.CustomValue
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO override_pin (id, override_id, role_id, volunteer_id, custom_value)
			SELECT $1, $2, r.id, $4, $5 FROM role r WHERE r.id = $3 AND r.organisation_id = $6
		`, pin.ID, o.ID, pin.RoleID, volunteerID, customValue, d.org())
		if err != nil {
			if isDuplicateOverridePin(err) {
				return ErrDuplicateOverridePin
			}
			return fmt.Errorf("failed to insert override pin: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("failed to insert override pin: no role %s", pin.RoleID)
		}
	}
	return nil
}

// DeleteOverride removes an Override with its Shape and Pins, reporting
// whether there was one to remove. The Pins it has already made on defined
// rotas are ordinary Pins and stay.
func (d *DB) DeleteOverride(ctx context.Context, id string) (bool, error) {
	tag, err := d.pool.Exec(ctx, `DELETE FROM override WHERE id = $1 AND organisation_id = $2`, id, d.org())
	if err != nil {
		return false, fmt.Errorf("failed to delete override %s: %w", id, err)
	}
	return tag.RowsAffected() > 0, nil
}

// ReorderOverrides puts the Overrides in the order given, first applied first.
// The ids must be every Override the Organisation has, each once; the caller
// checks that, and this refuses with ErrOverrideNotFound an id it cannot find.
func (d *DB) ReorderOverrides(ctx context.Context, ids []string) error {
	return d.inTx(ctx, func(tx pgx.Tx) error {
		for i, id := range ids {
			tag, err := tx.Exec(ctx, `
				UPDATE override SET position = $3 WHERE id = $1 AND organisation_id = $2
			`, id, d.org(), i+1)
			if err != nil {
				return fmt.Errorf("failed to move override %s: %w", id, err)
			}
			if tag.RowsAffected() == 0 {
				return ErrOverrideNotFound
			}
		}
		return nil
	})
}
