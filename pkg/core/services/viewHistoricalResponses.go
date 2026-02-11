package services

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/clients/formsclient"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// VolunteerRotaStatus represents a volunteer's response status for a single rotation
type VolunteerRotaStatus struct {
	Status         string // "no_form", "no_response", "no_availability", "available", "form_error"
	AvailableCount int    // number of shifts available (only set when Status == "available")
	ShiftCount     int    // total shifts in the rotation
}

// ViewHistoricalResponsesResult contains the historical response data for display
type ViewHistoricalResponsesResult struct {
	Rotations  []db.Rotation                              // sorted chronologically, last N
	Volunteers []model.Volunteer                          // all volunteers who appear in any of the selected rotations
	Matrix     map[string]map[string]VolunteerRotaStatus  // [volunteerID][rotaID] -> status
}

// ViewHistoricalResponsesStore defines the database operations needed
type ViewHistoricalResponsesStore interface {
	GetRotations(ctx context.Context) ([]db.Rotation, error)
	GetAvailabilityRequests(ctx context.Context) ([]db.AvailabilityRequest, error)
}

// HistoricalFormsClient defines the forms client operations needed for historical response fetching
type HistoricalFormsClient interface {
	GetFormResponseBefore(formID string, volunteerName string, shiftDates []time.Time, before time.Time) (*formsclient.FormResponse, error)
}

const maxConcurrentFormRequests = 10

