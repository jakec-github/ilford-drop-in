package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/shiftpattern"
)

// maxShiftLengthMinutes bounds the length a Shift pattern's Shifts run for. A
// day: a Shift longer than that would overlap the same Shift the next day on
// any daily pattern, and is not a session anybody runs.
const maxShiftLengthMinutes = 24 * 60

// maxPreviewDays bounds how much of a pattern one preview generates. A year
// and a bit — the calendar asks for three months at a time.
const maxPreviewDays = 400

// SaveShiftPattern writes the Shift pattern and returns the settings as they
// now stand. The pattern is VEVENT entries, each with its own start, length
// and rule (ADR 0014), read in the Organisation's time zone; it is stored as
// written, so the Organiser gets back exactly what they typed.
//
// It changes no rota already defined. Those Shifts carry their own times, and
// the next rota still carries on from where the last one stopped — under the
// new pattern, from the first start it generates after that point.
func SaveShiftPattern(ctx context.Context, store RotaDefaultsWriteStore, pattern string, logger *zap.Logger) (model.RotaDefaults, error) {
	defaults, err := RotaDefaults(ctx, store)
	if err != nil {
		return model.RotaDefaults{}, err
	}

	pattern = strings.TrimSpace(pattern)
	if _, err := shiftpattern.Compile(pattern, defaults.Timezone()); err != nil {
		return model.RotaDefaults{}, wrapf(ErrInvalidInput, "the shift pattern cannot be used - %s", err)
	}

	if err := store.SaveShiftPattern(ctx, pattern); err != nil {
		return model.RotaDefaults{}, fmt.Errorf("failed to save the shift pattern: %w", err)
	}

	logger.Info("Shift pattern saved", zap.String("pattern", pattern))

	defaults.ShiftPattern = pattern
	return defaults, nil
}

// SaveTimezone writes the zone the Organisation's wall-clock times are read in
// and returns the settings as they now stand.
//
// The zone must be one this server can load: every time the app renders —
// the calendar feed, a pattern's starts — goes through it. "Local" is a zone
// time.LoadLocation accepts, but it means wherever the server happens to run,
// which is not where anybody volunteers.
func SaveTimezone(ctx context.Context, store RotaDefaultsWriteStore, timezone string, logger *zap.Logger) (model.RotaDefaults, error) {
	timezone = strings.TrimSpace(timezone)
	if timezone == "" || timezone == "Local" {
		return model.RotaDefaults{}, wrapf(ErrInvalidInput, "a time zone is a name like %s", model.DefaultShiftTimezone)
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return model.RotaDefaults{}, wrapf(ErrInvalidInput, "%q is not a time zone this app knows - use a name like %s", timezone, model.DefaultShiftTimezone)
	}

	if err := store.SaveTimezone(ctx, timezone); err != nil {
		return model.RotaDefaults{}, fmt.Errorf("failed to save the time zone: %w", err)
	}

	logger.Info("Time zone saved", zap.String("timezone", timezone))

	return RotaDefaults(ctx, store)
}

// ShiftPatternPreviewParams is a pattern being written, and the stretch of it
// to show.
type ShiftPatternPreviewParams struct {
	Pattern string
	// From and To are dates, "2006-01-02": the starts shown are those on From
	// up to, but not including, To.
	From string
	To   string
}

// ShiftPatternPreview is what a pattern says, entry by entry, and where its
// Shifts start.
type ShiftPatternPreview struct {
	Reading shiftpattern.Reading
	// StartsFrom is the earliest entry's DTSTART, "2006-01-02", which is where
	// the first rota starts. Empty while the pattern has a problem.
	StartsFrom string
	// Starts are the Shifts in the range, in the order they start. None at all
	// while any entry has a problem: the pattern could not be saved, so it
	// generates nothing.
	Starts []shiftpattern.Start
}

// PreviewShiftPattern reads a pattern that has not been saved, for the screen
// it is being written on, in the zone the Organisation has saved. It writes
// nothing and refuses nothing about the pattern itself — a pattern with
// problems is answered with them, entry by entry — only a range it cannot
// read.
func PreviewShiftPattern(ctx context.Context, store RotaDefaultsStore, params ShiftPatternPreviewParams) (*ShiftPatternPreview, error) {
	from, err := time.Parse("2006-01-02", params.From)
	if err != nil {
		return nil, wrapf(ErrInvalidInput, "%q is not a date - write from as 2026-08-02", params.From)
	}
	to, err := time.Parse("2006-01-02", params.To)
	if err != nil {
		return nil, wrapf(ErrInvalidInput, "%q is not a date - write to as 2026-08-02", params.To)
	}
	if !to.After(from) || to.Sub(from) > maxPreviewDays*24*time.Hour {
		return nil, wrapf(ErrInvalidInput, "a preview covers from 1 to %d days, and %s to %s does not", maxPreviewDays, params.From, params.To)
	}

	defaults, err := RotaDefaults(ctx, store)
	if err != nil {
		return nil, err
	}

	reading, pattern := shiftpattern.Read(params.Pattern, defaults.Timezone())
	preview := &ShiftPatternPreview{Reading: reading, Starts: []shiftpattern.Start{}}
	if pattern != nil {
		preview.StartsFrom = pattern.First().Format("2006-01-02")
		preview.Starts = pattern.Between(from, to)
	}
	return preview, nil
}
