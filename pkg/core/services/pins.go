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
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// PinStore defines the database operations the pin flows
// need. The mutating flows do their state read, validation, and write inside
// WithRotaPinLock so a concurrent mutation or allocation of the same
// rota cannot slip between the duplicate/frozen checks and the write (issue #39,
// mirroring the changeRota locking discipline). ListPins reads outside
// any lock.
type PinStore interface {
	RoleStore
	GetShiftByID(ctx context.Context, id string) (*db.ShiftInRange, error)
	GetPinByID(ctx context.Context, id string) (*db.Pin, *db.Shift, error)
	GetPinsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.Pin, error)
	GetShiftsInRange(ctx context.Context, from, to time.Time) ([]db.ShiftInRange, error)
	WithRotaPinLock(ctx context.Context, rotaIDs []string, fn func(store db.PinTxStore) error) error
}

// AddPinParams holds the input for pinning one assignee to a shift.
// Exactly one of VolunteerID or Custom is set. RoleID names the Seat the pin
// fills and is required — a pin is a promise about a job.
type AddPinParams struct {
	ShiftID     string // Id of the shift to pin to
	VolunteerID string // Volunteer to pin
	Custom      string // Custom (non-volunteer) entry to pin
	RoleID      string // Id of the Role the pin fills
}

// PinView is the read model for one pin: who is pinned, to
// which shift, in which role. Date is the shift's, carried for display and
// ordering; ShiftID is what identifies it.
//
// Role carries both the id and the name, as StandingPreallocationView does and
// for the same reason: the id is what the row references and what an edit would
// name, the name is what an Organiser recognises (issue #195).
//
// There is one kind of these (issue #131). A pin somebody added by hand and a
// pin a Standing Preallocation seeded when the rota was defined are the same
// row, read the same way, and either may be removed — so nothing here says where
// it came from, and nothing downstream branches on it.
type PinView struct {
	ID          string
	ShiftID     string
	Date        string
	RoleID      string
	Role        string // the Role's name today
	VolunteerID string
	Custom      string
	Name        string // volunteer display name, or the custom entry verbatim
}

// ListPinsParams bounds a pin listing by shift date,
// mirroring ListShifts. A zero bound is left open.
type ListPinsParams struct {
	From string // inclusive lower bound (YYYY-MM-DD), optional
	To   string // inclusive upper bound (YYYY-MM-DD), optional
}

// AddPin validates and records a single pin. The volunteer
// fetch (network) happens outside the rota lock; the frozen-rota and
// duplicate-assignee checks and the insert happen inside it.
func AddPin(
	ctx context.Context,
	store PinStore,
	volunteerClient VolunteerClient,
	organisation *config.Organisation,
	params AddPinParams,
	logger *zap.Logger,
) (*PinView, error) {
	logger.Debug("Starting AddPin",
		zap.String("shift_id", params.ShiftID),
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
		volunteers, err := volunteerClient.ListVolunteers(organisation, roles)
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
		// Neither whether they are active nor whether they hold the Role is
		// checked: both are roster data, and the roster binds the allocator,
		// not the Organiser (ADR 0010). The pin grants the Role for this Shift
		// alone, and solveRota sends an inactive volunteer it names.
		name = vol.DisplayName
	}

	// Step 3: resolve the shift (unknown id → not found).
	if params.ShiftID == "" {
		return nil, wrapf(ErrInvalidInput, "shiftId is required")
	}
	found, err := store.GetShiftByID(ctx, params.ShiftID)
	if err != nil {
		return nil, fmt.Errorf("failed to look up shift %s: %w", params.ShiftID, err)
	}
	if found == nil {
		return nil, wrapf(ErrNotFound, "shift %s not found", params.ShiftID)
	}
	shift := found.Shift
	label := shiftLabel(shift)

	// A closed shift is one the Organisation does not run, so there is no Seat
	// to promise anyone. Reopening it is the way to make one.
	if shift.Closed {
		return nil, wrapf(ErrConflict, "the shift on %s is closed", label)
	}

	// Step 4: state read, duplicate/frozen checks, and insert under the rota
	// lock.
	created := db.Pin{
		ID:          uuid.New().String(),
		ShiftID:     shift.ID,
		RoleID:      role.ID,
		VolunteerID: params.VolunteerID,
		CustomValue: params.Custom,
	}

	err = store.WithRotaPinLock(ctx, []string{shift.RotaID}, func(tx db.PinTxStore) error {
		allocated, err := tx.RotaAllocated(ctx, shift.RotaID)
		if err != nil {
			return err
		}
		if allocated {
			return wrapf(ErrConflict, "the rota for %s is already allocated", label)
		}

		existing, err := tx.GetPinsByShiftIDs(ctx, []string{shift.ID})
		if err != nil {
			return err
		}
		// A volunteer may be promised a Shift once: a person fills at most one
		// Seat on it, so a second pin is a slip. A custom entry may be promised
		// it any number of times, because it is usually an organisation and an
		// organisation routinely sends two people (issue #195).
		//
		// The Seats are not counted. A Shape bounds the allocator, not people's
		// decisions (ADR 0010): a pin past a Role's Seats, or into a Role the
		// Shape asks for none of, is honoured, and the solver places nobody
		// else in that Role.
		for _, p := range existing {
			if params.VolunteerID != "" && p.VolunteerID == params.VolunteerID {
				return wrapf(ErrConflict, "volunteer %s is already pinned to %s", params.VolunteerID, label)
			}
		}

		return tx.InsertPin(ctx, created)
	})
	if err != nil {
		return nil, err
	}

	logger.Info("Pin recorded",
		zap.String("id", created.ID),
		zap.String("shift_id", created.ShiftID),
		zap.String("role", role.Name))

	return &PinView{
		ID:          created.ID,
		ShiftID:     shift.ID,
		Date:        shift.Date,
		RoleID:      created.RoleID,
		Role:        role.Name,
		VolunteerID: created.VolunteerID,
		Custom:      created.CustomValue,
		Name:        name,
	}, nil
}

