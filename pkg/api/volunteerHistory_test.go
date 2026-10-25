package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

func TestVolunteerHistoryEndpoint(t *testing.T) {
	store := &mockStore{
		rotations: []db.Rotation{{ID: "rota-1", Start: "2026-01-04", End: "2026-02-22", ShiftCount: 2, AllocatedDatetime: "2026-01-01T00:00:00Z"}},
		shifts: []db.Shift{
			{ID: "s1", RotaID: "rota-1", Date: "2026-01-04"},
			{ID: "s2", RotaID: "rota-1", Date: "2026-01-11"},
		},
		allocations:          []db.Allocation{{ShiftID: "s1", VolunteerID: "alice"}},
		availabilityRequests: []db.AvailabilityRequest{{ID: "r1", RotaID: "rota-1", VolunteerID: "alice"}},
	}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/volunteer-history", "", organiserCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body volunteerHistoryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, []historyRotaResponse{{ID: "rota-1", Start: "2026-01-04", End: "2026-02-22"}}, body.Rotations)
	require.Len(t, body.Volunteers, 1)
	assert.Equal(t, "alice", body.Volunteers[0].ID)
	assert.Equal(t, "Alice Adams", body.Volunteers[0].FullName)
	// One entry per rota, in the rotations' order.
	assert.Equal(t, []historyEntryResponse{{Availability: "no_response", OpenShifts: 2, Given: 1, Attended: 1}}, body.Volunteers[0].History)
}

// It names people against how reliable they have been: an Organiser's business
// alone.
func TestVolunteerHistoryEndpoint_OrganiserOnly(t *testing.T) {
	handler := newTestHandler(&mockStore{}, testVolunteers())
	assert.Equal(t, http.StatusForbidden, doRequest(t, handler, http.MethodGet, "/api/volunteer-history", "", rotaEditorCookie()).Code)
	assert.Equal(t, http.StatusUnauthorized, doRequest(t, handler, http.MethodGet, "/api/volunteer-history", "").Code)
}
