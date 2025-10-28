package services

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/clients/formsclient"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// mockAvailabilityRequestStore implements AvailabilityRequestStore for testing
type mockAvailabilityRequestStore struct {
	rotations            []db.Rotation
	availabilityRequests []db.AvailabilityRequest
	insertedRequests     []db.AvailabilityRequest
	getRotationsErr      error
	getRequestsErr       error
	insertErr            error
}

func (m *mockAvailabilityRequestStore) GetRotations(ctx context.Context) ([]db.Rotation, error) {
	if m.getRotationsErr != nil {
		return nil, m.getRotationsErr
	}
	return m.rotations, nil
}

func (m *mockAvailabilityRequestStore) GetAvailabilityRequests(ctx context.Context) ([]db.AvailabilityRequest, error) {
	if m.getRequestsErr != nil {
		return nil, m.getRequestsErr
	}
	return m.availabilityRequests, nil
}

func (m *mockAvailabilityRequestStore) InsertAvailabilityRequests(requests []db.AvailabilityRequest) error {
	if m.insertErr != nil {
		return m.insertErr
	}
	m.insertedRequests = append(m.insertedRequests, requests...)
	return nil
}

// mockVolunteerClient implements VolunteerClient for testing
type mockVolunteerClient struct {
	volunteers []model.Volunteer
	err        error
}

func (m *mockVolunteerClient) ListVolunteers(cfg *config.Config) ([]model.Volunteer, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.volunteers, nil
}

// mockFormsClient implements FormsClient for testing
type mockFormsClient struct {
	createdForms []string // Track volunteer IDs for which forms were created
	err          error
}

func (m *mockFormsClient) CreateAvailabilityForm(volunteerName string, shiftDates []time.Time) (*formsclient.AvailabilityFormResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	// Extract volunteer ID from name for tracking
	formID := "form-" + volunteerName
	m.createdForms = append(m.createdForms, volunteerName)
	return &formsclient.AvailabilityFormResult{
		FormID:       formID,
		ResponderURI: "https://forms.google.com/" + formID,
	}, nil
}

// mockGmailClient implements GmailClient for testing
type mockGmailClient struct {
	sentEmails []string // Track email addresses sent to
	failFor    []string // List of emails that should fail
	err        error
}

func (m *mockGmailClient) SendEmail(to, subject, body string) error {
	if m.err != nil {
		return m.err
	}
	// Check if this email should fail
	for _, failEmail := range m.failFor {
		if to == failEmail {
			return fmt.Errorf("mock error: failed to send to %s", to)
		}
	}
	m.sentEmails = append(m.sentEmails, to)
	return nil
}

