package services

import (
	"context"
	"fmt"
	"time"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services/utils"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// A rota proposal is the define form before an admin has touched it: where the
// rota they are about to define would begin (issue #140).
//
// It is arithmetic over the rotas that already exist, done here rather than in
// the browser so that "the Sunday after the last rota" is one rule in one
// place, and the same rule whether a person or a script is asking.
//
// It used to carry the hours and the Shape as well, the two settings the form
// filled itself in from. Those fields have gone with the boxes they filled
// (issue #176): the hours and the Shape are stated in the Rota Defaults and
// nowhere else, and defining spends them rather than restating them. What is
// left is the one answer that is not a setting — and it is still not binding,
// because a rota can begin after a break rather than the week after the last.

// RotaProposalStore is what proposing a rota needs: the rotas that exist, to
// count forward from.
type RotaProposalStore interface {
	GetRotations(ctx context.Context) ([]db.Rotation, error)
}

// RotaProposal is what the define form starts from.
type RotaProposal struct {
	// StartDate is the Sunday after the last rota's last shift, or the next
	// Sunday on a deployment with no rotas at all.
	StartDate string
}

// ProposeRota reads what the define form starts from.
//
// It answers whether or not a rota is in flight. A rota that cannot be defined
// yet still has a date it would start on — the one after the rota in the way —
// and this endpoint's job is that arithmetic rather than the lifecycle rule,
// which DefineRota enforces and GET /rotations/in-flight reports.
func ProposeRota(ctx context.Context, store RotaProposalStore) (*RotaProposal, error) {
	rotations, err := store.GetRotations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch rotations: %w", err)
	}

	startDate, err := nextRotaStart(rotations)
	if err != nil {
		return nil, err
	}

	return &RotaProposal{StartDate: startDate}, nil
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
