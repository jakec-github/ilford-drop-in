package services

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services/utils"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// PreallocationStore defines the database operations the manual-preallocation
// flows need. The mutating flows do their state read, validation, and write
// inside WithRotaPreallocationLock so a concurrent mutation or allocation of the
// same rota cannot slip between the duplicate/frozen checks and the write
// (issue #39, mirroring the changeRota locking discipline). ListPreallocations
// reads outside any lock.
type PreallocationStore interface {
	GetShiftByDate(ctx context.Context, date time.Time) (*db.Shift, error)
	GetManualPreallocationByID(ctx context.Context, id string) (*db.ManualPreallocation, *db.Shift, error)
	GetManualPreallocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.ManualPreallocation, error)
	GetShiftsInRange(ctx context.Context, from, to time.Time) ([]db.ShiftInRange, error)
	WithRotaPreallocationLock(ctx context.Context, rotaIDs []string, fn func(store db.PreallocationTxStore) error) error
}

// AddPreallocationParams holds the input for pinning one assignee to a shift.
// Exactly one of VolunteerID or Custom is set; TeamLead only accompanies a
// VolunteerID.
type AddPreallocationParams struct {
	Date        string // Target shift date (YYYY-MM-DD)
	VolunteerID string // Volunteer to pin
	Custom      string // Custom (non-volunteer) entry to pin
	TeamLead    bool   // Pin the volunteer in the team-lead slot
}

// PreallocationSource says where a pin came from. Both sources union at
// allocation time and are the same mechanism downstream (ADR 0003), but only a
// manual pin has a row behind it to edit or delete, so a reader has to be able
// to tell them apart.
type PreallocationSource string

const (
	// PreallocationSourceConfig is a pin derived from a config Rota Override.
	// It has no stored row and no id: changing it means editing the config.
	PreallocationSourceConfig PreallocationSource = "config"
	// PreallocationSourceManual is a pin recorded against a shift over HTTP.
	PreallocationSourceManual PreallocationSource = "manual"
)

// PreallocationView is the read model for one preallocation, whatever its
// source: who is pinned, to which date, in which role. Config pins carry no ID —
// they are resolved from the rota overrides on every read, not stored.
type PreallocationView struct {
	ID          string // empty for config pins
	Date        string
	Role        string
	VolunteerID string
	Custom      string
	Name        string // volunteer display name, or the custom entry verbatim
	Source      PreallocationSource
}

// ListPreallocationsParams bounds a preallocation listing by shift date,
// mirroring ListShifts. A zero bound is left open.
type ListPreallocationsParams struct {
	From string // inclusive lower bound (YYYY-MM-DD), optional
	To   string // inclusive upper bound (YYYY-MM-DD), optional
}

