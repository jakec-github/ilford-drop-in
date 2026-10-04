package services

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// A Shift's Shape is what that Shift asks for: which Roles, and how many Seats
// of each (issue #137). It is stored per Shift, copied from the default Shape
// when the rota was defined, and read here by everything that asks what a Shift
// needs — allocation, which solves for the Seats, and the availability round,
// which reports whether the answers coming in can fill them.
//
// Stored rather than recomputed is the whole point. Both of those paths used to
// resolve the Seats from the settings on every read, which meant editing the
// default Shape silently rewrote what every past Shift had asked for — a rota
// allocated for one Team lead and four Service volunteers re-reading as asking
// for something else, with nothing saying so.

// ShiftShapeStore reads the Shapes Shifts were minted with. Roles come with it
// because a Seat is stored against a Role id and is only legible as a Role.
type ShiftShapeStore interface {
	RoleStore
	GetShiftShapes(ctx context.Context, shiftIDs []string) (map[string][]db.ShiftRequirement, error)
}

// ShiftShapes reads what each of the given Shifts asks for, keyed by Shift id
// and each in the order its Seats are filled.
//
// A Shift with no stored Seats is absent from the map, so a lookup yields a nil
// Shape — a Shift asking for nobody. That is an ordinary answer here: it is
// what a Shift minted before this table existed looks like on a deployment that
// had not stated a default Shape. Allocation is the one path that refuses over
// it (shapesForAllocation); the pages that only render still render.
func ShiftShapes(ctx context.Context, store ShiftShapeStore, shiftIDs []string) (map[string]model.Shape, error) {
	roles, err := RoleTable(ctx, store)
	if err != nil {
		return nil, err
	}

	rows, err := store.GetShiftShapes(ctx, shiftIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to read the shifts' shapes: %w", err)
	}

	shapes := make(map[string]model.Shape, len(rows))
	for shiftID, seats := range rows {
		stored := make([]storedSeat, 0, len(seats))
		for _, seat := range seats {
			stored = append(stored, storedSeat{RoleID: seat.RoleID, Seats: seat.Seats})
		}
		shape, err := resolveShape(stored, roles, fmt.Sprintf("the shape of shift %s", shiftID))
		if err != nil {
			return nil, err
		}
		shapes[shiftID] = shape
	}
	return shapes, nil
}

// ShiftShapeWriteStore is what editing one Shift's Shape needs, kept apart from
// reading Shapes for the reason DefaultShapeWriteStore is: one screen writes,
// and the paths that spend a Shape only read. The lookup resolves the Shift to
// its Rotation; the checks and the write then happen under that Rotation's row
// lock.
type ShiftShapeWriteStore interface {
	ShiftShapeStore
	GetShiftByID(ctx context.Context, id string) (*db.ShiftInRange, error)
	WithRotaShapeLock(ctx context.Context, rotaIDs []string, fn func(store db.ShapeTxStore) error) error
}