func TestRequestAvailability_CreatesRequestsForVolunteersWithoutRequests(t *testing.T) {
	mockStore := &mockAvailabilityRequestStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2024-01-01", ShiftCount: 10},
		},
		availabilityRequests: []db.AvailabilityRequest{
			// vol-1 already has an unsent request (FormSent=false)
			{ID: "req-1", RotaID: "rota-1", VolunteerID: "vol-1", FormID: "existing-form-1", FormURL: "https://forms.google.com/existing-form-1", FormSent: false},
		},
	}
	mockVolunteerClient := &mockVolunteerClient{
		volunteers: []model.Volunteer{
			{ID: "vol-1", FirstName: "John", LastName: "Doe", Email: "john@example.com", Status: "Active"},
			{ID: "vol-2", FirstName: "Jane", LastName: "Smith", Email: "jane@example.com", Status: "Active"},
			{ID: "vol-3", FirstName: "Bob", LastName: "Jones", Email: "bob@example.com", Status: "Active"},
		},
	}
	mockFormsClient := &mockFormsClient{}
	mockGmailClient := &mockGmailClient{}
	logger := zap.NewNop()
	ctx := context.Background()
	cfg := &config.Config{}

	sentForms, failedEmails, err := RequestAvailability(ctx, mockStore, mockVolunteerClient, mockFormsClient, mockGmailClient, cfg, logger, "2024-01-15", false)

	require.NoError(t, err)
	require.NotNil(t, sentForms)
	require.NotNil(t, failedEmails)

	// Should have created 5 new requests:
	// - 2 unsent (vol-2, vol-3) - vol-1 already has unsent
	// - 3 sent (vol-1, vol-2, vol-3) - all get emails
	require.Len(t, mockStore.insertedRequests, 5)

	// Count requests by volunteer and form_sent status
	unsentByVolunteer := make(map[string]bool)
	sentByVolunteer := make(map[string]bool)
	for _, req := range mockStore.insertedRequests {
		assert.Equal(t, "rota-1", req.RotaID)
		assert.Equal(t, "2024-01-01", req.ShiftDate)
		assert.NotEmpty(t, req.ID)
		assert.NotEmpty(t, req.FormID)
		assert.NotEmpty(t, req.FormURL)

		if req.FormSent {
			sentByVolunteer[req.VolunteerID] = true
		} else {
			unsentByVolunteer[req.VolunteerID] = true
		}
	}

	// vol-2 and vol-3 should have both unsent and sent records
	assert.True(t, unsentByVolunteer["vol-2"], "Should have unsent request for vol-2")
	assert.True(t, sentByVolunteer["vol-2"], "Should have sent request for vol-2")
	assert.True(t, unsentByVolunteer["vol-3"], "Should have unsent request for vol-3")
	assert.True(t, sentByVolunteer["vol-3"], "Should have sent request for vol-3")

	// vol-1 should only have sent record (unsent already existed)
	assert.False(t, unsentByVolunteer["vol-1"], "Should not have created unsent request for vol-1 (already exists)")
	assert.True(t, sentByVolunteer["vol-1"], "Should have sent request for vol-1 (email was sent)")

	// Verify forms were created only for vol-2 and vol-3 (not vol-1, reused existing)
	assert.Len(t, mockFormsClient.createdForms, 2)

	// Verify emails were sent to all three volunteers
	assert.Len(t, mockGmailClient.sentEmails, 3)
	assert.Contains(t, mockGmailClient.sentEmails, "john@example.com")
	assert.Contains(t, mockGmailClient.sentEmails, "jane@example.com")
	assert.Contains(t, mockGmailClient.sentEmails, "bob@example.com")

	// Verify sent forms for all three
	assert.Len(t, sentForms, 3)

	// Verify no failed emails
	assert.Len(t, failedEmails, 0)
}

func TestRequestAvailability_ResendsForUnsentRequests(t *testing.T) {
	mockStore := &mockAvailabilityRequestStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2024-01-01", ShiftCount: 10},
		},
		availabilityRequests: []db.AvailabilityRequest{
			// vol-1 already has sent request - should be skipped
			{ID: "req-1", RotaID: "rota-1", VolunteerID: "vol-1", FormID: "form-1", FormURL: "https://forms.google.com/form-1", FormSent: true},
			// vol-2 has unsent request - should get email with existing form
			{ID: "req-2", RotaID: "rota-1", VolunteerID: "vol-2", FormID: "form-2", FormURL: "https://forms.google.com/form-2", FormSent: false},
		},
	}
	mockVolunteerClient := &mockVolunteerClient{
		volunteers: []model.Volunteer{
			{ID: "vol-1", FirstName: "John", LastName: "Doe", Email: "john@example.com", Status: "Active"},
			{ID: "vol-2", FirstName: "Jane", LastName: "Smith", Email: "jane@example.com", Status: "Active"},
		},
	}
	mockFormsClient := &mockFormsClient{}
	mockGmailClient := &mockGmailClient{}
	logger := zap.NewNop()
	ctx := context.Background()
	cfg := &config.Config{}

	sentForms, failedEmails, err := RequestAvailability(ctx, mockStore, mockVolunteerClient, mockFormsClient, mockGmailClient, cfg, logger, "2024-01-15", false)

	require.NoError(t, err)
	require.NotNil(t, sentForms)
	require.NotNil(t, failedEmails)

	// Should have created 1 sent request for vol-2
	require.Len(t, mockStore.insertedRequests, 1)
	assert.Equal(t, "vol-2", mockStore.insertedRequests[0].VolunteerID)
	assert.True(t, mockStore.insertedRequests[0].FormSent)
	assert.Equal(t, "req-2", mockStore.insertedRequests[0].ID) // Should reuse existing request ID

	// No new forms created (reused existing)
	assert.Len(t, mockFormsClient.createdForms, 0)

	// One email sent to vol-2
	assert.Len(t, mockGmailClient.sentEmails, 1)
	assert.Contains(t, mockGmailClient.sentEmails, "jane@example.com")

	// One sent form
	assert.Len(t, sentForms, 1)
	assert.Equal(t, "vol-2", sentForms[0].VolunteerID)

	// No failed emails
	assert.Len(t, failedEmails, 0)
}

