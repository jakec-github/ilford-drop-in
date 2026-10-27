package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/shiftpattern"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// Planning a rota is working out which Shifts defining one now would mint
// (ADR 0013): the next however many the Shift pattern generates after the
// exact start the previous rota stopped after, none before an optional date.
//
// It is its own step, rather than the first half of DefineRota, because the
// Define Rota screen shows the answer before anybody presses the button — the
// Shifts, where the last rota stopped, and why it would be refused — and that
// preview must be the same arithmetic as the definition, not a second copy of
// it in the browser.

// tailLength is how many of the previous rota's last Shifts a plan carries, so
// the screen can show the new rota following on from them.
const tailLength = 3

// maxShiftCount bounds one rota. Far past any rota anybody defines, and low
// enough that a mistyped count does not mint a decade of Shifts.
const maxShiftCount = 366

// PlanRotaStore is what planning a rota reads: the pattern and its length, the
// Overrides that change what it generates, the rotas that exist, to carry on
// from the latest, that rota's Shifts for its tail, and the Shifts already
// running at the planned times.
type PlanRotaStore interface {
	RotaDefaultsStore
	OverrideStore
	GetRotations(ctx context.Context) ([]db.Rotation, error)
	GetShiftsByRotaID(ctx context.Context, rotaID string) ([]db.Shift, error)
	GetShiftsOverlapping(ctx context.Context, startAt, endAt string) ([]db.Shift, error)
}

// RotaPlan is the rota defining one now would make.
type RotaPlan struct {
	// PatternSet is whether the Rota Defaults hold a Shift pattern at all.
	// Without one nothing can be planned, and the screen says where to set it.
	PatternSet bool
	// Previous is the rota this one carries on from, nil for the first.
	Previous *PreviousRota
	// Shifts are the Shifts that would be minted, in order, with no id or
	// rota yet: they do not exist until the rota is defined.
	Shifts []db.Shift
	// Overridden is what the Overrides make of each of Shifts, aligned with
	// it, and Overrides how far each Override reached, in list order. Both are
	// empty when the pattern could not fill the rota.
	Overridden []OverriddenShift
	Overrides  []OverrideReach
	// Refusal is why this rota cannot be defined as it stands — no pattern, a
	// pattern that has run out, a clash with a Shift that already exists — and
	// nil when it can. A plan with a refusal still carries whatever Shifts
	// could be worked out, so the screen can show the clash it names.
	Refusal error
}

// PreviousRota is where the rota before this one stopped.
type PreviousRota struct {
	ShiftCount int
	// StoppedAfter is the generated start of its last Shift, in
	// model.ShiftTimestampLayout.
	StoppedAfter string
	// Tail is its last few Shifts, in order, as they now stand.
	Tail []db.Shift
}

// PlanRota works out the rota defining one now would make.
//
// The error is for a request that cannot be planned at all — a count that is
// not a count, a date that is not a date — or a store that failed. Everything
// that only stops the rota being defined is the plan's Refusal.
func PlanRota(ctx context.Context, store PlanRotaStore, params DefineRotaParams) (*RotaPlan, error) {
	if params.ShiftCount <= 0 || params.ShiftCount > maxShiftCount {
		return nil, wrapf(ErrInvalidInput, "a rota has from 1 to %d shifts, not %d", maxShiftCount, params.ShiftCount)
	}
	var notBefore time.Time
	if params.NotBefore != "" {
		parsed, err := time.Parse("2006-01-02", params.NotBefore)
		if err != nil {
			return nil, wrapf(ErrInvalidInput, "%q is not a date - write not before as 2026-08-02", params.NotBefore)
		}
		notBefore = parsed
	}

	defaults, err := RotaDefaults(ctx, store)
	if err != nil {
		return nil, err
	}

	plan := &RotaPlan{PatternSet: defaults.HasShiftPattern()}
	plan.Previous, err = previousRota(ctx, store)
	if err != nil {
		return nil, err
	}

	if !plan.PatternSet {
		plan.Refusal = wrapf(ErrInvalidInput,
			"no shift pattern has been set, so there is nothing to say when shifts run; write one in the rota defaults before defining a rota")
		return plan, nil
	}

	pattern, err := compileStoredPattern(defaults)
	if err != nil {
		plan.Refusal = wrapf(ErrInvalidInput,
			"the shift pattern in the rota defaults cannot be used - %s; correct it before defining a rota", err)
		return plan, nil
	}

	var after time.Time
	if plan.Previous != nil {
		after, err = time.Parse(model.ShiftTimestampLayout, plan.Previous.StoppedAfter)
		if err != nil {
			return nil, fmt.Errorf("failed to read where the previous rota stopped: %w", err)
		}
	}

	length := time.Duration(defaults.ShiftLengthMinutes) * time.Minute
	for _, start := range pattern.Next(after, notBefore, params.ShiftCount) {
		plan.Shifts = append(plan.Shifts, db.Shift{
			Date:    start.Format("2006-01-02"),
			StartAt: start.Format(model.ShiftTimestampLayout),
			EndAt:   start.Add(length).Format(model.ShiftTimestampLayout),
		})
	}

	if len(plan.Shifts) < params.ShiftCount {
		plan.Refusal = ranOut(len(plan.Shifts), params.ShiftCount)
		return plan, nil
	}

	overrides, err := store.GetOverrides(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch overrides: %w", err)
	}
	plan.Overridden, plan.Overrides, err = applyOverrides(overrides, plan.Shifts)
	if errors.Is(err, ErrInvalidInput) {
		plan.Refusal = err
		return plan, nil
	}
	if err != nil {
		return nil, err
	}

	plan.Refusal, err = clash(ctx, store, plan.Shifts)
	if err != nil {
		return nil, err
	}
	return plan, nil
}

