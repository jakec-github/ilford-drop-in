package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// activeVolunteers is testVolunteers with statuses set, since pin
// set-time validation requires an active volunteer (unlike alterations).
func activeVolunteers() *mockVolunteerClient {
	return &mockVolunteerClient{
		volunteers: []model.Volunteer{
			{ID: "alice", DisplayName: "Alice", Roles: []string{"Team lead", "Service volunteer"}, Status: "Active"},
			{ID: "bob", DisplayName: "Bob", Roles: []string{"Service volunteer"}, Status: "Active"},
			{ID: "charlie", DisplayName: "Charlie", Roles: []string{"Service volunteer"}, Status: "Active"},
		},
	}
}

func pinTestStore() *mockStore {
	return &mockStore{
		shifts: []db.Shift{
			{ID: "s1", RotaID: "rota-1", Date: "2026-01-11"},
			{ID: "s2", RotaID: "rota-1", Date: "2026-01-18"},
		},
	}
}

func TestCreatePinEndpoint(t *testing.T) {
	store := pinTestStore()
	body := `{"date":"2026-01-11","volunteerId":"bob","roleId":"role-service-volunteer"}`

	rec := doRequest(t, newTestHandler(store, activeVolunteers()), http.MethodPost, "/api/pins", body, organiserCookie())
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var resp struct {
		ID          string `json:"id"`
		Date        string `json:"date"`
		RoleID      string `json:"roleId"`
		Role        string `json:"role"`
		VolunteerID string `json:"volunteerId"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp.ID)
	assert.Equal(t, "2026-01-11", resp.Date)
	// Both, as a standing pin answers: the id is what the row references, the
	// name is what an Organiser recognises.
	assert.Equal(t, "role-service-volunteer", resp.RoleID)
	assert.Equal(t, "Service volunteer", resp.Role)
	assert.Equal(t, "bob", resp.VolunteerID)

	// Proves the pin persisted through the store, on the right shift.
	require.Len(t, store.insertedPins, 1)
	assert.Equal(t, "s1", store.insertedPins[0].ShiftID)
	assert.Equal(t, "bob", store.insertedPins[0].VolunteerID)
}

func TestCreatePinEndpoint_TeamLead(t *testing.T) {
	store := pinTestStore()
	body := `{"date":"2026-01-11","volunteerId":"alice","roleId":"role-team-lead"}`

	rec := doRequest(t, newTestHandler(store, activeVolunteers()), http.MethodPost, "/api/pins", body, organiserCookie())
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Len(t, store.insertedPins, 1)
	assert.Equal(t, "role-team-lead", store.insertedPins[0].RoleID)
}

// A pin may name a Role the volunteer does not hold (ADR 0010).
func TestCreatePinEndpoint_RoleNotHeld(t *testing.T) {
	store := pinTestStore()
	body := `{"date":"2026-01-11","volunteerId":"bob","roleId":"role-team-lead"}`

	rec := doRequest(t, newTestHandler(store, activeVolunteers()), http.MethodPost, "/api/pins", body, organiserCookie())
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Len(t, store.insertedPins, 1)
	assert.Equal(t, "role-team-lead", store.insertedPins[0].RoleID)
}

func TestCreatePinEndpoint_Errors(t *testing.T) {
	seeded := func() *mockStore {
		s := pinTestStore()
		s.manualPins = []db.Pin{
			{ID: "existing", ShiftID: "s1", RoleID: "role-service-volunteer", VolunteerID: "bob"},
		}
		return s
	}

	tests := []struct {
		name       string
		body       string
		store      *mockStore
		wantStatus int
	}{
		{
			name:       "malformed JSON",
			body:       `{"date":`,
			store:      pinTestStore(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown field",
			body:       `{"date":"2026-01-11","volunteerId":"bob","bogus":true}`,
			store:      pinTestStore(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "neither volunteer nor custom",
			body:       `{"date":"2026-01-11","roleId":"role-service-volunteer"}`,
			store:      pinTestStore(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "both volunteer and custom",
			body:       `{"date":"2026-01-11","volunteerId":"bob","custom":"Helper","roleId":"role-service-volunteer"}`,
			store:      pinTestStore(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "no role at all",
			body:       `{"date":"2026-01-11","volunteerId":"charlie"}`,
			store:      pinTestStore(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "a role id nothing answers to",
			body:       `{"date":"2026-01-11","volunteerId":"bob","roleId":"role-food-collector"}`,
			store:      pinTestStore(),
			wantStatus: http.StatusBadRequest,
		},
		{
			// The wire named a Role by name until #195. A body still doing so
			// is refused rather than quietly pinning nobody to nothing.
			name:       "a role named the old way",
			body:       `{"date":"2026-01-11","volunteerId":"bob","role":"Service volunteer"}`,
			store:      pinTestStore(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown volunteer",
			body:       `{"date":"2026-01-11","volunteerId":"nobody","roleId":"role-service-volunteer"}`,
			store:      pinTestStore(),
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "unknown date",
			body:       `{"date":"2026-02-01","volunteerId":"bob","roleId":"role-service-volunteer"}`,
			store:      pinTestStore(),
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "duplicate assignee",
			body:       `{"date":"2026-01-11","volunteerId":"bob","roleId":"role-service-volunteer"}`,
			store:      seeded(),
			wantStatus: http.StatusConflict,
		},
		{
			name: "already allocated",
			body: `{"date":"2026-01-11","volunteerId":"bob","roleId":"role-service-volunteer"}`,
			store: func() *mockStore {
				s := pinTestStore()
				s.allocatedRotas = map[string]bool{"rota-1": true}
				return s
			}(),
			wantStatus: http.StatusConflict,
		},
		{
			name: "store insert failure",
			body: `{"date":"2026-01-11","volunteerId":"bob","roleId":"role-service-volunteer"}`,
			store: func() *mockStore {
				s := pinTestStore()
				s.insertErr = errors.New("disk full")
				return s
			}(),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doRequest(t, newTestHandler(tt.store, activeVolunteers()), http.MethodPost, "/api/pins", tt.body, organiserCookie())
			assert.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
		})
	}
}

func TestDeletePinEndpoint(t *testing.T) {
	store := pinTestStore()
	store.manualPins = []db.Pin{
		{ID: "pin-1", ShiftID: "s1", RoleID: "role-service-volunteer", VolunteerID: "bob"},
	}

	rec := doRequest(t, newTestHandler(store, activeVolunteers()), http.MethodDelete, "/api/pins/pin-1", "", organiserCookie())
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, []string{"pin-1"}, store.deletedPinIDs)
}

func TestDeletePinEndpoint_NotFound(t *testing.T) {
	rec := doRequest(t, newTestHandler(pinTestStore(), activeVolunteers()), http.MethodDelete, "/api/pins/ghost", "", organiserCookie())
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestDeletePinEndpoint_FrozenRota(t *testing.T) {
	store := pinTestStore()
	store.manualPins = []db.Pin{
		{ID: "pin-1", ShiftID: "s1", RoleID: "role-service-volunteer", VolunteerID: "bob"},
	}
	store.allocatedRotas = map[string]bool{"rota-1": true}

	rec := doRequest(t, newTestHandler(store, activeVolunteers()), http.MethodDelete, "/api/pins/pin-1", "", organiserCookie())
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Empty(t, store.deletedPinIDs, "a frozen rota must not delete the pin")
}

func TestListPinsEndpoint(t *testing.T) {
	store := pinTestStore()
	store.manualPins = []db.Pin{
		{ID: "pin-1", ShiftID: "s1", RoleID: "role-team-lead", VolunteerID: "alice"},
		{ID: "pin-2", ShiftID: "s2", RoleID: "role-service-volunteer", CustomValue: "External Helper"},
	}

	rec := doRequest(t, newTestHandler(store, activeVolunteers()), http.MethodGet, "/api/pins", "", organiserCookie())
	require.Equal(t, http.StatusOK, rec.Code)

	resp := decodePins(t, rec.Body.Bytes())
	require.Len(t, resp.Pins, 2)

	byID := map[string]pinJSON{}
	for _, p := range resp.Pins {
		byID[p.ID] = p
	}
	assert.Equal(t, "2026-01-11", byID["pin-1"].Date)
	assert.Equal(t, "Alice", byID["pin-1"].Name)
	assert.Equal(t, "2026-01-18", byID["pin-2"].Date)
	assert.Equal(t, "External Helper", byID["pin-2"].Name)
}

// pinJSON is the wire shape of one pin, so tests read the same fields
// a client does.
type pinJSON struct {
	ID          string `json:"id"`
	Date        string `json:"date"`
	Role        string `json:"role"`
	VolunteerID string `json:"volunteerId"`
	Custom      string `json:"custom"`
	Name        string `json:"name"`
}

func decodePins(t *testing.T, body []byte) struct {
	Pins []pinJSON `json:"pins"`
} {
	t.Helper()
	var resp struct {
		Pins []pinJSON `json:"pins"`
	}
	require.NoError(t, json.Unmarshal(body, &resp))
	return resp
}

func TestListPinsEndpoint_DateFilter(t *testing.T) {
	store := pinTestStore()
	store.manualPins = []db.Pin{
		{ID: "pin-1", ShiftID: "s1", RoleID: "role-service-volunteer", VolunteerID: "bob"},
		{ID: "pin-2", ShiftID: "s2", RoleID: "role-service-volunteer", VolunteerID: "charlie"},
	}

	rec := doRequest(t, newTestHandler(store, activeVolunteers()), http.MethodGet, "/api/pins?from=2026-01-12", "", organiserCookie())
	require.Equal(t, http.StatusOK, rec.Code)

	resp := decodePins(t, rec.Body.Bytes())
	require.Len(t, resp.Pins, 1)
	assert.Equal(t, "pin-2", resp.Pins[0].ID)
}

func TestPinsMethodNotAllowed(t *testing.T) {
	handler := newTestHandler(pinTestStore(), activeVolunteers())

	rec := doRequest(t, handler, http.MethodPut, "/api/pins", "")
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

// TestPinsRequireASession proves all three pin endpoints are gated:
// without a session they are rejected, nothing is persisted or deleted, and the
// listing gives nothing away — it names people against dates the rota has not
// published.
func TestPinsRequireASession(t *testing.T) {
	store := pinTestStore()
	store.manualPins = []db.Pin{
		{ID: "pin-1", ShiftID: "s1", RoleID: "role-service-volunteer", VolunteerID: "bob"},
	}
	handler := newTestHandler(store, activeVolunteers())

	rec := doRequest(t, handler, http.MethodPost, "/api/pins", `{"date":"2026-01-11","volunteerId":"bob"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, store.insertedPins, "an unauthenticated request must not pin anyone")

	rec = doRequest(t, handler, http.MethodDelete, "/api/pins/pin-1", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, store.deletedPinIDs, "an unauthenticated request must not delete a pin")

	rec = doRequest(t, handler, http.MethodGet, "/api/pins", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.NotContains(t, rec.Body.String(), "bob", "an unauthenticated request must not read who is pinned")
}
