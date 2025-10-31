package services

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/teambition/rrule-go"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/clients/formsclient"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// VolunteerResponse represents a volunteer's availability response status
type VolunteerResponse struct {
	VolunteerID      string
	VolunteerName    string
	Email            string
	Status           string // "Active" or other
	HasResponded     bool
	AvailableForAll  bool
	UnavailableDates []string
	AvailableDates   []string
	FormURL          string
}

// GroupResponse represents a group's aggregated availability response status
type GroupResponse struct {
	GroupKey         string
	GroupName        string   // Display name for the group
	MemberNames      []string // Names of all members in the group
	HasResponded     bool     // True if ANY member has responded
	UnavailableDates []string // Dates where ANY member is unavailable
	AvailableDates   []string // Dates where ALL members are available
}

// ShiftAvailabilityInfo contains availability information for a specific shift
type ShiftAvailabilityInfo struct {
	Date              string // Formatted date string
	ShiftSize         int    // Number of volunteers needed (from config + RRule overrides)
	AvailableCount    int    // Number of available volunteers (excluding team lead)
	Delta             int    // AvailableCount - ShiftSize (0 = exact, negative = understaffed)
	HasTeamLead       bool   // Whether an active team lead is available for this shift
}

// ViewResponsesResult contains the response data for display
type ViewResponsesResult struct {
	RotaID            string
	RotaStart         string
	ShiftCount        int
	ShiftDates        []time.Time
	ShiftAvailability []ShiftAvailabilityInfo // Per-shift availability information
	GroupResponses    []GroupResponse
	RespondedCount    int
	NotRespondedCount int
	TotalActiveCount  int
	AllocationResult  *AllocateRotaResult // Optional allocation result (when showAllocation=true)
}

// ViewResponsesStore defines the database operations needed for viewing responses
type ViewResponsesStore interface {
	GetRotations(ctx context.Context) ([]db.Rotation, error)
	GetAvailabilityRequests(ctx context.Context) ([]db.AvailabilityRequest, error)
	GetAllocations(ctx context.Context) ([]db.Allocation, error)
	InsertAllocations(allocations []db.Allocation) error
}

// FormsClientWithResponses defines the operations needed to fetch form responses
type FormsClientWithResponses interface {
	GetFormResponse(formID string, volunteerName string, shiftDates []time.Time) (*formsclient.FormResponse, error)
	HasResponse(formID string) (bool, error)
}