// SaveShiftShape rewrites what one Shift asks for, and returns it as it now
// stands (issue #138).
//
// Whole rather than a Seat at a time, exactly as the default Shape is written:
// a Role dropped from the list is a Role this Shift no longer asks for, and
// there is no way to say that one Seat at a time. Sending nothing leaves it
// asking for nobody, which allocation refuses over and nothing else minds.
//
// One thing can stop an edit, and it is about the rota rather than the Shape:
// the Rotation being allocated freezes it. The solver filled Seats against this
// Shape, so afterwards it is not a request but the record of what the rota was
// made from — changing it would leave the rota describing a Shift that never
// asked for those people.
//
// The pins on the Shift are not consulted. A Shape bounds the allocator, not
// people's decisions (ADR 0010), so shrinking it below its pins, or dropping a
// Role somebody is pinned to, is an ordinary edit: the pins sit past the new
// Shape, the solver honours them, and it places nobody else in that Role. It
// used to refuse such an edit (seatsHoldThePins, #138), back when the solver
// could not.
//
// Being closed does not freeze anything either. A closed Shift's Shape is what
// it will ask for when it reopens, and an Organiser fixing one should not have
// to reopen it first.
func SaveShiftShape(
	ctx context.Context,
	store ShiftShapeWriteStore,
	shiftID string,
	seats []SeatParams,
	logger *zap.Logger,
) (model.Shape, error) {
	roles, err := RoleTable(ctx, store)
	if err != nil {
		return nil, err
	}

	stated, err := statedSeats(seats, roles)
	if err != nil {
		return nil, err
	}

	shift, err := store.GetShiftByID(ctx, shiftID)
	if err != nil {
		return nil, fmt.Errorf("failed to look up shift %s: %w", shiftID, err)
	}
	if shift == nil {
		return nil, wrapf(ErrNotFound, "shift %s not found", shiftID)
	}

	rows := make([]db.ShiftRequirement, 0, len(stated))
	for _, seat := range stated {
		rows = append(rows, db.ShiftRequirement{ShiftID: shiftID, RoleID: seat.RoleID, Seats: seat.Seats})
	}

	err = store.WithRotaShapeLock(ctx, []string{shift.RotaID}, func(tx db.ShapeTxStore) error {
		allocated, err := tx.RotaAllocated(ctx, shift.RotaID)
		if err != nil {
			return err
		}
		if allocated {
			// Said as what it means rather than as a rule: the rota was worked
			// out around this shift asking for these people.
			return wrapf(ErrConflict,
				"the rota covering %s has already been allocated, so what its shifts ask for is fixed",
				readableDate(shift.Date))
		}

		written, err := tx.SetShiftShape(ctx, shiftID, rows)
		if err != nil {
			return err
		}
		if !written {
			// Lost a race with something that removed the shift under the lock.
			return wrapf(ErrNotFound, "shift %s not found", shiftID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	logger.Info("Shift shape saved",
		zap.String("shift_id", shiftID),
		zap.String("date", shift.Date),
		zap.Int("roles", len(rows)))

	return resolveShape(stated, roles, fmt.Sprintf("the shape of shift %s", shiftID))
}

// shapesForAllocation reads the rota's Shapes and refuses when an open Shift
// has none, naming the dates.
//
// It is the Shape half of the gate settingsForAllocation is the settings half
// of, and it is here for the same reason: a Shift asking for nobody solves
// perfectly and staffs nothing, so a rota comes back empty with nothing saying
// why. Reading and checking together means a caller cannot allocate by
// forgetting to check.
//
// Closed Shifts are exempt. Nobody works a day the drop-in is shut, so a closed
// Shift with no Seats is not a gap in anything.
//
// The state is only reachable on a deployment that defined a rota before it
// stated a default Shape — defining refuses without one now — or that ran the
// #137 migration with the default Shape still empty. Until per-Shift editing
// lands (#138) the answer in both cases is to state the Shape and define the
// rota again, which is what the message says.
func shapesForAllocation(ctx context.Context, store ShiftShapeStore, shifts []db.Shift) (map[string]model.Shape, error) {
	shiftIDs := make([]string, 0, len(shifts))
	for _, s := range shifts {
		shiftIDs = append(shiftIDs, s.ID)
	}

	shapes, err := ShiftShapes(ctx, store, shiftIDs)
	if err != nil {
		return nil, err
	}

	var shapeless []string
	for _, s := range shifts {
		if s.Closed {
			continue
		}
		if len(shapes[s.ID]) == 0 {
			shapeless = append(shapeless, s.Date)
		}
	}
	if len(shapeless) > 0 {
		return nil, wrapf(ErrInvalidInput,
			"%s %s %s for nobody; state the default shape on the settings screen and define the rota again",
			plural(len(shapeless), "the shift on", "the shifts on"),
			joinWithAnd(shapeless),
			plural(len(shapeless), "asks", "ask"))
	}

	return shapes, nil
}
