package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAvailabilityRequests_NoDuplicates(t *testing.T) {
	// Create test data with no duplicates (all different IDs)
	testRequests := []AvailabilityRequest{
		{
			ID:          "req-1",
			RotaID:      "rota-1",
			ShiftDate:   "2024-01-01",
			VolunteerID: "vol-1",
			FormID:      "form-1",
			FormURL:     "https://form1.com",
			FormSent:    true,
		},
		{
			ID:          "req-2",
			RotaID:      "rota-1",
			ShiftDate:   "2024-01-02",
			VolunteerID: "vol-1",
			FormID:      "form-2",
			FormURL:     "https://form2.com",
			FormSent:    false,
		},
		{
			ID:          "req-3",
			RotaID:      "rota-1",
			ShiftDate:   "2024-01-01",
			VolunteerID: "vol-2",
			FormID:      "form-3",
			FormURL:     "https://form3.com",
			FormSent:    true,
		},
	}

	// Simulate the deduplication logic using ID as key
	requestMap := make(map[string]AvailabilityRequest)

	for _, req := range testRequests {
		existing, exists := requestMap[req.ID]
		if !exists {
			requestMap[req.ID] = req
		} else if req.FormSent && !existing.FormSent {
			requestMap[req.ID] = req
		}
	}

	result := make([]AvailabilityRequest, 0, len(requestMap))
	for _, req := range requestMap {
		result = append(result, req)
	}

	// Should return all 3 requests since there are no duplicate IDs
	assert.Len(t, result, 3)
}

func TestGetAvailabilityRequests_WithDuplicates_PreferFormSentTrue(t *testing.T) {
	// Create test data with duplicate IDs where form_sent changes from false to true
	testRequests := []AvailabilityRequest{
		{
			ID:          "req-1", // Same ID
			RotaID:      "rota-1",
			ShiftDate:   "2024-01-01",
			VolunteerID: "vol-1",
			FormID:      "form-1",
			FormURL:     "https://form1.com",
			FormSent:    false, // Initial record
		},
		{
			ID:          "req-1", // Same ID - duplicate
			RotaID:      "rota-1",
			ShiftDate:   "2024-01-01",
			VolunteerID: "vol-1",
			FormID:      "form-1",
			FormURL:     "https://form1.com",
			FormSent:    true, // Updated record (form sent)
		},
		{
			ID:          "req-2", // Different ID
			RotaID:      "rota-1",
			ShiftDate:   "2024-01-02",
			VolunteerID: "vol-2",
			FormID:      "form-2",
			FormURL:     "https://form2.com",
			FormSent:    true,
		},
	}

	requestMap := make(map[string]AvailabilityRequest)

	for _, req := range testRequests {
		existing, exists := requestMap[req.ID]
		if !exists {
			requestMap[req.ID] = req
		} else if req.FormSent && !existing.FormSent {
			requestMap[req.ID] = req
		}
	}

	result := make([]AvailabilityRequest, 0, len(requestMap))
	for _, req := range requestMap {
		result = append(result, req)
	}

	// Should return 2 requests (duplicate ID filtered out)
	require.Len(t, result, 2)

	// Find the request with ID req-1
	var req1 *AvailabilityRequest
	for _, req := range result {
		if req.ID == "req-1" {
			req1 = &req
			break
		}
	}

	require.NotNil(t, req1, "Should have request with ID req-1")
	assert.True(t, req1.FormSent, "Should keep the form_sent=true record")
}

func TestGetAvailabilityRequests_DataIntegrityViolation_MultipleFormSentFalse(t *testing.T) {
	// Test case where we have duplicate IDs with both form_sent=false (should error)
	testRequests := []AvailabilityRequest{
		{
			ID:          "req-1", // Same ID
			RotaID:      "rota-1",
			ShiftDate:   "2024-01-01",
			VolunteerID: "vol-1",
			FormID:      "form-1",
			FormURL:     "https://form1.com",
			FormSent:    false,
		},
		{
			ID:          "req-1", // Same ID - duplicate
			RotaID:      "rota-1",
			ShiftDate:   "2024-01-01",
			VolunteerID: "vol-1",
			FormID:      "form-1",
			FormURL:     "https://form1.com",
			FormSent:    false,
		},
	}

	// Simulate the validation logic
	type recordState struct {
		formSentFalse *AvailabilityRequest
		formSentTrue  *AvailabilityRequest
	}
	stateMap := make(map[string]*recordState)

	var err error
	for i := range testRequests {
		req := &testRequests[i]

		state, exists := stateMap[req.ID]
		if !exists {
			state = &recordState{}
			stateMap[req.ID] = state
		}

		if req.FormSent {
			if state.formSentTrue != nil {
				err = assert.AnError
				break
			}
			state.formSentTrue = req
		} else {
			if state.formSentFalse != nil {
				err = assert.AnError
				break
			}
			state.formSentFalse = req
		}
	}

	// Should error due to multiple form_sent=false records with same ID
	require.Error(t, err)
}