// ViewResponses fetches and displays availability responses for a given rota (or latest if rotaID is empty)
// If showAllocation is true, also runs allocation algorithm in dry-run mode
func ViewResponses(
	ctx context.Context,
	database ViewResponsesStore,
	volunteerClient VolunteerClient,
	formsClient FormsClientWithResponses,
	cfg *config.Config,
	logger *zap.Logger,
	rotaID string,
	showAllocation bool,
) (*ViewResponsesResult, error) {
	logger.Debug("Starting viewResponses",
		zap.String("rota_id", rotaID),
		zap.Bool("show_allocation", showAllocation))

	// Step 1: Fetch all rotations
	logger.Debug("Fetching rotations")
	rotations, err := database.GetRotations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch rotations: %w", err)
	}
	logger.Debug("Found rotations", zap.Int("count", len(rotations)))

	if len(rotations) == 0 {
		return nil, fmt.Errorf("no rotations found - please define a rota first")
	}

	// Step 2: Find the target rota
	var targetRota *db.Rotation
	if rotaID == "" {
		// Use latest rota
		targetRota = findLatestRotation(rotations)
		logger.Debug("Using latest rota", zap.String("id", targetRota.ID))
	} else {
		// Find specific rota
		for i := range rotations {
			if rotations[i].ID == rotaID {
				targetRota = &rotations[i]
				break
			}
		}
		if targetRota == nil {
			return nil, fmt.Errorf("rota not found: %s", rotaID)
		}
		logger.Debug("Using specified rota", zap.String("id", targetRota.ID))
	}

	logger.Debug("Target rota",
		zap.String("id", targetRota.ID),
		zap.String("start", targetRota.Start),
		zap.Int("shift_count", targetRota.ShiftCount))

	// Calculate shift dates
	shiftDates, err := calculateShiftDates(targetRota.Start, targetRota.ShiftCount)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate shift dates: %w", err)
	}

	// Step 3: Fetch all availability requests for this rota
	logger.Debug("Fetching availability requests")
	allRequests, err := database.GetAvailabilityRequests(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch availability requests: %w", err)
	}
	logger.Debug("Found availability requests", zap.Int("count", len(allRequests)))

	// Filter to requests for target rota that were sent
	requestsForRota := filterSentRequestsByRotaID(allRequests, targetRota.ID)
	logger.Debug("Filtered sent requests for target rota", zap.Int("count", len(requestsForRota)))

	if len(requestsForRota) == 0 {
		return nil, fmt.Errorf("no availability requests found for rota %s - please run requestAvailability first", targetRota.ID)
	}

	// Step 4: Fetch all volunteers
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

	// Step 5: For each request, fetch the form response and build result (in parallel)
	// First, filter out volunteers that don't exist or are inactive
	type fetchRequest struct {
		req           db.AvailabilityRequest
		volunteer     model.Volunteer
		volunteerName string
	}

	fetchRequests := make([]fetchRequest, 0, len(requestsForRota))
	for _, req := range requestsForRota {
		volunteer, exists := volunteersByID[req.VolunteerID]
		if !exists {
			logger.Warn("Volunteer not found", zap.String("volunteer_id", req.VolunteerID))
			continue
		}

		// Skip inactive volunteers entirely
		if volunteer.Status != "Active" {
			logger.Debug("Skipping inactive volunteer",
				zap.String("volunteer_id", volunteer.ID),
				zap.String("volunteer_name", fmt.Sprintf("%s %s", volunteer.FirstName, volunteer.LastName)),
				zap.String("status", volunteer.Status))
			continue
		}

		volunteerName := fmt.Sprintf("%s %s", volunteer.FirstName, volunteer.LastName)
		fetchRequests = append(fetchRequests, fetchRequest{
			req:           req,
			volunteer:     volunteer,
			volunteerName: volunteerName,
		})
	}

	logger.Debug("Fetching form responses in parallel", zap.Int("count", len(fetchRequests)))

	// Fetch responses in parallel
	type fetchResult struct {
		response VolunteerResponse
		err      error
	}

	resultChan := make(chan fetchResult, len(fetchRequests))
	var wg sync.WaitGroup

	for _, fr := range fetchRequests {
		wg.Add(1)
		go func(fr fetchRequest) {
			defer wg.Done()

			logger.Debug("Fetching response for volunteer",
				zap.String("volunteer_id", fr.volunteer.ID),
				zap.String("volunteer_name", fr.volunteerName),
				zap.String("form_id", fr.req.FormID))

			// Get form response
			formResp, err := formsClient.GetFormResponse(fr.req.FormID, fr.volunteerName, shiftDates)
			if err != nil {
				resultChan <- fetchResult{
					err: fmt.Errorf("failed to get form response for volunteer %s: %w", fr.volunteer.ID, err),
				}
				return
			}

			resultChan <- fetchResult{
				response: VolunteerResponse{
					VolunteerID:      fr.volunteer.ID,
					VolunteerName:    fr.volunteerName,
					Email:            fr.volunteer.Email,
					Status:           fr.volunteer.Status,
					HasResponded:     formResp.HasResponded,
					AvailableForAll:  formResp.AvailableForAll,
					UnavailableDates: formResp.UnavailableDates,
					AvailableDates:   formResp.AvailableDates,
					FormURL:          fr.req.FormURL,
				},
			}
		}(fr)
	}

	// Wait for all goroutines to complete
	wg.Wait()
	close(resultChan)

	// Collect results
	responses := make([]VolunteerResponse, 0, len(fetchRequests))
	respondedCount := 0
	notRespondedCount := 0

	for result := range resultChan {
		if result.err != nil {
			return nil, result.err
		}

		if result.response.HasResponded {
			respondedCount++
		} else {
			notRespondedCount++
		}

		responses = append(responses, result.response)
	}

	activeCount := len(responses)

	// Aggregate responses by group
	groupResponses := aggregateByGroup(responses, shiftDates, volunteersByID)

	// Sort group responses: responded first, then by name
	sort.Slice(groupResponses, func(i, j int) bool {
		if groupResponses[i].HasResponded != groupResponses[j].HasResponded {
			return groupResponses[i].HasResponded
		}
		return groupResponses[i].GroupName < groupResponses[j].GroupName
	})

	// Calculate shift availability information (includes team lead availability)
	shiftAvailability := calculateShiftAvailability(responses, shiftDates, cfg, volunteersByID, logger)

	// Count shifts without team lead for logging
	shiftsWithoutTeamLead := 0
	for _, shift := range shiftAvailability {
		if !shift.HasTeamLead {
			shiftsWithoutTeamLead++
		}
	}

	logger.Debug("View responses completed",
		zap.Int("total_requests", len(responses)),
		zap.Int("responded", respondedCount),
		zap.Int("not_responded", notRespondedCount),
		zap.Int("total_active", activeCount),
		zap.Int("total_groups", len(groupResponses)),
		zap.Int("shifts_without_team_lead", shiftsWithoutTeamLead))

	// Optionally run allocation algorithm in dry-run mode
	var allocationResult *AllocateRotaResult
	if showAllocation {
		logger.Debug("Running allocation algorithm (dry-run)")
		allocResult, err := AllocateRota(
			ctx,
			database,
			volunteerClient,
			formsClient,
			cfg,
			logger,
			true,  // dryRun
			false, // forceCommit
		)
		if err != nil {
			return nil, fmt.Errorf("failed to run allocation: %w", err)
		}
		allocationResult = allocResult
		logger.Debug("Allocation completed",
			zap.Bool("success", allocResult.Success),
			zap.Int("validation_errors", len(allocResult.ValidationErrors)))
	}

	return &ViewResponsesResult{
		RotaID:            targetRota.ID,
		RotaStart:         targetRota.Start,
		ShiftCount:        targetRota.ShiftCount,
		ShiftDates:        shiftDates,
		ShiftAvailability: shiftAvailability,
		GroupResponses:    groupResponses,
		RespondedCount:    respondedCount,
		NotRespondedCount: notRespondedCount,
		TotalActiveCount:  activeCount,
		AllocationResult:  allocationResult,
	}, nil
}

