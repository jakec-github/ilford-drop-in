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
// from when, running what hours, asking for whom.
//
// Every field is stated rather than resolved from the Rota Defaults, and that
// is the whole of issue #140. The settings are what the define form *starts
// from* — RotaProposal reads them — and an admin may change any of them before
// submitting. Minting from the request rather than re-reading the settings is
// what makes the rota that appears the rota that was shown: the same principle
// ADR 0008 applies to allocating one.
//
// Nothing here writes back to the Rota Defaults. An admin who wants the next
// rota to start from different hours says so on the settings screen; changing
// them here changes this rota and no other.
type DefineRotaParams struct {
	ShiftCount int
	// StartDate is the first shift's date, in "2006-01-02". Its weekday is the
	// cadence: shifts are minted weekly from it. Deliberately stated rather
	// than derived, so a rota can start after a break (issue #140).
	StartDate string
	// ShiftStartTime and ShiftEndTime are the hours every minted Shift runs,
	// as times of day in model.ShiftTimeLayout ("19:30"). They are written onto
	// each Shift's own date and stay editable per Shift afterwards.
	ShiftStartTime string
	ShiftEndTime   string
	// Shape is what every minted Shift asks for. Each Shift is minted with its
	// own copy (issue #137), so editing one afterwards changes that Shift alone.
	Shape []SeatParams
}

// DefineRotaStore defines the database operations needed for defining a rota.
// The Roles come with it because a stated Seat names a Role by id while the
// pins seeded beside it record its name; the rotations, because one rota is in
// flight at a time and this is where that is enforced.
//
// Notably absent are the Rota Defaults and the default Shape. Defining used to
// spend them; it now mints what the request states, and reading the settings is
// RotaProposal's job on the way in.
type DefineRotaStore interface {
	RoleStore
	GetRotations(ctx context.Context) ([]db.Rotation, error)
	GetStandingPreallocations(ctx context.Context) ([]db.StandingPreallocation, error)
	InsertDefinedRota(ctx context.Context, rotation *db.Rotation, shifts []db.Shift, preallocations []db.Preallocation, requirements []db.ShiftRequirement) error
}

// definition is a validated DefineRotaParams: the same rota, in the spellings
// the database holds and with every question about it already answered.
type definition struct {
	shiftCount int
	startDate  time.Time
	// startTime and endTime are the stated times of day, re-spelled from
	// whatever was sent.
	startTime string
	endTime   string
	shape     []storedSeat
}

// validate reads a stated rota, or says why it will not.
//
// The checks are in the order an admin filled the form in, so the first thing
// they are told about is the first thing they typed.
func (p DefineRotaParams) validate(roles model.Roles) (definition, error) {
	if p.ShiftCount <= 0 {
		return definition{}, wrapf(ErrInvalidInput, "shift count must be positive, got %d", p.ShiftCount)
	}

	startDate, err := time.Parse("2006-01-02", p.StartDate)
	if err != nil {
		return definition{}, wrapf(ErrInvalidInput,
			"%q is not a date — write the first shift's date as 2026-08-02", p.StartDate)
	}

	// A Shift's date is the date of its start (ADR 0007), so a rota with no
	// times is not a rota with unknown hours, it is a rota on no days at all.
	// The same rules as the settings screen's, because it is the same question.
	start, end, err := shiftTimesOfDay(p.ShiftStartTime, p.ShiftEndTime)
	if err != nil {
		return definition{}, err
	}

	shape, err := statedSeats(p.Shape, roles)
	if err != nil {
		return definition{}, err
	}
	// A Shift asking for nobody solves perfectly and staffs nothing. One can be
	// reached per Shift afterwards, deliberately (issue #138) — a week the
	// drop-in is shut — but a whole rota of them is a mistake rather than an
	// intention.
	if len(shape) == 0 {
		return definition{}, wrapf(ErrInvalidInput,
			"a rota's shifts have to ask for somebody; say how many of each role each shift needs")
	}

	return definition{
		shiftCount: p.ShiftCount,
		startDate:  startDate,
		startTime:  start,
		endTime:    end,
		shape:      shape,
	}, nil
}

// DefineRota creates the rota an admin has stated and mints its weekly shifts,
// each carrying the stated hours and a copy of the stated Shape, with the
// Standing Preallocations seeded onto the Shifts their rules land on.
func DefineRota(ctx context.Context, database DefineRotaStore, logger *zap.Logger, params DefineRotaParams) (*RotaResult, error) {
	roles, err := RoleTable(ctx, database)
	if err != nil {
		return nil, err
	}

	stated, err := params.validate(roles)
	if err != nil {
		return nil, err
	}

	logger.Debug("Defining new rota",
		zap.Int("shift_count", stated.shiftCount),
		zap.String("start_date", params.StartDate))

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

		startAt, endAt, err := model.ShiftTimestamps(date, stated.startTime, stated.endTime)
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

	// Copy the stated Shape onto every Shift. From here it is that Shift's Shape
	// and nothing else's: editing it changes one evening, and editing the
	// setting it was prefilled from changes what the *next* rota starts from.
	// That is the whole of issue #137 in three lines, and the reason the copy is
	// made here rather than being resolved from the settings on every read.
	requirements := make([]db.ShiftRequirement, 0, len(shifts)*len(stated.shape))
	for _, s := range shifts {
		for _, seat := range stated.shape {
			requirements = append(requirements, db.ShiftRequirement{
				ShiftID: s.ID,
				RoleID:  seat.RoleID,
				Seats:   seat.Seats,
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
