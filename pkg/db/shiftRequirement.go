package db

import (
	"context"
	"fmt"
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
// transaction. It is unexported and takes a querier because the only moment a
// Shape is written is the moment its Shift is (InsertDefinedRota), and a Shape
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