// DeletePin removes a pin by id, rejecting a delete on an
// already-allocated (frozen) rota. Any of them may be removed, however it came
// to exist (issue #131). Resolving the pin to its rota happens
// before the lock; the frozen check and the delete happen inside it.
func DeletePin(
	ctx context.Context,
	store PinStore,
	id string,
	logger *zap.Logger,
) error {
	logger.Debug("Starting DeletePin", zap.String("id", id))

	_, shift, err := store.GetPinByID(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to look up pin %s: %w", id, err)
	}
	if shift == nil {
		return wrapf(ErrNotFound, "pin %s not found", id)
	}

	err = store.WithRotaPinLock(ctx, []string{shift.RotaID}, func(tx db.PinTxStore) error {
		allocated, err := tx.RotaAllocated(ctx, shift.RotaID)
		if err != nil {
			return err
		}
		if allocated {
			return wrapf(ErrConflict, "rota is already allocated")
		}
		deleted, err := tx.DeletePinByID(ctx, id)
		if err != nil {
			return err
		}
		if !deleted {
			// Lost a race with a concurrent delete under the same lock.
			return wrapf(ErrNotFound, "pin %s not found", id)
		}
		return nil
	})
	if err != nil {
		return err
	}

	logger.Info("Pin deleted", zap.String("id", id))
	return nil
}

// ListPins returns every pin whose shift falls in the given
// date range.
//
// It resolves the range to shifts first, so every pin comes back with its date
// and the id→date mapping stays honest.
func ListPins(
	ctx context.Context,
	store PinStore,
	volunteerClient VolunteerClient,
	organisation *config.Organisation,
	params ListPinsParams,
	logger *zap.Logger,
) ([]PinView, error) {
	from, to, err := parseDateRange(params.From, params.To)
	if err != nil {
		return nil, err
	}

	shifts, err := store.GetShiftsInRange(ctx, from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch shifts in range: %w", err)
	}

	// A closed shift carries no pins: the Organisation is not running, so nobody is
	// promised it, and InitShifts strips whatever it was carrying. Dropping them
	// here is what stops the rota page listing people against a shut date.
	shiftsByID := make(map[string]db.Shift, len(shifts))
	shiftIDs := make([]string, 0, len(shifts))
	for _, s := range shifts {
		if s.Closed {
			continue
		}
		shiftsByID[s.ID] = s.Shift
		shiftIDs = append(shiftIDs, s.ID)
	}

	// Names are resolved here rather than left to the caller: a pin is only
	// legible as a person, and the row holds a volunteer id.
	roles, err := RoleTable(ctx, store)
	if err != nil {
		return nil, err
	}

	volunteers, err := volunteerClient.ListVolunteers(organisation, roles)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch volunteers: %w", err)
	}
	volunteersByID := make(map[string]model.Volunteer, len(volunteers))
	for _, v := range volunteers {
		volunteersByID[v.ID] = v
	}

	pins, err := store.GetPinsByShiftIDs(ctx, shiftIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pins: %w", err)
	}
	views := make([]PinView, 0, len(pins))
	for _, p := range pins {
		views = append(views, PinView{
			ID:          p.ID,
			ShiftID:     p.ShiftID,
			Date:        shiftsByID[p.ShiftID].Date,
			RoleID:      p.RoleID,
			Role:        roleName(roles, p.RoleID),
			VolunteerID: p.VolunteerID,
			Custom:      p.CustomValue,
			Name:        pinName(p.VolunteerID, p.CustomValue, volunteersByID, logger),
		})
	}

	sortPinViews(views, shiftsByID, roles)
	return views, nil
}

// pinName resolves a pin to something a person can read. A custom
// entry is its own name; an unknown volunteer id degrades to the raw id rather
// than an empty chip, since that pin is exactly what will fail the pre-solve
// check and hiding it hides the fix.
func pinName(volunteerID, custom string, volunteersByID map[string]model.Volunteer, logger *zap.Logger) string {
	if custom != "" {
		return custom
	}
	volunteer, ok := volunteersByID[volunteerID]
	if !ok || volunteer.DisplayName == "" {
		logger.Warn("Pinned volunteer not found in roster, using raw ID",
			zap.String("volunteer_id", volunteerID))
		return volunteerID
	}
	return volunteer.DisplayName
}

// sortPinViews puts the pins in reading order — by when their shift starts,
// then by Role priority, then by name — so a listing is stable whatever order
// the rows came back in. Role priority is the order Seats are filled in, which
// is the order a shift reads in everywhere else.
func sortPinViews(views []PinView, shiftsByID map[string]db.Shift, roles model.Roles) {
	sort.Slice(views, func(i, j int) bool {
		a, b := views[i], views[j]
		if a.Date != b.Date {
			return a.Date < b.Date
		}
		if sa, sb := shiftsByID[a.ShiftID].StartAt, shiftsByID[b.ShiftID].StartAt; sa != sb {
			return sa < sb
		}
		if a.ShiftID != b.ShiftID {
			return a.ShiftID < b.ShiftID
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
