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
	shifts               []db.Shift
	availabilityRequests []db.AvailabilityRequest
	insertedRequests     []db.AvailabilityRequest
	markedSentIDs        []string
	getRotationsErr      error
	getRequestsErr       error
	insertErr            error
	markSentErr          error
}

func (m *mockAvailabilityRequestStore) GetRotations(ctx context.Context) ([]db.Rotation, error) {
	if m.getRotationsErr != nil {
		return nil, m.getRotationsErr
	}
	return m.rotations, nil
}

func (m *mockAvailabilityRequestStore) GetShiftsByRotaID(ctx context.Context, rotaID string) ([]db.Shift, error) {
	var filtered []db.Shift
	for _, s := range m.shifts {
		if s.RotaID == rotaID {
			filtered = append(filtered, s)
		}
	}
	return filtered, nil
}

func (m *mockAvailabilityRequestStore) GetAvailabilityRequestsByRotaID(ctx context.Context, rotaID string) ([]db.AvailabilityRequest, error) {
	if m.getRequestsErr != nil {
		return nil, m.getRequestsErr
	}
	var filtered []db.AvailabilityRequest
	for _, r := range m.availabilityRequests {
		if r.RotaID == rotaID {
			filtered = append(filtered, r)
		}
	}
	return filtered, nil
}

func (m *mockAvailabilityRequestStore) InsertAvailabilityRequests(ctx context.Context, requests []db.AvailabilityRequest) error {
	if m.insertErr != nil {
		return m.insertErr
	}
	m.insertedRequests = append(m.insertedRequests, requests...)
	return nil
}

func (m *mockAvailabilityRequestStore) MarkAvailabilityRequestsSent(ctx context.Context, ids []string) error {
	if m.markSentErr != nil {
		return m.markSentErr
	}
	m.markedSentIDs = append(m.markedSentIDs, ids...)
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
	createdForms      []string // Track volunteer IDs for which forms were created
	createdShiftDates [][]time.Time
	err               error
}

func (m *mockFormsClient) CreateAvailabilityForm(volunteerName string, shiftDates []time.Time) (*formsclient.AvailabilityFormResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	// Extract volunteer ID from name for tracking
	formID := "form-" + volunteerName
	m.createdForms = append(m.createdForms, volunteerName)
	m.createdShiftDates = append(m.createdShiftDates, append([]time.Time(nil), shiftDates...))
	return &formsclient.AvailabilityFormResult{
		FormID:       formID,
		ResponderURI: "https://forms.google.com/" + formID,
	}, nil
}

func TestRequestAvailability_KeepsAllShiftsWithoutOverrides(t *testing.T) {
	mockStore := &mockAvailabilityRequestStore{
		rotations: []db.Rotation{{ID: "rota-1", Start: "2025-01-05", ShiftCount: 3}},
		shifts:    sundayShifts("rota-1", "2025-01-05", 3),
	}
	mockVolunteerClient := &mockVolunteerClient{
		volunteers: []model.Volunteer{{
			ID: "vol-1", FirstName: "Jane", LastName: "Smith",
			Email: "jane@example.com", Status: "Active",
		}},
	}
	mockFormsClient := &mockFormsClient{}

	_, _, err := RequestAvailability(
		context.Background(), mockStore, mockVolunteerClient, mockFormsClient,
		&mockGmailClient{}, &config.Config{}, zap.NewNop(), "2025-01-31", true,
	)

	require.NoError(t, err)
	require.Len(t, mockFormsClient.createdShiftDates, 1)
	require.Len(t, mockFormsClient.createdShiftDates[0], 3)
	assert.Equal(t, "2025-01-05", mockFormsClient.createdShiftDates[0][0].Format("2006-01-02"))
	assert.Equal(t, "2025-01-19", mockFormsClient.createdShiftDates[0][2].Format("2006-01-02"))
}