// ViewHistoricalResponses fetches and summarises volunteer response status across recent allocated rotations
func ViewHistoricalResponses(
	ctx context.Context,
	database ViewHistoricalResponsesStore,
	volunteerClient VolunteerClient,
	formsClient HistoricalFormsClient,
	cfg *config.Config,
	logger *zap.Logger,
	count int,
) (*ViewHistoricalResponsesResult, error) {
	logger.Debug("Starting viewHistoricalResponses", zap.Int("count", count))

	// Step 1: Fetch all rotations and filter to allocated ones
	rotations, err := database.GetRotations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch rotations: %w", err)
	}

	// Filter to rotations with a non-null allocated_datetime
	var allocated []db.Rotation
	for _, r := range rotations {
		if r.AllocatedDatetime != "" {
			allocated = append(allocated, r)
		}
	}

	if len(allocated) == 0 {
		return nil, fmt.Errorf("no allocated rotations found")
	}

	// Sort chronologically by start date
	sort.Slice(allocated, func(i, j int) bool {
		return allocated[i].Start < allocated[j].Start
	})

	// Take the last `count` rotations
	if count > len(allocated) {
		count = len(allocated)
	}
	selectedRotations := allocated[len(allocated)-count:]

	logger.Debug("Selected rotations", zap.Int("count", len(selectedRotations)))

	// Step 2: Fetch all availability requests
	allRequests, err := database.GetAvailabilityRequests(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch availability requests: %w", err)
	}

	// Step 3: Fetch volunteer list
	allVolunteers, err := volunteerClient.ListVolunteers(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch volunteers: %w", err)
	}

	volunteersByID := make(map[string]model.Volunteer)
	for _, vol := range allVolunteers {
		volunteersByID[vol.ID] = vol
	}

	// Build set of volunteer IDs who appear in any selected rotation's availability requests
	relevantVolunteerIDs := make(map[string]bool)
	for _, rota := range selectedRotations {
		for _, req := range allRequests {
			if req.RotaID == rota.ID && req.FormSent {
				relevantVolunteerIDs[req.VolunteerID] = true
			}
		}
	}

	// Build the volunteer list (those who appear in any selected rotation)
	var volunteers []model.Volunteer
	for volID := range relevantVolunteerIDs {
		if vol, exists := volunteersByID[volID]; exists {
			volunteers = append(volunteers, vol)
		}
	}

	// Sort volunteers by display name for consistent output
	sort.Slice(volunteers, func(i, j int) bool {
		return volunteers[i].DisplayName < volunteers[j].DisplayName
	})

	// Step 4: For each rotation x volunteer, determine status
	matrix := make(map[string]map[string]VolunteerRotaStatus)

	// Build request lookup: [rotaID][volunteerID] -> AvailabilityRequest
	requestLookup := make(map[string]map[string]db.AvailabilityRequest)
	for _, req := range allRequests {
		if req.FormSent {
			if _, ok := requestLookup[req.RotaID]; !ok {
				requestLookup[req.RotaID] = make(map[string]db.AvailabilityRequest)
			}
			requestLookup[req.RotaID][req.VolunteerID] = req
		}
	}

	// Collect all form fetch tasks
	type formFetchTask struct {
		rotaID        string
		volunteerID   string
		volunteerName string
		formID        string
		shiftDates    []time.Time
		cutoff        time.Time
		shiftCount    int
	}

	var tasks []formFetchTask

	// Pre-calculate shift dates for each rotation
	rotaShiftDates := make(map[string][]time.Time)
	for _, rota := range selectedRotations {
		dates, err := calculateShiftDates(rota.Start, rota.ShiftCount)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate shift dates for rota %s: %w", rota.ID, err)
		}
		rotaShiftDates[rota.ID] = dates
	}

	for _, vol := range volunteers {
		matrix[vol.ID] = make(map[string]VolunteerRotaStatus)

		for _, rota := range selectedRotations {
			reqMap, rotaHasRequests := requestLookup[rota.ID]
			if !rotaHasRequests {
				matrix[vol.ID][rota.ID] = VolunteerRotaStatus{
					Status:     "no_form",
					ShiftCount: rota.ShiftCount,
				}
				continue
			}

			req, volHasRequest := reqMap[vol.ID]
			if !volHasRequest {
				matrix[vol.ID][rota.ID] = VolunteerRotaStatus{
					Status:     "no_form",
					ShiftCount: rota.ShiftCount,
				}
				continue
			}

			// Parse the allocated_datetime cutoff
			cutoff, err := time.Parse(time.RFC3339, rota.AllocatedDatetime)
			if err != nil {
				return nil, fmt.Errorf("failed to parse allocated_datetime for rota %s: %w", rota.ID, err)
			}

			volunteerName := fmt.Sprintf("%s %s", vol.FirstName, vol.LastName)

			tasks = append(tasks, formFetchTask{
				rotaID:        rota.ID,
				volunteerID:   vol.ID,
				volunteerName: volunteerName,
				formID:        req.FormID,
				shiftDates:    rotaShiftDates[rota.ID],
				cutoff:        cutoff,
				shiftCount:    rota.ShiftCount,
			})
		}
	}

	// Fetch form responses in parallel with semaphore
	type formFetchResult struct {
		rotaID      string
		volunteerID string
		status      VolunteerRotaStatus
		err         error
	}

	resultChan := make(chan formFetchResult, len(tasks))
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, maxConcurrentFormRequests)

	for _, task := range tasks {
		wg.Add(1)
		go func(t formFetchTask) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			formResp, err := formsClient.GetFormResponseBefore(t.formID, t.volunteerName, t.shiftDates, t.cutoff)
			if err != nil {
				resultChan <- formFetchResult{
					rotaID:      t.rotaID,
					volunteerID: t.volunteerID,
					status: VolunteerRotaStatus{
						Status:     "form_error",
						ShiftCount: t.shiftCount,
					},
				}
				logger.Warn("Form error for volunteer",
					zap.String("volunteer_id", t.volunteerID),
					zap.String("rota_id", t.rotaID),
					zap.Error(err))
				return
			}

			var status VolunteerRotaStatus
			if !formResp.HasResponded {
				status = VolunteerRotaStatus{
					Status:     "no_response",
					ShiftCount: t.shiftCount,
				}
			} else if len(formResp.AvailableDates) == 0 {
				status = VolunteerRotaStatus{
					Status:     "no_availability",
					ShiftCount: t.shiftCount,
				}
			} else {
				status = VolunteerRotaStatus{
					Status:         "available",
					AvailableCount: len(formResp.AvailableDates),
					ShiftCount:     t.shiftCount,
				}
			}

			resultChan <- formFetchResult{
				rotaID:      t.rotaID,
				volunteerID: t.volunteerID,
				status:      status,
			}
		}(task)
	}

	wg.Wait()
	close(resultChan)

	for result := range resultChan {
		if _, ok := matrix[result.volunteerID]; !ok {
			matrix[result.volunteerID] = make(map[string]VolunteerRotaStatus)
		}
		matrix[result.volunteerID][result.rotaID] = result.status
	}

	logger.Debug("ViewHistoricalResponses completed",
		zap.Int("rotations", len(selectedRotations)),
		zap.Int("volunteers", len(volunteers)))

	return &ViewHistoricalResponsesResult{
		Rotations:  selectedRotations,
		Volunteers: volunteers,
		Matrix:     matrix,
	}, nil
}
