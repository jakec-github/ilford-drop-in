package services

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/shiftpattern"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// Overrides are part of the Rota Defaults (ADR 0013): rules that pick out some
// of the Shifts the Shift pattern generates and change them as a rota is
// defined — a new start time on the same day, a new length, a whole Shape in
// place of the default one, and Pins. They are an
// ordered list, and a later one wins.
//
// Like the rest of the Rota Defaults they seed and never reach back. Defining a
// rota is the one moment they are read; changing one later changes what the
// next rota starts from, and leaves every rota already defined as it is.

// OverrideStore reads the Overrides.
type OverrideStore interface {
	GetOverrides(ctx context.Context) ([]db.Override, error)
}

// OverrideWriteStore is what editing the Overrides needs: the Roles their
// Shapes and Pins name, the time zone that says what today is, and the writes.
type OverrideWriteStore interface {
	RoleStore
	RotaDefaultsStore
	OverrideStore
	InsertOverride(ctx context.Context, o db.Override) error
	UpdateOverride(ctx context.Context, o db.Override) error
	DeleteOverride(ctx context.Context, id string) (bool, error)
	ReorderOverrides(ctx context.Context, ids []string) error
}

// OverrideParams is an Override as an Organiser states it, whole.
type OverrideParams struct {
	RRule string
	// StartFrom is the date the rule counts from, "2006-01-02". Empty means
	// today in the Organisation's zone, as for the pattern.
	StartFrom string
	// StartTime moves every Shift the Override matches to this time of its
	// own day, "15:04". Empty leaves the start the pattern generated.
	StartTime string
	// LengthMinutes is how long they run from wherever they start. Nil leaves
	// the pattern's length.
	LengthMinutes *int
	// Shape replaces the default Shape on every Shift the Override matches.
	// Nil leaves the default; an empty list asks for nobody.
	Shape []SeatParams
	Pins  []OverridePinParams
}

// OverridePinParams is one Pin an Override makes: a Role, and exactly one of a
// volunteer or a custom entry.
type OverridePinParams struct {
	RoleID      string
	VolunteerID string
	Custom      string
}

// OverrideView is an Override as the settings screen shows it.
type OverrideView struct {
	ID string
	// Number is its place in the list, counting from one: what the screen and
	// the Define Rota preview call it by.
	Number    int
	RRule     string
	StartFrom string
	// Granularity is db.OverrideMatchesTime or db.OverrideMatchesDate.
	Granularity string
	// Description is the rule in plain English, from the one evaluator.
	// Problems is set instead for a rule this build cannot read.
	Description string
	Problems    []string
	// StartTime is "" and LengthMinutes 0 when the Override leaves them.
	StartTime     string
	LengthMinutes int
	// Shape is nil when the Override leaves the default Shape.
	Shape model.Shape
	Pins  []OverridePinView
}

// OverridePinView is one of an Override's Pins, with the Role and the person
// both named.
type OverridePinView struct {
	ID          string
	RoleID      string
	Role        string
	VolunteerID string
	Custom      string
	Name        string
}

