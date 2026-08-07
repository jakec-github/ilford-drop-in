package services

import (
	"context"
	"fmt"
	"sort"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/allocator"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// The default Shape is what every Shift asks for: which Roles, and how many
// Seats of each (issue #129, ADR 0006). It is part of the Rota Defaults, and it
// is the successor to `defaultShiftSize` — one number that could only describe a
// rota with one Role, every other Role's count being its ceiling by
// construction.
//
// It is read at one moment only: defining a rota, which copies it onto every
// Shift it mints as Seats of that Shift's own (issue #137). Nothing that asks
// what an existing Shift needs comes here — allocation and the availability
// round read the Shift's stored Shape — which is what makes editing this
// setting change the next rota rather than every rota ever made.

// DefaultShapeStore reads the default Shape. Roles come with it because a Seat
// is stored against a Role id and is only legible as a Role.
type DefaultShapeStore interface {
	RoleStore
	GetDefaultShape(ctx context.Context) ([]db.DefaultShapeSeat, error)
}

// DefaultShapeWriteStore is what editing the Shape needs, kept apart from
// reading it for the same reason RoleWriteStore is: one screen writes it, the
// paths that spend it only read.
type DefaultShapeWriteStore interface {
	DefaultShapeStore
	SaveDefaultShape(ctx context.Context, shape []db.DefaultShapeSeat) error
}

// SeatParams is one line of the Shape as an admin states it: this many of this
// Role. A Role the Shape does not name is left out rather than sent as zero —
// zero Seats of a Role is not something a Shape can say.
type SeatParams struct {
	RoleID string
	Count  int
}

// DefaultShape reads the Shape every Shift starts from, with each Seat's Role
// resolved.
//
// An empty Shape is an ordinary answer rather than an error: nothing seeds one
// (ADR 0006), so it is the state every deployment starts in. Allocation is the
// one path that refuses over it.
func DefaultShape(ctx context.Context, store DefaultShapeStore) (model.Shape, error) {
	roles, err := RoleTable(ctx, store)
	if err != nil {
		return nil, err
	}

	rows, err := store.GetDefaultShape(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read the default shape: %w", err)
	}

	return resolveShape(storedSeats(rows), roles, "the default shape")
}

// storedSeat is one stored Seat as either Shape table holds it: a Role id and a
// count. `default_shape` and `shift_requirement` are the same two columns one
// level apart, and turning either into a model.Shape is the same work, so they
// meet here rather than having a resolver each.
type storedSeat struct {
	RoleID string
	Seats  int
}

// storedSeats reads the default Shape's rows as the pair of columns both Shape
// tables share.
func storedSeats(rows []db.DefaultShapeSeat) []storedSeat {
	seats := make([]storedSeat, 0, len(rows))
	for _, row := range rows {
		seats = append(seats, storedSeat{RoleID: row.RoleID, Seats: row.Seats})
	}
	return seats
}

// statedSeats reads a Shape an admin has stated, or says why it will not.
//
// Every rule here is about the Shape alone — which Roles exist, and what a Role
// can be asked for — so both Shapes an admin edits are checked by it: the
// default one on the settings screen, and one Shift's own (issue #138). What
// differs between them is what else the Shape has to agree with, and that stays
// with each caller.
func statedSeats(seats []SeatParams, roles model.Roles) ([]storedSeat, error) {
	rows := make([]storedSeat, 0, len(seats))
	named := make(map[string]bool, len(seats))
	for _, seat := range seats {
		role, ok := roles.ByID(seat.RoleID)
		if !ok {
			return nil, wrapf(ErrInvalidInput, "role %q is not a known role", seat.RoleID)
		}
		if named[seat.RoleID] {
			return nil, wrapf(ErrInvalidInput, "the shape names %s twice; say how many seats it needs once", role.Name)
		}
		named[seat.RoleID] = true

		// Zero is not a smaller Shape, it is a Role the Shape does not name.
		// Saying so rather than dropping it silently keeps the stored Shape and
		// the stated one the same thing.
		if seat.Count < 1 {
			return nil, wrapf(ErrInvalidInput,
				"a shape asks for at least one seat of a role it names; leave %s out instead of asking for %d",
				role.Name, seat.Count)
		}
		if role.Capped() && seat.Count > *role.Max {
			return nil, wrapf(ErrInvalidInput,
				"a shift may hold at most %d %s, so the shape cannot ask for %d",
				*role.Max, role.Name, seat.Count)
		}

		rows = append(rows, storedSeat{RoleID: role.ID, Seats: seat.Count})
	}
	return rows, nil
}

// resolveShape turns stored Seats into the domain's Shape, in the order the
// Seats are filled. `owner` names whose Shape it is, so a failure says which
// one.
//
// A Seat naming a Role the table does not hold fails rather than being skipped.
// The foreign key makes it unreachable, and Roles are permanent so it cannot
// arrive by deletion — but a Shape quietly missing a Seat is a rota quietly
// short of people, which is the failure mode this whole table exists to rule
// out.
func resolveShape(rows []storedSeat, roles model.Roles, owner string) (model.Shape, error) {
	shape := make(model.Shape, 0, len(rows))
	for _, row := range rows {
		role, ok := roles.ByID(row.RoleID)
		if !ok {
			return nil, fmt.Errorf("%s names role %s, which does not exist", owner, row.RoleID)
		}
		shape = append(shape, model.Seat{Role: role, Count: row.Seats})
	}

	// In the order the Seats are filled, whatever order they were stated or
	// stored in — so every reader of a Shape gets the same list, and only this
	// one function has an opinion about what that order is.
	sort.SliceStable(shape, func(i, j int) bool {
		if shape[i].Role.Priority != shape[j].Role.Priority {
			return shape[i].Role.Priority < shape[j].Role.Priority
		}
		return shape[i].Role.Name < shape[j].Role.Name
	})
	return shape, nil
}

// SaveDefaultShape writes the whole default Shape and returns it as it now
// stands.
//
// Whole rather than a Seat at a time, because that is what an edit to a Shape
// is: a Role dropped from the list is a Role the Shape no longer asks for, and
// there is no way to say that one Seat at a time. Sending nothing empties it.
//
// Nothing this validates is about a rota that exists. Editing the settings
// changes what the *next* rota starts from; the Shifts of a rota already
// defined hold their own copy and are untouched by it (issue #137).
func SaveDefaultShape(
	ctx context.Context,
	store DefaultShapeWriteStore,
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

	rows := make([]db.DefaultShapeSeat, 0, len(stated))
	for _, seat := range stated {
		rows = append(rows, db.DefaultShapeSeat{RoleID: seat.RoleID, Seats: seat.Seats})
	}

	if err := store.SaveDefaultShape(ctx, rows); err != nil {
		return nil, fmt.Errorf("failed to save the default shape: %w", err)
	}

	logger.Info("Default shape saved", zap.Int("roles", len(rows)))

	return resolveShape(storedSeats(rows), roles, "the default shape")
}

// convertShape renders a Shape as the Seats the solver receives. A Seat names
// its Role the way the roster, the pins and the solver all name one: by name.
func convertShape(shape model.Shape) []allocator.Seat {
	seats := make([]allocator.Seat, 0, len(shape))
	for _, seat := range shape {
		seats = append(seats, allocator.Seat{Role: seat.Role.Name, Count: seat.Count})
	}
	return seats
}
