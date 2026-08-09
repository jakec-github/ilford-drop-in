package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// RotaResult represents the result of defining a new rota: the rotation, the
// shifts it minted, in date order, and the Preallocations its Standing
// Preallocations seeded. Callers get the shifts themselves rather than bare
// dates, because a shift's id is its identity everywhere downstream (ADR 0001).
type RotaResult struct {
	Rotation *db.Rotation
	Shifts   []db.Shift
	// Preallocations are ordinary Preallocations from the moment they are
	// written: nothing marks them as having been seeded, and an admin may
	// remove any of them (issue #131).
	Preallocations []db.Preallocation
}

// DefineRotaParams is the rota an admin has decided to make: how many shifts,
// and from when.
//
// The hours and the Shape are not here. They are the Rota Defaults, and the
// Rota Defaults are the only place they are stated (issue #176) — defining
// spends them rather than restating them. What made them fields of this request
// was issue #140, which put the whole rota on the define form; the two ways to
// say the same thing turned out to be one way too many, so the form now shows
// the settings card itself and the settings are what the shifts are minted from.
//
// The shift count and the start date stay stated, because neither is a setting:
// how long the next rota runs is a decision nobody has made yet, and a rota can
// begin after a break rather than the week after the last one.
type DefineRotaParams struct {
	ShiftCount int
	// StartDate is the first shift's date, in "2006-01-02". Its weekday is the
	// cadence: shifts are minted weekly from it. Deliberately stated rather
	// than derived, so a rota can start after a break (issue #140).
	StartDate string
}

// DefineRotaStore defines the database operations needed for defining a rota.
// Defining a rota is where the Rota Defaults are spent, so it reads all of
// them: the shift times, because minting a Shift means deciding when it runs
// (ADR 0007), and the default Shape, which each Shift is minted with a copy of
// (issue #137). The Roles come with the Shape, because a Seat names a Role by
// id while the pins seeded beside it record its name; the rotations, because
// one rota is in flight at a time and this is where that is enforced.
type DefineRotaStore interface {
	RotaDefaultsStore
	DefaultShapeStore
	GetRotations(ctx context.Context) ([]db.Rotation, error)
	GetStandingPreallocations(ctx context.Context) ([]db.StandingPreallocation, error)
	InsertDefinedRota(ctx context.Context, rotation *db.Rotation, shifts []db.Shift, preallocations []db.Preallocation, requirements []db.ShiftRequirement) error
}

// definition is a validated DefineRotaParams: the same rota, in the spellings
// the database holds and with every question about it already answered.
type definition struct {
	shiftCount int
	startDate  time.Time
}

// validate reads a stated rota, or says why it will not.
//
// Only the two fields an admin states are checked here. The hours and the Shape
// are read from the settings, which were validated where they were written, and
// what defining asks of them is whether they were written at all.
func (p DefineRotaParams) validate() (definition, error) {
	if p.ShiftCount <= 0 {
		return definition{}, wrapf(ErrInvalidInput, "shift count must be positive, got %d", p.ShiftCount)
	}

	startDate, err := time.Parse("2006-01-02", p.StartDate)
	if err != nil {
		return definition{}, wrapf(ErrInvalidInput,
			"%q is not a date — write the first shift's date as 2026-08-02", p.StartDate)
	}

	return definition{shiftCount: p.ShiftCount, startDate: startDate}, nil
}