func TestRequestAvailability_NoVolunteersNeedEmails(t *testing.T) {
	mockStore := &mockAvailabilityRequestStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2024-01-01", ShiftCount: 10},
		},
		availabilityRequests: []db.AvailabilityRequest{
			// All volunteers have sent requests
			{ID: "req-1", RotaID: "rota-1", VolunteerID: "vol-1", FormSent: true},
			{ID: "req-2", RotaID: "rota-1", VolunteerID: "vol-2", FormSent: true},
		},
	}
	mockVolunteerClient := &mockVolunteerClient{
		volunteers: []model.Volunteer{
			{ID: "vol-1", FirstName: "John", LastName: "Doe", Email: "john@example.com", Status: "Active"},
			{ID: "vol-2", FirstName: "Jane", LastName: "Smith", Email: "jane@example.com", Status: "Active"},
		},
	}
	mockFormsClient := &mockFormsClient{}
	mockGmailClient := &mockGmailClient{}
	logger := zap.NewNop()
	ctx := context.Background()
	cfg := &config.Config{}

	sentForms, failedEmails, err := RequestAvailability(ctx, mockStore, mockVolunteerClient, mockFormsClient, mockGmailClient, cfg, logger, "2024-01-15", false)

	require.NoError(t, err)
	require.NotNil(t, sentForms)
	require.NotNil(t, failedEmails)

	// Should not have created any new requests
	assert.Len(t, mockStore.insertedRequests, 0)
	assert.Len(t, mockFormsClient.createdForms, 0)
	assert.Len(t, mockGmailClient.sentEmails, 0)

	// No sent forms
	assert.Len(t, sentForms, 0)

	// No failed emails
	assert.Len(t, failedEmails, 0)
}

func TestRequestAvailability_OnlyCreatesForLatestRota(t *testing.T) {
	mockStore := &mockAvailabilityRequestStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2024-01-01", ShiftCount: 10},
			{ID: "rota-2", Start: "2024-04-01", ShiftCount: 12}, // Latest
		},
		availabilityRequests: []db.AvailabilityRequest{
			// vol-1 has request for old rota only
			{ID: "req-1", RotaID: "rota-1", VolunteerID: "vol-1", FormSent: true},
		},
	}
	mockVolunteerClient := &mockVolunteerClient{
		volunteers: []model.Volunteer{
			{ID: "vol-1", FirstName: "John", LastName: "Doe", Email: "john@example.com", Status: "Active"},
		},
	}
	mockFormsClient := &mockFormsClient{}
	mockGmailClient := &mockGmailClient{}
	logger := zap.NewNop()
	ctx := context.Background()
	cfg := &config.Config{}

	sentForms, failedEmails, err := RequestAvailability(ctx, mockStore, mockVolunteerClient, mockFormsClient, mockGmailClient, cfg, logger, "2024-01-15", false)

	require.NoError(t, err)
	require.NotNil(t, sentForms)
	require.NotNil(t, failedEmails)

	// Should create 2 requests for vol-1 for the latest rota: 1 unsent + 1 sent
	require.Len(t, mockStore.insertedRequests, 2)
	for _, req := range mockStore.insertedRequests {
		assert.Equal(t, "vol-1", req.VolunteerID)
		assert.Equal(t, "rota-2", req.RotaID)
	}

	// Should have sent form to vol-1
	assert.Len(t, sentForms, 1)
	assert.Equal(t, "vol-1", sentForms[0].VolunteerID)

	// No failed emails
	assert.Len(t, failedEmails, 0)
}

