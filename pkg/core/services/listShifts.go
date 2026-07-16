package services

import (
	"context"
	"fmt"
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
// allocations and alterations supply the effective assignees for those that
// have been allocated.
type ListShiftsStore interface {
	GetShiftsInRange(ctx context.Context, from, to time.Time) ([]db.ShiftInRange, error)
	GetAllocationsInRange(ctx context.Context, from, to time.Time) ([]db.Allocation, error)
	GetAlterationsInRange(ctx context.Context, from, to time.Time) ([]db.Alteration, error)
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
}

// Shift is one minted shift after applying alterations. Unallocated shifts are
// included (their rota has not been allocated yet), carrying Allocated=false and
// no assignees.
type Shift struct {
	Date            string // YYYY-MM-DD
	Closed          bool
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

	allocations, err := database.GetAllocationsInRange(ctx, from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch allocations: %w", err)
	}

	alterations, err := database.GetAlterationsInRange(ctx, from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch alterations: %w", err)
	}

	volunteers, err := volunteerClient.ListVolunteers(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch volunteers: %w", err)
	}

	volunteersByID := make(map[string]model.Volunteer, len(volunteers))
	for _, v := range volunteers {
		volunteersByID[v.ID] = v
	}

	allocationsByDate := make(map[string][]db.Allocation)
	for _, a := range allocations {
		allocationsByDate[a.ShiftDate] = append(allocationsByDate[a.ShiftDate], a)
	}
	allocationsByDate = utils.ApplyAlterations(allocationsByDate, alterations)

	alterationCounts := make(map[string]int)
	lastChanged := make(map[string]time.Time)
	for _, alt := range alterations {
		alterationCounts[alt.ShiftDate]++
		setTime, err := time.Parse(time.RFC3339, alt.SetTime)
		if err != nil {
			logger.Warn("Failed to parse alteration set_time",
				zap.String("alteration_id", alt.ID),
				zap.String("set_time", alt.SetTime))
			continue
		}
		if setTime.After(lastChanged[alt.ShiftDate]) {
			lastChanged[alt.ShiftDate] = setTime
		}
	}

	// The shift table drives which dates appear (ADR 0001), whether or not the
	// rota has been allocated. Collect them both for output ordering and to
	// bound the rrule search window in isShiftClosed.
	allocatedByDate := make(map[string]bool, len(shiftsInRange))
	shiftDates := make([]time.Time, 0, len(shiftsInRange))
	for _, s := range shiftsInRange {
		date, err := time.Parse("2006-01-02", s.Date)
		if err != nil {
			logger.Warn("Skipping shift with unparseable date", zap.String("date", s.Date))
			continue
		}
		allocatedByDate[s.Date] = s.Allocated
		shiftDates = append(shiftDates, date)
	}
	sort.Slice(shiftDates, func(i, j int) bool { return shiftDates[i].Before(shiftDates[j]) })

	shifts := make([]Shift, 0, len(shiftDates))
	for _, date := range shiftDates {
		dateStr := date.Format("2006-01-02")

		shift := Shift{
			Date:            dateStr,
			Closed:          isShiftClosed(dateStr, cfg.RotaOverrides, shiftDates, logger),
			Allocated:       allocatedByDate[dateStr],
			AlterationCount: alterationCounts[dateStr],
			LastChanged:     lastChanged[dateStr],
		}

		// Assignees are meaningful only once the rota is allocated; an
		// unallocated shift has none. Closed shifts also carry none, mirroring
		// publishRota.
		if shift.Allocated && !shift.Closed {
			shift.Assignees = buildAssignees(allocationsByDate[dateStr], volunteersByID, logger)
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

// buildAssignees resolves allocation entries to named assignees, team lead
// first then alphabetical. Unknown volunteer IDs degrade to the raw ID rather
// than failing, so a volunteer removed from the sheet cannot break listings.
func buildAssignees(allocations []db.Allocation, volunteersByID map[string]model.Volunteer, logger *zap.Logger) []ShiftAssignee {
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
			}
		}
		assignees = append(assignees, assignee)
	}

	sort.Slice(assignees, func(i, j int) bool {
		iLead := assignees[i].Role == string(model.RoleTeamLead)
		jLead := assignees[j].Role == string(model.RoleTeamLead)
		if iLead != jLead {
			return iLead
		}
		return assignees[i].Name < assignees[j].Name
	})

	return assignees
}