// DefineRota creates the rota an admin has stated and mints its weekly shifts,
// each carrying the default hours and a copy of the default Shape, with the
// Standing Preallocations seeded onto the Shifts their rules land on.
func DefineRota(ctx context.Context, database DefineRotaStore, logger *zap.Logger, params DefineRotaParams) (*RotaResult, error) {
	stated, err := params.validate()
	if err != nil {
		return nil, err
	}

	logger.Debug("Defining new rota",
		zap.Int("shift_count", stated.shiftCount),
		zap.String("start_date", params.StartDate))

	// The hours each minted shift will run come from the Rota Defaults, and
	// nothing can be minted without them: a Shift's date is the date of its
	// start, so a Shift with no start is not a Shift with unknown hours, it is
	// a Shift on no day at all (issue #135, ADR 0007).
	//
	// This is the second path incomplete settings block, alongside allocation,
	// and it is a narrowing of ADR 0006's "allocation and nothing else". The
	// reason is the same one that made allocation the exception: defining a rota
	// is an act that creates something people are told to turn up to, not a page
	// that renders. Everything that only reads still reads.
	defaults, err := RotaDefaults(ctx, database)
	if err != nil {
		return nil, err
	}
	if missing := defaults.MissingShiftTimes(); len(missing) > 0 {
		return nil, wrapf(ErrInvalidInput,
			"the drop-in's settings are incomplete - %s %s not been set; set the rota defaults before defining a rota",
			joinWithAnd(missing), plural(len(missing), "has", "have"))
	}

	// The Shape each minted Shift will ask for, copied onto every one of them
	// below. An unset one is a refusal for the same reason the times are: a
	// rota whose shifts ask for nobody solves perfectly and staffs nothing.
	// One Shift can be emptied afterwards, deliberately (issue #138) — a week
	// the drop-in is shut — but a whole rota of them is a mistake rather than
	// an intention.
	shape, err := DefaultShape(ctx, database)
	if err != nil {
		return nil, err
	}
	if len(shape) == 0 {
		return nil, wrapf(ErrInvalidInput,
			"the default shape has not been set, so there is nothing for these shifts to ask for; state it in the rota defaults before defining a rota")
	}

	rotations, err := database.GetRotations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch rotations: %w", err)
	}

	// One rota is in flight at a time (issue #139). Everything downstream — the
	// availability round, the draft allocation, the rota screen — addresses "the
	// rota" without a picker, and that only reads as one thing while at most one
	// Rotation is unallocated. Refusing here is what makes it true.
	//
	// Read-then-insert, without a lock, is enough: a concurrent define that has
	// not committed yet is invisible to this read, but the one-Shift-per-date
	// unique index refuses whichever of the two commits second wherever their
	// dates overlap (hazard B1, InsertDefinedRota).
	if inFlight := unallocatedRota(rotations); inFlight != nil {
		return nil, wrapf(ErrConflict,
			"a rota is already in flight - the one running %s to %s has not been allocated yet; allocate it or discard it before defining another",
			inFlight.Start, inFlight.End)
	}

	rotation := &db.Rotation{
		ID:         uuid.New().String(),
		Start:      stated.startDate.Format("2006-01-02"),
		ShiftCount: stated.shiftCount,
	}

	// Mint this rotation's shifts, weekly from the stated start. Rota
	// definition is the sole place shift-date arithmetic lives, and the cadence
	// is not offered: anything richer than "the same weekday, every week" would
	// put pressure on the one-Shift-per-date rule.
	shifts := make([]db.Shift, stated.shiftCount)
	for i := range shifts {
		date := stated.startDate.AddDate(0, 0, 7*i).Format("2006-01-02")

		startAt, endAt, err := model.ShiftTimestamps(date, defaults.ShiftStartTime, defaults.ShiftEndTime)
		if err != nil {
			return nil, fmt.Errorf("failed to derive shift times for %s: %w", date, err)
		}

		shifts[i] = db.Shift{
			ID:      uuid.New().String(),
			RotaID:  rotation.ID,
			Date:    date,
			StartAt: startAt,
			EndAt:   endAt,
		}
	}

	// The rotation's span is its shifts' span (ADR 0001): setting End here means
	// the returned rotation reads the same as one fetched back from the store,
	// which derives both ends from the shift rows.
	rotation.End = shifts[len(shifts)-1].Date

	// Copy the default Shape onto every Shift. From here it is that Shift's
	// Shape and nothing else's: editing it changes one evening, and editing the
	// setting it was copied from changes what the *next* rota asks for. That is
	// the whole of issue #137 in three lines, and the reason the copy is made
	// here rather than being resolved from the settings on every read.
	requirements := make([]db.ShiftRequirement, 0, len(shifts)*len(shape))
	for _, s := range shifts {
		for _, seat := range shape {
			requirements = append(requirements, db.ShiftRequirement{
				ShiftID: s.ID,
				RoleID:  seat.Role.ID,
				Seats:   seat.Count,
			})
		}
	}

	// Spend the Standing Preallocations: they become ordinary Preallocations on
	// the Shifts their rules land on. Defining is the only moment they are read,
	// which is what makes them a convenience rather than a standing fact —
	// editing one later changes what the next rota starts from, never what this
	// one holds.
	standing, err := database.GetStandingPreallocations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch standing preallocations: %w", err)
	}
	// A pin names its Role by id and records its name, so the Roles are read
	// here rather than being read off the Shape: a Standing Preallocation may
	// promise a Role this rota's Shape does not name.
	roles, err := RoleTable(ctx, database)
	if err != nil {
		return nil, err
	}
	preallocations, err := seedPreallocations(standing, shifts, roles, logger)
	if err != nil {
		return nil, err
	}

	// Insert the rotation, its shifts, their Shapes and its seeded pins
	// atomically, so a rota can never exist half-formed.
	if err := database.InsertDefinedRota(ctx, rotation, shifts, preallocations, requirements); err != nil {
		// The one-Shift-per-date index answering. Reachable by hand now that the
		// start date is an admin's to state — a rota begun a week too early
		// overlaps the last one — so it is answered as the ordinary mistake it
		// is rather than as a failed insert.
		if errors.Is(err, db.ErrShiftDateTaken) {
			return nil, datesAlreadyTaken(shifts, rotations)
		}
		return nil, fmt.Errorf("failed to insert rotation and shifts: %w", err)
	}

	logger.Debug("Rotation created successfully",
		zap.String("rotation_id", rotation.ID),
		zap.Int("shift_count", stated.shiftCount),
		zap.Int("preallocation_count", len(preallocations)),
		zap.String("first_shift", shifts[0].Date),
		zap.String("last_shift", shifts[len(shifts)-1].Date))

	return &RotaResult{
		Rotation:       rotation,
		Shifts:         shifts,
		Preallocations: preallocations,
	}, nil
}

