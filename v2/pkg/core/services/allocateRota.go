package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/teambition/rrule-go"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/allocator"
	"github.com/jakechorley/ilford-drop-in/pkg/core/allocator/criteria"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

const (
	// Built-in ranking weights for volunteer group prioritization
	// These are used by the allocator's ranking algorithm to score groups

	// WeightCurrentRotaUrgency is the weight applied based on how difficult this group
	// is to schedule. More difficult volunteers are allocated first to ensure they get
	// the shifts they want.
	WeightCurrentRotaUrgency = 1

	// WeightOverallFrequencyFairness is the weight applied based on how many allocations
	// the group needs to reach their target frequency over time (historical + current).
	// Higher values prioritize fairness across all rotas.
	WeightOverallFrequencyFairness = 1

	// WeightPromoteGroup is the weight applied to groups over individuals.
	// Higher values prioritise groups more strongly. Group size does not affect score
	WeightPromoteGroup = 1

	// Criterion-specific weights
	// These control how much each criterion influences group ranking and shift selection

	// ShiftSize criterion weights
	WeightShiftSizeGroup    = 2.0 // Prioritize groups when shifts need filling
	WeightShiftSizeAffinity = 2.0 // Prefer shifts that need more volunteers

	// TeamLead criterion weights
	WeightTeamLeadGroup    = 0.5 // Slightly prioritize groups with team leads
	WeightTeamLeadAffinity = 2.0 // Strongly prefer shifts that need team leads

	// MaleBalance criterion weights
	WeightMaleBalanceGroup    = 0.5 // Slightly prioritize groups with males
	WeightMaleBalanceAffinity = 1.0 // Prefer shifts that need male volunteers

	// NoDoubleShifts criterion weights
	WeightNoDoubleShiftsAffinity = 1.0 // Prefer shifts that preserve more options

	// ShiftSpread criterion weights
	WeightShiftSpreadAffinity = 0.5 // Slightly prefer shifts that spread out allocations
)

// AllocateRotaResult contains the allocation results
type AllocateRotaResult struct {
	RotaID              string
	RotaStart           string
	ShiftCount          int
	ShiftDates          []time.Time
	Success             bool
	AllocatedShifts     []*allocator.Shift
	ValidationErrors    []allocator.ShiftValidationError
	UnderutilizedGroups []*allocator.VolunteerGroup
}

// AllocateRotaStore defines the database operations needed for allocating a rota
type AllocateRotaStore interface {
	GetRotations(ctx context.Context) ([]db.Rotation, error)
	GetAvailabilityRequests(ctx context.Context) ([]db.AvailabilityRequest, error)
	GetAllocations(ctx context.Context) ([]db.Allocation, error)
	InsertAllocations(allocations []db.Allocation) error
}