// AddPreallocation validates and records a single manual preallocation. The
// volunteer fetch (network) happens outside the rota lock; the frozen-rota and
// duplicate-assignee checks and the insert happen inside it.
func AddPreallocation(
	ctx context.Context,
	store PreallocationStore,
	volunteerClient VolunteerClient,
	cfg *config.Config,
	params AddPreallocationParams,
	logger *zap.Logger,
) (*PreallocationView, error) {
	logger.Debug("Starting AddPreallocation",
		zap.String("date", params.Date),
		zap.String("volunteer_id", params.VolunteerID),
		zap.String("custom", params.Custom),
		zap.Bool("team_lead", params.TeamLead))

	// Step 1: input shape — exactly one of volunteer / custom, and team lead only
	// applies to a volunteer pin.
	if (params.VolunteerID == "") == (params.Custom == "") {
		return nil, wrapf(ErrInvalidInput, "exactly one of volunteerId or custom must be provided")
	}
	if params.TeamLead && params.VolunteerID == "" {
		return nil, wrapf(ErrInvalidInput, "teamLead can only be set for a volunteer pin")
	}

	// Step 2: volunteer validation (network fetch, OUTSIDE the lock). The
	// display name comes back with it, so the created view reads the same as a
	// listed one without a second fetch.
	name := params.Custom
	if params.VolunteerID != "" {
		volunteers, err := volunteerClient.ListVolunteers(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch volunteers: %w", err)
		}
		var vol *model.Volunteer
		for i := range volunteers {
			if volunteers[i].ID == params.VolunteerID {
				vol = &volunteers[i]
				break
			}
		}
		if vol == nil {
			return nil, wrapf(ErrNotFound, "volunteer %s not found", params.VolunteerID)
		}
		if len(utils.FilterActiveVolunteers([]model.Volunteer{*vol})) == 0 {
			return nil, wrapf(ErrInvalidInput, "volunteer %s is not active", params.VolunteerID)
		}
		if params.TeamLead && !vol.Holds(string(model.RoleTeamLead)) {
			return nil, wrapf(ErrInvalidInput, "volunteer %s is not a team lead", params.VolunteerID)
		}
		name = vol.DisplayName
	}

	// Step 3: resolve the date to its shift (unknown date → not found).
	date, err := time.Parse("2006-01-02", params.Date)
	if err != nil {
		return nil, wrapf(ErrInvalidInput, "invalid date format %q: expected YYYY-MM-DD", params.Date)
	}
	shift, err := store.GetShiftByDate(ctx, date)
	if err != nil {
		return nil, fmt.Errorf("failed to look up shift for date %s: %w", params.Date, err)
	}
	if shift == nil {
		return nil, wrapf(ErrNotFound, "date %s is not in any rota", params.Date)
	}

	// Step 4: config checks for the date (no network). A Closed override blocks
	// any pin; config is authoritative for the single-valued team-lead slot.
	closed, configPinsTL, err := configPreallocationState(cfg, date)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate config overrides for %s: %w", params.Date, err)
	}
	if closed {
		return nil, wrapf(ErrConflict, "shift for %s is closed", params.Date)
	}
	if params.TeamLead && configPinsTL {
		return nil, wrapf(ErrConflict, "config already pins a team lead for %s", params.Date)
	}

	// Step 5: state read, duplicate/frozen checks, and insert under the rota
	// lock.
	role := string(model.RoleVolunteer)
	if params.TeamLead {
		role = string(model.RoleTeamLead)
	}
	created := db.ManualPreallocation{
		ID:          uuid.New().String(),
		ShiftID:     shift.ID,
		Role:        role,
		VolunteerID: params.VolunteerID,
		CustomValue: params.Custom,
	}

	err = store.WithRotaPreallocationLock(ctx, []string{shift.RotaID}, func(tx db.PreallocationTxStore) error {
		allocated, err := tx.RotaAllocated(ctx, shift.RotaID)
		if err != nil {
			return err
		}
		if allocated {
			return wrapf(ErrConflict, "rota for %s is already allocated", params.Date)
		}

		existing, err := tx.GetManualPreallocationsByShiftIDs(ctx, []string{shift.ID})
		if err != nil {
			return err
		}
		for _, p := range existing {
			if params.VolunteerID != "" && p.VolunteerID == params.VolunteerID {
				return wrapf(ErrConflict, "volunteer %s is already pinned to %s", params.VolunteerID, params.Date)
			}
			if params.Custom != "" && p.CustomValue == params.Custom {
				return wrapf(ErrConflict, "custom entry %q is already pinned to %s", params.Custom, params.Date)
			}
			if params.TeamLead && p.Role == string(model.RoleTeamLead) {
				return wrapf(ErrConflict, "a team lead is already pinned to %s", params.Date)
			}
		}

		return tx.InsertManualPreallocation(ctx, created)
	})
	if err != nil {
		return nil, err
	}

	logger.Info("Manual preallocation recorded",
		zap.String("id", created.ID),
		zap.String("shift_id", created.ShiftID),
		zap.String("role", created.Role))

	return &PreallocationView{
		ID:          created.ID,
		Date:        shift.Date,
		Role:        created.Role,
		VolunteerID: created.VolunteerID,
		Custom:      created.CustomValue,
		Name:        name,
		Source:      PreallocationSourceManual,
	}, nil
}

