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

// FailedEmail represents an email that failed to send
type FailedEmail struct {
	VolunteerID   string
	VolunteerName string
	Email         string
	Error         string
}

// AvailabilityRequestResult represents the result of requesting availability
type AvailabilityRequestResult struct {
	LatestRota   *db.Rotation
	UnsentForms  []UnsentForm
	FailedEmails []FailedEmail
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

// GmailClient defines the operations needed to send emails
type GmailClient interface {
	SendEmail(to, subject, body string) error
}

// RequestAvailability creates availability forms for volunteers, sends emails, and returns results
// It fetches the latest rota, creates forms for volunteers without requests, sends emails,
// inserts DB records, and returns all unsent forms and any failed emails
func RequestAvailability(
	ctx context.Context,
	database AvailabilityRequestStore,
	volunteerClient VolunteerClient,
	formsClient FormsClient,
	gmailClient GmailClient,
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
	unsentRequests := make([]db.AvailabilityRequest, 0, len(volunteersWithoutRequests))

	// Map to track form details by volunteer ID for email sending
	type formDetails struct {
		requestID string
		formURL   string
	}
	formsByVolunteer := make(map[string]formDetails)

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

		// Build availability request record with form_sent=false
		requestID := uuid.New().String()
		unsentRequests = append(unsentRequests, db.AvailabilityRequest{
			ID:          requestID,
			RotaID:      latestRota.ID,
			ShiftDate:   latestRota.Start,
			VolunteerID: volunteer.ID,
			FormID:      formResult.FormID,
			FormURL:     formResult.ResponderURI,
			FormSent:    false,
		})

		formsByVolunteer[volunteer.ID] = formDetails{
			requestID: requestID,
			formURL:   formResult.ResponderURI,
		}
	}

	// Insert all unsent availability requests
	if len(unsentRequests) > 0 {
		logger.Info("Inserting unsent availability requests", zap.Int("count", len(unsentRequests)))
		if err := database.InsertAvailabilityRequests(unsentRequests); err != nil {
			return nil, fmt.Errorf("failed to insert availability requests: %w", err)
		}
		logger.Info("Unsent availability requests inserted successfully")
	}

	// Step 9: Send emails and create form_sent=true records for successful sends
	sentRequests := make([]db.AvailabilityRequest, 0, len(volunteersWithoutRequests))
	failedEmails := []FailedEmail{}

	for _, volunteer := range volunteersWithoutRequests {
		volunteerName := fmt.Sprintf("%s %s", volunteer.FirstName, volunteer.LastName)
		formInfo := formsByVolunteer[volunteer.ID]

		// Send email with form link
		subject := fmt.Sprintf("Ilford drop-in availability (please complete by %s)", deadline)
		body := fmt.Sprintf("Hey %s\n\nPlease use this form to let us know your availability.\n%s:\n\nDeadline for responses is %s when we will create the rota.\nYou can change your response as many times as you like before the deadline.\n\nThanks\nThe Ilford drop-in team\n",
			volunteer.FirstName, formInfo.formURL, deadline)

		logger.Debug("Sending email",
			zap.String("volunteer_id", volunteer.ID),
			zap.String("email", volunteer.Email))

		if err := gmailClient.SendEmail(volunteer.Email, subject, body); err != nil {
			logger.Warn("Failed to send email",
				zap.String("volunteer_id", volunteer.ID),
				zap.String("email", volunteer.Email),
				zap.Error(err))

			failedEmails = append(failedEmails, FailedEmail{
				VolunteerID:   volunteer.ID,
				VolunteerName: volunteerName,
				Email:         volunteer.Email,
				Error:         err.Error(),
			})
			continue
		}

		logger.Info("Email sent successfully",
			zap.String("volunteer_id", volunteer.ID),
			zap.String("email", volunteer.Email))

		// Create form_sent=true record for successful email
		// Find the original request to get the FormID
		var originalFormID string
		for _, req := range unsentRequests {
			if req.VolunteerID == volunteer.ID {
				originalFormID = req.FormID
				break
			}
		}

		sentRequests = append(sentRequests, db.AvailabilityRequest{
			ID:          formInfo.requestID, // Same ID as unsent record
			RotaID:      latestRota.ID,
			ShiftDate:   latestRota.Start,
			VolunteerID: volunteer.ID,
			FormID:      originalFormID,
			FormURL:     formInfo.formURL,
			FormSent:    true,
		})
	}

	// If all emails failed, return error
	if len(failedEmails) == len(volunteersWithoutRequests) && len(volunteersWithoutRequests) > 0 {
		return nil, fmt.Errorf("all %d email send attempts failed", len(failedEmails))
	}

	// Insert form_sent=true records for successful emails
	if len(sentRequests) > 0 {
		logger.Info("Inserting sent availability requests", zap.Int("count", len(sentRequests)))
		if err := database.InsertAvailabilityRequests(sentRequests); err != nil {
			return nil, fmt.Errorf("failed to insert sent availability requests: %w", err)
		}
		logger.Info("Sent availability requests inserted successfully")
	}

	// Step 10: Build list of unsent forms for the current rota
	// Combine existing requests with newly created unsent ones (not the sent ones)
	allRequestsForRota := append(requestsForRota, unsentRequests...)

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
		zap.Int("emails_sent", len(sentRequests)),
		zap.Int("emails_failed", len(failedEmails)),
		zap.Int("total_unsent_forms", len(unsentForms)))

	return &AvailabilityRequestResult{
		LatestRota:   latestRota,
		UnsentForms:  unsentForms,
		FailedEmails: failedEmails,
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
