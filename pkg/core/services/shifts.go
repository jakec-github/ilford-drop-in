package services

import (
	"context"
	"fmt"
	"time"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
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

// shiftLabel is a Shift as a sentence an Organiser is shown names it: its date
// and the time it starts — "9 August 2026 at 19:30". The date alone stopped
// being enough once a day could hold two Shifts (issue #293). A Shift whose
// start cannot be read is named by its date alone, for the reason readableDate
// gives.
func shiftLabel(shift db.Shift) string {
	startAt, err := time.Parse(model.ShiftTimestampLayout, shift.StartAt)
	if err != nil {
		return readableDate(shift.Date)
	}
	return readableDate(shift.Date) + " at " + startAt.Format("15:04")
}