// datesAlreadyTaken says that this rota would have run on a day the drop-in
// already runs, and points at the rota in the way.
//
// The index names no date, so this works out which day it must have been: the
// first minted date that falls inside a rota that already exists. That is the
// day an admin has to move off, and naming the wrong one would be worse than
// naming none — so where nothing overlaps, the refusal says only what is
// certain.
func datesAlreadyTaken(shifts []db.Shift, rotations []db.Rotation) error {
	for _, s := range shifts {
		for _, r := range rotations {
			if r.Start <= s.Date && s.Date <= r.End {
				return wrapf(ErrConflict,
					"the drop-in already runs on %s, and it cannot run twice on one day - the rota running %s to %s covers it, so start after %s",
					readableDate(s.Date), readableDate(r.Start), readableDate(r.End), readableDate(r.End))
			}
		}
	}
	return wrapf(ErrConflict,
		"one of those dates already has a shift on it, and the drop-in cannot run twice on one day - start the rota on a different date")
}

// unallocatedRota returns the earliest-starting rota that has not been
// allocated, or nil when every one has. Earliest rather than any, so that a
// deployment which somehow holds two is told about the one that has to be dealt
// with first.
func unallocatedRota(rotations []db.Rotation) *db.Rotation {
	var earliest *db.Rotation
	for i, r := range rotations {
		if r.AllocatedDatetime != "" {
			continue
		}
		if earliest == nil || r.Start < earliest.Start {
			earliest = &rotations[i]
		}
	}
	return earliest
}
