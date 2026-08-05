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

// BackfillShiftClosedStore is what the one-off closed backfill needs: every
// minted shift, and a way to write the flag onto one.
type BackfillShiftClosedStore interface {
	GetShiftsInRange(ctx context.Context, from, to time.Time) ([]db.ShiftInRange, error)
	SetShiftClosed(ctx context.Context, shiftID string, closed bool) (bool, error)
}

// BackfillShiftClosedResult reports what a run saw and what it changed.
type BackfillShiftClosedResult struct {
	// Scanned is every minted shift the run considered.
	Scanned int
	// Closed names the dates this run shut, in ascending order. A shift the
	// rules match that was already closed is not listed — nothing changed.
	Closed []string
	// AlreadyClosed counts shifts the rules match that were shut already, which
	// is what a second run of an already-backfilled environment reports.
	AlreadyClosed int
}

// BackfillShiftClosed stamps the closed state the config's Rota Overrides
// currently give a date onto the Shift itself (issue #132).
//
// It is a one-off, run once against each environment while `closed` is still a
// config key, and deleted with that key. Every Shift is minted open, so the
// backfill only ever closes: it never reopens a Shift the rules do not match.
// That makes it re-runnable — a second run changes nothing — and means it
// cannot undo a closure an admin has since made by hand, which is the state the
// rules stop being the authority on the moment this has run.
//
// Allocated rotas are backfilled too. The freeze is on an admin changing an
// allocated rota's inputs; history has to keep saying the drop-in was shut on
// Christmas Day, or the rota page and the published Sheet would both start
// claiming it ran.
func BackfillShiftClosed(
	ctx context.Context,
	store BackfillShiftClosedStore,
	cfg *config.Config,
	dryRun bool,
	logger *zap.Logger,
) (*BackfillShiftClosedResult, error) {
	shifts, err := store.GetShiftsInRange(ctx, time.Time{}, time.Time{})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch shifts: %w", err)
	}

	result := &BackfillShiftClosedResult{Scanned: len(shifts)}
	if len(shifts) == 0 {
		return result, nil
	}

	// One window over every shift that exists, so an rrule spanning several
	// rotas is evaluated once rather than per rota.
	dates := make([]time.Time, 0, len(shifts))
	for _, s := range shifts {
		date, err := time.Parse("2006-01-02", s.Date)
		if err != nil {
			return nil, fmt.Errorf("shift %s has an unparseable date %q: %w", s.ID, s.Date, err)
		}
		dates = append(dates, date)
	}

	// Unparseable rrules fail the run rather than being skipped: a backfill that
	// quietly leaves a closure behind is worse than one that does not finish,
	// because the omission is invisible afterwards.
	var matchers []func(string) bool
	for i, override := range cfg.RotaOverrides {
		if !override.Closed {
			continue
		}
		matches, err := utils.NewRRuleMatcher(override.RRule, dates)
		if err != nil {
			return nil, fmt.Errorf("rota override %d has an invalid rrule %q: %w", i, override.RRule, err)
		}
		matchers = append(matchers, matches)
	}

	for _, s := range shifts {
		if !anyMatch(matchers, s.Date) {
			continue
		}
		if s.Closed {
			result.AlreadyClosed++
			continue
		}
		if !dryRun {
			updated, err := store.SetShiftClosed(ctx, s.ID, true)
			if err != nil {
				return nil, fmt.Errorf("failed to close shift %s (%s): %w", s.ID, s.Date, err)
			}
			if !updated {
				return nil, fmt.Errorf("shift %s (%s) vanished mid-backfill", s.ID, s.Date)
			}
		}
		logger.Info("Closing shift from config rules",
			zap.String("shift_id", s.ID),
			zap.String("date", s.Date),
			zap.Bool("dry_run", dryRun))
		result.Closed = append(result.Closed, s.Date)
	}

	return result, nil
}

func anyMatch(matchers []func(string) bool, date string) bool {
	for _, matches := range matchers {
		if matches(date) {
			return true
		}
	}
	return false
}