// aggregateByGroup aggregates volunteer responses into group responses
// A group has responded if ANY member has responded
// A group is unavailable on dates where ANY member is unavailable
func aggregateByGroup(responses []VolunteerResponse, shiftDates []time.Time, volunteersByID map[string]model.Volunteer) []GroupResponse {
	// Group responses by GroupKey
	groupMap := make(map[string][]VolunteerResponse)

	for _, resp := range responses {
		volunteer := volunteersByID[resp.VolunteerID]
		groupKey := volunteer.GroupKey

		// If volunteer has no group, create a unique group for them
		if groupKey == "" {
			groupKey = "individual_" + resp.VolunteerID
		}

		groupMap[groupKey] = append(groupMap[groupKey], resp)
	}

	// Convert to GroupResponse slice
	groupResponses := make([]GroupResponse, 0, len(groupMap))

	for groupKey, memberResponses := range groupMap {
		// Determine if group has responded (ANY member responded)
		hasResponded := false
		memberNames := make([]string, 0, len(memberResponses))

		for _, resp := range memberResponses {
			memberNames = append(memberNames, resp.VolunteerName)
			if resp.HasResponded {
				hasResponded = true
			}
		}

		// Build set of unavailable dates (ANY responding member explicitly unavailable)
		unavailableSet := make(map[string]bool)
		for _, resp := range memberResponses {
			if resp.HasResponded {
				// Only consider their unavailable dates if they've responded
				for _, dateStr := range resp.UnavailableDates {
					unavailableSet[dateStr] = true
				}
			}
			// If member hasn't responded, ignore them (don't assume unavailable)
		}

		// Convert unavailable set to slice
		unavailableDates := make([]string, 0, len(unavailableSet))
		for dateStr := range unavailableSet {
			unavailableDates = append(unavailableDates, dateStr)
		}

		// Calculate available dates (dates NOT in unavailable set)
		availableDates := make([]string, 0)
		for _, date := range shiftDates {
			dateStr := date.Format("Mon Jan 2 2006")
			if !unavailableSet[dateStr] {
				availableDates = append(availableDates, dateStr)
			}
		}

		// Determine group name
		groupName := groupKey
		if len(memberNames) == 1 {
			// Single member, use their name
			groupName = memberNames[0]
		} else {
			// Multiple members, use group key
			groupName = groupKey
		}

		groupResponses = append(groupResponses, GroupResponse{
			GroupKey:         groupKey,
			GroupName:        groupName,
			MemberNames:      memberNames,
			HasResponded:     hasResponded,
			UnavailableDates: unavailableDates,
			AvailableDates:   availableDates,
		})
	}

	return groupResponses
}

