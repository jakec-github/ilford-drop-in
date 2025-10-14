package services

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// ReminderSent represents a volunteer who was successfully sent a reminder
type ReminderSent struct {
	VolunteerID   string
	VolunteerName string
	Email         string
	FormURL       string
}

// AvailabilityRemindersStore defines the database operations needed for sending reminders
type AvailabilityRemindersStore interface {
	GetRotations(ctx context.Context) ([]db.Rotation, error)
	GetAvailabilityRequests(ctx context.Context) ([]db.AvailabilityRequest, error)
}

// FormsClientWithResponse defines the operations needed to check form responses
type FormsClientWithResponse interface {
	HasResponse(formID string) (bool, error)
}

// SendAvailabilityReminders sends reminder emails to volunteers who haven't responded to availability forms
// Returns volunteers who were sent reminders and those where sending failed
func SendAvailabilityReminders(
	ctx context.Context,
	database AvailabilityRemindersStore,
	volunteerClient VolunteerClient,
	formsClient FormsClientWithResponse,
	gmailClient GmailClient,
	cfg *config.Config,
	logger *zap.Logger,
	deadline string,
) ([]ReminderSent, []FailedEmail, error) {
	logger.Debug("Starting sendAvailabilityReminders", zap.String("deadline", deadline))

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

	latestRota := findLatestRotation(rotations)
	logger.Debug("Found latest rota",
		zap.String("id", latestRota.ID),
		zap.String("start", latestRota.Start),
		zap.Int("shift_count", latestRota.ShiftCount))

	// Step 3: Fetch all availability requests
	logger.Debug("Fetching availability requests")
	allRequests, err := database.GetAvailabilityRequests(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch availability requests: %w", err)
	}
	logger.Debug("Found availability requests", zap.Int("count", len(allRequests)))

	// Step 4: Filter to requests for the current rota that were sent
	requestsForRota := filterSentRequestsByRotaID(allRequests, latestRota.ID)
	logger.Debug("Filtered sent requests for latest rota", zap.Int("count", len(requestsForRota)))

	if len(requestsForRota) == 0 {
		logger.Info("No availability requests sent for latest rota")
		return []ReminderSent{}, []FailedEmail{}, nil
	}

	// Step 5: Fetch volunteers
	logger.Debug("Fetching volunteers")
	allVolunteers, err := volunteerClient.ListVolunteers(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch volunteers: %w", err)
	}
	logger.Debug("Found volunteers", zap.Int("count", len(allVolunteers)))

	// Build map of volunteers by ID for quick lookup
	volunteersByID := make(map[string]model.Volunteer)
	for _, vol := range allVolunteers {
		volunteersByID[vol.ID] = vol
	}

	// Step 6: Build a map of groups that have at least one response
	groupsWithResponses := make(map[string]bool)
	for _, req := range requestsForRota {
		volunteer, exists := volunteersByID[req.VolunteerID]
		if !exists {
			continue
		}

		// Check if this volunteer's form has a response
		hasResponse, err := formsClient.HasResponse(req.FormID)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to check response for form %s: %w", req.FormID, err)
		}

		if hasResponse && volunteer.GroupKey != "" {
			groupsWithResponses[volunteer.GroupKey] = true
		}
	}

	logger.Debug("Groups with responses", zap.Int("count", len(groupsWithResponses)))

	// Step 7: Identify volunteers who need reminders (active + no response + group hasn't responded)
	volunteersNeedingReminders := []db.AvailabilityRequest{}
	for _, req := range requestsForRota {
		volunteer, exists := volunteersByID[req.VolunteerID]
		if !exists {
			logger.Warn("Volunteer not found in list",
				zap.String("volunteer_id", req.VolunteerID))
			continue
		}

		// Skip inactive volunteers
		if volunteer.Status != "Active" {
			logger.Debug("Skipping inactive volunteer",
				zap.String("volunteer_id", req.VolunteerID),
				zap.String("status", volunteer.Status))
			continue
		}

		// Skip if volunteer's group has already responded
		if volunteer.GroupKey != "" && groupsWithResponses[volunteer.GroupKey] {
			logger.Debug("Skipping volunteer - group member already responded",
				zap.String("volunteer_id", req.VolunteerID),
				zap.String("group_key", volunteer.GroupKey))
			continue
		}

		// Check if form has responses
		hasResponse, err := formsClient.HasResponse(req.FormID)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to check response for form %s: %w", req.FormID, err)
		}

		if !hasResponse {
			volunteersNeedingReminders = append(volunteersNeedingReminders, req)
		}
	}

	logger.Debug("Found volunteers needing reminders", zap.Int("count", len(volunteersNeedingReminders)))

	if len(volunteersNeedingReminders) == 0 {
		logger.Info("No volunteers need reminders")
		return []ReminderSent{}, []FailedEmail{}, nil
	}

	// Step 8: Send reminder emails
	remindersSent := []ReminderSent{}
	failedEmails := []FailedEmail{}

	for _, req := range volunteersNeedingReminders {
		volunteer := volunteersByID[req.VolunteerID]
		volunteerName := fmt.Sprintf("%s %s", volunteer.FirstName, volunteer.LastName)

		// Send reminder email with form link
		subject := fmt.Sprintf("Reminder: Ilford drop-in availability (please complete by %s)", deadline)
		body := fmt.Sprintf("Hey %s\n\nThis is a reminder to please complete your availability form if you haven't already.\n%s\n\nDeadline for responses is %s when we will create the rota.\nYou can change your response as many times as you like before the deadline.\n\nThanks\nThe Ilford drop-in team\n",
			volunteer.FirstName, req.FormURL, deadline)

		logger.Info("Sending reminder email",
			zap.String("volunteer_id", volunteer.ID),
			zap.String("email", volunteer.Email))

		if err := gmailClient.SendEmail(volunteer.Email, subject, body); err != nil {
			logger.Warn("Failed to send reminder email",
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

		logger.Debug("Reminder email sent successfully",
			zap.String("volunteer_id", volunteer.ID),
			zap.String("email", volunteer.Email))

		remindersSent = append(remindersSent, ReminderSent{
			VolunteerID:   volunteer.ID,
			VolunteerName: volunteerName,
			Email:         volunteer.Email,
			FormURL:       req.FormURL,
		})
	}

	// If all emails failed, return error
	if len(failedEmails) == len(volunteersNeedingReminders) {
		return nil, nil, fmt.Errorf("all %d reminder email send attempts failed", len(failedEmails))
	}

	logger.Debug("Send availability reminders completed",
		zap.Int("reminders_sent", len(remindersSent)),
		zap.Int("reminders_failed", len(failedEmails)))

	return remindersSent, failedEmails, nil
}

// filterSentRequestsByRotaID filters availability requests to only those for a specific rota that were sent
func filterSentRequestsByRotaID(requests []db.AvailabilityRequest, rotaID string) []db.AvailabilityRequest {
	filtered := []db.AvailabilityRequest{}
	for _, req := range requests {
		if req.RotaID == rotaID && req.FormSent {
			filtered = append(filtered, req)
		}
	}
	return filtered
}