// DeletePreallocation removes a manual preallocation by id, rejecting a delete
// on an already-allocated (frozen) rota. Resolving the pin to its rota happens
// before the lock; the frozen check and the delete happen inside it.
func DeletePreallocation(
	ctx context.Context,
	store PreallocationStore,
	id string,
	logger *zap.Logger,
) error {
	logger.Debug("Starting DeletePreallocation", zap.String("id", id))

	_, shift, err := store.GetManualPreallocationByID(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to look up manual preallocation %s: %w", id, err)
	}
	if shift == nil {
		return wrapf(ErrNotFound, "manual preallocation %s not found", id)
	}

	err = store.WithRotaPreallocationLock(ctx, []string{shift.RotaID}, func(tx db.PreallocationTxStore) error {
		allocated, err := tx.RotaAllocated(ctx, shift.RotaID)
		if err != nil {
			return err
		}
		if allocated {
			return wrapf(ErrConflict, "rota is already allocated")
		}
		deleted, err := tx.DeleteManualPreallocationByID(ctx, id)
		if err != nil {
			return err
		}
		if !deleted {
			// Lost a race with a concurrent delete under the same lock.
			return wrapf(ErrNotFound, "manual preallocation %s not found", id)
		}
		return nil
	})
	if err != nil {
		return err
	}

	logger.Info("Manual preallocation deleted", zap.String("id", id))
	return nil
}

// ListPreallocations returns every preallocation that applies to a shift in the
// given date range, from both sources: the manual pins stored against those
// shifts, and the pins the config's Rota Overrides resolve to for their dates.
// Config pins are not stored anywhere, so they are re-derived here through the
// same override machinery allocation uses — what this returns is what the
// allocator will be handed.
//
// It resolves the range to shifts first, so every pin comes back with its date
// and the id→date mapping stays honest.
func ListPreallocations(
	ctx context.Context,
	store PreallocationStore,
	volunteerClient VolunteerClient,
	cfg *config.Config,
	params ListPreallocationsParams,
	logger *zap.Logger,
) ([]PreallocationView, error) {
	from, to, err := parseDateRange(params.From, params.To)
	if err != nil {
		return nil, err
	}

	shifts, err := store.GetShiftsInRange(ctx, from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch shifts in range: %w", err)
	}

	dateByShiftID := make(map[string]string, len(shifts))
	shiftIDs := make([]string, 0, len(shifts))
	// The rrule matchers search a window bounded by these dates, so a shift
	// whose date will not parse is dropped rather than widening it wrongly.
	shiftDates := make([]time.Time, 0, len(shifts))
	for _, s := range shifts {
		dateByShiftID[s.ID] = s.Date
		shiftIDs = append(shiftIDs, s.ID)
		date, err := time.Parse("2006-01-02", s.Date)
		if err != nil {
			logger.Warn("Skipping shift with unparseable date", zap.String("date", s.Date))
			continue
		}
		shiftDates = append(shiftDates, date)
	}

	// Names are resolved here rather than left to the caller: a pin is only
	// legible as a person, and a config pin is a bare id in a YAML file.
	volunteers, err := volunteerClient.ListVolunteers(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch volunteers: %w", err)
	}
	volunteersByID := make(map[string]model.Volunteer, len(volunteers))
	for _, v := range volunteers {
		volunteersByID[v.ID] = v
	}

	views, err := configPreallocationViews(cfg, shiftDates, volunteersByID, logger)
	if err != nil {
		return nil, err
	}

	pins, err := store.GetManualPreallocationsByShiftIDs(ctx, shiftIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch manual preallocations: %w", err)
	}
	for _, p := range pins {
		views = append(views, PreallocationView{
			ID:          p.ID,
			Date:        dateByShiftID[p.ShiftID],
			Role:        p.Role,
			VolunteerID: p.VolunteerID,
			Custom:      p.CustomValue,
			Name:        preallocationName(p.VolunteerID, p.CustomValue, volunteersByID, logger),
			Source:      PreallocationSourceManual,
		})
	}

	sortPreallocationViews(views)
	return views, nil
}

