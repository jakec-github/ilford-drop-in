package services

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/allocator"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services/utils"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// AllocateRotaStore defines the database operations needed for allocating a rota
type AllocateRotaStore interface {
	GetRotations(ctx context.Context) ([]db.Rotation, error)
	GetShiftsByRotaID(ctx context.Context, rotaID string) ([]db.Shift, error)
	GetAvailabilityRequestsByRotaID(ctx context.Context, rotaID string) ([]db.AvailabilityRequest, error)
	GetAllocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.Allocation, error)
	GetAlterationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.Alteration, error)
	GetManualPreallocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.ManualPreallocation, error)
	InsertAllocationsAndSetAllocated(ctx context.Context, allocations []db.Allocation, rotaID string, datetime time.Time) error
}

// fetchAvailabilityResponses fetches form responses and converts them to allocator availability format
func fetchAvailabilityResponses(
	ctx context.Context,
	requests []db.AvailabilityRequest,
	volunteersByID map[string]model.Volunteer,
	shiftDates []time.Time,
	formsClient FormsClientWithResponses,
	logger *zap.Logger,
) ([]allocator.VolunteerAvailability, error) {
	availability := make([]allocator.VolunteerAvailability, 0, len(requests))

	for _, req := range requests {
		volunteer, exists := volunteersByID[req.VolunteerID]
		if !exists {
			logger.Warn("Volunteer not found in map", zap.String("volunteer_id", req.VolunteerID))
			continue
		}

		volunteerName := fmt.Sprintf("%s %s", volunteer.FirstName, volunteer.LastName)

		// Get form response
		formResp, err := formsClient.GetFormResponse(req.FormID, volunteerName, shiftDates)
		if err != nil {
			return nil, fmt.Errorf("failed to get form response for volunteer %s: %w", volunteer.ID, err)
		}

		// Convert unavailable dates to shift indices
		unavailableIndices := make([]int, 0)
		for _, unavailableDateStr := range formResp.UnavailableDates {
			// Find the index of this date in shiftDates
			for i, shiftDate := range shiftDates {
				if shiftDate.Format("Mon Jan 2 2006") == unavailableDateStr {
					unavailableIndices = append(unavailableIndices, i)
					break
				}
			}
		}

		availability = append(availability, allocator.VolunteerAvailability{
			VolunteerID:             req.VolunteerID,
			HasResponded:            formResp.HasResponded,
			UnavailableShiftIndices: unavailableIndices,
		})
	}

	return availability, nil
}

// convertToAllocatorVolunteers converts model.Volunteer to allocator.Volunteer
func convertToAllocatorVolunteers(volunteers []model.Volunteer) []allocator.Volunteer {
	result := make([]allocator.Volunteer, len(volunteers))
	for i, vol := range volunteers {

		result[i] = allocator.Volunteer{
			ID:          vol.ID,
			FirstName:   vol.FirstName,
			LastName:    vol.LastName,
			DisplayName: vol.DisplayName,
			Gender:      vol.Gender,
			IsTeamLead:  vol.Role == model.RoleTeamLead,
			GroupKey:    vol.GroupKey,
		}
	}
	return result
}

// convertToDBAllocations converts allocator shifts to database allocation
// records, resolving each solver-output date to its minted shift id via
// shiftIDByDate. A date with no minted shift is a broken invariant (the solver
// only ever sees minted dates); it fails loudly here rather than tripping the
// shift_id FK on insert (ADR 0001).
func convertToDBAllocations(shiftIDByDate map[string]string, shifts []*allocator.Shift) ([]db.Allocation, error) {
	allocations := make([]db.Allocation, 0)

	for _, shift := range shifts {
		shiftID, ok := shiftIDByDate[shift.Date]
		if !ok {
			return nil, fmt.Errorf("solver produced an allocation for date %s with no minted shift", shift.Date)
		}

		// Add allocations for regular volunteers in groups
		for _, group := range shift.AllocatedGroups {
			for _, member := range group.Members {
				// Skip team lead if they're also the designated team lead for the shift
				if shift.TeamLead != nil && member.ID == shift.TeamLead.ID {
					continue
				}

				allocations = append(allocations, db.Allocation{
					ID:          uuid.New().String(),
					ShiftID:     shiftID,
					Role:        string(model.RoleVolunteer),
					VolunteerID: member.ID,
					CustomEntry: "",
				})
			}
		}

		// Add team lead allocation
		if shift.TeamLead != nil {
			allocations = append(allocations, db.Allocation{
				ID:          uuid.New().String(),
				ShiftID:     shiftID,
				Role:        string(model.RoleTeamLead),
				VolunteerID: shift.TeamLead.ID,
				CustomEntry: "",
			})
		}

		// Add pre-allocated volunteers
		for _, preAllocatedID := range shift.CustomPreallocations {
			allocations = append(allocations, db.Allocation{
				ID:          uuid.New().String(),
				ShiftID:     shiftID,
				Role:        string(model.RoleVolunteer),
				VolunteerID: "",
				CustomEntry: preAllocatedID,
			})
		}
	}

	return allocations, nil
}