func TestRequestAvailability_PartialEmailFailures(t *testing.T) {
	mockStore := &mockAvailabilityRequestStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2024-01-01", ShiftCount: 10},
		},
		availabilityRequests: []db.AvailabilityRequest{},
	}
	mockVolunteerClient := &mockVolunteerClient{
		volunteers: []model.Volunteer{
			{ID: "vol-1", FirstName: "John", LastName: "Doe", Email: "john@example.com", Status: "Active"},
			{ID: "vol-2", FirstName: "Jane", LastName: "Smith", Email: "jane@example.com", Status: "Active"},
			{ID: "vol-3", FirstName: "Bob", LastName: "Jones", Email: "bob@example.com", Status: "Active"},
		},
	}
	mockFormsClient := &mockFormsClient{}
	mockGmailClient := &mockGmailClient{
		failFor: []string{"jane@example.com"}, // Fail for vol-2
	}
	logger := zap.NewNop()
	ctx := context.Background()
	cfg := &config.Config{}

	sentForms, failedEmails, err := RequestAvailability(ctx, mockStore, mockVolunteerClient, mockFormsClient, mockGmailClient, cfg, logger, "2024-01-15", false)

	require.NoError(t, err)
	require.NotNil(t, sentForms)
	require.NotNil(t, failedEmails)

	// Should have created: 3 unsent (all) + 2 sent (vol-1, vol-3 only - vol-2 failed)
	require.Len(t, mockStore.insertedRequests, 5)

	unsentByVolunteer := make(map[string]bool)
	sentByVolunteer := make(map[string]bool)
	for _, req := range mockStore.insertedRequests {
		if req.FormSent {
			sentByVolunteer[req.VolunteerID] = true
		} else {
			unsentByVolunteer[req.VolunteerID] = true
		}
	}

	// All should have unsent records (forms were created)
	assert.True(t, unsentByVolunteer["vol-1"])
	assert.True(t, unsentByVolunteer["vol-2"])
	assert.True(t, unsentByVolunteer["vol-3"])

	// Only vol-1 and vol-3 should have sent records (email succeeded)
	assert.True(t, sentByVolunteer["vol-1"])
	assert.False(t, sentByVolunteer["vol-2"], "Should not have sent record for failed email")
	assert.True(t, sentByVolunteer["vol-3"])

	// Should have 2 sent forms (vol-1 and vol-3)
	require.Len(t, sentForms, 2)
	sentVolunteerIDs := make(map[string]bool)
	for _, sf := range sentForms {
		sentVolunteerIDs[sf.VolunteerID] = true
	}
	assert.True(t, sentVolunteerIDs["vol-1"])
	assert.True(t, sentVolunteerIDs["vol-3"])
	assert.False(t, sentVolunteerIDs["vol-2"])

	// Should have 1 failed email
	require.Len(t, failedEmails, 1)
	assert.Equal(t, "vol-2", failedEmails[0].VolunteerID)
	assert.Equal(t, "jane@example.com", failedEmails[0].Email)
}

func TestRequestAvailability_AllEmailsFail(t *testing.T) {
	mockStore := &mockAvailabilityRequestStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2024-01-01", ShiftCount: 10},
		},
		availabilityRequests: []db.AvailabilityRequest{},
	}
	mockVolunteerClient := &mockVolunteerClient{
		volunteers: []model.Volunteer{
			{ID: "vol-1", FirstName: "John", LastName: "Doe", Email: "john@example.com", Status: "Active"},
			{ID: "vol-2", FirstName: "Jane", LastName: "Smith", Email: "jane@example.com", Status: "Active"},
		},
	}
	mockFormsClient := &mockFormsClient{}
	mockGmailClient := &mockGmailClient{
		err: fmt.Errorf("gmail service unavailable"),
	}
	logger := zap.NewNop()
	ctx := context.Background()
	cfg := &config.Config{}

	sentForms, failedEmails, err := RequestAvailability(ctx, mockStore, mockVolunteerClient, mockFormsClient, mockGmailClient, cfg, logger, "2024-01-15", false)

	require.Error(t, err)
	assert.Nil(t, sentForms)
	assert.Nil(t, failedEmails)
	assert.Contains(t, err.Error(), "all 2 email send attempts failed")
}

// Helper function tests
func TestFilterVolunteersWithoutRequests(t *testing.T) {
	volunteers := []model.Volunteer{
		{ID: "vol-1"},
		{ID: "vol-2"},
		{ID: "vol-3"},
	}
	withRequests := map[string]bool{
		"vol-1": true,
		"vol-3": true,
	}

	without := filterVolunteersWithoutRequests(volunteers, withRequests)

	require.Len(t, without, 1)
	assert.Equal(t, "vol-2", without[0].ID)
}