// compileStoredPattern reads the pattern the Rota Defaults hold. It was valid
// when it was saved, so a refusal here is a pattern written by an older build,
// or by hand.
func compileStoredPattern(defaults model.RotaDefaults) (*shiftpattern.Pattern, error) {
	startFrom, err := time.Parse("2006-01-02", defaults.PatternStartFrom)
	if err != nil {
		return nil, fmt.Errorf("its start from %q is not a date", defaults.PatternStartFrom)
	}
	return shiftpattern.Compile(defaults.ShiftPattern, startFrom)
}

// previousRota finds the rota that stopped latest, which is the one the next
// carries on from. Latest by where it stopped rather than by when it was
// defined: they are the same rota unless somebody has been defining rotas out
// of order by hand, and then carrying on from the furthest one is the only
// choice that cannot mint a Shift twice.
func previousRota(ctx context.Context, store PlanRotaStore) (*PreviousRota, error) {
	rotations, err := store.GetRotations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch rotations: %w", err)
	}

	var latest *db.Rotation
	for i, r := range rotations {
		if latest == nil || r.StoppedAfter > latest.StoppedAfter {
			latest = &rotations[i]
		}
	}
	if latest == nil {
		return nil, nil
	}

	shifts, err := store.GetShiftsByRotaID(ctx, latest.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch the previous rota's shifts: %w", err)
	}
	if len(shifts) > tailLength {
		shifts = shifts[len(shifts)-tailLength:]
	}

	return &PreviousRota{
		ShiftCount:   latest.ShiftCount,
		StoppedAfter: latest.StoppedAfter,
		Tail:         shifts,
	}, nil
}

// ranOut refuses a rota the pattern cannot fill: a COUNT or an UNTIL has run
// out, or nothing generates within shiftpattern.Horizon.
func ranOut(found, wanted int) error {
	if found == 0 {
		return wrapf(ErrInvalidInput,
			"the shift pattern generates no more shifts - it has run out; change it in the rota defaults")
	}
	return wrapf(ErrInvalidInput,
		"the shift pattern generates only %d more %s, not the %d asked for; ask for fewer, or change the pattern in the rota defaults",
		found, plural(found, "shift", "shifts"), wanted)
}

// clash refuses a plan any of whose Shifts would run at the same moment as
// another: one already defined, or one of its own neighbours, which a length
// longer than the gap between two generated starts makes. It names the first
// clash, since that is the one to fix first.
func clash(ctx context.Context, store PlanRotaStore, shifts []db.Shift) (error, error) {
	for i := 1; i < len(shifts); i++ {
		if shifts[i-1].EndAt > shifts[i].StartAt {
			return wrapf(ErrConflict,
				"the shift on %s would still be running when the one on %s starts; shorten the shift length or change the pattern in the rota defaults",
				shiftSpan(shifts[i-1]), shiftSpan(shifts[i])), nil
		}
	}

	existing, err := store.GetShiftsOverlapping(ctx, shifts[0].StartAt, shifts[len(shifts)-1].EndAt)
	if err != nil {
		return nil, err
	}
	for _, s := range shifts {
		for _, other := range existing {
			// The layout sorts as it reads, so comparing the strings compares
			// the times.
			if s.StartAt < other.EndAt && other.StartAt < s.EndAt {
				return wrapf(ErrConflict,
					"the shift on %s would overlap the one already defined on %s, and two shifts cannot run at once; leave a gap with not before, or change the pattern in the rota defaults",
					shiftSpan(s), shiftSpan(other)), nil
			}
		}
	}
	return nil, nil
}