// convertRotaOverrides converts config.RotaOverride to allocator.ShiftOverride
// RRule strings are parsed and converted to date-matching functions
// shiftDates provides the actual date range for the rota, which may span years
func convertRotaOverrides(configOverrides []config.RotaOverride, shiftDates []time.Time, logger *zap.Logger) ([]allocator.ShiftOverride, error) {
	result := make([]allocator.ShiftOverride, 0, len(configOverrides))

	for i, override := range configOverrides {
		// Parse the RRule into a date matcher; allocation fails hard on an
		// unparseable rrule.
		appliesTo, err := utils.NewRRuleMatcher(override.RRule, shiftDates)
		if err != nil {
			return nil, fmt.Errorf("failed to parse rrule for override %d: %w", i, err)
		}

		result = append(result, allocator.ShiftOverride{
			AppliesTo:                appliesTo,
			ShiftSize:                override.ShiftSize,
			CustomPreallocations:     override.CustomPreallocations,
			Closed:                   override.Closed,
			PreallocatedVolunteerIDs: override.PreallocatedVolunteerIDs,
			PreallocatedTeamLeadID:   override.PreallocatedTeamLeadID,
		})

		logger.Debug("Converted override",
			zap.Int("index", i),
			zap.String("rrule", override.RRule),
			zap.Bool("has_shift_size", override.ShiftSize != nil),
			zap.Int("custom_preallocated_count", len(override.CustomPreallocations)),
			zap.Int("preallocated_volunteer_count", len(override.PreallocatedVolunteerIDs)),
			zap.Bool("has_preallocated_team_lead", override.PreallocatedTeamLeadID != ""),
			zap.Bool("closed", override.Closed))
	}

	return result, nil
}

