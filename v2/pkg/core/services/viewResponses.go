package services

import (
	"context"
	"fmt"
	"sort"
	"time"

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

// ViewResponsesResult contains the response data for display
type ViewResponsesResult struct {
	RotaID            string
	RotaStart         string
	ShiftCount        int
	ShiftDates        []time.Time
	GroupResponses    []GroupResponse
	RespondedCount    int
	NotRespondedCount int
	TotalActiveCount  int
}

// ViewResponsesStore defines the database operations needed for viewing responses
type ViewResponsesStore interface {
	GetRotations(ctx context.Context) ([]db.Rotation, error)
	GetAvailabilityRequests(ctx context.Context) ([]db.AvailabilityRequest, error)
}

// FormsClientWithResponses defines the operations needed to fetch form responses
type FormsClientWithResponses interface {
	GetFormResponse(formID string, volunteerName string, shiftDates []time.Time) (*formsclient.FormResponse, error)
	HasResponse(formID string) (bool, error)
}

// ViewResponses fetches and displays availability responses for a given rota (or latest if rotaID is empty)
func ViewResponses(
	ctx context.Context,
	database ViewResponsesStore,
	volunteerClient VolunteerClient,
	formsClient FormsClientWithResponses,
	cfg *config.Config,
	logger *zap.Logger,
	rotaID string,
) (*ViewResponsesResult, error) {
	logger.Debug("Starting viewResponses", zap.String("rota_id", rotaID))

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

	// Step 5: For each request, fetch the form response and build result
	responses := make([]VolunteerResponse, 0, len(requestsForRota))
	respondedCount := 0
	notRespondedCount := 0
	activeCount := 0

	for _, req := range requestsForRota {
		volunteer, exists := volunteersByID[req.VolunteerID]
		if !exists {
			logger.Warn("Volunteer not found", zap.String("volunteer_id", req.VolunteerID))
			continue
		}

		volunteerName := fmt.Sprintf("%s %s", volunteer.FirstName, volunteer.LastName)

		// Only count active volunteers
		isActive := volunteer.Status == "Active"
		if isActive {
			activeCount++
		}

		logger.Debug("Fetching response for volunteer",
			zap.String("volunteer_id", volunteer.ID),
			zap.String("volunteer_name", volunteerName),
			zap.String("form_id", req.FormID))

		// Get form response
		formResp, err := formsClient.GetFormResponse(req.FormID, volunteerName, shiftDates)
		if err != nil {
			return nil, fmt.Errorf("failed to get form response for volunteer %s: %w", volunteer.ID, err)
		}

		// Track response counts (only for active volunteers)
		if isActive {
			if formResp.HasResponded {
				respondedCount++
			} else {
				notRespondedCount++
			}
		}

		responses = append(responses, VolunteerResponse{
			VolunteerID:      volunteer.ID,
			VolunteerName:    volunteerName,
			Email:            volunteer.Email,
			Status:           volunteer.Status,
			HasResponded:     formResp.HasResponded,
			AvailableForAll:  formResp.AvailableForAll,
			UnavailableDates: formResp.UnavailableDates,
			AvailableDates:   formResp.AvailableDates,
			FormURL:          req.FormURL,
		})
	}

	// Aggregate responses by group
	groupResponses := aggregateByGroup(responses, shiftDates, volunteersByID)

	// Sort group responses: responded first, then by name
	sort.Slice(groupResponses, func(i, j int) bool {
		if groupResponses[i].HasResponded != groupResponses[j].HasResponded {
			return groupResponses[i].HasResponded
		}
		return groupResponses[i].GroupName < groupResponses[j].GroupName
	})

	logger.Debug("View responses completed",
		zap.Int("total_requests", len(responses)),
		zap.Int("responded", respondedCount),
		zap.Int("not_responded", notRespondedCount),
		zap.Int("total_active", activeCount),
		zap.Int("total_groups", len(groupResponses)))

	return &ViewResponsesResult{
		RotaID:            targetRota.ID,
		RotaStart:         targetRota.Start,
		ShiftCount:        targetRota.ShiftCount,
		ShiftDates:        shiftDates,
		GroupResponses:    groupResponses,
		RespondedCount:    respondedCount,
		NotRespondedCount: notRespondedCount,
		TotalActiveCount:  activeCount,
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
