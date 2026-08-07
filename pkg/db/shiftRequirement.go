package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ShiftRequirement is one entry of one Shift's stored Shape: how many Seats of
// one Role that Shift asks for (issue #137).
//
// The Role travels as an id, with a foreign key behind it, because a Shape is a
// live question — what this Shift still needs — and a live question has to
// survive a rename. The rows that record what *happened* do the opposite and
// keep the name (allocation.role, alteration.role, preallocation.role), so a
// rota already made reads as it was made.
type ShiftRequirement struct {
	ShiftID string // UUID, references shift(id)
	RoleID  string // UUID, references role(id)
	Seats   int
}

// GetShiftShapes reads the Shapes of the given Shifts, grouped by Shift id, each
// in the order its Seats are filled — by the Role's priority, with the name
// breaking a tie, exactly as ListRoles and GetDefaultShape order them — so
// nothing downstream has to sort a Shape it reads.
//
// A Shift with no stored Seats is absent from the map rather than present and
// empty: there is one way to say "this Shift asks for nothing", and it is the
// zero value a lookup already returns. Asking about no Shifts is not a query.
func (d *DB) GetShiftShapes(ctx context.Context, shiftIDs []string) (map[string][]ShiftRequirement, error) {
	if len(shiftIDs) == 0 {
		return map[string][]ShiftRequirement{}, nil
	}

	rows, err := d.pool.Query(ctx, `
		SELECT sr.shift_id, sr.role_id, sr.seats
		FROM shift_requirement sr
		JOIN role r ON r.id = sr.role_id
		WHERE sr.shift_id = ANY($1)
		ORDER BY r.priority, r.name
	`, shiftIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to query shift shapes: %w", err)
	}
	defer rows.Close()

	shapes := make(map[string][]ShiftRequirement)
	for rows.Next() {
		var seat ShiftRequirement
		if err := rows.Scan(&seat.ShiftID, &seat.RoleID, &seat.Seats); err != nil {
			return nil, fmt.Errorf("failed to scan shift shape seat: %w", err)
		}
		shapes[seat.ShiftID] = append(shapes[seat.ShiftID], seat)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating shift shapes: %w", err)
	}

	return shapes, nil
}

// insertShiftRequirements writes Seats onto Shifts within a caller's
// transaction. It is unexported and takes a querier because a Shape is only
// ever written alongside something else — its Shift, when the rota is defined
// (InsertDefinedRota), or the Shape it replaces (setShiftShape) — and a Shape
// arriving without its Shift is the state this table must never be in.
func insertShiftRequirements(ctx context.Context, q querier, requirements []ShiftRequirement) error {
	for _, seat := range requirements {
		if _, err := q.Exec(ctx, `
			INSERT INTO shift_requirement (shift_id, role_id, seats)
			VALUES ($1, $2, $3)
		`, seat.ShiftID, seat.RoleID, seat.Seats); err != nil {
			return fmt.Errorf("failed to write %d seats of role %s on shift %s: %w", seat.Seats, seat.RoleID, seat.ShiftID, err)
		}
	}
	return nil
}

// setShiftShape replaces one Shift's Shape with the Seats given, reporting
// whether the Shift exists at all (issue #138).
//
// Replaced rather than merged, for the reason SaveDefaultShape is: a Role
// dropped from a Shape is a Role that Shift no longer asks for, and no upsert
// can say that. Passing nothing leaves the Shift asking for nobody, which is a
// state the table can hold and allocation refuses over.
//
// The existence check is a separate statement because the delete cannot answer
// it: a Shift with no Seats and a Shift that does not exist both delete nought
// rows, and only one of them is a caller's mistake. It runs inside the caller's
// locking transaction, so nothing can remove the Shift between the two.
func setShiftShape(ctx context.Context, q querier, shiftID string, seats []ShiftRequirement) (bool, error) {
	var exists bool
	if err := q.QueryRow(ctx, `SELECT true FROM shift WHERE id = $1`, shiftID).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("failed to look up shift %s: %w", shiftID, err)
	}

	if _, err := q.Exec(ctx, `DELETE FROM shift_requirement WHERE shift_id = $1`, shiftID); err != nil {
		return false, fmt.Errorf("failed to clear the shape of shift %s: %w", shiftID, err)
	}

	if err := insertShiftRequirements(ctx, q, seats); err != nil {
		return false, err
	}
	return true, nil
}
