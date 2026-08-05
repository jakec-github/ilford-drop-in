package services

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// SetShiftClosedStore is what closing or reopening one Shift needs. The read
// resolves the shift to its rota; the write happens under that rota's row lock,
// so the frozen-after-allocation guard cannot be overtaken by an allocation
// landing between the two.
type SetShiftClosedStore interface {
	GetShiftByID(ctx context.Context, id string) (*db.ShiftInRange, error)
	WithRotaShiftLock(ctx context.Context, rotaIDs []string, fn func(store db.ShiftTxStore) error) error
}

// ShiftClosure is one Shift's closed state after a change: enough for a caller
// to name the date it just shut, and nothing more.
type ShiftClosure struct {
	ID     string
	Date   string
	Closed bool
}

// SetShiftClosed closes or reopens one Shift.
//
// Being Closed is an allocator input, so it is frozen once the Rotation is
// allocated — unlike a Shift's times, which are descriptive and stay editable.
// Setting the flag to what it already is succeeds rather than conflicting: the
// caller asked for a state, and the shift is in it.
func SetShiftClosed(
	ctx context.Context,
	store SetShiftClosedStore,
	shiftID string,
	closed bool,
	logger *zap.Logger,
) (*ShiftClosure, error) {
	shift, err := store.GetShiftByID(ctx, shiftID)
	if err != nil {
		return nil, fmt.Errorf("failed to look up shift %s: %w", shiftID, err)
	}
	if shift == nil {
		return nil, wrapf(ErrNotFound, "shift %s not found", shiftID)
	}

	err = store.WithRotaShiftLock(ctx, []string{shift.RotaID}, func(tx db.ShiftTxStore) error {
		allocated, err := tx.RotaAllocated(ctx, shift.RotaID)
		if err != nil {
			return err
		}
		if allocated {
			// Said as what it means rather than as a rule: the rota has been
			// worked out around this shift running, or not.
			return wrapf(ErrConflict, "the rota covering %s has already been allocated, so its shifts cannot be closed or reopened", shift.Date)
		}

		updated, err := tx.SetShiftClosed(ctx, shiftID, closed)
		if err != nil {
			return err
		}
		if !updated {
			// Lost a race with something that removed the shift under the lock.
			return wrapf(ErrNotFound, "shift %s not found", shiftID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	logger.Info("Shift closure changed",
		zap.String("shift_id", shiftID),
		zap.String("date", shift.Date),
		zap.Bool("closed", closed))

	return &ShiftClosure{ID: shiftID, Date: shift.Date, Closed: closed}, nil
}