// ListOverrides reads the Overrides in the order they apply, each described
// and with its people named.
//
// A roster that cannot be read leaves the people named by id rather than
// failing the list: this is the one screen the Overrides are edited on, and a
// Sheets hiccup should not lock an Organiser out of it.
func ListOverrides(
	ctx context.Context,
	store OverrideWriteStore,
	volunteerClient VolunteerClient,
	organisation *config.Organisation,
	logger *zap.Logger,
) ([]OverrideView, error) {
	roles, err := RoleTable(ctx, store)
	if err != nil {
		return nil, err
	}
	rows, err := store.GetOverrides(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch overrides: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	volunteersByID := map[string]model.Volunteer{}
	if hasVolunteerPin(rows) {
		volunteers, err := volunteerClient.ListVolunteers(organisation, roles)
		if err != nil {
			logger.Warn("Could not read the roster to name the overrides' pins", zap.Error(err))
		}
		for _, v := range volunteers {
			volunteersByID[v.ID] = v
		}
	}

	views := make([]OverrideView, 0, len(rows))
	for i, row := range rows {
		view := OverrideView{
			ID:            row.ID,
			Number:        i + 1,
			RRule:         row.RRule,
			StartFrom:     row.StartFrom,
			Granularity:   row.Granularity,
			StartTime:     row.StartTime,
			LengthMinutes: row.LengthMinutes,
		}
		if line, _ := readOverrideRule(row.RRule, row.StartFrom); len(line.Problems) > 0 {
			view.Problems = line.Problems
		} else {
			view.Description = line.Description
		}
		if row.SetsShape {
			seats := make([]storedSeat, 0, len(row.Seats))
			for _, s := range row.Seats {
				seats = append(seats, storedSeat{RoleID: s.RoleID, Seats: s.Seats})
			}
			shape, err := resolveShape(seats, roles, fmt.Sprintf("override %d", i+1))
			if err != nil {
				return nil, err
			}
			view.Shape = shape
		}
		for _, pin := range row.Pins {
			view.Pins = append(view.Pins, OverridePinView{
				ID:          pin.ID,
				RoleID:      pin.RoleID,
				Role:        roleName(roles, pin.RoleID),
				VolunteerID: pin.VolunteerID,
				Custom:      pin.CustomValue,
				Name:        pinName(pin.VolunteerID, pin.CustomValue, volunteersByID, logger),
			})
		}
		sortOverridePins(view.Pins, roles)
		views = append(views, view)
	}
	return views, nil
}

func hasVolunteerPin(rows []db.Override) bool {
	for _, row := range rows {
		for _, pin := range row.Pins {
			if pin.VolunteerID != "" {
				return true
			}
		}
	}
	return false
}

// sortOverridePins puts an Override's Pins in reading order: by the order
// their Seats are filled, then by name, as a Shift reads everywhere else.
func sortOverridePins(pins []OverridePinView, roles model.Roles) {
	sort.SliceStable(pins, func(i, j int) bool {
		if pi, pj := rolePriority(roles, pins[i].RoleID), rolePriority(roles, pins[j].RoleID); pi != pj {
			return pi < pj
		}
		return pins[i].Name < pins[j].Name
	})
}

// AddOverride records a new Override at the end of the list, where it wins
// over every one before it.
func AddOverride(
	ctx context.Context,
	store OverrideWriteStore,
	volunteerClient VolunteerClient,
	organisation *config.Organisation,
	params OverrideParams,
	logger *zap.Logger,
) error {
	row, err := statedOverride(ctx, store, volunteerClient, organisation, params)
	if err != nil {
		return err
	}
	row.ID = uuid.New().String()
	if err := store.InsertOverride(ctx, row); err != nil {
		return overrideWriteError(err)
	}
	logger.Info("Override added", zap.String("id", row.ID), zap.String("rrule", row.RRule),
		zap.Bool("sets_shape", row.SetsShape), zap.Int("pins", len(row.Pins)))
	return nil
}

// UpdateOverride rewrites an Override whole, keeping its place in the list.
func UpdateOverride(
	ctx context.Context,
	store OverrideWriteStore,
	volunteerClient VolunteerClient,
	organisation *config.Organisation,
	id string,
	params OverrideParams,
	logger *zap.Logger,
) error {
	row, err := statedOverride(ctx, store, volunteerClient, organisation, params)
	if err != nil {
		return err
	}
	row.ID = id
	if err := store.UpdateOverride(ctx, row); err != nil {
		return overrideWriteError(err)
	}
	logger.Info("Override updated", zap.String("id", row.ID), zap.String("rrule", row.RRule),
		zap.Bool("sets_shape", row.SetsShape), zap.Int("pins", len(row.Pins)))
	return nil
}

func overrideWriteError(err error) error {
	switch {
	case errors.Is(err, db.ErrOverrideNotFound):
		return wrapf(ErrNotFound, "that override no longer exists - reload to see the overrides as they stand")
	case errors.Is(err, db.ErrDuplicateOverridePin):
		return wrapf(ErrInvalidInput, "one override pins the same person twice; pin them once")
	}
	return fmt.Errorf("failed to save the override: %w", err)
}

// RemoveOverride deletes an Override. The Pins it has already made on defined
// rotas are ordinary Pins and stay.
func RemoveOverride(ctx context.Context, store OverrideWriteStore, id string, logger *zap.Logger) error {
	deleted, err := store.DeleteOverride(ctx, id)
	if err != nil {
		return err
	}
	if !deleted {
		return wrapf(ErrNotFound, "that override no longer exists - reload to see the overrides as they stand")
	}
	logger.Info("Override removed", zap.String("id", id))
	return nil
}

// ReorderOverrides puts the Overrides in the order given, the first applied
// first and the last winning. The order must name every Override once: one
// written from a stale list would otherwise drop an Override somebody else
// added, or leave two sharing a place.
func ReorderOverrides(ctx context.Context, store OverrideWriteStore, ids []string, logger *zap.Logger) error {
	rows, err := store.GetOverrides(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch overrides: %w", err)
	}
	current := make([]string, 0, len(rows))
	for _, row := range rows {
		current = append(current, row.ID)
	}
	given := slices.Clone(ids)
	slices.Sort(current)
	slices.Sort(given)
	if !slices.Equal(current, given) {
		return wrapf(ErrConflict, "the overrides have changed since they were listed - reload and move them again")
	}
	if err := store.ReorderOverrides(ctx, ids); err != nil {
		if errors.Is(err, db.ErrOverrideNotFound) {
			return wrapf(ErrConflict, "the overrides have changed since they were listed - reload and move them again")
		}
		return fmt.Errorf("failed to reorder the overrides: %w", err)
	}
	logger.Info("Overrides reordered", zap.Strings("ids", ids))
	return nil
}

// OverrideRuleReading is what an Override's rule says, for the screen it is
// being written on.
type OverrideRuleReading struct {
	StartFrom   string
	Granularity string
	Description string
	Problems    []string
}

// ReadOverrideRule reads a rule that has not been saved: what it says, what it
// matches, or what is wrong with it. It refuses only a Start from that is not a
// date. A blank one is today, as saving would make it.
func ReadOverrideRule(ctx context.Context, store RotaDefaultsStore, rrule, startFrom string) (*OverrideRuleReading, error) {
	startFrom, err := overrideStartFrom(ctx, store, startFrom)
	if err != nil {
		return nil, err
	}
	line, rule := readOverrideRule(rrule, startFrom)
	reading := &OverrideRuleReading{StartFrom: startFrom, Description: line.Description, Problems: line.Problems}
	if rule != nil {
		reading.Granularity = granularityOf(rule)
	}
	return reading, nil
}

// statedOverride turns an Organiser's Override into the row to write, or says
// why it will not.
//
// It refuses only what cannot be represented (ADR 0010): a rule the evaluator
// cannot read, a start that is not a time of day, a length no Shift can run
// for, a Role that does not exist, a Pin naming nobody or two people, one
// person pinned twice. A start or length that makes Shifts overlap is refused
// when a rota is defined, where the Shifts it would clash with are known. Pinning somebody into a Role they do not hold, or
// past the Seats a Shape asks for, is an Organiser's decision.
func statedOverride(
	ctx context.Context,
	store OverrideWriteStore,
	volunteerClient VolunteerClient,
	organisation *config.Organisation,
	params OverrideParams,
) (db.Override, error) {
	startFrom, err := overrideStartFrom(ctx, store, params.StartFrom)
	if err != nil {
		return db.Override{}, err
	}
	rrule := strings.TrimSpace(params.RRule)
	line, rule := readOverrideRule(rrule, startFrom)
	if rule == nil {
		return db.Override{}, wrapf(ErrInvalidInput, "the rule cannot be used - %s", strings.Join(line.Problems, "; "))
	}

	roles, err := RoleTable(ctx, store)
	if err != nil {
		return db.Override{}, err
	}

	row := db.Override{RRule: rrule, StartFrom: startFrom, Granularity: granularityOf(rule)}
	if start := strings.TrimSpace(params.StartTime); start != "" {
		at, err := time.Parse("15:04", start)
		if err != nil {
			return db.Override{}, wrapf(ErrInvalidInput, "%q is not a time of day - write the start time as 19:00", start)
		}
		row.StartTime = at.Format("15:04")
	}
	if params.LengthMinutes != nil {
		length := *params.LengthMinutes
		if length <= 0 || length > maxShiftLengthMinutes {
			return db.Override{}, wrapf(ErrInvalidInput, "a shift lasts from 1 minute to 24 hours, not %d minutes", length)
		}
		row.LengthMinutes = length
	}
	if params.Shape != nil {
		seats, err := statedSeats(params.Shape, roles)
		if err != nil {
			return db.Override{}, err
		}
		row.SetsShape = true
		for _, s := range seats {
			row.Seats = append(row.Seats, db.OverrideSeat{RoleID: s.RoleID, Seats: s.Seats})
		}
	}

	pinned := map[string]bool{}
	checkRoster := false
	for _, p := range params.Pins {
		if _, ok := roles.ByID(p.RoleID); !ok {
			return db.Override{}, wrapf(ErrInvalidInput, "role %q is not a known role", p.RoleID)
		}
		custom := strings.TrimSpace(p.Custom)
		if (p.VolunteerID == "") == (custom == "") {
			return db.Override{}, wrapf(ErrInvalidInput, "a pin names exactly one of a volunteer or a custom entry")
		}
		if p.VolunteerID != "" {
			if pinned[p.VolunteerID] {
				return db.Override{}, wrapf(ErrInvalidInput, "one override pins the same person twice; pin them once")
			}
			pinned[p.VolunteerID] = true
			checkRoster = true
		}
		row.Pins = append(row.Pins, db.OverridePin{
			ID:          uuid.New().String(),
			RoleID:      p.RoleID,
			VolunteerID: p.VolunteerID,
			CustomValue: custom,
		})
	}

	// Checked against the roster as it stands today, which is a courtesy
	// rather than a guarantee: a person can leave between now and the next
	// rota, and the pre-solve check is what catches that.
	if checkRoster {
		volunteers, err := volunteerClient.ListVolunteers(organisation, roles)
		if err != nil {
			return db.Override{}, fmt.Errorf("failed to fetch volunteers: %w", err)
		}
		known := make(map[string]bool, len(volunteers))
		for _, v := range volunteers {
			known[v.ID] = true
		}
		for id := range pinned {
			if !known[id] {
				return db.Override{}, wrapf(ErrNotFound, "volunteer %s not found", id)
			}
		}
	}
	return row, nil
}

// overrideStartFrom reads the date a rule counts from, a blank one being
// today in the Organisation's zone.
func overrideStartFrom(ctx context.Context, store RotaDefaultsStore, startFrom string) (string, error) {
	startFrom = strings.TrimSpace(startFrom)
	if startFrom == "" {
		defaults, err := RotaDefaults(ctx, store)
		if err != nil {
			return "", err
		}
		loc, err := time.LoadLocation(defaults.Timezone())
		if err != nil {
			loc = time.UTC
		}
		return time.Now().In(loc).Format("2006-01-02"), nil
	}
	if _, err := time.Parse("2006-01-02", startFrom); err != nil {
		return "", wrapf(ErrInvalidInput, "%q is not a date - write start from as 2026-08-02", startFrom)
	}
	return startFrom, nil
}

// readOverrideRule reads a stored or stated rule against a Start from that is
// already known to be a date.
func readOverrideRule(rrule, startFrom string) (shiftpattern.Line, *shiftpattern.Rule) {
	anchor, err := time.Parse("2006-01-02", startFrom)
	if err != nil {
		return shiftpattern.Line{Text: rrule, Problems: []string{fmt.Sprintf("its start from %q is not a date", startFrom)}}, nil
	}
	return shiftpattern.ReadRule(rrule, anchor)
}

func granularityOf(rule *shiftpattern.Rule) string {
	if rule.Exact() {
		return db.OverrideMatchesTime
	}
	return db.OverrideMatchesDate
}