// configPreallocationViews resolves the config Rota Overrides to per-date pins
// for the given shift dates. It goes through convertRotaOverrides and
// configPreallocationsForDate — the same pair allocation uses — so a date's pins
// read here exactly as InitShifts will apply them: sets, not sequences (two
// overrides pinning the same person are one seat), the last matching override
// winning the single-valued team-lead slot, and a closed date carrying none.
func configPreallocationViews(
	cfg *config.Config,
	shiftDates []time.Time,
	volunteersByID map[string]model.Volunteer,
	logger *zap.Logger,
) ([]PreallocationView, error) {
	if cfg == nil || len(cfg.RotaOverrides) == 0 || len(shiftDates) == 0 {
		return nil, nil
	}

	overrides, err := convertRotaOverrides(cfg.RotaOverrides, shiftDates, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate config overrides: %w", err)
	}

	var views []PreallocationView
	for _, d := range shiftDates {
		date := d.Format("2006-01-02")
		volIDs, teamLead, customs, closed := configPreallocationsForDate(date, overrides)
		if closed {
			continue
		}

		configView := func(role model.LegacyRole, volunteerID, custom string) PreallocationView {
			return PreallocationView{
				Date:        date,
				Role:        string(role),
				VolunteerID: volunteerID,
				Custom:      custom,
				Name:        preallocationName(volunteerID, custom, volunteersByID, logger),
				Source:      PreallocationSourceConfig,
			}
		}

		if teamLead != "" {
			views = append(views, configView(model.RoleTeamLead, teamLead, ""))
		}
		for id := range volIDs {
			// Someone named as both the team lead and an ordinary preallocation
			// still occupies one seat; showing them twice would misreport the
			// shift as fuller than it is.
			if id == teamLead {
				continue
			}
			views = append(views, configView(model.RoleVolunteer, id, ""))
		}
		for custom := range customs {
			views = append(views, configView(model.RoleVolunteer, "", custom))
		}
	}
	return views, nil
}

// preallocationName resolves a pin to something a person can read. A custom
// entry is its own name; an unknown volunteer id degrades to the raw id rather
// than an empty chip, since that pin is exactly what will fail the pre-solve
// check and hiding it hides the fix.
func preallocationName(volunteerID, custom string, volunteersByID map[string]model.Volunteer, logger *zap.Logger) string {
	if custom != "" {
		return custom
	}
	volunteer, ok := volunteersByID[volunteerID]
	if !ok || volunteer.DisplayName == "" {
		logger.Warn("Preallocated volunteer not found in roster, using raw ID",
			zap.String("volunteer_id", volunteerID))
		return volunteerID
	}
	return volunteer.DisplayName
}

// sortPreallocationViews puts the pins in reading order — by date, then the team
// lead first, then by name — so a listing is stable whichever order the two
// sources produced their entries in (config pins come out of maps).
func sortPreallocationViews(views []PreallocationView) {
	sort.Slice(views, func(i, j int) bool {
		a, b := views[i], views[j]
		if a.Date != b.Date {
			return a.Date < b.Date
		}
		aLead := a.Role == string(model.RoleTeamLead)
		bLead := b.Role == string(model.RoleTeamLead)
		if aLead != bLead {
			return aLead
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		return a.ID < b.ID
	})
}

// configPreallocationState resolves the config Rota Overrides for a single date,
// reporting whether the date is closed and whether config pins a team lead
// there. It builds one rrule matcher per override over a single-date window
// (NewRRuleMatcher widens the window by a week, so a lone date matches
// correctly).
func configPreallocationState(cfg *config.Config, date time.Time) (closed, pinsTeamLead bool, err error) {
	if cfg == nil {
		return false, false, nil
	}
	dateStr := date.Format("2006-01-02")
	for _, o := range cfg.RotaOverrides {
		matcher, err := utils.NewRRuleMatcher(o.RRule, []time.Time{date})
		if err != nil {
			return false, false, fmt.Errorf("invalid rrule %q: %w", o.RRule, err)
		}
		if !matcher(dateStr) {
			continue
		}
		if o.Closed {
			closed = true
		}
		for _, pin := range o.Preallocations {
			if pin.Role == string(model.RoleTeamLead) && pin.VolunteerID != "" {
				pinsTeamLead = true
			}
		}
	}
	return closed, pinsTeamLead, nil
}

// parseDateRange parses optional from/to bounds (YYYY-MM-DD), leaving a blank
// bound as a zero time (open).
func parseDateRange(fromStr, toStr string) (from, to time.Time, err error) {
	if fromStr != "" {
		from, err = time.Parse("2006-01-02", fromStr)
		if err != nil {
			return time.Time{}, time.Time{}, wrapf(ErrInvalidInput, "invalid from date %q: expected YYYY-MM-DD", fromStr)
		}
	}
	if toStr != "" {
		to, err = time.Parse("2006-01-02", toStr)
		if err != nil {
			return time.Time{}, time.Time{}, wrapf(ErrInvalidInput, "invalid to date %q: expected YYYY-MM-DD", toStr)
		}
	}
	return from, to, nil
}