func TestRequestAvailability_ExcludesClosedShifts(t *testing.T) {
	mockStore := &mockAvailabilityRequestStore{
		rotations: []db.Rotation{{ID: "rota-1", Start: "2025-01-05", ShiftCount: 3}},
		shifts:    sundayShifts("rota-1", "2025-01-05", 3),
	}
	mockVolunteerClient := &mockVolunteerClient{
		volunteers: []model.Volunteer{{
			ID: "vol-1", FirstName: "Jane", LastName: "Smith",
			Email: "jane@example.com", Status: "Active",
		}},
	}
	mockFormsClient := &mockFormsClient{}
	cfg := &config.Config{RotaOverrides: []config.RotaOverride{{
		RRule:  "FREQ=YEARLY;BYMONTH=1;BYMONTHDAY=5,19",
		Closed: true,
	}}}

	_, _, err := RequestAvailability(
		context.Background(), mockStore, mockVolunteerClient, mockFormsClient,
		&mockGmailClient{}, cfg, zap.NewNop(), "2025-01-31", true,
	)

	require.NoError(t, err)
	require.Len(t, mockFormsClient.createdShiftDates, 1)
	require.Len(t, mockFormsClient.createdShiftDates[0], 1)
	assert.Equal(t, "2025-01-12", mockFormsClient.createdShiftDates[0][0].Format("2006-01-02"))
}

