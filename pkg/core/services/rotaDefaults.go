package services

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
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
	UpdateAllocationSettings(ctx context.Context, edit func(current string) (string, error)) error
}

// RotaDefaults reads what an Organiser has decided about how the Organisation runs.
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

// parseAllocationSettings reads the stored document, and answers "no rules"
// for anything it cannot make sense of.
//
// It never fails, which is the rule from ADR 0006 read to its conclusion: a
// document this build cannot understand must not be able to take down the one
// screen an Organiser could fix it on. No rules is a safe answer — allocation
// still runs, with nothing optional applied.
//
// Keys this build does not know need no handling at all: encoding/json ignores
// them, which is the leniency the ADR asks for, for free. A rule of a kind it
// does not know still reads; AllocationSettings.Applicable is what leaves it
// out of a solve.
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

// settingsForAllocation refuses when the Organisation's settings are too
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
func settingsForAllocation(ctx context.Context, store RotaDefaultsStore, logger *zap.Logger) (model.RotaDefaults, error) {
	defaults, err := RotaDefaults(ctx, store)
	if err != nil {
		return model.RotaDefaults{}, err
	}

	if missing := defaults.MissingShiftTimes(); len(missing) > 0 {
		return model.RotaDefaults{}, wrapf(ErrInvalidInput,
			"the organisation's settings are incomplete - %s %s not been set; fill them in on the settings screen before allocating",
			joinWithAnd(missing), plural(len(missing), "has", "have"))
	}

	// A rule this build cannot apply is dropped, not refused — but it is worth
	// saying so once, here, because the rota about to be allocated is not the
	// one an Organiser who added that rule expected.
	for _, rule := range defaults.AllocationSettings.Ignored() {
		logger.Warn("Ignoring an Allocation Rule this build cannot apply",
			zap.String("rule_id", rule.ID),
			zap.String("kind", rule.Kind),
			zap.Error(rule.Validate()))
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

// readableDate is a stored date as it reads in a sentence an Organiser is shown —
// "9 August 2026" rather than "2026-08-09". A value that is not a date comes
// back unchanged: a refusal saying the wrong-looking thing is better than a
// refusal that fails to be written.
func readableDate(date string) string {
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil {
		return date
	}
	return parsed.Format("2 January 2006")
}

// ShiftTimeParams is the shift-time settings as an Organiser states them: a start,
// an end and the zone they are read in. All three together, because they are
// one form and one idea — a start with no end describes nothing.
type ShiftTimeParams struct {
	// Start and End are times of day in model.ShiftTimeLayout ("19:30").
	Start string
	End   string
	// Timezone is an IANA zone name. Empty means the default, so an Organiser who
	// never touches the field still saves settings that compute a time.
	Timezone string
}

// shiftTimesOfDay reads a pair of stated shift times, or says why it will not,
// and hands them back spelled in ShiftTimeLayout.
//
// Both places an Organiser states shift times come here: the settings screen, which
// says what every rota starts from, and the define form, which says what one
// rota runs (issue #140). It is the same question in both, so it has one answer
// and one wording.
func shiftTimesOfDay(statedStart, statedEnd string) (start, end string, err error) {
	from, err := parseShiftTime(strings.TrimSpace(statedStart), "start")
	if err != nil {
		return "", "", err
	}

	to, err := parseShiftTime(strings.TrimSpace(statedEnd), "end")
	if err != nil {
		return "", "", err
	}

	// A shift ends the evening it starts. Forbidding one that runs past
	// midnight is deliberate: the times become a start and an end on the
	// Shift's own date, so "22:00 to 00:30" would describe a shift ending
	// before it began rather than one lasting half an hour.
	if !to.After(from) {
		return "", "", wrapf(ErrInvalidInput, "a shift has to end after it starts, and %s is not after %s", statedEnd, statedStart)
	}

	return from.Format(model.ShiftTimeLayout), to.Format(model.ShiftTimeLayout), nil
}

// validate turns an Organiser's answers into the row to write, or says why it will
// not.
func (p ShiftTimeParams) validate() (db.RotaDefaults, error) {
	start, end, err := shiftTimesOfDay(p.Start, p.End)
	if err != nil {
		return db.RotaDefaults{}, err
	}

	timezone := strings.TrimSpace(p.Timezone)
	if timezone == "" {
		timezone = model.DefaultShiftTimezone
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return db.RotaDefaults{}, wrapf(ErrInvalidInput, "%q is not a timezone this app knows — use a name like %s", timezone, model.DefaultShiftTimezone)
	}

	return db.RotaDefaults{
		ShiftStartTime: start,
		ShiftEndTime:   end,
		ShiftTimezone:  timezone,
	}, nil
}

// parseShiftTime reads one time of day, naming which one it was when it cannot.
//
// Blank is refused rather than stored as unset: the settings record starts
// empty and an Organiser filling in the form is filling it in. Clearing a time an
// allocation may already be gated on is not an edit anybody wants to make by
// leaving a box blank.
func parseShiftTime(value, which string) (time.Time, error) {
	if value == "" {
		return time.Time{}, wrapf(ErrInvalidInput, "a shift needs both a start and an end time, and the %s is missing", which)
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

// allocationDocument is the stored Allocation Settings document as an edit
// sees it: every key kept verbatim, and the rules as they were written.
//
// Raw rather than decoded into model.AllocationSettings, so that an edit
// touches only the one rule it is about. A rule of a kind this build does not
// know, a field a newer build added to a rule, and any key beside the rules
// all survive an edit byte for byte (ADR 0006).
type allocationDocument struct {
	keys  map[string]json.RawMessage
	rules []json.RawMessage
}

// readAllocationDocument reads what the column holds. A document this build
// cannot read at all is started over: it already reads as no rules
// everywhere else, and refusing to edit it would leave an Organiser no way
// to replace it.
func readAllocationDocument(document string) allocationDocument {
	doc := allocationDocument{keys: map[string]json.RawMessage{}}
	if document == "" || json.Unmarshal([]byte(document), &doc.keys) != nil {
		doc.keys = map[string]json.RawMessage{}
		return doc
	}
	if raw, ok := doc.keys["rules"]; ok {
		if json.Unmarshal(raw, &doc.rules) != nil {
			doc.rules = nil
		}
	}
	return doc
}

// find returns the position of the rule with this id, or -1.
func (d allocationDocument) find(id string) int {
	for i, raw := range d.rules {
		var rule struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &rule) == nil && rule.ID == id {
			return i
		}
	}
	return -1
}

func (d allocationDocument) encode() (string, error) {
	rules := d.rules
	if rules == nil {
		rules = []json.RawMessage{}
	}
	raw, err := json.Marshal(rules)
	if err != nil {
		return "", fmt.Errorf("failed to encode allocation rules: %w", err)
	}
	d.keys["rules"] = raw

	document, err := json.Marshal(d.keys)
	if err != nil {
		return "", fmt.Errorf("failed to encode allocation settings: %w", err)
	}
	return string(document), nil
}

// validRule normalises a rule and says why it cannot be stored, as an
// Organiser's mistake rather than a failure.
func validRule(rule model.AllocationRule) (model.AllocationRule, error) {
	rule = rule.Normalised()
	if err := rule.Validate(); err != nil {
		return model.AllocationRule{}, wrapf(ErrInvalidInput, "%s", err.Error())
	}
	return rule, nil
}

// AddAllocationRule adds a rule from the library, minting its id, and returns
// it as stored.
func AddAllocationRule(
	ctx context.Context,
	store RotaDefaultsWriteStore,
	rule model.AllocationRule,
	logger *zap.Logger,
) (model.AllocationRule, error) {
	rule.ID = uuid.New().String()
	rule, err := validRule(rule)
	if err != nil {
		return model.AllocationRule{}, err
	}

	raw, err := json.Marshal(rule)
	if err != nil {
		return model.AllocationRule{}, fmt.Errorf("failed to encode allocation rule: %w", err)
	}

	if err := store.UpdateAllocationSettings(ctx, func(current string) (string, error) {
		doc := readAllocationDocument(current)
		doc.rules = append(doc.rules, raw)
		return doc.encode()
	}); err != nil {
		return model.AllocationRule{}, fmt.Errorf("failed to add allocation rule: %w", err)
	}

	logger.Info("Allocation Rule added", zap.String("rule_id", rule.ID), zap.String("kind", rule.Kind))
	return rule, nil
}

// EditAllocationRule replaces a rule's value, Scope and note in place and
// returns it as stored. Its id and kind are what it is, so whatever the edit
// says of them is ignored.
func EditAllocationRule(
	ctx context.Context,
	store RotaDefaultsWriteStore,
	id string,
	edit model.AllocationRule,
	logger *zap.Logger,
) (model.AllocationRule, error) {
	var edited model.AllocationRule
	err := store.UpdateAllocationSettings(ctx, func(current string) (string, error) {
		doc := readAllocationDocument(current)
		i := doc.find(id)
		if i < 0 {
			return "", wrapf(ErrNotFound, "there is no Allocation Rule %q", id)
		}

		var stored model.AllocationRule
		if err := json.Unmarshal(doc.rules[i], &stored); err != nil {
			return "", fmt.Errorf("failed to read allocation rule %q: %w", id, err)
		}

		edit.ID = stored.ID
		edit.Kind = stored.Kind
		rule, err := validRule(edit)
		if err != nil {
			return "", err
		}

		raw, err := json.Marshal(rule)
		if err != nil {
			return "", fmt.Errorf("failed to encode allocation rule: %w", err)
		}
		doc.rules[i] = raw
		edited = rule
		return doc.encode()
	})
	if err != nil {
		return model.AllocationRule{}, err
	}

	logger.Info("Allocation Rule edited", zap.String("rule_id", id), zap.String("kind", edited.Kind))
	return edited, nil
}

// RemoveAllocationRule removes a rule, whatever its kind — a rule this build
// does not understand can still be taken off.
func RemoveAllocationRule(ctx context.Context, store RotaDefaultsWriteStore, id string, logger *zap.Logger) error {
	err := store.UpdateAllocationSettings(ctx, func(current string) (string, error) {
		doc := readAllocationDocument(current)
		i := doc.find(id)
		if i < 0 {
			return "", wrapf(ErrNotFound, "there is no Allocation Rule %q", id)
		}
		doc.rules = slices.Delete(doc.rules, i, i+1)
		return doc.encode()
	})
	if err != nil {
		return err
	}

	logger.Info("Allocation Rule removed", zap.String("rule_id", id))
	return nil
}
