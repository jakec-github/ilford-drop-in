package services

import (
	"context"
	"fmt"
	"sort"
	"time"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// VolunteerRotaStatus represents a volunteer's response status for a single rotation.
//
// The four statuses come straight off the stored round: no request row is
// "no_form"; a request with no generation before the cut-off is "no_response";
// a generation that ticked nothing is "no_availability"; anything else is
// "available". The Forms path had a fifth, "form_error", for a form that could
// not be read — there is no API left to fail, so it is gone.
type VolunteerRotaStatus struct {
	Status         string // "no_form", "no_response", "no_availability", "available"
	AvailableCount int    // number of shifts available (only set when Status == "available")
	ShiftCount     int    // total shifts in the rotation
}

// ViewHistoricalResponsesResult contains the historical response data for display
type ViewHistoricalResponsesResult struct {
	Rotations  []db.Rotation                             // sorted chronologically, last N
	Volunteers []model.Volunteer                         // all volunteers who appear in any of the selected rotations
	Matrix     map[string]map[string]VolunteerRotaStatus // [volunteerID][rotaID] -> status
}

// ViewHistoricalResponsesStore defines the database operations needed
type ViewHistoricalResponsesStore interface {
	GetRotations(ctx context.Context) ([]db.Rotation, error)
	GetAvailabilityRequestsByRotaID(ctx context.Context, rotaID string) ([]db.AvailabilityRequest, error)
	GetLatestAvailability(ctx context.Context, requestIDs []string, cutoff *time.Time) (map[string]db.AvailabilityGeneration, error)
}

// ViewHistoricalResponses summarises volunteer response status across recent
// allocated rotations, reading the stored rounds rather than Google Forms.
// If volunteerIDs is non-empty, only those volunteers are included; otherwise
// every volunteer who was asked in any selected rotation is shown.
//
// The report is longitudinal — it is looking for who habitually does not reply
// in time — so each rota is read as at its own `allocated_datetime`. An answer
// changed after the rota went out is not an answer that counted.
func ViewHistoricalResponses(
	ctx context.Context,
	database ViewHistoricalResponsesStore,
	volunteerClient VolunteerClient,
	cfg *config.Config,
	logger *zap.Logger,
	count int,
	volunteerIDs []string,
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

	// Step 2: Read each selected rota's round — who was asked, and what they had
	// said by the time the rota was allocated.
	statusByRota := make(map[string]map[string]VolunteerRotaStatus, len(selectedRotations))
	askedVolunteerIDs := make(map[string]bool)
	for _, rota := range selectedRotations {
		statuses, err := rotaResponseStatuses(ctx, database, rota)
		if err != nil {
			return nil, fmt.Errorf("rota %s: %w", rota.ID, err)
		}
		statusByRota[rota.ID] = statuses
		for volunteerID := range statuses {
			askedVolunteerIDs[volunteerID] = true
		}
	}

	// Step 3: Fetch volunteer list
	allVolunteers, err := volunteerClient.ListVolunteers(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch volunteers: %w", err)
	}

	// If specific volunteer IDs were requested, restrict to those
	if len(volunteerIDs) > 0 {
		filterSet := make(map[string]bool, len(volunteerIDs))
		for _, id := range volunteerIDs {
			filterSet[id] = true
		}
		for id := range askedVolunteerIDs {
			if !filterSet[id] {
				delete(askedVolunteerIDs, id)
			}
		}
	}

	// Build the volunteer list (those who appear in any selected rotation)
	var volunteers []model.Volunteer
	for _, vol := range allVolunteers {
		if askedVolunteerIDs[vol.ID] {
			volunteers = append(volunteers, vol)
		}
	}

	// Sort volunteers by display name for consistent output
	sort.Slice(volunteers, func(i, j int) bool {
		return volunteers[i].DisplayName < volunteers[j].DisplayName
	})

	// Step 4: Fill the matrix. A volunteer with no request for a rota was not
	// asked at all, which is what "no_form" has always meant.
	matrix := make(map[string]map[string]VolunteerRotaStatus, len(volunteers))
	for _, vol := range volunteers {
		matrix[vol.ID] = make(map[string]VolunteerRotaStatus, len(selectedRotations))
		for _, rota := range selectedRotations {
			status, asked := statusByRota[rota.ID][vol.ID]
			if !asked {
				status = VolunteerRotaStatus{Status: "no_form", ShiftCount: rota.ShiftCount}
			}
			matrix[vol.ID][rota.ID] = status
		}
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

// rotaResponseStatuses reads one allocated rota's round and returns a status per
// volunteer who was asked, keyed by volunteer id. Volunteers who were not asked
// are simply absent — the caller decides what that means.
func rotaResponseStatuses(
	ctx context.Context,
	database ViewHistoricalResponsesStore,
	rota db.Rotation,
) (map[string]VolunteerRotaStatus, error) {
	cutoff, err := time.Parse(time.RFC3339, rota.AllocatedDatetime)
	if err != nil {
		return nil, fmt.Errorf("failed to parse allocated_datetime: %w", err)
	}

	requests, err := database.GetAvailabilityRequestsByRotaID(ctx, rota.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch availability requests: %w", err)
	}

	requestIDs := make([]string, 0, len(requests))
	for _, r := range requests {
		requestIDs = append(requestIDs, r.ID)
	}
	latest, err := database.GetLatestAvailability(ctx, requestIDs, &cutoff)
	if err != nil {
		return nil, fmt.Errorf("failed to read availability: %w", err)
	}

	statuses := make(map[string]VolunteerRotaStatus, len(requests))
	for _, request := range requests {
		status := VolunteerRotaStatus{ShiftCount: rota.ShiftCount}
		generation, replied := latest[request.ID]
		switch {
		case !replied:
			status.Status = "no_response"
		case len(generation.Answers) == 0:
			// Available for nothing: a real answer, and one the Forms encoding
			// could not tell apart from silence.
			status.Status = "no_availability"
		default:
			status.Status = "available"
			status.AvailableCount = len(generation.Answers)
		}
		statuses[request.VolunteerID] = status
	}

	return statuses, nil
}
