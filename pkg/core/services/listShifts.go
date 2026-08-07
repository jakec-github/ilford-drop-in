package services

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services/utils"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// ListShiftsStore defines the database operations needed for listing shifts.
// The shift table is the authority on which shifts exist in range (ADR 0001);
// allocations and alterations are then fetched for exactly those shifts (by id,
// not a second date scan), and supply the effective assignees for shifts whose
// rota has been allocated.
type ListShiftsStore interface {
	ShiftShapeStore
	GetShiftsInRange(ctx context.Context, from, to time.Time) ([]db.ShiftInRange, error)
	GetAllocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.Allocation, error)
	GetAlterationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.Alteration, error)
}

// ListShiftsParams holds optional filters for listing shifts
type ListShiftsParams struct {
	From string // Inclusive lower bound (YYYY-MM-DD), empty for no bound
	To   string // Inclusive upper bound (YYYY-MM-DD), empty for no bound
}

// ShiftAssignee is one person (or custom entry) on an effective shift
type ShiftAssignee struct {
	VolunteerID string // empty for custom entries
	CustomEntry string // empty for volunteers
	Name        string // volunteer display name, or the custom entry verbatim
	Role        string
	Group       string // volunteer's group key; empty for custom entries and ungrouped volunteers
}

// Shift is one minted shift after applying alterations. Unallocated shifts are
// included (their rota has not been allocated yet), carrying Allocated=false and
// no assignees.
type Shift struct {
	ID   string // UUID; how a client addresses the shift to change it
	Date string // YYYY-MM-DD, the date the shift starts
	// StartAt and EndAt are the shift's own local wall-clock times,
	// "2006-01-02T15:04:05", carrying no zone (ADR 0007). Both empty means a
	// shift minted before an admin set the drop-in's times; readers that need a
	// moment turn these into one with model.RotaDefaults.ShiftInstants, and
	// leave the time out when they are empty.
	StartAt string
	EndAt   string
	Closed  bool
	// Shape is what this shift asks for: which Roles, and how many Seats of
	// each, in the order they are filled. It is the shift's own — copied from
	// the default Shape when the rota was defined and editable per shift until
	// the rota is allocated (issues #137, #138) — so two shifts of one rota may
	// legitimately differ. Empty for a shift nobody has stated a Shape for,
	// which is a shift asking for nobody and the one thing allocation refuses
	// over.
	Shape           model.Shape
	Allocated       bool // rota's allocated_datetime is set; assignees are meaningful only when true
	Assignees       []ShiftAssignee
	AlterationCount int       // number of alterations recorded for the date
	LastChanged     time.Time // latest alteration set_time for the date; zero if unaltered
}

// ListShifts returns every minted shift in range (ADR 0001: the shift table is
// the authority on which shifts exist), sorted by date ascending and optionally
// bounded by params. Allocated shifts carry their effective assignees (base
// allocations with alterations applied); unallocated shifts carry none.
func ListShifts(
	ctx context.Context,
	database ListShiftsStore,
	volunteerClient VolunteerClient,
	cfg *config.Config,
	params ListShiftsParams,
	logger *zap.Logger,
) ([]Shift, error) {
	from, to, err := parseShiftDateBounds(params)
	if err != nil {
		return nil, err
	}

	shiftsInRange, err := database.GetShiftsInRange(ctx, from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch shifts: %w", err)
	}

	// Scope allocations and alterations by the shifts already resolved rather
	// than a second date-range scan: they are children of these shifts (ADR
	// 0001), so the two sets cannot disagree.
	shiftIDs := make([]string, 0, len(shiftsInRange))
	for _, s := range shiftsInRange {
		shiftIDs = append(shiftIDs, s.ID)
	}

	allocations, err := database.GetAllocationsByShiftIDs(ctx, shiftIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch allocations: %w", err)
	}

	alterations, err := database.GetAlterationsByShiftIDs(ctx, shiftIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch alterations: %w", err)
	}

	roles, err := RoleTable(ctx, database)
	if err != nil {
		return nil, err
	}

	// What each shift asks for, read here rather than left to a second call: it
	// is a property of the shift, and the screen that edits one Shape shows the
	// rota it belongs to.
	shapes, err := ShiftShapes(ctx, database, shiftIDs)
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

	allocationsByShiftID := make(map[string][]db.Allocation)
	for _, a := range allocations {
		allocationsByShiftID[a.ShiftID] = append(allocationsByShiftID[a.ShiftID], a)
	}
	allocationsByShiftID = utils.ApplyAlterations(allocationsByShiftID, alterations, roles.UncappedName())

	alterationCounts := make(map[string]int)
	lastChanged := make(map[string]time.Time)
	for _, alt := range alterations {
		alterationCounts[alt.ShiftID]++
		setTime, err := time.Parse(time.RFC3339, alt.SetTime)
		if err != nil {
			logger.Warn("Failed to parse alteration set_time",
				zap.String("alteration_id", alt.ID),
				zap.String("set_time", alt.SetTime))
			continue
		}
		if setTime.After(lastChanged[alt.ShiftID]) {
			lastChanged[alt.ShiftID] = setTime
		}
	}

	// The shift table drives which shifts appear (ADR 0001), whether or not the
	// rota has been allocated, and it is the authority on which of them are
	// closed. shiftsInRange is already date-ordered by the DB.
	shifts := make([]Shift, 0, len(shiftsInRange))
	for _, s := range shiftsInRange {
		shift := Shift{
			ID:              s.ID,
			Date:            s.Date,
			StartAt:         s.StartAt,
			EndAt:           s.EndAt,
			Closed:          s.Closed,
			Shape:           shapes[s.ID],
			Allocated:       s.Allocated,
			AlterationCount: alterationCounts[s.ID],
			LastChanged:     lastChanged[s.ID],
		}

		// Assignees are meaningful only once the rota is allocated; an
		// unallocated shift has none. Closed shifts also carry none, mirroring
		// publishRota.
		if shift.Allocated && !shift.Closed {
			shift.Assignees = buildAssignees(allocationsByShiftID[s.ID], volunteersByID, roles, logger)
		}

		shifts = append(shifts, shift)
	}

	logger.Debug("Listed shifts", zap.Int("count", len(shifts)))
	return shifts, nil
}

