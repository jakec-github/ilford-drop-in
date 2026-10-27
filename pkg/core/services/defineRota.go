package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// RotaResult represents the result of defining a new rota: the rotation, the
// shifts it minted, in date order, and the Pins its Overrides seeded. Callers get the shifts themselves rather than bare
// dates, because a shift's id is its identity everywhere downstream (ADR 0001).
type RotaResult struct {
	Rotation *db.Rotation
	Shifts   []db.Shift
	// Pins are ordinary Pins from the moment they are
	// written: nothing marks them as having been seeded, and an Organiser may
	// remove any of them (issue #131).
	Pins []db.Pin
	// Asked is how many volunteers were given a link by the round this
	// definition opened. Zero where the roster could not be read — the rota is
	// defined either way, and the Allocation tab offers to start the round.
	Asked int
}

// DefineRotaParams is the rota an Organiser has decided to make: how many
// Shifts, and optionally a date none of them may start before.
//
// Neither the dates nor the hours are here. Which Shifts come next is the Shift
// pattern's to say (ADR 0013): the rota carries on from the exact Shift the
// previous one stopped after, so the only thing left to decide is how far it
// runs, and whether to leave a gap first.
type DefineRotaParams struct {
	ShiftCount int
	// NotBefore is a date, "2006-01-02", before which no Shift of this rota
	// starts: a break between rotas. Empty means no gap.
	NotBefore string
}

// DefineRotaStore defines the database operations needed for defining a rota:
// everything planning one reads, Overrides included, the default Shape each
// Shift is minted with a copy of (issue #137) and the Roles that come with it,
// and the links the round is opened with.
type DefineRotaStore interface {
	PlanRotaStore
	DefaultShapeStore
	MintRequestsStore
	InsertDefinedRota(ctx context.Context, rotation *db.Rotation, shifts []db.Shift, pins []db.Pin, requirements []db.ShiftRequirement) error
}

