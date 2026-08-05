package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// RotaDefaultsStore reads the settings record. It is embedded by every store
// interface on a path that renders a time, which is more of them than it looks:
// the rota listing, the calendar feed and — once the times are written onto
// Shifts — defining a rota.
type RotaDefaultsStore interface {
	GetRotaDefaults(ctx context.Context) (db.RotaDefaults, error)
}

// RotaDefaultsWriteStore is what saving the settings needs, kept apart from
// reading them for the same reason RoleWriteStore is: one screen writes them,
// half the app reads them.
type RotaDefaultsWriteStore interface {
	SaveRotaDefaults(ctx context.Context, defaults db.RotaDefaults) error
}

// RotaDefaults reads what an admin has decided about how the drop-in runs.
//
// Read per call rather than held from startup: it is one row of a handful of
// columns, and the whole point of moving it out of the config file is that
// changing it takes effect without a redeploy (ADR 0006).
//
// A deployment nobody has configured yields the zero value rather than an
// error. That is the state every deployment starts in, and the pages that only
// render should still render there; allocation is the one path that refuses.
func RotaDefaults(ctx context.Context, store RotaDefaultsStore) (model.RotaDefaults, error) {
	row, err := store.GetRotaDefaults(ctx)
	if err != nil {
		return model.RotaDefaults{}, fmt.Errorf("failed to read rota defaults: %w", err)
	}

	return model.RotaDefaults{
		ShiftStartTime: row.ShiftStartTime,
		ShiftEndTime:   row.ShiftEndTime,
		ShiftTimezone:  row.ShiftTimezone,
	}, nil
}

// ShiftTimeParams is the shift-time settings as an admin states them: a start,
// an end and the zone they are read in. All three together, because they are
// one form and one idea — a start with no end describes nothing.
type ShiftTimeParams struct {
	// Start and End are times of day in model.ShiftTimeLayout ("19:30").
	Start string
	End   string
	// Timezone is an IANA zone name. Empty means the default, so an admin who
	// never touches the field still saves settings that compute a time.
	Timezone string
}

// validate turns an admin's answers into the row to write, or says why it will
// not.
func (p ShiftTimeParams) validate() (db.RotaDefaults, error) {
	start, err := parseShiftTime(strings.TrimSpace(p.Start), "start")
	if err != nil {
		return db.RotaDefaults{}, err
	}

	end, err := parseShiftTime(strings.TrimSpace(p.End), "end")
	if err != nil {
		return db.RotaDefaults{}, err
	}

	// A shift ends the evening it starts. Forbidding one that runs past
	// midnight is deliberate: the times become a start and an end on the
	// Shift's own date, so "22:00 to 00:30" would describe a shift ending
	// before it began rather than one lasting half an hour.
	if !end.After(start) {
		return db.RotaDefaults{}, wrapf(ErrInvalidInput, "a shift has to end after it starts, and %s is not after %s", p.End, p.Start)
	}

	timezone := strings.TrimSpace(p.Timezone)
	if timezone == "" {
		timezone = model.DefaultShiftTimezone
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return db.RotaDefaults{}, wrapf(ErrInvalidInput, "%q is not a timezone this app knows — use a name like %s", timezone, model.DefaultShiftTimezone)
	}

	return db.RotaDefaults{
		ShiftStartTime: start.Format(model.ShiftTimeLayout),
		ShiftEndTime:   end.Format(model.ShiftTimeLayout),
		ShiftTimezone:  timezone,
	}, nil
}

// parseShiftTime reads one time of day, naming which one it was when it cannot.
//
// Blank is refused rather than stored as unset: the settings record starts
// empty and an admin filling in the form is filling it in. Clearing a time an
// allocation may already be gated on is not an edit anybody wants to make by
// leaving a box blank.
func parseShiftTime(value, which string) (time.Time, error) {
	if value == "" {
		return time.Time{}, wrapf(ErrInvalidInput, "a shift needs a %s time", which)
	}

	parsed, err := time.Parse(model.ShiftTimeLayout, value)
	if err != nil {
		return time.Time{}, wrapf(ErrInvalidInput, "%q is not a time of day — write the %s as 24-hour HH:MM, e.g. 19:30", value, which)
	}
	return parsed, nil
}

// SaveShiftTimeDefaults writes the default shift start, end and timezone, and
// returns the settings as they now stand.
//
// This is the whole of the shift-time section of the settings screen. It writes
// all three at once because they are read all at once: a time of day means
// nothing without the zone it is read in.
func SaveShiftTimeDefaults(ctx context.Context, store RotaDefaultsWriteStore, params ShiftTimeParams, logger *zap.Logger) (model.RotaDefaults, error) {
	row, err := params.validate()
	if err != nil {
		return model.RotaDefaults{}, err
	}

	if err := store.SaveRotaDefaults(ctx, row); err != nil {
		return model.RotaDefaults{}, fmt.Errorf("failed to save shift time defaults: %w", err)
	}

	logger.Info("Shift time defaults saved",
		zap.String("start", row.ShiftStartTime),
		zap.String("end", row.ShiftEndTime),
		zap.String("timezone", row.ShiftTimezone))

	return model.RotaDefaults{
		ShiftStartTime: row.ShiftStartTime,
		ShiftEndTime:   row.ShiftEndTime,
		ShiftTimezone:  row.ShiftTimezone,
	}, nil
}
