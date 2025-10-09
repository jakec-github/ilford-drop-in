package services

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// mockFormsClientWithResponse implements FormsClientWithResponse for testing
type mockFormsClientWithResponse struct {
	formResponses map[string]bool // formID -> hasResponse
	err           error
}

func (m *mockFormsClientWithResponse) HasResponse(formID string) (bool, error) {
	if m.err != nil {
		return false, m.err
	}
	hasResponse, exists := m.formResponses[formID]
	if !exists {
		return false, nil // Default to no response if not specified
	}
	return hasResponse, nil
}

func TestSendAvailabilityReminders_SendsToActiveVolunteersWithoutResponses(t *testing.T) {
	mockStore := &mockAvailabilityRequestStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2024-01-01", ShiftCount: 10},
		},
		availabilityRequests: []db.AvailabilityRequest{
			{ID: "req-1", RotaID: "rota-1", VolunteerID: "vol-1", FormID: "form-1", FormURL: "https://forms.google.com/form-1", FormSent: true},
			{ID: "req-2", RotaID: "rota-1", VolunteerID: "vol-2", FormID: "form-2", FormURL: "https://forms.google.com/form-2", FormSent: true},
			{ID: "req-3", RotaID: "rota-1", VolunteerID: "vol-3", FormID: "form-3", FormURL: "https://forms.google.com/form-3", FormSent: true},
		},
	}
	mockVolunteerClient := &mockVolunteerClient{
		volunteers: []model.Volunteer{
			{ID: "vol-1", FirstName: "John", LastName: "Doe", Email: "john@example.com", Status: "Active"},
			{ID: "vol-2", FirstName: "Jane", LastName: "Smith", Email: "jane@example.com", Status: "Active"},
			{ID: "vol-3", FirstName: "Bob", LastName: "Jones", Email: "bob@example.com", Status: "Inactive"},
		},
	}
	mockFormsClient := &mockFormsClientWithResponse{
		formResponses: map[string]bool{
			"form-1": false, // vol-1 hasn't responded
			"form-2": true,  // vol-2 has responded
			"form-3": false, // vol-3 hasn't responded but is inactive
		},
	}
	mockGmailClient := &mockGmailClient{}
	logger := zap.NewNop()
	ctx := context.Background()
	cfg := &config.Config{}

	remindersSent, failedEmails, err := SendAvailabilityReminders(ctx, mockStore, mockVolunteerClient, mockFormsClient, mockGmailClient, cfg, logger, "2024-01-15")

	require.NoError(t, err)
	require.NotNil(t, remindersSent)
	require.NotNil(t, failedEmails)

	// Should have sent reminder only to vol-1 (active + no response)
	assert.Len(t, remindersSent, 1)
	assert.Equal(t, "vol-1", remindersSent[0].VolunteerID)
	assert.Equal(t, "john@example.com", remindersSent[0].Email)

	// No failed emails
	assert.Len(t, failedEmails, 0)

	// Verify email was sent
	assert.Len(t, mockGmailClient.sentEmails, 1)
	assert.Contains(t, mockGmailClient.sentEmails, "john@example.com")
}

func TestSendAvailabilityReminders_NoRemindersNeeded(t *testing.T) {
	mockStore := &mockAvailabilityRequestStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2024-01-01", ShiftCount: 10},
		},
		availabilityRequests: []db.AvailabilityRequest{
			{ID: "req-1", RotaID: "rota-1", VolunteerID: "vol-1", FormID: "form-1", FormURL: "https://forms.google.com/form-1", FormSent: true},
		},
	}
	mockVolunteerClient := &mockVolunteerClient{
		volunteers: []model.Volunteer{
			{ID: "vol-1", FirstName: "John", LastName: "Doe", Email: "john@example.com", Status: "Active"},
		},
	}
	mockFormsClient := &mockFormsClientWithResponse{
		formResponses: map[string]bool{
			"form-1": true, // vol-1 has already responded
		},
	}
	mockGmailClient := &mockGmailClient{}
	logger := zap.NewNop()
	ctx := context.Background()
	cfg := &config.Config{}

	remindersSent, failedEmails, err := SendAvailabilityReminders(ctx, mockStore, mockVolunteerClient, mockFormsClient, mockGmailClient, cfg, logger, "2024-01-15")

	require.NoError(t, err)
	assert.Len(t, remindersSent, 0)
	assert.Len(t, failedEmails, 0)
	assert.Len(t, mockGmailClient.sentEmails, 0)
}

