package services

import (
	"context"
	"fmt"
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

// PreallocationView is the read model for a manual preallocation: the stored
// row plus its shift's date, so callers render dates without re-resolving them.
type PreallocationView struct {
	ID          string
	Date        string
	Role        string
	VolunteerID string
	Custom      string
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

	// Step 2: volunteer validation (network fetch, OUTSIDE the lock).
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
		if params.TeamLead && vol.Role != model.RoleTeamLead {
			return nil, wrapf(ErrInvalidInput, "volunteer %s is not a team lead", params.VolunteerID)
		}
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

// ListPreallocations returns the manual preallocations whose shift falls in the
// given date range. It resolves the range to shifts first, so each pin can be
// returned with its date and the id→date mapping stays honest.
func ListPreallocations(
	ctx context.Context,
	store PreallocationStore,
	params ListPreallocationsParams,
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
	for _, s := range shifts {
		dateByShiftID[s.ID] = s.Date
		shiftIDs = append(shiftIDs, s.ID)
	}

	pins, err := store.GetManualPreallocationsByShiftIDs(ctx, shiftIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch manual preallocations: %w", err)
	}

	views := make([]PreallocationView, 0, len(pins))
	for _, p := range pins {
		views = append(views, PreallocationView{
			ID:          p.ID,
			Date:        dateByShiftID[p.ShiftID],
			Role:        p.Role,
			VolunteerID: p.VolunteerID,
			Custom:      p.CustomValue,
		})
	}
	return views, nil
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
		if o.PreallocatedTeamLeadID != "" {
			pinsTeamLead = true
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
