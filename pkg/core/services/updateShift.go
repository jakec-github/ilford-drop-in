package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// UpdateShiftStore is what editing one Shift needs. The read resolves the shift
// to its rota; the writes happen under that rota's row lock, so the
// frozen-after-allocation guard cannot be overtaken by an allocation landing
// between the two, and an edit moving two things at once cannot land half done.
type UpdateShiftStore interface {
	GetShiftByID(ctx context.Context, id string) (*db.ShiftInRange, error)
	WithRotaShiftLock(ctx context.Context, rotaIDs []string, fn func(store db.ShiftTxStore) error) error
}

// UpdateShiftParams is one per-Shift edit as a client states it. Every field is
// optional and an absent one is left alone, so a client changing when a Shift
// runs never has to restate whether it runs at all.
type UpdateShiftParams struct {
	// Closed shuts the Shift or opens it again. Nil leaves it as it is, which
	// is what makes "open it" distinguishable from "say nothing about it".
	Closed *bool
	// StartAt and EndAt are the Shift's own local wall-clock times, spelled
	// model.ShiftTimestampLayout — or without the seconds, which is what a
	// browser's datetime-local field sends. Both or neither: a start with no
	// end describes nothing.
	StartAt string
	EndAt   string
}

// stated reports whether the params ask for anything at all.
func (p UpdateShiftParams) stated() bool {
	return p.Closed != nil || p.StartAt != "" || p.EndAt != ""
}

// ShiftState is one Shift after an edit: what it now says about itself, and
// nothing about who is on it. The client re-reads the rota anyway, and a change
// should not have to assemble a projection that needs the roster.
type ShiftState struct {
	ID      string
	Date    string
	StartAt string
	EndAt   string
	Closed  bool
}

// UpdateShift changes one Shift.
//
// The two things it can change have opposite freeze rules, and that is the
// whole shape of this function. Being Closed is an allocator input, so it is
// frozen once the Rotation is allocated. A Shift's times are descriptive — the
// solver works in dates — so they stay editable afterwards (ADR 0007). The rule
// is not "everything freezes at allocation", it is "allocator inputs freeze at
// allocation".
//
// Setting something to what it already is succeeds rather than conflicting: the
// caller asked for a state, and the shift is in it.
func UpdateShift(
	ctx context.Context,
	store UpdateShiftStore,
	shiftID string,
	params UpdateShiftParams,
	logger *zap.Logger,
) (*ShiftState, error) {
	if !params.stated() {
		return nil, wrapf(ErrInvalidInput, "there is nothing to change: say whether the shift is closed, or when it runs")
	}

	startAt, endAt, err := params.times()
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

	updated := ShiftState{
		ID:      shift.ID,
		Date:    shift.Date,
		StartAt: shift.StartAt,
		EndAt:   shift.EndAt,
		Closed:  shift.Closed,
	}

	err = store.WithRotaShiftLock(ctx, []string{shift.RotaID}, func(tx db.ShiftTxStore) error {
		if params.Closed != nil {
			allocated, err := tx.RotaAllocated(ctx, shift.RotaID)
			if err != nil {
				return err
			}
			if allocated {
				// Said as what it means rather than as a rule: the rota has
				// been worked out around this shift running, or not.
				return wrapf(ErrConflict, "the rota covering %s has already been allocated, so its shifts cannot be closed or reopened", shift.Date)
			}

			written, err := tx.SetShiftClosed(ctx, shiftID, *params.Closed)
			if err != nil {
				return err
			}
			if !written {
				return wrapf(ErrNotFound, "shift %s not found", shiftID)
			}
			updated.Closed = *params.Closed
		}

		if startAt != "" {
			written, err := tx.SetShiftTimes(ctx, shiftID, startAt, endAt)
			if errors.Is(err, db.ErrShiftDateTaken) {
				return dateTakenConflict(startAt)
			}
			if err != nil {
				return err
			}
			if !written {
				// Lost a race with something that removed the shift under the
				// lock.
				return wrapf(ErrNotFound, "shift %s not found", shiftID)
			}
			// The date is the date of the start, so it moved with it.
			updated.StartAt, updated.EndAt = startAt, endAt
			updated.Date = dayOf(startAt)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	logger.Info("Shift updated",
		zap.String("shift_id", shiftID),
		zap.String("date", updated.Date),
		zap.String("start_at", updated.StartAt),
		zap.Bool("closed", updated.Closed))

	return &updated, nil
}

// times reads the start and end an edit states, in the layout the Shift stores
// them in, or says why it will not. Two empty strings mean the edit says
// nothing about when the Shift runs, and come back as two empty strings.
func (p UpdateShiftParams) times() (startAt, endAt string, err error) {
	if p.StartAt == "" && p.EndAt == "" {
		return "", "", nil
	}
	if p.StartAt == "" || p.EndAt == "" {
		return "", "", wrapf(ErrInvalidInput, "a shift needs a start and an end, and only one of them was given")
	}

	start, err := parseShiftTimestamp(p.StartAt, "start")
	if err != nil {
		return "", "", err
	}
	end, err := parseShiftTimestamp(p.EndAt, "end")
	if err != nil {
		return "", "", err
	}

	// Only that it ends after it starts. A Shift may run past midnight where
	// the default times may not, and it has to be able to: the migration that
	// made times mandatory gave the whole of their day to Shifts nobody had
	// ever stated times for, and an admin correcting one has to be able to save
	// what they see.
	if !end.After(start) {
		return "", "", wrapf(ErrInvalidInput, "a shift has to end after it starts, and %s is not after %s", p.EndAt, p.StartAt)
	}

	return start.Format(model.ShiftTimestampLayout), end.Format(model.ShiftTimestampLayout), nil
}

// parseShiftTimestamp reads one of a Shift's own times, naming which one it was
// when it cannot.
//
// The seconds are optional because a browser's datetime-local field leaves them
// off. An offset is refused because a Shift's times are wall-clock facts about
// Ilford rather than instants on a global timeline (ADR 0007): a value carrying
// "Z" is a different kind of thing, and reading it as though it were not would
// silently move the shift.
func parseShiftTimestamp(value, which string) (time.Time, error) {
	for _, layout := range []string{model.ShiftTimestampLayout, "2006-01-02T15:04"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, wrapf(ErrInvalidInput,
		"%q is not a date and time — write the %s as 2026-08-02T19:30, in the drop-in's own local time", value, which)
}

// dayOf is the date half of one of a Shift's timestamps. Safe on any value
// parseShiftTimestamp has accepted, which is the only kind that reaches it.
func dayOf(timestamp string) string {
	return timestamp[:len("2006-01-02")]
}

// dateTakenConflict turns the one-Shift-per-date index refusing a write into
// the refusal an admin reads, naming the day two shifts would have shared.
func dateTakenConflict(startAt string) error {
	return wrapf(ErrConflict,
		"the drop-in already runs on %s, and it cannot run twice on one day — move that shift first",
		readableDate(dayOf(startAt)))
}