// calculateShiftAvailability calculates availability information for each shift
// including shift size (from config + overrides), available volunteer count, and delta
// Uses group-based counting: if ANY member of a group has responded, ALL active members
// are counted as available unless they explicitly marked themselves unavailable
func calculateShiftAvailability(
	responses []VolunteerResponse,
	shiftDates []time.Time,
	cfg *config.Config,
	volunteersByID map[string]model.Volunteer,
	logger *zap.Logger,
) []ShiftAvailabilityInfo {
	result := make([]ShiftAvailabilityInfo, 0, len(shiftDates))

	// Build shift size calculator
	shiftSizeCalc := buildShiftSizeCalculator(cfg, shiftDates, logger)

	// Group responses by GroupKey (same logic as aggregateByGroup)
	groupMap := make(map[string][]VolunteerResponse)
	for _, resp := range responses {
		volunteer := volunteersByID[resp.VolunteerID]
		groupKey := volunteer.GroupKey

		// If volunteer has no group, create a unique group for them
		if groupKey == "" {
			groupKey = "individual_" + resp.VolunteerID
		}

		groupMap[groupKey] = append(groupMap[groupKey], resp)
	}

	for _, shiftDate := range shiftDates {
		dateStr := shiftDate.Format("Mon Jan 2 2006")
		dateKey := shiftDate.Format("2006-01-02")

		// Calculate shift size for this date
		shiftSize := shiftSizeCalc(dateKey)

		// Count available volunteers for this shift using group-based logic
		availableCount := 0
		hasTeamLead := false

		// Iterate through groups
		for _, memberResponses := range groupMap {
			// Check if ANY member of this group has responded
			groupHasResponded := false
			for _, resp := range memberResponses {
				if resp.HasResponded {
					groupHasResponded = true
					break
				}
			}

			// Skip this group if no one has responded
			if !groupHasResponded {
				continue
			}

			// Check if ANY responding member is unavailable for this date
			groupUnavailable := false
			for _, resp := range memberResponses {
				if resp.HasResponded {
					for _, unavailDate := range resp.UnavailableDates {
						if unavailDate == dateStr {
							groupUnavailable = true
							break
						}
					}
					if groupUnavailable {
						break
					}
				}
			}

			// If group is unavailable for this date, skip counting its members
			if groupUnavailable {
				continue
			}

			// Group is available - count ALL active members
			for _, resp := range memberResponses {
				volunteer := volunteersByID[resp.VolunteerID]

				// Skip if not active
				if volunteer.Status != "Active" {
					continue
				}

				// Check role and count appropriately
				if volunteer.Role == model.RoleTeamLead {
					// Available team lead found
					hasTeamLead = true
				} else if volunteer.Role == model.RoleVolunteer {
					// Available volunteer - count them
					availableCount++
				}
			}
		}

		// Calculate delta
		delta := availableCount - shiftSize

		result = append(result, ShiftAvailabilityInfo{
			Date:           dateStr,
			ShiftSize:      shiftSize,
			AvailableCount: availableCount,
			Delta:          delta,
			HasTeamLead:    hasTeamLead,
		})
	}

	return result
}