func TestRequestAvailability_RejectsRotaWithAllShiftsClosed(t *testing.T) {
	mockStore := &mockAvailabilityRequestStore{
		rotations: []db.Rotation{{ID: "rota-1", Start: "2025-01-05", ShiftCount: 3}},
		shifts:    sundayShifts("rota-1", "2025-01-05", 3),
	}
	mockFormsClient := &mockFormsClient{}
	cfg := &config.Config{RotaOverrides: []config.RotaOverride{{
		RRule:  "FREQ=WEEKLY;BYDAY=SU",
		Closed: true,
	}}}

	_, _, err := RequestAvailability(
		context.Background(), mockStore, &mockVolunteerClient{}, mockFormsClient,
		&mockGmailClient{}, cfg, zap.NewNop(), "2025-01-31", true,
	)

	require.EqualError(t, err, "rota rota-1 has no open shifts")
	assert.Empty(t, mockFormsClient.createdShiftDates)
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
		shifts: sundayShifts("rota-1", "2024-01-01", 10),
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

	// Should have inserted 2 new unsent requests (vol-2, vol-3) -
	// vol-1's unsent request already exists
	require.Len(t, mockStore.insertedRequests, 2)

	insertedByVolunteer := make(map[string]db.AvailabilityRequest)
	insertedIDsByVolunteer := make(map[string]string)
	for _, req := range mockStore.insertedRequests {
		assert.Equal(t, "rota-1", req.RotaID)
		assert.NotEmpty(t, req.ID)
		assert.NotEmpty(t, req.FormID)
		assert.NotEmpty(t, req.FormURL)
		assert.False(t, req.FormSent, "New requests should be inserted unsent")
		insertedByVolunteer[req.VolunteerID] = req
		insertedIDsByVolunteer[req.VolunteerID] = req.ID
	}

	assert.Contains(t, insertedByVolunteer, "vol-2")
	assert.Contains(t, insertedByVolunteer, "vol-3")
	assert.NotContains(t, insertedByVolunteer, "vol-1", "Should not have created unsent request for vol-1 (already exists)")

	// All three requests should be marked sent (vol-1's existing plus the two new)
	require.Len(t, mockStore.markedSentIDs, 3)
	assert.Contains(t, mockStore.markedSentIDs, "req-1")
	assert.Contains(t, mockStore.markedSentIDs, insertedIDsByVolunteer["vol-2"])
	assert.Contains(t, mockStore.markedSentIDs, insertedIDsByVolunteer["vol-3"])

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
		shifts: sundayShifts("rota-1", "2024-01-01", 10),
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

	// No new requests inserted; vol-2's existing unsent request is marked sent
	assert.Len(t, mockStore.insertedRequests, 0)
	require.Len(t, mockStore.markedSentIDs, 1)
	assert.Equal(t, "req-2", mockStore.markedSentIDs[0]) // Should reuse existing request ID

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
		shifts: sundayShifts("rota-1", "2024-01-01", 10),
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
		shifts: append(
			sundayShifts("rota-1", "2024-01-01", 10),
			sundayShifts("rota-2", "2024-04-01", 12)...,
		),
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

	// Should create 1 unsent request for vol-1 for the latest rota, then mark it sent
	require.Len(t, mockStore.insertedRequests, 1)
	assert.Equal(t, "vol-1", mockStore.insertedRequests[0].VolunteerID)
	assert.Equal(t, "rota-2", mockStore.insertedRequests[0].RotaID)
	assert.False(t, mockStore.insertedRequests[0].FormSent)
	require.Len(t, mockStore.markedSentIDs, 1)
	assert.Equal(t, mockStore.insertedRequests[0].ID, mockStore.markedSentIDs[0])

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
		shifts:               sundayShifts("rota-1", "2024-01-01", 10),
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

	// Should have inserted 3 unsent requests (forms created for all)
	require.Len(t, mockStore.insertedRequests, 3)

	insertedIDsByVolunteer := make(map[string]string)
	for _, req := range mockStore.insertedRequests {
		assert.False(t, req.FormSent)
		insertedIDsByVolunteer[req.VolunteerID] = req.ID
	}
	assert.Contains(t, insertedIDsByVolunteer, "vol-1")
	assert.Contains(t, insertedIDsByVolunteer, "vol-2")
	assert.Contains(t, insertedIDsByVolunteer, "vol-3")

	// Only vol-1 and vol-3 should be marked sent (email succeeded)
	require.Len(t, mockStore.markedSentIDs, 2)
	assert.Contains(t, mockStore.markedSentIDs, insertedIDsByVolunteer["vol-1"])
	assert.NotContains(t, mockStore.markedSentIDs, insertedIDsByVolunteer["vol-2"], "Should not mark failed email's request as sent")
	assert.Contains(t, mockStore.markedSentIDs, insertedIDsByVolunteer["vol-3"])

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
		shifts:               sundayShifts("rota-1", "2024-01-01", 10),
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

func TestRequestAvailability_NoEmail_CreatesFormsButDoesNotSend(t *testing.T) {
	mockStore := &mockAvailabilityRequestStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2024-01-01", ShiftCount: 10},
		},
		shifts:               sundayShifts("rota-1", "2024-01-01", 10),
		availabilityRequests: []db.AvailabilityRequest{},
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

	// Call with noEmail=true
	sentForms, failedEmails, err := RequestAvailability(ctx, mockStore, mockVolunteerClient, mockFormsClient, mockGmailClient, cfg, logger, "2024-01-15", true)

	require.NoError(t, err)
	require.NotNil(t, sentForms)
	require.NotNil(t, failedEmails)

	// Should have created unsent requests only (no sent requests)
	require.Len(t, mockStore.insertedRequests, 2)
	for _, req := range mockStore.insertedRequests {
		assert.False(t, req.FormSent, "All requests should have FormSent=false")
		assert.Equal(t, "rota-1", req.RotaID)
	}

	// Forms should have been created
	assert.Len(t, mockFormsClient.createdForms, 2)

	// No emails should have been sent
	assert.Len(t, mockGmailClient.sentEmails, 0)

	// Should return empty slices (not nil)
	assert.Len(t, sentForms, 0)
	assert.Len(t, failedEmails, 0)
}

