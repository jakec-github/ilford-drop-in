package services

import (
	"context"
	"errors"
	"fmt"
	"sort"
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
	// Shifts are the Shifts that would be minted, in the order they start,
	// with no id or rota yet: they do not exist until the rota is defined.
	// Each is where the Overrides put it and as long as they make it.
	Shifts []db.Shift
	// Generated is the start the pattern generated for each of Shifts,
	// aligned with it. It differs from the Shift's own start only where an
	// Override moved it, and it is what the next rota carries on from.
	Generated []string
	// Clashing marks, aligned with Shifts, each that would run at the same
	// moment as another, so the screen can show every one and not only the
	// first the Refusal names. Nil when none does.
	Clashing []bool
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

	// Each Shift runs as long as the entry that made it (ADR 0014).
	for _, start := range pattern.Next(after, notBefore, params.ShiftCount) {
		plan.Shifts = append(plan.Shifts, db.Shift{
			Date:    start.At.Format("2006-01-02"),
			StartAt: start.At.Format(model.ShiftTimestampLayout),
			EndAt:   start.At.Add(start.Length).Format(model.ShiftTimestampLayout),
		})
		plan.Generated = append(plan.Generated, start.At.Format(model.ShiftTimestampLayout))
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
	if err := moveOverridden(plan); err != nil {
		return nil, err
	}

	plan.Clashing, plan.Refusal, err = clash(ctx, store, plan.Shifts)
	if err != nil {
		return nil, err
	}
	return plan, nil
}

// moveOverridden puts each Shift where the Overrides say and makes it as long
// as they say, then puts the Shifts back in the order they start, keeping
// everything aligned with them. A Shift is moved within its own day — the
// start time is a time of that date, in the pattern's wall-clock time — and
// runs on from there, past midnight if it is long enough. An Override that
// sets no length leaves the Shift as long as its entry made it.
func moveOverridden(plan *RotaPlan) error {
	moved := false
	for i := range plan.Shifts {
		o := plan.Overridden[i]
		if o.StartTime == "" && o.LengthMinutes == 0 {
			continue
		}
		s := &plan.Shifts[i]
		start, err := time.Parse(model.ShiftTimestampLayout, s.StartAt)
		if err != nil {
			return fmt.Errorf("planned shift has an unreadable start %q: %w", s.StartAt, err)
		}
		end, err := time.Parse(model.ShiftTimestampLayout, s.EndAt)
		if err != nil {
			return fmt.Errorf("planned shift has an unreadable end %q: %w", s.EndAt, err)
		}
		length := end.Sub(start)
		if o.StartTime != "" {
			start, err = time.Parse("2006-01-02 15:04", s.Date+" "+o.StartTime)
			if err != nil {
				return fmt.Errorf("override start time %q cannot be read: %w", o.StartTime, err)
			}
			moved = true
		}
		if o.LengthMinutes != 0 {
			length = time.Duration(o.LengthMinutes) * time.Minute
		}
		s.StartAt = start.Format(model.ShiftTimestampLayout)
		s.EndAt = start.Add(length).Format(model.ShiftTimestampLayout)
	}
	if !moved {
		return nil
	}

	order := make([]int, len(plan.Shifts))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return plan.Shifts[order[a]].StartAt < plan.Shifts[order[b]].StartAt
	})
	shifts := make([]db.Shift, len(order))
	generated := make([]string, len(order))
	overridden := make([]OverriddenShift, len(order))
	for to, from := range order {
		shifts[to] = plan.Shifts[from]
		generated[to] = plan.Generated[from]
		overridden[to] = plan.Overridden[from]
	}
	plan.Shifts, plan.Generated, plan.Overridden = shifts, generated, overridden
	return nil
}

// compileStoredPattern reads the pattern the Rota Defaults hold. It was valid
// when it was saved, so a refusal here is a pattern written by an older build,
// or by hand.
func compileStoredPattern(defaults model.RotaDefaults) (*shiftpattern.Pattern, error) {
	return shiftpattern.Compile(defaults.ShiftPattern, defaults.Timezone())
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
// another: one already defined, or one of its own, which a length longer than
// the gap between two starts makes, or an Override moving one onto another. It
// marks every Shift that clashes and names the first clash, since that is the
// one to fix first. The Shifts are in the order they start.
func clash(ctx context.Context, store PlanRotaStore, shifts []db.Shift) ([]bool, error, error) {
	var marks []bool
	mark := func(i int) {
		if marks == nil {
			marks = make([]bool, len(shifts))
		}
		marks[i] = true
	}
	var refusal error

	// The layout sorts as it reads, so comparing the strings compares the
	// times. A long Shift can run past more than its next neighbour, so each
	// is checked against every earlier one still running.
	end := ""
	for i := range shifts {
		for j := 0; j < i; j++ {
			if shifts[j].EndAt > shifts[i].StartAt {
				mark(i)
				mark(j)
				if refusal == nil {
					refusal = wrapf(ErrConflict,
						"the shift on %s would still be running when the one on %s starts; shorten it, or change the pattern or an override in the rota defaults",
						shiftSpan(shifts[j]), shiftSpan(shifts[i]))
				}
			}
		}
		if shifts[i].EndAt > end {
			end = shifts[i].EndAt
		}
	}

	existing, err := store.GetShiftsOverlapping(ctx, shifts[0].StartAt, end)
	if err != nil {
		return nil, nil, err
	}
	for i, s := range shifts {
		for _, other := range existing {
			if s.StartAt < other.EndAt && other.StartAt < s.EndAt {
				mark(i)
				if refusal == nil {
					refusal = wrapf(ErrConflict,
						"the shift on %s would overlap the one already defined on %s, and two shifts cannot run at once; leave a gap with not before, or change the pattern or an override in the rota defaults",
						shiftSpan(s), shiftSpan(other))
				}
			}
		}
	}
	return marks, refusal, nil
}
