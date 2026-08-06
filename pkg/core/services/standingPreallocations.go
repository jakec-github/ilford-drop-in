package services

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services/utils"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// A Standing Preallocation is a Preallocation an admin expects to make every
// rota — the team who always take the first Sunday — kept in the Rota Defaults
// and used to seed ordinary Preallocations when a Rotation is defined
// (issue #131, ADR 0006).
//
// It is a convenience at definition, not a standing fact. Nothing reads one
// after the seeding: the Preallocations it made are ordinary, belong to the rota
// that minted them, and outlive any later change to it. That is what makes
// "there is one kind of Preallocation" true — the pins these produce are not a
// second sort of thing with a different authority, and an admin may remove any
// of them.

// StandingPreallocationStore is what reading and editing the Standing
// Preallocations needs. Roles come with it because one is only legible as a
// Role name, and the rows hold a Role id.
type StandingPreallocationStore interface {
	RoleStore
	GetStandingPreallocations(ctx context.Context) ([]db.StandingPreallocation, error)
	InsertStandingPreallocation(ctx context.Context, standing db.StandingPreallocation) error
	DeleteStandingPreallocationByID(ctx context.Context, id string) (bool, error)
}

// StandingPreallocationView is one Standing Preallocation as a screen reads it:
// who, in which Role, on which Shifts.
//
// Role carries both the id and the name because they answer different
// questions — the id is what the row references and what an edit would name, the
// name is what an admin recognises.
type StandingPreallocationView struct {
	ID          string
	RRule       string
	RoleID      string
	Role        string // the Role's name today
	VolunteerID string
	Custom      string
	Name        string // volunteer display name, or the custom entry verbatim
}

// AddStandingPreallocationParams is one promise an admin is making: this person,
// in this Role, on the Shifts this rule names. Exactly one of VolunteerID and
// Custom is set.
type AddStandingPreallocationParams struct {
	RRule       string
	RoleID      string
	VolunteerID string
	Custom      string
}

// ListStandingPreallocations reads them in the order they read on screen, with
// each subject resolved to a name.
func ListStandingPreallocations(
	ctx context.Context,
	store StandingPreallocationStore,
	volunteerClient VolunteerClient,
	cfg *config.Config,
	logger *zap.Logger,
) ([]StandingPreallocationView, error) {
	roles, err := RoleTable(ctx, store)
	if err != nil {
		return nil, err
	}

	rows, err := store.GetStandingPreallocations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch standing preallocations: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}

	volunteers, err := volunteerClient.ListVolunteers(cfg, roles)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch volunteers: %w", err)
	}
	volunteersByID := make(map[string]model.Volunteer, len(volunteers))
	for _, v := range volunteers {
		volunteersByID[v.ID] = v
	}

	views := make([]StandingPreallocationView, 0, len(rows))
	for _, row := range rows {
		views = append(views, StandingPreallocationView{
			ID:          row.ID,
			RRule:       row.RRule,
			RoleID:      row.RoleID,
			Role:        roleName(roles, row.RoleID),
			VolunteerID: row.VolunteerID,
			Custom:      row.CustomValue,
			Name:        preallocationName(row.VolunteerID, row.CustomValue, volunteersByID, logger),
		})
	}

	sortStandingPreallocationViews(views, roles)
	return views, nil
}

