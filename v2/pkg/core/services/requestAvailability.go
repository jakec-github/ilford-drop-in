package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/clients/formsclient"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// UnsentForm represents a form that needs to be sent
type UnsentForm struct {
	VolunteerID   string
	VolunteerName string
	FormURL       string
}

// AvailabilityRequestResult represents the result of requesting availability
type AvailabilityRequestResult struct {
	LatestRota  *db.Rotation
	UnsentForms []UnsentForm
}

// AvailabilityRequestStore defines the database operations needed for request availability
type AvailabilityRequestStore interface {
	GetRotations(ctx context.Context) ([]db.Rotation, error)
	GetAvailabilityRequests(ctx context.Context) ([]db.AvailabilityRequest, error)
	InsertAvailabilityRequests(requests []db.AvailabilityRequest) error
}

// VolunteerClient defines the operations needed to fetch volunteers
type VolunteerClient interface {
	ListVolunteers(cfg *config.Config) ([]model.Volunteer, error)
}

// FormsClient defines the operations needed to create forms
type FormsClient interface {
	CreateAvailabilityForm(volunteerName string, rotaID string, shiftDates []time.Time) (*formsclient.AvailabilityFormResult, error)
}

// RequestAvailability creates availability forms for volunteers and returns unsent forms
// It fetches the latest rota, creates forms for volunteers without requests, inserts DB records,
// and returns all unsent forms for the current rota
func RequestAvailability(
	ctx context.Context,
	database AvailabilityRequestStore,
	volunteerClient VolunteerClient,
	formsClient FormsClient,
	cfg *config.Config,
	logger *zap.Logger,
	deadline string,
) (*AvailabilityRequestResult, error) {
	logger.Info("Starting requestAvailability", zap.String("deadline", deadline))

	// Step 1: Fetch all rotations
	logger.Debug("Fetching rotations")
	rotations, err := database.GetRotations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch rotations: %w", err)
	}
	logger.Debug("Found rotations", zap.Int("count", len(rotations)))

	// Step 2: Find latest rota
	if len(rotations) == 0 {
		return nil, fmt.Errorf("no rotations found - please define a rota first")
	}

	latestRota := findLatestRotation(rotations)
	logger.Info("Found latest rota",
		zap.String("id", latestRota.ID),
		zap.String("start", latestRota.Start),
		zap.Int("shift_count", latestRota.ShiftCount))

	// Calculate shift dates for the rota
	shiftDates, err := calculateShiftDates(latestRota.Start, latestRota.ShiftCount)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate shift dates: %w", err)
	}

	// Step 3: Fetch all availability requests
	logger.Debug("Fetching availability requests")
	allRequests, err := database.GetAvailabilityRequests(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch availability requests: %w", err)
	}
	logger.Debug("Found availability requests", zap.Int("count", len(allRequests)))

	// Step 4: Filter to requests for the current rota
	requestsForRota := filterRequestsByRotaID(allRequests, latestRota.ID)
	logger.Info("Filtered requests for latest rota", zap.Int("count", len(requestsForRota)))

	// Build set of volunteer IDs who already have requests for this rota
	volunteerIDsWithRequests := make(map[string]bool)
	for _, req := range requestsForRota {
		volunteerIDsWithRequests[req.VolunteerID] = true
	}
	logger.Debug("Volunteers with existing requests", zap.Int("count", len(volunteerIDsWithRequests)))

	// Step 5: Fetch volunteers
	logger.Debug("Fetching volunteers")
	allVolunteers, err := volunteerClient.ListVolunteers(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch volunteers: %w", err)
	}
	logger.Debug("Found volunteers", zap.Int("count", len(allVolunteers)))

	// Step 6: Filter to active volunteers
	activeVolunteers := filterActiveVolunteers(allVolunteers)
	logger.Info("Filtered to active volunteers", zap.Int("count", len(activeVolunteers)))

	// Step 7: Find volunteers without availability requests
	volunteersWithoutRequests := filterVolunteersWithoutRequests(activeVolunteers, volunteerIDsWithRequests)
	logger.Info("Found volunteers without requests",
		zap.Int("count", len(volunteersWithoutRequests)),
		zap.Strings("volunteer_ids", getVolunteerIDs(volunteersWithoutRequests)))

	// Step 8: Create forms for volunteers without requests
	logger.Info("Creating forms for volunteers", zap.Int("count", len(volunteersWithoutRequests)))
	newRequests := make([]db.AvailabilityRequest, 0, len(volunteersWithoutRequests))

	for _, volunteer := range volunteersWithoutRequests {
		volunteerName := fmt.Sprintf("%s %s", volunteer.FirstName, volunteer.LastName)
		logger.Debug("Creating form for volunteer",
			zap.String("volunteer_id", volunteer.ID),
			zap.String("volunteer_name", volunteerName))

		// Create the form
		formResult, err := formsClient.CreateAvailabilityForm(volunteerName, latestRota.ID, shiftDates)
		if err != nil {
			return nil, fmt.Errorf("failed to create form for volunteer %s: %w", volunteer.ID, err)
		}

		logger.Info("Form created",
			zap.String("volunteer_id", volunteer.ID),
			zap.String("form_id", formResult.FormID),
			zap.String("form_url", formResult.ResponderURI))

		// Build availability request record
		requestID := uuid.New().String()
		newRequests = append(newRequests, db.AvailabilityRequest{
			ID:          requestID,
			RotaID:      latestRota.ID,
			ShiftDate:   latestRota.Start, // Use rota start date as representative date
			VolunteerID: volunteer.ID,
			FormID:      formResult.FormID,
			FormURL:     formResult.ResponderURI,
			FormSent:    false, // Not yet sent
		})
	}

	// Insert all new availability requests in batch
	if len(newRequests) > 0 {
		logger.Info("Inserting availability requests", zap.Int("count", len(newRequests)))
		if err := database.InsertAvailabilityRequests(newRequests); err != nil {
			return nil, fmt.Errorf("failed to insert availability requests: %w", err)
		}
		logger.Info("Availability requests inserted successfully")
	}

	// Step 9: Build list of unsent forms for the current rota
	// Combine existing requests with newly created ones
	allRequestsForRota := append(requestsForRota, newRequests...)

	unsentForms := []UnsentForm{}
	for _, req := range allRequestsForRota {
		if !req.FormSent {
			// Find volunteer name
			var volunteerName string
			for _, vol := range allVolunteers {
				if vol.ID == req.VolunteerID {
					volunteerName = fmt.Sprintf("%s %s", vol.FirstName, vol.LastName)
					break
				}
			}

			unsentForms = append(unsentForms, UnsentForm{
				VolunteerID:   req.VolunteerID,
				VolunteerName: volunteerName,
				FormURL:       req.FormURL,
			})
		}
	}

	logger.Info("Request availability completed",
		zap.Int("forms_created", len(volunteersWithoutRequests)),
		zap.Int("total_unsent_forms", len(unsentForms)))

	return &AvailabilityRequestResult{
		LatestRota:  latestRota,
		UnsentForms: unsentForms,
	}, nil
}

// filterVolunteersWithoutRequests filters volunteers to only those who don't have requests yet
func filterVolunteersWithoutRequests(volunteers []model.Volunteer, volunteerIDsWithRequests map[string]bool) []model.Volunteer {
	without := make([]model.Volunteer, 0)
	for _, vol := range volunteers {
		if !volunteerIDsWithRequests[vol.ID] {
			without = append(without, vol)
		}
	}
	return without
}