// AllocateRota runs the allocation algorithm to assign volunteers to shifts
// If dryRun is true, allocations are not saved to the database
// If forceCommit is true, allocations are saved even if validation fails
func AllocateRota(
	ctx context.Context,
	database AllocateRotaStore,
	volunteerClient VolunteerClient,
	formsClient FormsClientWithResponses,
	cfg *config.Config,
	logger *zap.Logger,
	dryRun bool,
	forceCommit bool,
) (*AllocateRotaResult, error) {
	logger.Debug("Starting allocateRota",
		zap.Bool("dry_run", dryRun),
		zap.Bool("force_commit", forceCommit))

	// Step 1: DB query - Fetch rota list
	logger.Debug("Fetching rotations")
	rotations, err := database.GetRotations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch rotations: %w", err)
	}
	logger.Debug("Found rotations", zap.Int("count", len(rotations)))

	if len(rotations) == 0 {
		return nil, fmt.Errorf("no rotations found - please define a rota first")
	}

	// Step 2: Find latest rota
	targetRota := findLatestRotation(rotations)
	logger.Debug("Using latest rota", zap.String("id", targetRota.ID))

	logger.Debug("Target rota",
		zap.String("id", targetRota.ID),
		zap.String("start", targetRota.Start),
		zap.Int("shift_count", targetRota.ShiftCount))

	// Calculate shift dates
	shiftDates, err := calculateShiftDates(targetRota.Start, targetRota.ShiftCount)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate shift dates: %w", err)
	}

	// Step 3: DB query - Fetch availability requests
	logger.Debug("Fetching availability requests")
	allRequests, err := database.GetAvailabilityRequests(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch availability requests: %w", err)
	}
	logger.Debug("Found availability requests", zap.Int("count", len(allRequests)))

	// Step 4: Find availability requests for resolved rota
	requestsForRota := filterSentRequestsByRotaID(allRequests, targetRota.ID)
	logger.Debug("Filtered sent requests for target rota", zap.Int("count", len(requestsForRota)))

	if len(requestsForRota) == 0 {
		return nil, fmt.Errorf("no availability requests found for rota %s - please run requestAvailability first", targetRota.ID)
	}

	// Step 5: Sheets query - Fetch volunteers
	logger.Debug("Fetching volunteers")
	allVolunteers, err := volunteerClient.ListVolunteers(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch volunteers: %w", err)
	}
	logger.Debug("Found volunteers", zap.Int("count", len(allVolunteers)))

	// Build map of volunteers by ID
	volunteersByID := make(map[string]model.Volunteer)
	for _, vol := range allVolunteers {
		volunteersByID[vol.ID] = vol
	}

	// Filter to active volunteers
	activeVolunteers := filterActiveVolunteers(allVolunteers)
	logger.Debug("Active volunteers", zap.Int("count", len(activeVolunteers)))

	// Step 6: Forms query - Get responses matching form IDs
	logger.Debug("Fetching form responses")
	availability, err := fetchAvailabilityResponses(
		ctx,
		requestsForRota,
		volunteersByID,
		shiftDates,
		formsClient,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch availability responses: %w", err)
	}
	logger.Debug("Processed availability responses", zap.Int("count", len(availability)))

	// Convert model volunteers to allocator volunteers
	allocatorVolunteers := convertToAllocatorVolunteers(activeVolunteers)
	logger.Debug("Converted volunteers for allocator", zap.Int("count", len(allocatorVolunteers)))

	// Convert shift dates to strings for allocator
	shiftDateStrings := make([]string, len(shiftDates))
	for i, date := range shiftDates {
		shiftDateStrings[i] = date.Format("2006-01-02")
	}

	// Step 7: Fetch and build historical shifts from previous rota
	logger.Debug("Fetching allocations for historical shifts")
	historicalShifts, err := buildHistoricalShifts(
		ctx,
		database,
		rotations,
		targetRota,
		allocatorVolunteers,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build historical shifts: %w", err)
	}
	logger.Debug("Built historical shifts", zap.Int("count", len(historicalShifts)))

	// Configure allocation criteria
	allocationCriteria := []allocator.Criterion{
		criteria.NewShiftSizeCriterion(WeightShiftSizeGroup, WeightShiftSizeAffinity),
		criteria.NewTeamLeadCriterion(WeightTeamLeadGroup, WeightTeamLeadAffinity),
		criteria.NewMaleBalanceCriterion(WeightMaleBalanceGroup, WeightMaleBalanceAffinity),
		criteria.NewNoDoubleShiftsCriterion(WeightNoDoubleShiftsAffinity),
		criteria.NewShiftSpreadCriterion(WeightShiftSpreadAffinity),
	}

	// Convert config overrides to allocator overrides
	logger.Debug("Converting rota overrides", zap.Int("count", len(cfg.RotaOverrides)))
	allocatorOverrides, err := convertRotaOverrides(cfg.RotaOverrides, shiftDates, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to convert rota overrides: %w", err)
	}
	logger.Debug("Converted overrides", zap.Int("count", len(allocatorOverrides)))

	// Build allocation config
	allocConfig := allocator.AllocationConfig{
		Criteria:                       allocationCriteria,
		MaxAllocationFrequency:         cfg.MaxAllocationFrequency,
		HistoricalShifts:               historicalShifts,
		Volunteers:                     allocatorVolunteers,
		Availability:                   availability,
		ShiftDates:                     shiftDateStrings,
		DefaultShiftSize:               cfg.DefaultShiftSize,
		Overrides:                      allocatorOverrides,
		WeightCurrentRotaUrgency:       WeightCurrentRotaUrgency,
		WeightOverallFrequencyFairness: WeightOverallFrequencyFairness,
		WeightPromoteGroup:             WeightPromoteGroup,
	}

	// Run the allocation algorithm
	logger.Info("Running allocation algorithm")
	outcome, err := allocator.Allocate(allocConfig)
	if err != nil {
		return nil, fmt.Errorf("allocation failed: %w", err)
	}

	logger.Info("Allocation completed",
		zap.Bool("success", outcome.Success),
		zap.Int("validation_errors", len(outcome.ValidationErrors)),
		zap.Int("underutilized_groups", len(outcome.UnderutilizedGroups)))

	// Log validation errors
	for _, verr := range outcome.ValidationErrors {
		logger.Warn("Validation error",
			zap.String("criterion", verr.CriterionName),
			zap.Int("shift_index", verr.ShiftIndex),
			zap.String("shift_date", verr.ShiftDate),
			zap.String("description", verr.Description))
	}

	// Determine if we should save allocations to database
	shouldSave := !dryRun && (outcome.Success || forceCommit)

	if shouldSave {
		logger.Info("Saving allocations to database",
			zap.Bool("success", outcome.Success),
			zap.Bool("forced", forceCommit && !outcome.Success))
		dbAllocations := convertToDBAllocations(targetRota.ID, outcome.State.Shifts)
		if err := database.InsertAllocations(dbAllocations); err != nil {
			return nil, fmt.Errorf("failed to save allocations: %w", err)
		}
		logger.Info("Allocations saved", zap.Int("count", len(dbAllocations)))
	} else if dryRun {
		logger.Info("Dry run mode - allocations not saved")
	} else {
		logger.Warn("Allocation unsuccessful - not saving to database (use forceCommit to save anyway)")
	}

	return &AllocateRotaResult{
		RotaID:              targetRota.ID,
		RotaStart:           targetRota.Start,
		ShiftCount:          targetRota.ShiftCount,
		ShiftDates:          shiftDates,
		Success:             outcome.Success,
		AllocatedShifts:     outcome.State.Shifts,
		ValidationErrors:    outcome.ValidationErrors,
		UnderutilizedGroups: outcome.UnderutilizedGroups,
	}, nil
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
			ID:         vol.ID,
			FirstName:  vol.FirstName,
			LastName:   vol.LastName,
			Gender:     vol.Gender,
			IsTeamLead: vol.Role == model.RoleTeamLead,
			GroupKey:   vol.GroupKey,
		}
	}
	return result
}

