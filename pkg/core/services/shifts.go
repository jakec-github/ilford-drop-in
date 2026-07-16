package services

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services/utils"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// shiftReader fetches a rota's shifts. Satisfied by the per-service stores and
// by *db.DB.
type shiftReader interface {
	GetShiftsByRotaID(ctx context.Context, rotaID string) ([]db.Shift, error)
}

// rotaShiftDates reads a rota's shift dates from the shift table, sorted
// ascending, replacing the old date arithmetic (ADR 0001). A rotation always
// has at least one shift, so an empty result means a broken invariant and
// fails loudly rather than silently producing an empty rota.
func rotaShiftDates(ctx context.Context, store shiftReader, rotaID string) ([]time.Time, error) {
	shifts, err := store.GetShiftsByRotaID(ctx, rotaID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch shifts: %w", err)
	}
	if len(shifts) == 0 {
		return nil, fmt.Errorf("rota %s has no shifts", rotaID)
	}
	return utils.ShiftDatesFromShifts(shifts)
}

// partitionShiftDates splits a rota's shift dates into the dates the drop-in is
// open and the dates a closed-shift override marks as Closed. Both slices keep
// the ascending order of the input. It reuses isShiftClosed so the availability
// flow resolves closed shifts the same way as the publish/list paths, rather
// than inventing a second closed-shift check.
func partitionShiftDates(shiftDates []time.Time, overrides []config.RotaOverride, logger *zap.Logger) (open, closed []time.Time) {
	for _, date := range shiftDates {
		if isShiftClosed(date.Format("2006-01-02"), overrides, shiftDates, logger) {
			closed = append(closed, date)
		} else {
			open = append(open, date)
		}
	}
	return open, closed
}