func TestRequestAvailability_NoEmail_ReusesExistingUnsentForms(t *testing.T) {
	mockStore := &mockAvailabilityRequestStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2024-01-01", ShiftCount: 10},
		},
		shifts: sundayShifts("rota-1", "2024-01-01", 10),
		availabilityRequests: []db.AvailabilityRequest{
			// vol-1 already has an unsent request
			{ID: "req-1", RotaID: "rota-1", VolunteerID: "vol-1", FormID: "existing-form-1", FormURL: "https://forms.google.com/existing", FormSent: false},
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

	// Call with noEmail=true
	sentForms, failedEmails, err := RequestAvailability(ctx, mockStore, mockVolunteerClient, mockFormsClient, mockGmailClient, cfg, logger, "2024-01-15", true)

	require.NoError(t, err)

	// Should only create unsent request for vol-2 (vol-1 already has one)
	require.Len(t, mockStore.insertedRequests, 1)
	assert.Equal(t, "vol-2", mockStore.insertedRequests[0].VolunteerID)
	assert.False(t, mockStore.insertedRequests[0].FormSent)

	// Only one form created (for vol-2, vol-1 reused existing)
	assert.Len(t, mockFormsClient.createdForms, 1)

	// No emails sent
	assert.Len(t, mockGmailClient.sentEmails, 0)

	// Empty results
	assert.Len(t, sentForms, 0)
	assert.Len(t, failedEmails, 0)
}

func TestRequestAvailability_NoEmail_SkipsVolunteersWithSentRequests(t *testing.T) {
	mockStore := &mockAvailabilityRequestStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2024-01-01", ShiftCount: 10},
		},
		shifts: sundayShifts("rota-1", "2024-01-01", 10),
		availabilityRequests: []db.AvailabilityRequest{
			// vol-1 already has a sent request - should be completely skipped
			{ID: "req-1", RotaID: "rota-1", VolunteerID: "vol-1", FormID: "form-1", FormURL: "https://forms.google.com/form-1", FormSent: true},
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

	// Call with noEmail=true
	sentForms, failedEmails, err := RequestAvailability(ctx, mockStore, mockVolunteerClient, mockFormsClient, mockGmailClient, cfg, logger, "2024-01-15", true)

	require.NoError(t, err)

	// Should only create unsent request for vol-2 (vol-1 already has sent request)
	require.Len(t, mockStore.insertedRequests, 1)
	assert.Equal(t, "vol-2", mockStore.insertedRequests[0].VolunteerID)
	assert.False(t, mockStore.insertedRequests[0].FormSent)

	// Only one form created (for vol-2)
	assert.Len(t, mockFormsClient.createdForms, 1)

	// No emails sent
	assert.Len(t, mockGmailClient.sentEmails, 0)

	// Empty results
	assert.Len(t, sentForms, 0)
	assert.Len(t, failedEmails, 0)
}

func TestRequestAvailability_NoEmail_AllAlreadySent(t *testing.T) {
	mockStore := &mockAvailabilityRequestStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2024-01-01", ShiftCount: 10},
		},
		shifts: sundayShifts("rota-1", "2024-01-01", 10),
		availabilityRequests: []db.AvailabilityRequest{
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

	sentForms, failedEmails, err := RequestAvailability(ctx, mockStore, mockVolunteerClient, mockFormsClient, mockGmailClient, cfg, logger, "2024-01-15", true)

	require.NoError(t, err)

	// No records inserted, no forms created, no emails sent
	assert.Len(t, mockStore.insertedRequests, 0)
	assert.Len(t, mockFormsClient.createdForms, 0)
	assert.Len(t, mockGmailClient.sentEmails, 0)
	assert.Len(t, sentForms, 0)
	assert.Len(t, failedEmails, 0)
}