// convertToDBAllocations converts allocator shifts to database allocation records
func convertToDBAllocations(rotaID string, shifts []*allocator.Shift) []db.Allocation {
	allocations := make([]db.Allocation, 0)

	for _, shift := range shifts {
		// Add allocations for regular volunteers in groups
		for _, group := range shift.AllocatedGroups {
			for _, member := range group.Members {
				// Skip team lead if they're also the designated team lead for the shift
				if shift.TeamLead != nil && member.ID == shift.TeamLead.ID {
					continue
				}

				allocations = append(allocations, db.Allocation{
					ID:          uuid.New().String(),
					RotaID:      rotaID,
					ShiftDate:   shift.Date,
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
				RotaID:      rotaID,
				ShiftDate:   shift.Date,
				Role:        string(model.RoleTeamLead),
				VolunteerID: shift.TeamLead.ID,
				CustomEntry: "",
			})
		}

		// Add pre-allocated volunteers
		for _, preAllocatedID := range shift.CustomPreallocations {
			allocations = append(allocations, db.Allocation{
				ID:          uuid.New().String(),
				RotaID:      rotaID,
				ShiftDate:   shift.Date,
				Role:        string(model.RoleVolunteer),
				VolunteerID: "",
				CustomEntry: preAllocatedID,
			})
		}
	}

	return allocations
}

// convertRotaOverrides converts config.RotaOverride to allocator.ShiftOverride
// RRule strings are parsed and converted to date-matching functions
// shiftDates provides the actual date range for the rota, which may span years
func convertRotaOverrides(configOverrides []config.RotaOverride, shiftDates []time.Time, logger *zap.Logger) ([]allocator.ShiftOverride, error) {
	result := make([]allocator.ShiftOverride, 0, len(configOverrides))

	// Determine the date range for RRule generation from actual shift dates
	var rotaStart, rotaEnd time.Time
	if len(shiftDates) > 0 {
		rotaStart = shiftDates[0]
		rotaEnd = shiftDates[len(shiftDates)-1]
	}

	for i, override := range configOverrides {
		// Parse the RRule
		rule, err := rrule.StrToRRule(override.RRule)
		if err != nil {
			return nil, fmt.Errorf("failed to parse rrule for override %d: %w", i, err)
		}

		// Create the AppliesTo function that checks if a date matches the RRule
		// We need to capture the rule by value to avoid closure issues
		ruleForClosure := rule
		appliesTo := func(dateStr string) bool {
			// Check if this date is in the RRule set
			// Use the rota date range, with a small buffer for edge cases
			searchStart := rotaStart.AddDate(0, 0, -7) // 1 week before start
			searchEnd := rotaEnd.AddDate(0, 0, 7)      // 1 week after end

			// Set the RRule's DTSTART to start of search range
			ruleForClosure.DTStart(searchStart)

			occurrences := ruleForClosure.Between(searchStart, searchEnd, true)
			for _, occurrence := range occurrences {
				if occurrence.Format("2006-01-02") == dateStr {
					return true
				}
			}
			return false
		}

		result = append(result, allocator.ShiftOverride{
			AppliesTo:            appliesTo,
			ShiftSize:            override.ShiftSize,
			CustomPreallocations: override.CustomPreallocations,
		})

		logger.Debug("Converted override",
			zap.Int("index", i),
			zap.String("rrule", override.RRule),
			zap.Bool("has_shift_size", override.ShiftSize != nil),
			zap.Int("preallocated_count", len(override.CustomPreallocations)))
	}

	return result, nil
}

// buildHistoricalShifts fetches allocations from the previous rota and builds historical shift objects.
// Only includes Date and AllocatedGroups fields. Filters out inactive volunteers.
// If any volunteers from a group have been allocated, includes the entire group.
func buildHistoricalShifts(
	ctx context.Context,
	database AllocateRotaStore,
	allRotations []db.Rotation,
	targetRota *db.Rotation,
	activeVolunteers []allocator.Volunteer,
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

	// Fetch all allocations
	allAllocations, err := database.GetAllocations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch allocations: %w", err)
	}

	// Filter to allocations from previous rota only
	previousRotaAllocations := filterAllocationsByRotaID(allAllocations, previousRota.ID)
	logger.Debug("Filtered allocations from previous rota", zap.Int("count", len(previousRotaAllocations)))

	if len(previousRotaAllocations) == 0 {
		logger.Info("No allocations found in previous rota")
		return []*allocator.Shift{}, nil
	}

	// Build a map of active volunteers by ID for quick lookup
	volunteersByID := make(map[string]allocator.Volunteer)
	for _, vol := range activeVolunteers {
		volunteersByID[vol.ID] = vol
	}

	// Group allocations by shift date
	allocationsByDate := make(map[string][]db.Allocation)
	for _, allocation := range previousRotaAllocations {
		// Skip allocations for inactive volunteers (not in volunteersByID) or custom entries
		if allocation.VolunteerID == "" {
			continue
		}
		if _, isActive := volunteersByID[allocation.VolunteerID]; !isActive {
			continue
		}
		allocationsByDate[allocation.ShiftDate] = append(allocationsByDate[allocation.ShiftDate], allocation)
	}

	// Build historical shifts
	historicalShifts := make([]*allocator.Shift, 0, len(allocationsByDate))
	for shiftDate, allocations := range allocationsByDate {
		// Group volunteers by their GroupKey to reconstruct volunteer groups
		volunteersByGroup := make(map[string][]allocator.Volunteer)
		for _, allocation := range allocations {
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
			Date:            shiftDate,
			AllocatedGroups: allocatedGroups,
		})
	}

	logger.Debug("Built historical shifts", zap.Int("shift_count", len(historicalShifts)))

	return historicalShifts, nil
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

// filterAllocationsByRotaID filters allocations to only those for the specified rota
func filterAllocationsByRotaID(allocations []db.Allocation, rotaID string) []db.Allocation {
	filtered := make([]db.Allocation, 0)
	for _, allocation := range allocations {
		if allocation.RotaID == rotaID {
			filtered = append(filtered, allocation)
		}
	}
	return filtered
}
