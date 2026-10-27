package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/shiftpattern"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// maxShiftLengthMinutes bounds the length a Shift pattern's Shifts run for. A
// day: a Shift longer than that would overlap the same Shift the next day on
// any daily pattern, and is not a session anybody runs.
const maxShiftLengthMinutes = 24 * 60

// maxPreviewDays bounds how much of a pattern one preview generates. A year
// and a bit — the calendar asks for three months at a time.
const maxPreviewDays = 400

// ShiftPatternParams is the Shift pattern section of the Rota Defaults as an
// Organiser states it (ADR 0013).
type ShiftPatternParams struct {
	// Pattern is RRULE lines, one per line.
	Pattern string
	// StartFrom is the date the pattern counts from, "2006-01-02". Empty means
	// today in the Organisation's zone, which is what the ADR has it default to.
	StartFrom string
	// Timezone is an IANA zone name. Empty means the default.
	Timezone string
	// LengthMinutes is how long each Shift runs.
	LengthMinutes int
}

// validate turns an Organiser's answers into the row to write, or says why it
// will not.
func (p ShiftPatternParams) validate(now time.Time) (db.RotaDefaults, error) {
	timezone := strings.TrimSpace(p.Timezone)
	if timezone == "" {
		timezone = model.DefaultShiftTimezone
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return db.RotaDefaults{}, wrapf(ErrInvalidInput, "%q is not a timezone this app knows - use a name like %s", timezone, model.DefaultShiftTimezone)
	}

	startFrom := strings.TrimSpace(p.StartFrom)
	if startFrom == "" {
		startFrom = now.In(loc).Format("2006-01-02")
	}
	anchor, err := time.Parse("2006-01-02", startFrom)
	if err != nil {
		return db.RotaDefaults{}, wrapf(ErrInvalidInput, "%q is not a date - write start from as 2026-08-02", startFrom)
	}

	if p.LengthMinutes <= 0 || p.LengthMinutes > maxShiftLengthMinutes {
		return db.RotaDefaults{}, wrapf(ErrInvalidInput, "a shift lasts from 1 minute to 24 hours, not %d minutes", p.LengthMinutes)
	}

	pattern := strings.TrimSpace(p.Pattern)
	if _, err := shiftpattern.Compile(pattern, anchor); err != nil {
		return db.RotaDefaults{}, wrapf(ErrInvalidInput, "the shift pattern cannot be used - %s", err)
	}

	return db.RotaDefaults{
		ShiftPattern:       pattern,
		PatternStartFrom:   startFrom,
		ShiftTimezone:      timezone,
		ShiftLengthMinutes: p.LengthMinutes,
	}, nil
}

// SaveShiftPattern writes the Shift pattern section and returns the settings
// as they now stand.
//
// It changes no rota already defined. Those Shifts carry their own times, and
// the next rota still carries on from where the last one stopped — under the
// new pattern, from the first start it generates after that point.
func SaveShiftPattern(ctx context.Context, store RotaDefaultsWriteStore, params ShiftPatternParams, logger *zap.Logger) (model.RotaDefaults, error) {
	row, err := params.validate(time.Now())
	if err != nil {
		return model.RotaDefaults{}, err
	}

	if err := store.SaveShiftPattern(ctx, row); err != nil {
		return model.RotaDefaults{}, fmt.Errorf("failed to save the shift pattern: %w", err)
	}

	logger.Info("Shift pattern saved",
		zap.String("pattern", row.ShiftPattern),
		zap.String("start_from", row.PatternStartFrom),
		zap.Int("length_minutes", row.ShiftLengthMinutes),
		zap.String("timezone", row.ShiftTimezone))

	return model.RotaDefaults{
		ShiftPattern:       row.ShiftPattern,
		PatternStartFrom:   row.PatternStartFrom,
		ShiftLengthMinutes: row.ShiftLengthMinutes,
		ShiftTimezone:      row.ShiftTimezone,
	}, nil
}

// ShiftPatternPreviewParams is a pattern being written, and the stretch of it
// to show.
type ShiftPatternPreviewParams struct {
	Pattern string
	// StartFrom is "2006-01-02". Empty means today, as saving would.
	StartFrom string
	// Timezone settles what today is when StartFrom is empty.
	Timezone string
	// From and To are dates, "2006-01-02": the starts shown are those on From
	// up to, but not including, To.
	From string
	To   string
}

// ShiftPatternPreview is what a pattern says, line by line, and where its
// Shifts start.
type ShiftPatternPreview struct {
	StartFrom string
	Lines     []shiftpattern.Line
	// Starts are in model.ShiftTimestampLayout. None at all while any line
	// has a problem: the pattern could not be saved, so it generates nothing.
	Starts []string
}

// PreviewShiftPattern reads a pattern that has not been saved, for the screen
// it is being written on. It writes nothing and refuses nothing about the
// pattern itself — a pattern with problems is answered with them, line by
// line — only a range it cannot read.
func PreviewShiftPattern(params ShiftPatternPreviewParams) (*ShiftPatternPreview, error) {
	loc, err := time.LoadLocation(strings.TrimSpace(params.Timezone))
	if err != nil || strings.TrimSpace(params.Timezone) == "" {
		loc, _ = time.LoadLocation(model.DefaultShiftTimezone)
	}
	startFrom := strings.TrimSpace(params.StartFrom)
	if startFrom == "" {
		startFrom = time.Now().In(loc).Format("2006-01-02")
	}
	anchor, err := time.Parse("2006-01-02", startFrom)
	if err != nil {
		return nil, wrapf(ErrInvalidInput, "%q is not a date - write start from as 2026-08-02", startFrom)
	}

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

	lines, pattern := shiftpattern.Read(params.Pattern, anchor)
	preview := &ShiftPatternPreview{StartFrom: startFrom, Lines: lines, Starts: []string{}}
	if pattern != nil {
		for _, start := range pattern.Between(from, to) {
			preview.Starts = append(preview.Starts, start.Format(model.ShiftTimestampLayout))
		}
	}
	return preview, nil
}
