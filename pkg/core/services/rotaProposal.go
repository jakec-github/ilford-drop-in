package services

import (
	"context"
	"fmt"
	"time"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services/utils"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// A rota proposal is the define form before an admin has touched it: the rota
// that would be made if they pressed the button straight away (issue #140).
//
// It exists because the answers come from two places an admin cannot see at
// once. The start date is arithmetic over the rotas that already exist; the
// hours and the Shape are the Rota Defaults. Working them out here rather than
// in the browser keeps "the Sunday after the last rota" one rule in one place,
// and it is the same rule whether a person or a script is asking.
//
// Nothing about it is binding. What comes back is a suggestion the define
// request may contradict in every field, which is the point of the ticket: a
// rota can start after a break, and one week can run different hours from the
// last, without the settings being edited first.

// RotaProposalStore is what proposing a rota needs: the rotas that exist, to
// count forward from, and the settings the form starts from.
type RotaProposalStore interface {
	RotaDefaultsStore
	DefaultShapeStore
	GetRotations(ctx context.Context) ([]db.Rotation, error)
}

// RotaProposal is what the define form starts from.
type RotaProposal struct {
	// StartDate is the Sunday after the last rota's last shift, or the next
	// Sunday on a deployment with no rotas at all.
	StartDate string
	// ShiftStartTime and ShiftEndTime are the default hours, as times of day in
	// model.ShiftTimeLayout. Empty where an admin has not stated them — the
	// state every deployment starts in, and one the form renders as empty boxes
	// rather than refusing over (ADR 0006).
	ShiftStartTime string
	ShiftEndTime   string
	// Shape is the default Shape, with each Seat's Role resolved. Empty where
	// none has been stated, for the same reason.
	Shape model.Shape
}

// ProposeRota reads what the define form starts from.
//
// It answers whether or not a rota is in flight. A rota that cannot be defined
// yet still has a date it would start on — the one after the rota in the way —
// and this endpoint's job is that arithmetic rather than the lifecycle rule,
// which DefineRota enforces and GET /rotations/in-flight reports.
func ProposeRota(ctx context.Context, store RotaProposalStore) (*RotaProposal, error) {
	defaults, err := RotaDefaults(ctx, store)
	if err != nil {
		return nil, err
	}

	shape, err := DefaultShape(ctx, store)
	if err != nil {
		return nil, err
	}

	rotations, err := store.GetRotations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch rotations: %w", err)
	}

	startDate, err := nextRotaStart(rotations)
	if err != nil {
		return nil, err
	}

	return &RotaProposal{
		StartDate:      startDate,
		ShiftStartTime: defaults.ShiftStartTime,
		ShiftEndTime:   defaults.ShiftEndTime,
		Shape:          shape,
	}, nil
}

// nextRotaStart is the date a rota would begin on if one were defined now: the
// Sunday after the latest rota's last shift, or the next Sunday where there are
// no rotas to follow.
//
// Sunday rather than "the same weekday as last time" because that is the day
// the drop-in runs, and a rota whose start date has been moved by hand is a
// one-off rather than a new cadence to inherit.
func nextRotaStart(rotations []db.Rotation) (string, error) {
	latest := utils.FindLatestRotation(rotations)
	if latest == nil {
		return nextSunday(time.Now()).Format("2006-01-02"), nil
	}

	end, err := time.Parse("2006-01-02", latest.End)
	if err != nil {
		return "", fmt.Errorf("failed to parse latest rota end date: %w", err)
	}
	return nextSunday(end).Format("2006-01-02"), nil
}

// nextSunday returns the next Sunday after the given date, never the date
// itself: a rota that ended on a Sunday is followed by the week after, not by
// the day it finished.
func nextSunday(from time.Time) time.Time {
	normalized := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)

	daysUntilSunday := (7 - int(normalized.Weekday())) % 7
	if daysUntilSunday == 0 {
		daysUntilSunday = 7
	}

	return normalized.AddDate(0, 0, daysUntilSunday)
}