func TestSendAvailabilityReminders_PartialEmailFailures(t *testing.T) {
	mockStore := &mockAvailabilityRequestStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2024-01-01", ShiftCount: 10},
		},
		availabilityRequests: []db.AvailabilityRequest{
			{ID: "req-1", RotaID: "rota-1", VolunteerID: "vol-1", FormID: "form-1", FormURL: "https://forms.google.com/form-1", FormSent: true},
			{ID: "req-2", RotaID: "rota-1", VolunteerID: "vol-2", FormID: "form-2", FormURL: "https://forms.google.com/form-2", FormSent: true},
		},
	}
	mockVolunteerClient := &mockVolunteerClient{
		volunteers: []model.Volunteer{
			{ID: "vol-1", FirstName: "John", LastName: "Doe", Email: "john@example.com", Status: "Active"},
			{ID: "vol-2", FirstName: "Jane", LastName: "Smith", Email: "jane@example.com", Status: "Active"},
		},
	}
	mockFormsClient := &mockFormsClientWithResponse{
		formResponses: map[string]bool{
			"form-1": false, // vol-1 hasn't responded
			"form-2": false, // vol-2 hasn't responded
		},
	}
	mockGmailClient := &mockGmailClient{
		failFor: []string{"jane@example.com"}, // Fail for vol-2
	}
	logger := zap.NewNop()
	ctx := context.Background()
	cfg := &config.Config{}

	remindersSent, failedEmails, err := SendAvailabilityReminders(ctx, mockStore, mockVolunteerClient, mockFormsClient, mockGmailClient, cfg, logger, "2024-01-15")

	require.NoError(t, err)

	// Should have sent reminder to vol-1 only
	require.Len(t, remindersSent, 1)
	assert.Equal(t, "vol-1", remindersSent[0].VolunteerID)

	// Should have failed email for vol-2
	require.Len(t, failedEmails, 1)
	assert.Equal(t, "vol-2", failedEmails[0].VolunteerID)
	assert.Equal(t, "jane@example.com", failedEmails[0].Email)
}

func TestSendAvailabilityReminders_AllEmailsFail(t *testing.T) {
	mockStore := &mockAvailabilityRequestStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2024-01-01", ShiftCount: 10},
		},
		availabilityRequests: []db.AvailabilityRequest{
			{ID: "req-1", RotaID: "rota-1", VolunteerID: "vol-1", FormID: "form-1", FormURL: "https://forms.google.com/form-1", FormSent: true},
		},
	}
	mockVolunteerClient := &mockVolunteerClient{
		volunteers: []model.Volunteer{
			{ID: "vol-1", FirstName: "John", LastName: "Doe", Email: "john@example.com", Status: "Active"},
		},
	}
	mockFormsClient := &mockFormsClientWithResponse{
		formResponses: map[string]bool{
			"form-1": false, // vol-1 hasn't responded
		},
	}
	mockGmailClient := &mockGmailClient{
		err: fmt.Errorf("gmail service unavailable"),
	}
	logger := zap.NewNop()
	ctx := context.Background()
	cfg := &config.Config{}

	remindersSent, failedEmails, err := SendAvailabilityReminders(ctx, mockStore, mockVolunteerClient, mockFormsClient, mockGmailClient, cfg, logger, "2024-01-15")

	require.Error(t, err)
	assert.Nil(t, remindersSent)
	assert.Nil(t, failedEmails)
	assert.Contains(t, err.Error(), "all 1 reminder email send attempts failed")
}

func TestSendAvailabilityReminders_NoRequestsSent(t *testing.T) {
	mockStore := &mockAvailabilityRequestStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2024-01-01", ShiftCount: 10},
		},
		availabilityRequests: []db.AvailabilityRequest{
			{ID: "req-1", RotaID: "rota-1", VolunteerID: "vol-1", FormID: "form-1", FormURL: "https://forms.google.com/form-1", FormSent: false}, // Not sent
		},
	}
	mockVolunteerClient := &mockVolunteerClient{
		volunteers: []model.Volunteer{
			{ID: "vol-1", FirstName: "John", LastName: "Doe", Email: "john@example.com", Status: "Active"},
		},
	}
	mockFormsClient := &mockFormsClientWithResponse{}
	mockGmailClient := &mockGmailClient{}
	logger := zap.NewNop()
	ctx := context.Background()
	cfg := &config.Config{}

	remindersSent, failedEmails, err := SendAvailabilityReminders(ctx, mockStore, mockVolunteerClient, mockFormsClient, mockGmailClient, cfg, logger, "2024-01-15")

	require.NoError(t, err)
	assert.Len(t, remindersSent, 0)
	assert.Len(t, failedEmails, 0)
}

// Helper function tests
func TestFilterSentRequestsByRotaID(t *testing.T) {
	requests := []db.AvailabilityRequest{
		{ID: "req-1", RotaID: "rota-1", FormSent: true},
		{ID: "req-2", RotaID: "rota-1", FormSent: false}, // Not sent
		{ID: "req-3", RotaID: "rota-2", FormSent: true},  // Different rota
		{ID: "req-4", RotaID: "rota-1", FormSent: true},
	}

	filtered := filterSentRequestsByRotaID(requests, "rota-1")

	require.Len(t, filtered, 2)
	assert.Equal(t, "req-1", filtered[0].ID)
	assert.Equal(t, "req-4", filtered[1].ID)
}