// AddStandingPreallocation validates and records one.
//
// It validates what it can and no more. The rule and the Role are checked
// because a rota is defined against them and a mistake there is silent; the
// volunteer is checked against the roster as it stands today, which is a
// courtesy rather than a guarantee — a person can leave between now and the next
// rota, and the pre-solve check is what catches that.
func AddStandingPreallocation(
	ctx context.Context,
	store StandingPreallocationStore,
	volunteerClient VolunteerClient,
	cfg *config.Config,
	params AddStandingPreallocationParams,
	logger *zap.Logger,
) (*StandingPreallocationView, error) {
	if (params.VolunteerID == "") == (params.Custom == "") {
		return nil, wrapf(ErrInvalidInput, "exactly one of volunteerId or custom must be provided")
	}

	rrule := strings.TrimSpace(params.RRule)
	if rrule == "" {
		return nil, wrapf(ErrInvalidInput, "a standing preallocation needs a rule saying which shifts it applies to")
	}
	if _, err := utils.ParseRRule(rrule); err != nil {
		return nil, wrapf(ErrInvalidInput, "%q is not a recurrence rule this app understands: %v", rrule, err)
	}

	roles, err := RoleTable(ctx, store)
	if err != nil {
		return nil, err
	}
	role, ok := roles.ByID(params.RoleID)
	if !ok {
		return nil, wrapf(ErrInvalidInput, "role %q is not a known role", params.RoleID)
	}

	name := strings.TrimSpace(params.Custom)
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
		if !vol.Holds(role.Name) {
			return nil, wrapf(ErrInvalidInput, "volunteer %s does not hold the role %q", params.VolunteerID, role.Name)
		}
		name = vol.DisplayName
	}

	created := db.StandingPreallocation{
		ID:          uuid.New().String(),
		RRule:       rrule,
		RoleID:      role.ID,
		VolunteerID: params.VolunteerID,
		CustomValue: strings.TrimSpace(params.Custom),
	}
	if err := store.InsertStandingPreallocation(ctx, created); err != nil {
		if errors.Is(err, db.ErrDuplicateStandingPreallocation) {
			return nil, wrapf(ErrConflict, "%s is already pinned on those shifts", name)
		}
		return nil, fmt.Errorf("failed to save standing preallocation: %w", err)
	}

	logger.Info("Standing preallocation recorded",
		zap.String("id", created.ID),
		zap.String("rrule", created.RRule),
		zap.String("role", role.Name))

	return &StandingPreallocationView{
		ID:          created.ID,
		RRule:       created.RRule,
		RoleID:      created.RoleID,
		Role:        role.Name,
		VolunteerID: created.VolunteerID,
		Custom:      created.CustomValue,
		Name:        name,
	}, nil
}

// DeleteStandingPreallocation removes one. There is no rota state to check
// against and nothing cascades: the Preallocations it has already seeded belong
// to the rotas that minted them and stay exactly as they are.
func DeleteStandingPreallocation(ctx context.Context, store StandingPreallocationStore, id string, logger *zap.Logger) error {
	deleted, err := store.DeleteStandingPreallocationByID(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to delete standing preallocation %s: %w", id, err)
	}
	if !deleted {
		return wrapf(ErrNotFound, "standing preallocation %s not found", id)
	}

	logger.Info("Standing preallocation deleted", zap.String("id", id))
	return nil
}