// buildHistoricalShifts fetches allocations from the previous rota, applies that
// rota's alterations (covers/swaps) so history reflects who actually worked, and
// builds historical shift objects sorted ascending by date. Only includes Date
// and AllocatedGroups fields. Callers pass ALL volunteers (inactive included) so
// shifts worked by now-inactive volunteers keep their groups — dropping them
// would shift the back-to-back boundary onto an earlier date. Allocations whose
// volunteer id is unknown (deleted from the sheet) and custom entries are
// skipped; a date is still emitted even if no groups remain.
func buildHistoricalShifts(
	ctx context.Context,
	database AllocateRotaStore,
	allRotations []db.Rotation,
	targetRota *db.Rotation,
	volunteers []allocator.Volunteer,
	logger *zap.Logger,
) ([]*allocator.Shift, error) {
	// Find the previous rota (the one before the target rota)
	previousRota := findPreviousRotation(allRotations, targetRota)
	if previousRota == nil {
		logger.Info("No previous rota found, historical shifts will be empty")
		return []*allocator.Shift{}, nil
	}

	logger.Debug("Found previous rota",
		zap.String("id", previousRota.ID),
		zap.String("start", previousRota.Start))

	// Read the previous rota's shifts to scope its allocations/alterations by id
	// and to recover each shift's date for the historical output (ADR 0001).
	previousRotaShifts, err := database.GetShiftsByRotaID(ctx, previousRota.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch shifts: %w", err)
	}
	shiftIDs := make([]string, len(previousRotaShifts))
	dateByShiftID := make(map[string]string, len(previousRotaShifts))
	for i, s := range previousRotaShifts {
		shiftIDs[i] = s.ID
		dateByShiftID[s.ID] = s.Date
	}

	// Fetch the previous rota's allocations
	previousRotaAllocations, err := database.GetAllocationsByShiftIDs(ctx, shiftIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch allocations: %w", err)
	}
	logger.Debug("Fetched allocations from previous rota", zap.Int("count", len(previousRotaAllocations)))

	if len(previousRotaAllocations) == 0 {
		logger.Info("No allocations found in previous rota")
		return []*allocator.Shift{}, nil
	}

	// Group allocations by shift id, custom entries included so
	// alterations that remove them can match.
	allocationsByShiftID := make(map[string][]db.Allocation)
	for _, allocation := range previousRotaAllocations {
		allocationsByShiftID[allocation.ShiftID] = append(allocationsByShiftID[allocation.ShiftID], allocation)
	}

	// Apply the previous rota's alterations so history reflects who
	// actually worked (covers and swaps), not the rota as first published.
	previousRotaAlterations, err := database.GetAlterationsByShiftIDs(ctx, shiftIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch alterations: %w", err)
	}
	logger.Debug("Applying alterations to historical shifts", zap.Int("count", len(previousRotaAlterations)))
	allocationsByShiftID = utils.ApplyAlterations(allocationsByShiftID, previousRotaAlterations)

	// Build a map of volunteers by ID for quick lookup
	volunteersByID := make(map[string]allocator.Volunteer)
	for _, vol := range volunteers {
		volunteersByID[vol.ID] = vol
	}

	// Build historical shifts
	historicalShifts := make([]*allocator.Shift, 0, len(allocationsByShiftID))
	for shiftID, allocations := range allocationsByShiftID {
		// Group volunteers by their GroupKey to reconstruct volunteer groups,
		// skipping custom entries and unknown volunteer ids
		volunteersByGroup := make(map[string][]allocator.Volunteer)
		for _, allocation := range allocations {
			if allocation.VolunteerID == "" {
				continue
			}
			volunteer, exists := volunteersByID[allocation.VolunteerID]
			if !exists {
				continue
			}
			volunteersByGroup[volunteer.GroupKey] = append(volunteersByGroup[volunteer.GroupKey], volunteer)
		}

		// Build AllocatedGroups for this shift using the allocator's BuildVolunteerGroup helper
		allocatedGroups := make([]*allocator.VolunteerGroup, 0, len(volunteersByGroup))
		for groupKey, members := range volunteersByGroup {
			group := allocator.BuildVolunteerGroup(groupKey, members)
			allocatedGroups = append(allocatedGroups, group)
		}

		// Create the historical shift with only Date and AllocatedGroups
		historicalShifts = append(historicalShifts, &allocator.Shift{
			Date:            dateByShiftID[shiftID],
			AllocatedGroups: allocatedGroups,
		})
	}

	// Consumers treat the last element as the boundary shift (and measure
	// index distances), so the order must be by date, not map iteration.
	sort.Slice(historicalShifts, func(i, j int) bool {
		return historicalShifts[i].Date < historicalShifts[j].Date
	})

	logger.Debug("Built historical shifts", zap.Int("shift_count", len(historicalShifts)))

	return historicalShifts, nil
}

// configPreallocationsForDate collects the config-derived preallocations that
// apply to a single date, mirroring InitShifts' per-date append semantics: the
// set of preallocated volunteer ids, the (last) team-lead id, the set of custom
// entries, and whether config closes the date. Manual pins are deduped against
// exactly what config already contributes for that date.
func configPreallocationsForDate(date string, overrides []allocator.ShiftOverride) (volIDs map[string]bool, teamLead string, customs map[string]bool, closed bool) {
	volIDs = make(map[string]bool)
	customs = make(map[string]bool)
	for _, o := range overrides {
		if !o.AppliesTo(date) {
			continue
		}
		if o.Closed {
			closed = true
			continue
		}
		for _, id := range o.PreallocatedVolunteerIDs {
			volIDs[id] = true
		}
		for _, c := range o.CustomPreallocations {
			customs[c] = true
		}
		if o.PreallocatedTeamLeadID != "" {
			teamLead = o.PreallocatedTeamLeadID
		}
	}
	return volIDs, teamLead, customs, closed
}

// exactDateMatcher returns an AppliesTo predicate matching exactly one date, so
// a synthetic manual-preallocation override touches only its own shift.
func exactDateMatcher(date string) func(string) bool {
	return func(d string) bool { return d == date }
}