// FilterShiftsByVolunteer returns the open shifts that include the given volunteer
func FilterShiftsByVolunteer(shifts []Shift, volunteerID string) []Shift {
	filtered := make([]Shift, 0)
	for _, s := range shifts {
		if s.Closed {
			continue
		}
		for _, a := range s.Assignees {
			if a.VolunteerID == volunteerID {
				filtered = append(filtered, s)
				break
			}
		}
	}
	return filtered
}

// parseShiftDateBounds validates the optional from/to filters
func parseShiftDateBounds(params ListShiftsParams) (from, to time.Time, err error) {
	if params.From != "" {
		from, err = time.Parse("2006-01-02", params.From)
		if err != nil {
			return time.Time{}, time.Time{}, wrapf(ErrInvalidInput, "invalid from date %q: expected YYYY-MM-DD", params.From)
		}
	}
	if params.To != "" {
		to, err = time.Parse("2006-01-02", params.To)
		if err != nil {
			return time.Time{}, time.Time{}, wrapf(ErrInvalidInput, "invalid to date %q: expected YYYY-MM-DD", params.To)
		}
	}
	return from, to, nil
}

// buildAssignees resolves allocation entries to named assignees, ordered by
// their Role's configured priority and then alphabetically. Unknown volunteer
// IDs degrade to the raw ID rather than failing, so a volunteer removed from
// the sheet cannot break listings.
func buildAssignees(
	allocations []db.Allocation,
	volunteersByID map[string]model.Volunteer,
	roles model.Roles,
	logger *zap.Logger,
) []ShiftAssignee {
	assignees := make([]ShiftAssignee, 0, len(allocations))
	for _, a := range allocations {
		assignee := ShiftAssignee{
			VolunteerID: a.VolunteerID,
			CustomEntry: a.CustomEntry,
			Role:        a.Role,
		}
		switch {
		case a.CustomEntry != "":
			assignee.Name = a.CustomEntry
		default:
			volunteer, ok := volunteersByID[a.VolunteerID]
			if !ok {
				logger.Warn("Volunteer not found in sheet, using raw ID",
					zap.String("volunteer_id", a.VolunteerID))
				assignee.Name = a.VolunteerID
			} else {
				assignee.Name = volunteer.DisplayName
				assignee.Group = volunteer.GroupKey
			}
		}
		assignees = append(assignees, assignee)
	}

	// A Role the config does not know — an allocation written before it was
	// removed — sorts after every configured one rather than jumping to the
	// front, which is where an unrecognised name would otherwise land.
	priority := func(role string) int {
		if r, ok := roles.ByName(role); ok {
			return r.Priority
		}
		return math.MaxInt
	}
	sort.Slice(assignees, func(i, j int) bool {
		iPriority, jPriority := priority(assignees[i].Role), priority(assignees[j].Role)
		if iPriority != jPriority {
			return iPriority < jPriority
		}
		return assignees[i].Name < assignees[j].Name
	})

	return assignees
}
