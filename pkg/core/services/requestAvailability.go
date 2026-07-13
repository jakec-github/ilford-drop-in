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
	"github.com/jakechorley/ilford-drop-in/pkg/core/services/utils"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// SentForm represents a volunteer who was successfully sent a form
type SentForm struct {
	VolunteerID   string
	VolunteerName string
	Email         string
}

// FailedEmail represents an email that failed to send
type FailedEmail struct {
	VolunteerID   string
	VolunteerName string
	Email         string
	Error         string
}

// AvailabilityRequestStore defines the database operations needed for request availability
type AvailabilityRequestStore interface {
	GetRotations(ctx context.Context) ([]db.Rotation, error)
	GetShiftsByRotaID(ctx context.Context, rotaID string) ([]db.Shift, error)
	GetAvailabilityRequestsByRotaID(ctx context.Context, rotaID string) ([]db.AvailabilityRequest, error)
	InsertAvailabilityRequests(ctx context.Context, requests []db.AvailabilityRequest) error
	MarkAvailabilityRequestsSent(ctx context.Context, ids []string) error
}

// VolunteerClient defines the operations needed to fetch volunteers
type VolunteerClient interface {
	ListVolunteers(cfg *config.Config) ([]model.Volunteer, error)
}

// FormsClient defines the operations needed to create forms
type FormsClient interface {
	CreateAvailabilityForm(volunteerName string, shiftDates []time.Time) (*formsclient.AvailabilityFormResult, error)
}

// GmailClient defines the operations needed to send emails
type GmailClient interface {
	SendEmail(to, subject, body string) error
}