// buildManualPreallocationOverrides turns each manual pin into a synthetic,
// exact-date allocator.ShiftOverride so InitShifts unions them with the
// config-derived overrides through its existing append semantics — no new merge
// logic in the solver (issue #39 / ADR 0003). Manual is add-only: a pin that
// duplicates a config contribution for the same date (same volunteer id, same
// custom entry, or a team lead when config already pins one) is dropped so it
// never doubles a seat; config stays authoritative for the single-valued
// team-lead slot. A pin whose date config closes contributes nothing.
func buildManualPreallocationOverrides(
	manualPins []db.ManualPreallocation,
	dateByShiftID map[string]string,
	configOverrides []allocator.ShiftOverride,
) ([]allocator.ShiftOverride, error) {
	overrides := make([]allocator.ShiftOverride, 0, len(manualPins))

	for _, pin := range manualPins {
		date, ok := dateByShiftID[pin.ShiftID]
		if !ok {
			return nil, fmt.Errorf("manual preallocation %s references shift %s with no minted date", pin.ID, pin.ShiftID)
		}

		configVolIDs, configTL, configCustoms, configClosed := configPreallocationsForDate(date, configOverrides)
		if configClosed {
			// Config closes this date; a manual pin cannot reopen it.
			continue
		}

		override := allocator.ShiftOverride{AppliesTo: exactDateMatcher(date)}
		switch {
		case pin.Role == string(model.RoleTeamLead):
			if configTL != "" {
				continue // config is authoritative for the team-lead slot
			}
			override.PreallocatedTeamLeadID = pin.VolunteerID
		case pin.VolunteerID != "":
			if configVolIDs[pin.VolunteerID] {
				continue // already preallocated by config
			}
			override.PreallocatedVolunteerIDs = []string{pin.VolunteerID}
		case pin.CustomValue != "":
			if configCustoms[pin.CustomValue] {
				continue // identical custom entry already preallocated by config
			}
			override.CustomPreallocations = []string{pin.CustomValue}
		default:
			return nil, fmt.Errorf("manual preallocation %s has neither a volunteer nor a custom value", pin.ID)
		}

		overrides = append(overrides, override)
	}

	return overrides, nil
}

// checkPreallocationsResolve verifies, before the solver runs, that every
// preallocated volunteer still resolves to an active volunteer. A pin whose
// volunteer has gone inactive or been deleted would otherwise surface as the
// solver's opaque ProblemError; here it fails loudly, naming the offending
// pin(s). It covers both manual pins and config preallocations (ADR 0003 asks
// the check to shield config too). Custom (non-volunteer) pins carry no id and
// are not checked.
func checkPreallocationsResolve(
	manualPins []db.ManualPreallocation,
	dateByShiftID map[string]string,
	configOverrides []allocator.ShiftOverride,
	shiftDates []time.Time,
	activeIDs map[string]bool,
) error {
	var offenders []string

	for _, pin := range manualPins {
		if pin.VolunteerID == "" {
			continue
		}
		if !activeIDs[pin.VolunteerID] {
			offenders = append(offenders, fmt.Sprintf("manual pin for %s: volunteer %s is not active", dateByShiftID[pin.ShiftID], pin.VolunteerID))
		}
	}

	for _, date := range shiftDates {
		dateStr := date.Format("2006-01-02")
		for _, o := range configOverrides {
			if !o.AppliesTo(dateStr) || o.Closed {
				continue
			}
			for _, id := range o.PreallocatedVolunteerIDs {
				if !activeIDs[id] {
					offenders = append(offenders, fmt.Sprintf("config pin for %s: volunteer %s is not active", dateStr, id))
				}
			}
			if o.PreallocatedTeamLeadID != "" && !activeIDs[o.PreallocatedTeamLeadID] {
				offenders = append(offenders, fmt.Sprintf("config pin for %s: team lead %s is not active", dateStr, o.PreallocatedTeamLeadID))
			}
		}
	}

	if len(offenders) > 0 {
		return fmt.Errorf("preallocated volunteers are no longer active: %s", strings.Join(offenders, "; "))
	}
	return nil
}

// findPreviousRotation finds the rotation immediately before the target rotation
func findPreviousRotation(rotations []db.Rotation, targetRota *db.Rotation) *db.Rotation {
	targetDate, err := time.Parse("2006-01-02", targetRota.Start)
	if err != nil {
		return nil
	}

	var previousRota *db.Rotation
	var previousDate time.Time

	for i := range rotations {
		rota := &rotations[i]
		if rota.ID == targetRota.ID {
			continue
		}

		rotaDate, err := time.Parse("2006-01-02", rota.Start)
		if err != nil {
			continue
		}

		// Only consider rotas that start before the target rota
		if rotaDate.Before(targetDate) {
			// If this is our first match or it's more recent than our current previous
			if previousRota == nil || rotaDate.After(previousDate) {
				previousRota = rota
				previousDate = rotaDate
			}
		}
	}

	return previousRota
}