// buildShiftSizeCalculator creates a function that returns the shift size for a given date
// It considers the default shift size, any RRule-based overrides, and subtracts custom preallocations
// Shift size comes from the first matching rule with an explicit shiftSize
// Preallocations are summed across ALL matching rules
func buildShiftSizeCalculator(cfg *config.Config, shiftDates []time.Time, logger *zap.Logger) func(dateKey string) int {
	// Parse all RRule overrides and create matchers
	type overrideMatcher struct {
		matches              func(string) bool
		shiftSize            *int     // nil if not specified
		preallocationCount   int
		overrideIndex        int
	}
	matchers := make([]overrideMatcher, 0)

	// Determine the date range for RRule generation
	var rotaStart, rotaEnd time.Time
	if len(shiftDates) > 0 {
		rotaStart = shiftDates[0]
		rotaEnd = shiftDates[len(shiftDates)-1]
	}

	for i, override := range cfg.RotaOverrides {
		// Parse the RRule
		rule, err := rrule.StrToRRule(override.RRule)
		if err != nil {
			logger.Warn("Failed to parse rrule for override",
				zap.Int("override_index", i),
				zap.String("rrule", override.RRule),
				zap.Error(err))
			continue
		}

		preallocationCount := len(override.CustomPreallocations)

		var shiftSizePtr *int
		if override.ShiftSize != nil {
			shiftSizePtr = override.ShiftSize
		}

		logger.Debug("Added override",
			zap.Int("override_index", i),
			zap.String("rrule", override.RRule),
			zap.Any("shift_size", shiftSizePtr),
			zap.Int("preallocation_count", preallocationCount))

		// Create matcher function
		overrideIndex := i
		ruleForClosure := rule
		matcher := func(dateKey string) bool {
			searchStart := rotaStart.AddDate(0, 0, -7)
			searchEnd := rotaEnd.AddDate(0, 0, 7)
			ruleForClosure.DTStart(searchStart)
			occurrences := ruleForClosure.Between(searchStart, searchEnd, true)
			for _, occurrence := range occurrences {
				if occurrence.Format("2006-01-02") == dateKey {
					return true
				}
			}
			return false
		}

		matchers = append(matchers, overrideMatcher{
			matches:            matcher,
			shiftSize:          shiftSizePtr,
			preallocationCount: preallocationCount,
			overrideIndex:      overrideIndex,
		})
	}

	// Return calculator function
	return func(dateKey string) int {
		// Find base shift size (first matching rule with explicit shiftSize, or default)
		var baseShiftSize int
		foundShiftSize := false

		for _, matcher := range matchers {
			if matcher.matches(dateKey) && matcher.shiftSize != nil {
				baseShiftSize = *matcher.shiftSize
				foundShiftSize = true
				logger.Debug("Found shift size from override",
					zap.Int("override_index", matcher.overrideIndex),
					zap.String("date", dateKey),
					zap.Int("shift_size", baseShiftSize))
				break
			}
		}

		if !foundShiftSize {
			baseShiftSize = cfg.DefaultShiftSize
			logger.Debug("Using default shift size",
				zap.String("date", dateKey),
				zap.Int("default_shift_size", baseShiftSize))
		}

		// Sum preallocations from ALL matching rules
		totalPreallocations := 0
		for _, matcher := range matchers {
			if matcher.matches(dateKey) && matcher.preallocationCount > 0 {
				totalPreallocations += matcher.preallocationCount
				logger.Debug("Adding preallocations from override",
					zap.Int("override_index", matcher.overrideIndex),
					zap.String("date", dateKey),
					zap.Int("preallocation_count", matcher.preallocationCount),
					zap.Int("total_so_far", totalPreallocations))
			}
		}

		// Calculate effective shift size
		effectiveSize := baseShiftSize - totalPreallocations
		if effectiveSize < 0 {
			effectiveSize = 0
		}

		logger.Debug("Calculated effective shift size",
			zap.String("date", dateKey),
			zap.Int("base_shift_size", baseShiftSize),
			zap.Int("total_preallocations", totalPreallocations),
			zap.Int("effective_shift_size", effectiveSize))

		return effectiveSize
	}
}