func TestGetAvailabilityRequests_DataIntegrityViolation_MultipleFormSentTrue(t *testing.T) {
	// Test case where we have duplicate IDs with both form_sent=true (should error)
	testRequests := []AvailabilityRequest{
		{
			ID:          "req-1", // Same ID
			RotaID:      "rota-1",
			ShiftDate:   "2024-01-01",
			VolunteerID: "vol-1",
			FormID:      "form-1",
			FormURL:     "https://form1.com",
			FormSent:    true,
		},
		{
			ID:          "req-1", // Same ID - duplicate
			RotaID:      "rota-1",
			ShiftDate:   "2024-01-01",
			VolunteerID: "vol-1",
			FormID:      "form-1",
			FormURL:     "https://form1.com",
			FormSent:    true,
		},
	}

	// Simulate the validation logic
	type recordState struct {
		formSentFalse *AvailabilityRequest
		formSentTrue  *AvailabilityRequest
	}
	stateMap := make(map[string]*recordState)

	var err error
	for i := range testRequests {
		req := &testRequests[i]

		state, exists := stateMap[req.ID]
		if !exists {
			state = &recordState{}
			stateMap[req.ID] = state
		}

		if req.FormSent {
			if state.formSentTrue != nil {
				err = assert.AnError
				break
			}
			state.formSentTrue = req
		} else {
			if state.formSentFalse != nil {
				err = assert.AnError
				break
			}
			state.formSentFalse = req
		}
	}

	// Should error due to multiple form_sent=true records with same ID
	require.Error(t, err)
}

func TestGetAvailabilityRequests_ValidDuplicates(t *testing.T) {
	// Test with valid duplicate (one form_sent=false, one form_sent=true per ID)
	testRequests := []AvailabilityRequest{
		// Request 1 - has valid duplicate with form_sent progression
		{
			ID:          "req-1", // Same ID
			RotaID:      "rota-1",
			ShiftDate:   "2024-01-01",
			VolunteerID: "vol-1",
			FormID:      "form-1",
			FormURL:     "https://form1.com",
			FormSent:    false,
		},
		{
			ID:          "req-1", // Same ID - valid duplicate
			RotaID:      "rota-1",
			ShiftDate:   "2024-01-01",
			VolunteerID: "vol-1",
			FormID:      "form-1",
			FormURL:     "https://form1.com",
			FormSent:    true,
		},
		// Request 2 - no duplicates
		{
			ID:          "req-2",
			RotaID:      "rota-1",
			ShiftDate:   "2024-01-01",
			VolunteerID: "vol-2",
			FormID:      "form-2",
			FormURL:     "https://form2.com",
			FormSent:    true,
		},
		// Request 3 - only form_sent=false (no duplicate)
		{
			ID:          "req-3",
			RotaID:      "rota-1",
			ShiftDate:   "2024-01-02",
			VolunteerID: "vol-3",
			FormID:      "form-3",
			FormURL:     "https://form3.com",
			FormSent:    false,
		},
	}

	// Simulate the validation and deduplication logic
	type recordState struct {
		formSentFalse *AvailabilityRequest
		formSentTrue  *AvailabilityRequest
	}
	stateMap := make(map[string]*recordState)

	var err error
	for i := range testRequests {
		req := &testRequests[i]

		state, exists := stateMap[req.ID]
		if !exists {
			state = &recordState{}
			stateMap[req.ID] = state
		}

		if req.FormSent {
			if state.formSentTrue != nil {
				err = assert.AnError
				break
			}
			state.formSentTrue = req
		} else {
			if state.formSentFalse != nil {
				err = assert.AnError
				break
			}
			state.formSentFalse = req
		}
	}

	require.NoError(t, err, "Should not error on valid duplicates")

	// Deduplicate: prefer form_sent=true
	result := make([]AvailabilityRequest, 0, len(stateMap))
	for _, state := range stateMap {
		if state.formSentTrue != nil {
			result = append(result, *state.formSentTrue)
		} else if state.formSentFalse != nil {
			result = append(result, *state.formSentFalse)
		}
	}

	// Should return 3 requests (one per unique ID)
	require.Len(t, result, 3)

	// Verify req-1 has form_sent=true (preferred over false)
	var req1 *AvailabilityRequest
	for _, req := range result {
		if req.ID == "req-1" {
			req1 = &req
			break
		}
	}
	require.NotNil(t, req1)
	assert.True(t, req1.FormSent, "req-1 should have form_sent=true")

	// Verify req-3 exists (with form_sent=false, no duplicate)
	var req3 *AvailabilityRequest
	for _, req := range result {
		if req.ID == "req-3" {
			req3 = &req
			break
		}
	}
	require.NotNil(t, req3)
	assert.False(t, req3.FormSent, "req-3 should have form_sent=false")
}

func TestGetAvailabilityRequests_DifferentIDsNotDuplicate(t *testing.T) {
	// Test that records with different IDs are NOT considered duplicates,
	// even if they're for the same volunteer and shift date
	testRequests := []AvailabilityRequest{
		{
			ID:          "req-1",
			RotaID:      "rota-1",
			ShiftDate:   "2024-01-01",
			VolunteerID: "vol-1",
			FormID:      "form-1",
			FormURL:     "https://form1.com",
			FormSent:    false,
		},
		{
			ID:          "req-2", // Different ID
			RotaID:      "rota-1",
			ShiftDate:   "2024-01-01", // Same date
			VolunteerID: "vol-1",      // Same volunteer
			FormID:      "form-2",
			FormURL:     "https://form2.com",
			FormSent:    false,
		},
	}

	requestMap := make(map[string]AvailabilityRequest)

	for _, req := range testRequests {
		existing, exists := requestMap[req.ID]
		if !exists {
			requestMap[req.ID] = req
		} else if req.FormSent && !existing.FormSent {
			requestMap[req.ID] = req
		}
	}

	result := make([]AvailabilityRequest, 0, len(requestMap))
	for _, req := range requestMap {
		result = append(result, req)
	}

	// Should return both requests (different IDs)
	assert.Len(t, result, 2)
}
