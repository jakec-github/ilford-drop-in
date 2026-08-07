package services

import (
	"context"
	"fmt"

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
