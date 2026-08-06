package services

import (
	"context"
	"encoding/json"
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
//
// One method per section, because the settings screen is sections and a save
// of one must not blank another.
type RotaDefaultsWriteStore interface {
	SaveRotaDefaults(ctx context.Context, defaults db.RotaDefaults) error
	SaveAllocationSettings(ctx context.Context, settings string) error
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
		ShiftStartTime:     row.ShiftStartTime,
		ShiftEndTime:       row.ShiftEndTime,
		ShiftTimezone:      row.ShiftTimezone,
		AllocationSettings: parseAllocationSettings(row.AllocationSettings),
	}, nil
}

// AllocationSettingsStore is everything the allocation gate reads: the settings
// record itself, and the default Shape, which is stored beside it as rows.
type AllocationSettingsStore interface {
	RotaDefaultsStore
	DefaultShapeStore
}

// parseAllocationSettings reads the stored document, and answers "no rules"
// for anything it cannot make sense of.
//
// It never fails, which is the rule from ADR 0006 read to its conclusion: a
// document this build cannot understand must not be able to take down the one
// screen an admin could fix it on. Every rule reading as off is a safe answer
// — allocation still runs, with nothing optional applied — and the section
// re-saves cleanly over it.
//
// Keys this build does not know need no handling at all: encoding/json ignores
// them, which is the leniency the ADR asks for, for free.
func parseAllocationSettings(document string) model.AllocationSettings {
	if document == "" {
		return model.AllocationSettings{}
	}

	var settings model.AllocationSettings
	if err := json.Unmarshal([]byte(document), &settings); err != nil {
		return model.AllocationSettings{}
	}
	return settings
}

// settingsForAllocation refuses when the drop-in's settings are too
// incomplete to allocate against, naming what is missing.
//
// This is where the rule from ADR 0006 is enforced: incomplete settings block
// allocation and **nothing else**. The rota still renders, availability still
// works, and the calendar feed still answers — because a value nobody has
// filled in yet is the ordinary first state of a deployment, not a fault, and
// the site should not fall over on it. Allocation is the exception because it
// is the one act that turns the settings into a rota people are told to turn up
// for: a rota allocated against times nobody has chosen is one nobody can be
// given the hours of.
//
// It reads the settings itself and hands back what it read, so a caller cannot
// allocate by forgetting to check — the only way to get the settings for a
// solve is to go through the gate.
func settingsForAllocation(ctx context.Context, store AllocationSettingsStore, logger *zap.Logger) (model.RotaDefaults, error) {
	defaults, err := RotaDefaults(ctx, store)
	if err != nil {
		return model.RotaDefaults{}, err
	}

	// Every section at once, so an admin is told everything they have to go
	// and fill in rather than one thing per attempt.
	missing := append(defaults.MissingShiftTimes(), defaults.AllocationSettings.Missing()...)

	// A Shape asking for nobody solves perfectly and staffs nothing, which is
	// the worst way for an unset setting to behave: a rota comes back empty and
	// nothing says why.
	shape, err := DefaultShape(ctx, store)
	if err != nil {
		return model.RotaDefaults{}, err
	}
	if len(shape) == 0 {
		missing = append(missing, "the default shape")
	}

	if len(missing) > 0 {
		return model.RotaDefaults{}, wrapf(ErrInvalidInput,
			"the drop-in's settings are incomplete - %s %s not been set; fill them in on the settings screen before allocating",
			joinWithAnd(missing), plural(len(missing), "has", "have"))
	}

	// An answer for a rule this build no longer has is dropped, not refused —
	// but it is worth saying so once, here, because the rota about to be
	// allocated is not the one an admin who switched that rule on expected.
	if unknown := defaults.AllocationSettings.UnknownConstraints(); len(unknown) > 0 {
		logger.Warn("Ignoring allocation settings for rules this build does not have",
			zap.Strings("rules", unknown))
	}

	return defaults, nil
}

// joinWithAnd lists what is missing the way a sentence does, so the refusal
// reads as a sentence rather than as a dump of field names.
func joinWithAnd(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	default:
		return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
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

// AllocationSettingsParams is the allocation-settings section of the settings
// screen as an admin states it: an answer for every rule they were offered,
// plus the one value a rule carries.
//
// Stated whole, like the shift times: the screen shows every rule at once, and
// a partial save could not express switching one off.
type AllocationSettingsParams struct {
	// Enabled is one answer per rule, keyed by the constraint's name. A rule
	// left out is off.
	Enabled map[string]bool
	// MaxFrequency is the share of a rota's shifts one volunteer may work.
	// Required when max_frequency is on, and kept as given when it is off.
	MaxFrequency float64
}

// validate turns an admin's answers into the settings to store, or says why it
// will not.
//
// Answers for rules this build does not have are dropped rather than refused.
// Storing one would mean nothing to anything that later read it — the registry
// is the authority on which rules exist (ADR 0006) — and the caller is told
// what was kept, so a client working from an older list can see what happened.
func (p AllocationSettingsParams) validate() (model.AllocationSettings, error) {
	enabled := make(map[string]bool, len(p.Enabled))
	for _, constraint := range model.SwitchableConstraints {
		if p.Enabled[constraint.Name] {
			enabled[constraint.Name] = true
		}
	}

	settings := model.AllocationSettings{Enabled: enabled, MaxFrequency: p.MaxFrequency}

	// The value is only asked for when the rule that reads it is on. Off, it
	// is kept as given: it constrains nothing there, and blanking it would
	// lose the number an admin would want back on switching the rule on again.
	if settings.IsEnabled(model.MaxFrequencyConstraint) && (p.MaxFrequency <= 0 || p.MaxFrequency > 1) {
		return model.AllocationSettings{}, wrapf(ErrInvalidInput,
			"the maximum allocation frequency is a share of a rota between 0 and 1, and %v is not one", p.MaxFrequency)
	}

	return settings, nil
}

// SaveAllocationSettings writes which optional allocator rules apply, and
// returns the settings as they now stand — including any answer that was
// dropped for naming a rule this build does not have.
func SaveAllocationSettings(
	ctx context.Context,
	store RotaDefaultsWriteStore,
	params AllocationSettingsParams,
	logger *zap.Logger,
) (model.AllocationSettings, error) {
	settings, err := params.validate()
	if err != nil {
		return model.AllocationSettings{}, err
	}

	document, err := json.Marshal(settings)
	if err != nil {
		return model.AllocationSettings{}, fmt.Errorf("failed to encode allocation settings: %w", err)
	}

	if err := store.SaveAllocationSettings(ctx, string(document)); err != nil {
		return model.AllocationSettings{}, fmt.Errorf("failed to save allocation settings: %w", err)
	}

	logger.Info("Allocation settings saved",
		zap.Strings("enabled", settings.EnabledConstraints()),
		zap.Float64("max_frequency", settings.MaxFrequency))

	return settings, nil
}
