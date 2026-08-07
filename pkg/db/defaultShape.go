package db

import (
	"context"
	"fmt"
)

// DefaultShapeSeat is one entry in the default Shape: how many Seats of one
// Role every Shift asks for.
//
// The Role travels as an id rather than a name because the Shape outlives any
// number of renames, and because the id is what the foreign key holds — the one
// thing this table exists to guarantee (ADR 0006). Per-rota rows still record
// the name they were made under; this is not one of those.
type DefaultShapeSeat struct {
	RoleID string // UUID, references role(id)
	Seats  int
}

// GetDefaultShape reads the default Shape in the order its Seats are filled —
// by the Role's priority, with the name breaking a tie, exactly as ListRoles
// orders Roles — so nothing downstream has to sort a Shape it reads.
//
// An empty Shape is an ordinary answer: no migration seeds one, so a database
// nobody has configured yet asks for nothing. What that costs is decided
// elsewhere; it blocks allocation and nothing else.
func (d *DB) GetDefaultShape(ctx context.Context) ([]DefaultShapeSeat, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT s.role_id, s.seats
		FROM default_shape s
		JOIN role r ON r.id = s.role_id
		ORDER BY r.priority, r.name
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query default shape: %w", err)
	}
	defer rows.Close()

	var shape []DefaultShapeSeat
	for rows.Next() {
		var seat DefaultShapeSeat
		if err := rows.Scan(&seat.RoleID, &seat.Seats); err != nil {
			return nil, fmt.Errorf("failed to scan default shape seat: %w", err)
		}
		shape = append(shape, seat)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating default shape: %w", err)
	}

	return shape, nil
}

// SaveDefaultShape writes the whole default Shape, replacing whatever was
// there.
//
// Whole rather than per-Seat, because that is what an edit to a Shape is: a
// Role dropped from it is a Role the Shape no longer asks for, and there is no
// way to say that with an upsert. Passing nothing empties it, which is the state
// a deployment starts in.
//
// The delete and the inserts share a transaction, so a Seat naming a Role that
// does not exist takes the whole save down rather than leaving the Shape
// half-rewritten.
func (d *DB) SaveDefaultShape(ctx context.Context, shape []DefaultShapeSeat) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM default_shape`); err != nil {
		return fmt.Errorf("failed to clear default shape: %w", err)
	}

	for _, seat := range shape {
		if _, err := tx.Exec(ctx, `
			INSERT INTO default_shape (role_id, seats)
			VALUES ($1, $2)
		`, seat.RoleID, seat.Seats); err != nil {
			return fmt.Errorf("failed to write %d seats of role %s: %w", seat.Seats, seat.RoleID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit default shape: %w", err)
	}
	return nil
}
