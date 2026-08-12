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

// PreallocationStore defines the database operations the preallocation flows
// need. The mutating flows do their state read, validation, and write inside
// WithRotaPreallocationLock so a concurrent mutation or allocation of the same
// rota cannot slip between the duplicate/frozen checks and the write (issue #39,
// mirroring the changeRota locking discipline). ListPreallocations reads outside
// any lock.
type PreallocationStore interface {
	RoleStore
	GetShiftByDate(ctx context.Context, date time.Time) (*db.Shift, error)
	GetPreallocationByID(ctx context.Context, id string) (*db.Preallocation, *db.Shift, error)
	GetPreallocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.Preallocation, error)
	GetShiftsInRange(ctx context.Context, from, to time.Time) ([]db.ShiftInRange, error)
	WithRotaPreallocationLock(ctx context.Context, rotaIDs []string, fn func(store db.PreallocationTxStore) error) error
}

// AddPreallocationParams holds the input for pinning one assignee to a shift.
// Exactly one of VolunteerID or Custom is set. RoleID names the Seat the pin
// fills and is required — a pin is a promise about a job.
type AddPreallocationParams struct {
	Date        string // Target shift date (YYYY-MM-DD)
	VolunteerID string // Volunteer to pin
	Custom      string // Custom (non-volunteer) entry to pin
	RoleID      string // Id of the Role the pin fills
}

// PreallocationView is the read model for one preallocation: who is pinned, to
// which date, in which role.
//
// Role carries both the id and the name, as StandingPreallocationView does and
// for the same reason: the id is what the row references and what an edit would
// name, the name is what an admin recognises (issue #195).
//
// There is one kind of these (issue #131). A pin an admin added by hand and a
// pin a Standing Preallocation seeded when the rota was defined are the same
// row, read the same way, and either may be removed — so nothing here says where
// it came from, and nothing downstream branches on it.
type PreallocationView struct {
	ID          string
	Date        string
	RoleID      string
	Role        string // the Role's name today
	VolunteerID string
	Custom      string
	Name        string // volunteer display name, or the custom entry verbatim
}

// ListPreallocationsParams bounds a preallocation listing by shift date,
// mirroring ListShifts. A zero bound is left open.
type ListPreallocationsParams struct {
	From string // inclusive lower bound (YYYY-MM-DD), optional
	To   string // inclusive upper bound (YYYY-MM-DD), optional
}

// AddPreallocation validates and records a single preallocation. The volunteer
// fetch (network) happens outside the rota lock; the frozen-rota and
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
		zap.String("role_id", params.RoleID))

	// Step 1: input shape — exactly one of volunteer / custom, filling a Role
	// that exists. An unknown Role would reach the solver as a Seat no Shape
	// has, so it is refused here rather than at solve time.
	if (params.VolunteerID == "") == (params.Custom == "") {
		return nil, wrapf(ErrInvalidInput, "exactly one of volunteerId or custom must be provided")
	}
	if params.RoleID == "" {
		return nil, wrapf(ErrInvalidInput, "role is required")
	}
	roles, err := RoleTable(ctx, store)
	if err != nil {
		return nil, err
	}
	role, ok := roles.ByID(params.RoleID)
	if !ok {
		return nil, wrapf(ErrInvalidInput, "role %q is not a known role", params.RoleID)
	}

	// Step 2: volunteer validation (network fetch, OUTSIDE the lock). The
	// display name comes back with it, so the created view reads the same as a
	// listed one without a second fetch.
	name := params.Custom
	if params.VolunteerID != "" {
		volunteers, err := volunteerClient.ListVolunteers(cfg, roles)
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
		// By name, because the roster is a Google Sheet that spells a Role out
		// in a cell. The id is what the pin is stored under; the name is how
		// the sheet says who holds it.
		if !vol.Holds(role.Name) {
			return nil, wrapf(ErrInvalidInput, "volunteer %s does not hold the role %q", params.VolunteerID, role.Name)
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

	// A closed shift is a day the drop-in does not run, so there is no Seat to
	// promise anyone. Reopening it is the way to make one.
	if shift.Closed {
		return nil, wrapf(ErrConflict, "shift for %s is closed", params.Date)
	}

	// Step 4: state read, duplicate/frozen checks, and insert under the rota
	// lock.
	created := db.Preallocation{
		ID:          uuid.New().String(),
		ShiftID:     shift.ID,
		RoleID:      role.ID,
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

		existing, err := tx.GetPreallocationsByShiftIDs(ctx, []string{shift.ID})
		if err != nil {
			return err
		}
		// A volunteer may be promised a Shift once: a person fills at most one
		// Seat on it, so a second pin is a slip. A custom entry may be promised
		// it twice, because it is usually an organisation and an organisation
		// routinely sends two people (issue #195) — the Seats below are what
		// bounds how many.
		filled := 0
		for _, p := range existing {
			if params.VolunteerID != "" && p.VolunteerID == params.VolunteerID {
				return wrapf(ErrConflict, "volunteer %s is already pinned to %s", params.VolunteerID, params.Date)
			}
			if p.RoleID == role.ID {
				filled++
			}
		}
		// A Role has only the Seats this Shift's Shape gives it, and pinning
		// past them would hand the solver a shift it cannot fill legally. It
		// used to be the Role's own ceiling that said so; the Shape is what
		// says it now, which is the same rule a Shape edit is held to from the
		// other side (seatsHoldThePins, issue #185).
		shapes, err := tx.GetShiftShapes(ctx, []string{shift.ID})
		if err != nil {
			return err
		}
		if filled >= seatsForRole(shapes[shift.ID], role) {
			return wrapf(ErrConflict, "every %s seat for %s is already pinned", role.Name, params.Date)
		}

		return tx.InsertPreallocation(ctx, created)
	})
	if err != nil {
		return nil, err
	}

	logger.Info("Preallocation recorded",
		zap.String("id", created.ID),
		zap.String("shift_id", created.ShiftID),
		zap.String("role", role.Name))

	return &PreallocationView{
		ID:          created.ID,
		Date:        shift.Date,
		RoleID:      created.RoleID,
		Role:        role.Name,
		VolunteerID: created.VolunteerID,
		Custom:      created.CustomValue,
		Name:        name,
	}, nil
}