// seedPreallocations turns the Standing Preallocations into the ordinary
// Preallocations a newly-defined rota starts with: one row per subject per Shift
// their rule lands on. What comes back is indistinguishable from a pin an admin
// added by hand, which is the point.
//
// A person fills at most one Seat on a Shift, so two rules naming the same
// subject on one date collapse to one pin — in the Role whose Seats are filled
// first, since that is the promise the more important of the two made.
//
// An unparseable rule fails the definition rather than being warned past. Every
// other reader of an rrule in this package warns and skips because it is
// rendering something an admin can still act on; here, skipping would mint a
// rota quietly missing pins that were promised, and the rota is the thing an
// admin then works from.
func seedPreallocations(
	standing []db.StandingPreallocation,
	shifts []db.Shift,
	roles model.Roles,
	logger *zap.Logger,
) ([]db.Preallocation, error) {
	if len(standing) == 0 || len(shifts) == 0 {
		return nil, nil
	}

	dates := make([]time.Time, 0, len(shifts))
	for _, s := range shifts {
		date, err := time.Parse("2006-01-02", s.Date)
		if err != nil {
			return nil, fmt.Errorf("shift %s has an unparseable date %q: %w", s.ID, s.Date, err)
		}
		dates = append(dates, date)
	}

	// Highest-priority Role first, so the collapse below keeps the Seat that is
	// filled first when one subject is named twice for a date. Id breaks the tie
	// so a rota defined twice from the same settings seeds the same pins.
	ordered := make([]db.StandingPreallocation, len(standing))
	copy(ordered, standing)
	sort.SliceStable(ordered, func(i, j int) bool {
		pi, pj := rolePriority(roles, ordered[i].RoleID), rolePriority(roles, ordered[j].RoleID)
		if pi != pj {
			return pi < pj
		}
		return ordered[i].ID < ordered[j].ID
	})

	// Keyed by shift id and subject: the one pin per person per Shift rule.
	seen := make(map[string]bool)
	// Built shift by shift so the rows come back in rota order, which is the
	// order everything downstream reads a rota in.
	byShift := make(map[string][]db.Preallocation, len(shifts))

	for _, s := range ordered {
		role, ok := roles.ByID(s.RoleID)
		if !ok {
			// The foreign key makes this unreachable short of the Roles being
			// edited mid-definition; seeding a pin into a Role nobody has heard
			// of would reach the solver as a Seat no Shape has.
			return nil, fmt.Errorf("standing preallocation %s names role %s, which does not exist", s.ID, s.RoleID)
		}

		matcher, err := utils.NewRRuleMatcher(s.RRule, dates)
		if err != nil {
			return nil, fmt.Errorf("standing preallocation %s has an unusable rule %q: %w", s.ID, s.RRule, err)
		}

		for _, shift := range shifts {
			if !matcher(shift.Date) {
				continue
			}
			key := shift.ID + "\x00" + subjectKey(s.VolunteerID, s.CustomValue)
			if seen[key] {
				logger.Debug("Standing preallocation already covered for this shift",
					zap.String("standing_id", s.ID),
					zap.String("shift_id", shift.ID))
				continue
			}
			seen[key] = true
			byShift[shift.ID] = append(byShift[shift.ID], db.Preallocation{
				ID:          uuid.New().String(),
				ShiftID:     shift.ID,
				Role:        role.Name,
				VolunteerID: s.VolunteerID,
				CustomValue: s.CustomValue,
			})
		}
	}

	seeded := make([]db.Preallocation, 0, len(seen))
	for _, shift := range shifts {
		seeded = append(seeded, byShift[shift.ID]...)
	}
	return seeded, nil
}

// subjectKey identifies who a pin is about, whichever kind of subject it names.
// A custom entry's text is its identity, exactly as it is for an ordinary pin.
func subjectKey(volunteerID, custom string) string {
	if volunteerID != "" {
		return "volunteer:" + volunteerID
	}
	return "custom:" + custom
}

// roleName reads a Role id as the name an admin knows it by, degrading to the
// raw id rather than to an empty string — a Role that has vanished under a
// Standing Preallocation is exactly what needs to be visible.
func roleName(roles model.Roles, roleID string) string {
	if role, ok := roles.ByID(roleID); ok {
		return role.Name
	}
	return roleID
}

// rolePriority is the order a Role's Seats are filled in; a Role the table does
// not know sorts last.
func rolePriority(roles model.Roles, roleID string) int {
	if role, ok := roles.ByID(roleID); ok {
		return role.Priority
	}
	return math.MaxInt
}

// sortStandingPreallocationViews puts them in reading order: by the order their
// Seats are filled, then by name, then by rule. Role first because that is how a
// Shift reads everywhere else in this app.
func sortStandingPreallocationViews(views []StandingPreallocationView, roles model.Roles) {
	sort.Slice(views, func(i, j int) bool {
		a, b := views[i], views[j]
		if pa, pb := rolePriority(roles, a.RoleID), rolePriority(roles, b.RoleID); pa != pb {
			return pa < pb
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		if a.RRule != b.RRule {
			return a.RRule < b.RRule
		}
		return a.ID < b.ID
	})
}