// RequestAvailability creates availability forms for volunteers, sends emails, and returns results
// It fetches the latest rota, identifies volunteers without form_sent=true requests, creates forms
// for those who need them (or reuses existing unsent forms), sends emails, and inserts DB records.
// Returns volunteers who were successfully sent forms and those that failed.
// If noEmail is true, forms are created but emails are not sent and records are not marked as sent.
func RequestAvailability(
	ctx context.Context,
	database AvailabilityRequestStore,
	volunteerClient VolunteerClient,
	formsClient FormsClient,
	gmailClient GmailClient,
	cfg *config.Config,
	logger *zap.Logger,
	deadline string,
	noEmail bool,
) ([]SentForm, []FailedEmail, error) {
	logger.Debug("Starting requestAvailability", zap.String("deadline", deadline))

	// Step 1: Fetch all rotations
	logger.Debug("Fetching rotations")
	rotations, err := database.GetRotations(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch rotations: %w", err)
	}
	logger.Debug("Found rotations", zap.Int("count", len(rotations)))

	// Step 2: Find latest rota
	if len(rotations) == 0 {
		return nil, nil, fmt.Errorf("no rotations found - please define a rota first")
	}

	latestRota := utils.FindLatestRotation(rotations)
	logger.Debug("Found latest rota",
		zap.String("id", latestRota.ID),
		zap.String("start", latestRota.Start),
		zap.Int("shift_count", latestRota.ShiftCount))

	// Read the rota's shift dates from the shift table (ADR 0001)
	shiftDates, err := rotaShiftDates(ctx, database, latestRota.ID)
	if err != nil {
		return nil, nil, err
	}

	// Step 3: Fetch the availability requests for the current rota
	logger.Debug("Fetching availability requests")
	requestsForRota, err := database.GetAvailabilityRequestsByRotaID(ctx, latestRota.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch availability requests: %w", err)
	}
	logger.Debug("Found availability requests for latest rota", zap.Int("count", len(requestsForRota)))

	// Build set of volunteer IDs who already have SENT requests for this rota
	volunteerIDsWithSentRequests := make(map[string]bool)
	unsentRequestsByVolunteer := make(map[string]db.AvailabilityRequest)
	for _, req := range requestsForRota {
		if req.FormSent {
			volunteerIDsWithSentRequests[req.VolunteerID] = true
		} else {
			// Track unsent requests so we can reuse the form URL
			unsentRequestsByVolunteer[req.VolunteerID] = req
		}
	}
	logger.Debug("Volunteers with sent requests", zap.Int("count", len(volunteerIDsWithSentRequests)))
	logger.Debug("Volunteers with unsent requests", zap.Int("count", len(unsentRequestsByVolunteer)))

	// Step 5: Fetch volunteers
	logger.Debug("Fetching volunteers")
	allVolunteers, err := volunteerClient.ListVolunteers(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch volunteers: %w", err)
	}
	logger.Debug("Found volunteers", zap.Int("count", len(allVolunteers)))

	// Step 6: Filter to active volunteers
	activeVolunteers := utils.FilterActiveVolunteers(allVolunteers)
	logger.Debug("Filtered to active volunteers", zap.Int("count", len(activeVolunteers)))

	// Step 7: Find volunteers without SENT availability requests
	volunteersNeedingEmails := filterVolunteersWithoutSentRequests(activeVolunteers, volunteerIDsWithSentRequests)
	logger.Debug("Found volunteers needing emails (no sent requests)",
		zap.Int("count", len(volunteersNeedingEmails)),
		zap.Strings("volunteer_ids", utils.GetVolunteerIDs(volunteersNeedingEmails)))

	// Step 8: Create forms for volunteers who need them (those without unsent requests)
	logger.Debug("Processing volunteers needing emails", zap.Int("count", len(volunteersNeedingEmails)))
	unsentRequests := make([]db.AvailabilityRequest, 0)

	// Map to track form details by volunteer ID for email sending
	type formDetails struct {
		requestID string
		formURL   string
		formID    string
	}
	formsByVolunteer := make(map[string]formDetails)

	for _, volunteer := range volunteersNeedingEmails {
		volunteerName := fmt.Sprintf("%s %s", volunteer.FirstName, volunteer.LastName)

		// Check if this volunteer has an existing unsent request
		if unsentReq, exists := unsentRequestsByVolunteer[volunteer.ID]; exists {
			// Reuse the existing form
			logger.Debug("Reusing existing form for volunteer",
				zap.String("volunteer_id", volunteer.ID),
				zap.String("volunteer_name", volunteerName),
				zap.String("existing_form_id", unsentReq.FormID))

			formsByVolunteer[volunteer.ID] = formDetails{
				requestID: unsentReq.ID,
				formURL:   unsentReq.FormURL,
				formID:    unsentReq.FormID,
			}
		} else {
			// Create a new form
			logger.Debug("Creating new form for volunteer",
				zap.String("volunteer_id", volunteer.ID),
				zap.String("volunteer_name", volunteerName))

			formResult, err := formsClient.CreateAvailabilityForm(volunteerName, shiftDates)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to create form for volunteer %s: %w", volunteer.ID, err)
			}

			logger.Debug("Form created",
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
				formID:    formResult.FormID,
			}
		}
	}

	// Insert all unsent availability requests
	if len(unsentRequests) > 0 {
		logger.Debug("Inserting unsent availability requests", zap.Int("count", len(unsentRequests)))
		if err := database.InsertAvailabilityRequests(ctx, unsentRequests); err != nil {
			return nil, nil, fmt.Errorf("failed to insert availability requests: %w", err)
		}
		logger.Debug("Unsent availability requests inserted successfully")
	}

	// If noEmail flag is set, exit early without sending emails or marking as sent
	if noEmail {
		logger.Info("No-email mode: forms created but emails not sent",
			zap.Int("forms_created", len(unsentRequests)),
			zap.Int("forms_reused", len(volunteersNeedingEmails)-len(unsentRequests)))
		return []SentForm{}, []FailedEmail{}, nil
	}

	// Step 9: Send emails and mark requests as sent for successful sends
	sentRequestIDs := make([]string, 0, len(volunteersNeedingEmails))
	sentForms := []SentForm{}
	failedEmails := []FailedEmail{}

	for _, volunteer := range volunteersNeedingEmails {
		displayName := volunteer.DisplayName
		formInfo := formsByVolunteer[volunteer.ID]

		subject := AvailabilityEmailSubject(deadline)
		body := AvailabilityEmailBody(volunteer.FirstName, formInfo.formURL, deadline)

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
				VolunteerName: displayName,
				Email:         volunteer.Email,
				Error:         err.Error(),
			})
			continue
		}

		logger.Info("Email sent successfully",
			zap.String("volunteer_id", volunteer.ID),
			zap.String("email", volunteer.Email))

		// Add to sent forms list
		sentForms = append(sentForms, SentForm{
			VolunteerID:   volunteer.ID,
			VolunteerName: displayName,
			Email:         volunteer.Email,
		})

		// Mark the request as sent for successful email
		sentRequestIDs = append(sentRequestIDs, formInfo.requestID)
	}

	// If all emails failed, return error
	if len(failedEmails) == len(volunteersNeedingEmails) && len(volunteersNeedingEmails) > 0 {
		return nil, nil, fmt.Errorf("all %d email send attempts failed", len(failedEmails))
	}

	// Mark requests whose email succeeded as sent
	if len(sentRequestIDs) > 0 {
		logger.Debug("Marking availability requests as sent", zap.Int("count", len(sentRequestIDs)))
		if err := database.MarkAvailabilityRequestsSent(ctx, sentRequestIDs); err != nil {
			return nil, nil, fmt.Errorf("failed to mark availability requests as sent: %w", err)
		}
		logger.Debug("Availability requests marked as sent")
	}

	logger.Debug("Request availability completed",
		zap.Int("volunteers_processed", len(volunteersNeedingEmails)),
		zap.Int("emails_sent", len(sentForms)),
		zap.Int("emails_failed", len(failedEmails)))

	return sentForms, failedEmails, nil
}

// AvailabilityEmailSubject returns the subject line for an availability request email.
func AvailabilityEmailSubject(deadline string) string {
	return fmt.Sprintf("Ilford drop-in availability (please complete by %s)", deadline)
}

// AvailabilityEmailBody returns the body for an availability request email.
func AvailabilityEmailBody(firstName, formURL, deadline string) string {
	return fmt.Sprintf("Hey %s\n\nPlease use this form to let us know your availability.\n%s\n\nDeadline for responses is %s when we will create the rota.\nYou can change your response as many times as you like before the deadline.\n\nThanks\nThe Ilford drop-in team\n",
		firstName, formURL, deadline)
}

// filterVolunteersWithoutSentRequests filters volunteers to only those who don't have sent requests yet
func filterVolunteersWithoutSentRequests(volunteers []model.Volunteer, volunteerIDsWithSentRequests map[string]bool) []model.Volunteer {
	without := make([]model.Volunteer, 0)
	for _, vol := range volunteers {
		if !volunteerIDsWithSentRequests[vol.ID] {
			without = append(without, vol)
		}
	}
	return without
}