// DefineRota creates the rota an Organiser has asked for and mints the next
// Shifts its Shift pattern generates, each with a copy of the default Shape,
// then applies the Overrides to the Shifts their rules match: their Shapes in
// place of the default, and their Pins. It then opens the rota's availability
// round, giving every active volunteer their link.
//
// The round is opened here rather than being an Organiser's next click because
// there is no rota a round is not wanted for: asking is how a rota is staffed,
// and a defined rota with nobody asked is a rota nothing can happen to. It also
// means every rota has a round from the moment it exists, which is what lets
// the draft solve from the first read rather than refusing until somebody has
// pressed a button (issue #188).
//
// Minting stays separate from *sending*: the links are written, and not one
// email goes out until an Organiser sends the round with a deadline in it.
func DefineRota(
	ctx context.Context,
	database DefineRotaStore,
	volunteerClient VolunteerClient,
	organisation *config.Organisation,
	logger *zap.Logger,
	params DefineRotaParams,
) (*RotaResult, error) {
	plan, err := PlanRota(ctx, database, params)
	if err != nil {
		return nil, err
	}
	if plan.Refusal != nil {
		return nil, plan.Refusal
	}

	logger.Debug("Defining new rota",
		zap.Int("shift_count", len(plan.Shifts)),
		zap.String("not_before", params.NotBefore))

	// The Shape each minted Shift will ask for, copied onto every one of them
	// below. An unset one is a refusal: a rota whose shifts ask for nobody
	// solves perfectly and staffs nothing. One Shift can be emptied afterwards,
	// deliberately (issue #138) — a week the Organisation is shut — but a whole
	// rota of them is a mistake rather than an intention.
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
	// not committed yet is invisible to this read, but the no-overlap
	// constraint refuses whichever of the two commits second, since both carry
	// on from the same stop point and so mint the same hours (hazard B1,
	// InsertDefinedRota).
	if inFlight := unallocatedRota(rotations); inFlight != nil {
		return nil, wrapf(ErrConflict,
			"a rota is already in flight - the one running %s to %s has not been allocated yet; allocate it or discard it before defining another",
			inFlight.Start, inFlight.End)
	}

	rotation := &db.Rotation{ID: uuid.New().String()}
	shifts := make([]db.Shift, len(plan.Shifts))
	for i, planned := range plan.Shifts {
		planned.ID = uuid.New().String()
		planned.RotaID = rotation.ID
		shifts[i] = planned
	}

	// The rotation's span is its shifts' span (ADR 0001): setting it here means
	// the returned rotation reads the same as one fetched back from the store,
	// which derives both ends from the shift rows. Where it stopped is the
	// generated start of its last Shift, which is the one thing about it the
	// next rota needs.
	last := shifts[len(shifts)-1]
	rotation.Start = shifts[0].Date
	rotation.End = last.Date
	rotation.ShiftCount = len(shifts)
	rotation.StoppedAfter = last.StartAt

	// Copy the default Shape onto every Shift, or the Shape the Overrides gave
	// it. From here it is that Shift's Shape and nothing else's: editing it
	// changes one evening, and editing the setting it was copied from changes
	// what the *next* rota asks for. That is the whole of issue #137, and the
	// reason the copy is made here rather than being resolved from the
	// settings on every read.
	//
	// The Overrides' Pins become ordinary Pins on the same Shifts: nothing marks
	// them as seeded, and an Organiser may remove any of them.
	requirements := make([]db.ShiftRequirement, 0, len(shifts)*len(shape))
	var pins []db.Pin
	for i, s := range shifts {
		overridden := plan.Overridden[i]
		if overridden.SetsShape {
			for _, seat := range overridden.Seats {
				requirements = append(requirements, db.ShiftRequirement{
					ShiftID: s.ID,
					RoleID:  seat.RoleID,
					Seats:   seat.Seats,
				})
			}
		} else {
			for _, seat := range shape {
				requirements = append(requirements, db.ShiftRequirement{
					ShiftID: s.ID,
					RoleID:  seat.Role.ID,
					Seats:   seat.Count,
				})
			}
		}
		for _, pin := range overridden.Pins {
			pins = append(pins, db.Pin{
				ID:          uuid.New().String(),
				ShiftID:     s.ID,
				RoleID:      pin.RoleID,
				VolunteerID: pin.VolunteerID,
				CustomValue: pin.CustomValue,
			})
		}
	}

	// Insert the rotation, its shifts, their Shapes and its seeded pins
	// atomically, so a rota can never exist half-formed.
	if err := database.InsertDefinedRota(ctx, rotation, shifts, pins, requirements); err != nil {
		// The no-overlap constraint answering. PlanRota has already looked for
		// a clash, so this is a Shift written between that read and this
		// write — another rota defined at the same moment.
		if errors.Is(err, db.ErrShiftOverlap) {
			return nil, wrapf(ErrConflict,
				"another shift was defined at one of these times while this rota was being defined - reload and try again")
		}
		return nil, fmt.Errorf("failed to insert rotation and shifts: %w", err)
	}

	logger.Debug("Rotation created successfully",
		zap.String("rotation_id", rotation.ID),
		zap.Int("shift_count", len(shifts)),
		zap.Int("pin_count", len(pins)),
		zap.String("first_shift", shifts[0].StartAt),
		zap.String("stopped_after", rotation.StoppedAfter))

	return &RotaResult{
		Rotation: rotation,
		Shifts:   shifts,
		Pins:     pins,
		Asked:    openRound(ctx, database, volunteerClient, organisation, logger, rotation.ID),
	}, nil
}

// openRound gives every active volunteer their link for the rota just defined,
// and reports how many were asked.
//
// It cannot fail the definition, which is why it returns no error. The rota is
// committed by the time it runs, and the one thing likely to go wrong here is
// the roster read — a Google Sheets hiccup, or a deployment with no credentials
// — which is a reason to be short a round, never a reason to lose a rota that
// is already on the database. Minting is idempotent and the Allocation tab
// offers it as "Start round" whenever a rota has none, so the recovery is a
// click and the screen already shows that it is needed.
func openRound(
	ctx context.Context,
	database DefineRotaStore,
	volunteerClient VolunteerClient,
	organisation *config.Organisation,
	logger *zap.Logger,
	rotaID string,
) int {
	roles, err := RoleTable(ctx, database)
	if err != nil {
		logger.Warn("Defined the rota but could not read the Roles to open its round",
			zap.String("rotation_id", rotaID), zap.Error(err))
		return 0
	}

	volunteers, err := volunteerClient.ListVolunteers(organisation, roles)
	if err != nil {
		logger.Warn("Defined the rota but could not read the roster to open its round",
			zap.String("rotation_id", rotaID), zap.Error(err))
		return 0
	}

	asked, err := mintRequestsFor(ctx, database, logger, rotaID, volunteers)
	if err != nil {
		logger.Warn("Defined the rota but could not open its round",
			zap.String("rotation_id", rotaID), zap.Error(err))
		return 0
	}
	return asked
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