// DeletePreallocation removes a preallocation by id, rejecting a delete on an
// already-allocated (frozen) rota. Any of them may be removed, however it came
// to exist (issue #131). Resolving the pin to its rota happens
// before the lock; the frozen check and the delete happen inside it.
func DeletePreallocation(
	ctx context.Context,
	store PreallocationStore,
	id string,
	logger *zap.Logger,
) error {
	logger.Debug("Starting DeletePreallocation", zap.String("id", id))

	_, shift, err := store.GetPreallocationByID(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to look up preallocation %s: %w", id, err)
	}
	if shift == nil {
		return wrapf(ErrNotFound, "preallocation %s not found", id)
	}

	err = store.WithRotaPreallocationLock(ctx, []string{shift.RotaID}, func(tx db.PreallocationTxStore) error {
		allocated, err := tx.RotaAllocated(ctx, shift.RotaID)
		if err != nil {
			return err
		}
		if allocated {
			return wrapf(ErrConflict, "rota is already allocated")
		}
		deleted, err := tx.DeletePreallocationByID(ctx, id)
		if err != nil {
			return err
		}
		if !deleted {
			// Lost a race with a concurrent delete under the same lock.
			return wrapf(ErrNotFound, "preallocation %s not found", id)
		}
		return nil
	})
	if err != nil {
		return err
	}

	logger.Info("Preallocation deleted", zap.String("id", id))
	return nil
}

// ListPreallocations returns every preallocation whose shift falls in the given
// date range.
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

	// A closed shift carries no pins: the drop-in is not running, so nobody is
	// promised it, and InitShifts strips whatever it was carrying. Dropping them
	// here is what stops the rota page listing people against a shut date.
	dateByShiftID := make(map[string]string, len(shifts))
	shiftIDs := make([]string, 0, len(shifts))
	for _, s := range shifts {
		if s.Closed {
			continue
		}
		dateByShiftID[s.ID] = s.Date
		shiftIDs = append(shiftIDs, s.ID)
	}

	// Names are resolved here rather than left to the caller: a pin is only
	// legible as a person, and the row holds a volunteer id.
	roles, err := RoleTable(ctx, store)
	if err != nil {
		return nil, err
	}

	volunteers, err := volunteerClient.ListVolunteers(cfg, roles)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch volunteers: %w", err)
	}
	volunteersByID := make(map[string]model.Volunteer, len(volunteers))
	for _, v := range volunteers {
		volunteersByID[v.ID] = v
	}

	pins, err := store.GetPreallocationsByShiftIDs(ctx, shiftIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch preallocations: %w", err)
	}
	views := make([]PreallocationView, 0, len(pins))
	for _, p := range pins {
		views = append(views, PreallocationView{
			ID:          p.ID,
			Date:        dateByShiftID[p.ShiftID],
			RoleID:      p.RoleID,
			Role:        roleName(roles, p.RoleID),
			VolunteerID: p.VolunteerID,
			Custom:      p.CustomValue,
			Name:        preallocationName(p.VolunteerID, p.CustomValue, volunteersByID, logger),
		})
	}

	sortPreallocationViews(views, roles)
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

// sortPreallocationViews puts the pins in reading order — by date, then by Role
// priority, then by name — so a listing is stable whatever order the rows came
// back in. Role priority is the order Seats are filled in, which is the order a
// shift reads in everywhere else.
func sortPreallocationViews(views []PreallocationView, roles model.Roles) {
	sort.Slice(views, func(i, j int) bool {
		a, b := views[i], views[j]
		if a.Date != b.Date {
			return a.Date < b.Date
		}
		if pa, pb := rolePriority(roles, a.RoleID), rolePriority(roles, b.RoleID); pa != pb {
			return pa < pb
		}
		if a.Role != b.Role {
			return a.Role < b.Role
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.ID < b.ID
	})
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