func TestRequestAvailability_NoEmail_ThenSendEmails(t *testing.T) {
	// Phase 1: noEmail creates forms and unsent records
	store := &mockAvailabilityRequestStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2024-01-01", ShiftCount: 10},
		},
		shifts:               sundayShifts("rota-1", "2024-01-01", 10),
		availabilityRequests: []db.AvailabilityRequest{},
	}
	volunteers := &mockVolunteerClient{
		volunteers: []model.Volunteer{
			{ID: "vol-1", FirstName: "John", LastName: "Doe", Email: "john@example.com", Status: "Active"},
			{ID: "vol-2", FirstName: "Jane", LastName: "Smith", Email: "jane@example.com", Status: "Active"},
		},
	}
	formsClient := &mockFormsClient{}
	gmailClient := &mockGmailClient{}
	logger := zap.NewNop()
	ctx := context.Background()
	cfg := &config.Config{}

	sentForms, failedEmails, err := RequestAvailability(ctx, store, volunteers, formsClient, gmailClient, cfg, logger, "2024-01-15", true)
	require.NoError(t, err)
	assert.Len(t, sentForms, 0)
	assert.Len(t, failedEmails, 0)

	// Verify phase 1 created unsent records
	require.Len(t, store.insertedRequests, 2)
	for _, req := range store.insertedRequests {
		assert.False(t, req.FormSent)
	}
	assert.Len(t, formsClient.createdForms, 2)
	assert.Len(t, gmailClient.sentEmails, 0)

	// Phase 2: normal run should reuse the unsent forms and send emails
	// Simulate the unsent records being visible in the DB
	store.availabilityRequests = append(store.availabilityRequests, store.insertedRequests...)
	store.insertedRequests = nil
	gmailClient.sentEmails = nil

	sentForms, failedEmails, err = RequestAvailability(ctx, store, volunteers, formsClient, gmailClient, cfg, logger, "2024-01-15", false)
	require.NoError(t, err)

	// Should not create new forms (reuses existing unsent forms)
	assert.Len(t, formsClient.createdForms, 2, "Should not create additional forms")

	// Should send emails to both volunteers
	assert.Len(t, gmailClient.sentEmails, 2)
	assert.Contains(t, gmailClient.sentEmails, "john@example.com")
	assert.Contains(t, gmailClient.sentEmails, "jane@example.com")

	// Should not insert new records; the existing requests are marked sent
	assert.Len(t, store.insertedRequests, 0, "Phase 2 should not insert new records")
	assert.Len(t, store.markedSentIDs, 2)

	assert.Len(t, sentForms, 2)
	assert.Len(t, failedEmails, 0)
}

func TestRequestAvailability_NoEmail_FiltersInactiveVolunteers(t *testing.T) {
	mockStore := &mockAvailabilityRequestStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2024-01-01", ShiftCount: 10},
		},
		shifts:               sundayShifts("rota-1", "2024-01-01", 10),
		availabilityRequests: []db.AvailabilityRequest{},
	}
	mockVolunteerClient := &mockVolunteerClient{
		volunteers: []model.Volunteer{
			{ID: "vol-1", FirstName: "John", LastName: "Doe", Email: "john@example.com", Status: "Active"},
			{ID: "vol-2", FirstName: "Jane", LastName: "Smith", Email: "jane@example.com", Status: "Inactive"},
			{ID: "vol-3", FirstName: "Bob", LastName: "Jones", Email: "bob@example.com", Status: "On Leave"},
		},
	}
	mockFormsClient := &mockFormsClient{}
	mockGmailClient := &mockGmailClient{}
	logger := zap.NewNop()
	ctx := context.Background()
	cfg := &config.Config{}

	sentForms, failedEmails, err := RequestAvailability(ctx, mockStore, mockVolunteerClient, mockFormsClient, mockGmailClient, cfg, logger, "2024-01-15", true)

	require.NoError(t, err)

	// Only vol-1 (Active) should get a form
	require.Len(t, mockStore.insertedRequests, 1)
	assert.Equal(t, "vol-1", mockStore.insertedRequests[0].VolunteerID)
	assert.False(t, mockStore.insertedRequests[0].FormSent)

	assert.Len(t, mockFormsClient.createdForms, 1)
	assert.Len(t, mockGmailClient.sentEmails, 0)
	assert.Len(t, sentForms, 0)
	assert.Len(t, failedEmails, 0)
}

// Helper function tests
func TestFilterVolunteersWithoutSentRequests(t *testing.T) {
	volunteers := []model.Volunteer{
		{ID: "vol-1"},
		{ID: "vol-2"},
		{ID: "vol-3"},
	}
	withSentRequests := map[string]bool{
		"vol-1": true,
		"vol-3": true,
	}

	without := filterVolunteersWithoutSentRequests(volunteers, withSentRequests)

	require.Len(t, without, 1)
	assert.Equal(t, "vol-2", without[0].ID)
}
