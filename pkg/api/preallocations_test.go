package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// activeVolunteers is testVolunteers with statuses set, since preallocation
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

func preallocationTestStore() *mockStore {
	return &mockStore{
		shifts: []db.Shift{
			{ID: "s1", RotaID: "rota-1", Date: "2026-01-11"},
			{ID: "s2", RotaID: "rota-1", Date: "2026-01-18"},
		},
	}
}

func TestCreatePreallocationEndpoint(t *testing.T) {
	store := preallocationTestStore()
	body := `{"date":"2026-01-11","volunteerId":"bob","role":"Service volunteer"}`

	rec := doRequest(t, newTestHandler(store, activeVolunteers()), http.MethodPost, "/api/preallocations", body, adminCookie())
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var resp struct {
		ID          string `json:"id"`
		Date        string `json:"date"`
		Role        string `json:"role"`
		VolunteerID string `json:"volunteerId"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp.ID)
	assert.Equal(t, "2026-01-11", resp.Date)
	assert.Equal(t, "Service volunteer", resp.Role)
	assert.Equal(t, "bob", resp.VolunteerID)

	// Proves the pin persisted through the store, on the right shift.
	require.Len(t, store.insertedPreallocations, 1)
	assert.Equal(t, "s1", store.insertedPreallocations[0].ShiftID)
	assert.Equal(t, "bob", store.insertedPreallocations[0].VolunteerID)
}

func TestCreatePreallocationEndpoint_TeamLead(t *testing.T) {
	store := preallocationTestStore()
	body := `{"date":"2026-01-11","volunteerId":"alice","role":"Team lead"}`

	rec := doRequest(t, newTestHandler(store, activeVolunteers()), http.MethodPost, "/api/preallocations", body, adminCookie())
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Len(t, store.insertedPreallocations, 1)
	assert.Equal(t, "Team lead", store.insertedPreallocations[0].Role)
}

func TestCreatePreallocationEndpoint_Errors(t *testing.T) {
	seeded := func() *mockStore {
		s := preallocationTestStore()
		s.manualPreallocations = []db.ManualPreallocation{
			{ID: "existing", ShiftID: "s1", Role: "Service volunteer", VolunteerID: "bob"},
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
			store:      preallocationTestStore(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown field",
			body:       `{"date":"2026-01-11","volunteerId":"bob","bogus":true}`,
			store:      preallocationTestStore(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "neither volunteer nor custom",
			body:       `{"date":"2026-01-11","role":"Service volunteer"}`,
			store:      preallocationTestStore(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "both volunteer and custom",
			body:       `{"date":"2026-01-11","volunteerId":"bob","custom":"Helper","role":"Service volunteer"}`,
			store:      preallocationTestStore(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "a role the volunteer does not hold",
			body:       `{"date":"2026-01-11","volunteerId":"bob","role":"Team lead"}`,
			store:      preallocationTestStore(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "no role at all",
			body:       `{"date":"2026-01-11","volunteerId":"charlie"}`,
			store:      preallocationTestStore(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "a role config does not name",
			body:       `{"date":"2026-01-11","volunteerId":"bob","role":"Food collector"}`,
			store:      preallocationTestStore(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown volunteer",
			body:       `{"date":"2026-01-11","volunteerId":"nobody","role":"Service volunteer"}`,
			store:      preallocationTestStore(),
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "unknown date",
			body:       `{"date":"2026-02-01","volunteerId":"bob","role":"Service volunteer"}`,
			store:      preallocationTestStore(),
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "duplicate assignee",
			body:       `{"date":"2026-01-11","volunteerId":"bob","role":"Service volunteer"}`,
			store:      seeded(),
			wantStatus: http.StatusConflict,
		},
		{
			name: "already allocated",
			body: `{"date":"2026-01-11","volunteerId":"bob","role":"Service volunteer"}`,
			store: func() *mockStore {
				s := preallocationTestStore()
				s.allocatedRotas = map[string]bool{"rota-1": true}
				return s
			}(),
			wantStatus: http.StatusConflict,
		},
		{
			name: "store insert failure",
			body: `{"date":"2026-01-11","volunteerId":"bob","role":"Service volunteer"}`,
			store: func() *mockStore {
				s := preallocationTestStore()
				s.insertErr = errors.New("disk full")
				return s
			}(),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doRequest(t, newTestHandler(tt.store, activeVolunteers()), http.MethodPost, "/api/preallocations", tt.body, adminCookie())
			assert.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
		})
	}
}

func TestDeletePreallocationEndpoint(t *testing.T) {
	store := preallocationTestStore()
	store.manualPreallocations = []db.ManualPreallocation{
		{ID: "pin-1", ShiftID: "s1", Role: "Service volunteer", VolunteerID: "bob"},
	}

	rec := doRequest(t, newTestHandler(store, activeVolunteers()), http.MethodDelete, "/api/preallocations/pin-1", "", adminCookie())
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, []string{"pin-1"}, store.deletedPreallocationIDs)
}

func TestDeletePreallocationEndpoint_NotFound(t *testing.T) {
	rec := doRequest(t, newTestHandler(preallocationTestStore(), activeVolunteers()), http.MethodDelete, "/api/preallocations/ghost", "", adminCookie())
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestDeletePreallocationEndpoint_FrozenRota(t *testing.T) {
	store := preallocationTestStore()
	store.manualPreallocations = []db.ManualPreallocation{
		{ID: "pin-1", ShiftID: "s1", Role: "Service volunteer", VolunteerID: "bob"},
	}
	store.allocatedRotas = map[string]bool{"rota-1": true}

	rec := doRequest(t, newTestHandler(store, activeVolunteers()), http.MethodDelete, "/api/preallocations/pin-1", "", adminCookie())
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Empty(t, store.deletedPreallocationIDs, "a frozen rota must not delete the pin")
}

func TestListPreallocationsEndpoint(t *testing.T) {
	store := preallocationTestStore()
	store.manualPreallocations = []db.ManualPreallocation{
		{ID: "pin-1", ShiftID: "s1", Role: "Team lead", VolunteerID: "alice"},
		{ID: "pin-2", ShiftID: "s2", Role: "Service volunteer", CustomValue: "External Helper"},
	}

	rec := doRequest(t, newTestHandler(store, activeVolunteers()), http.MethodGet, "/api/preallocations", "", adminCookie())
	require.Equal(t, http.StatusOK, rec.Code)

	resp := decodePreallocations(t, rec.Body.Bytes())
	require.Len(t, resp.Preallocations, 2)

	byID := map[string]preallocationJSON{}
	for _, p := range resp.Preallocations {
		byID[p.ID] = p
	}
	assert.Equal(t, "2026-01-11", byID["pin-1"].Date)
	assert.Equal(t, "Alice", byID["pin-1"].Name)
	assert.Equal(t, "manual", byID["pin-1"].Source)
	assert.Equal(t, "2026-01-18", byID["pin-2"].Date)
	assert.Equal(t, "External Helper", byID["pin-2"].Name)
	assert.Equal(t, "manual", byID["pin-2"].Source)
}

// preallocationJSON is the wire shape of one pin, so tests read the same fields
// a client does.
type preallocationJSON struct {
	ID          string `json:"id"`
	Date        string `json:"date"`
	Role        string `json:"role"`
	VolunteerID string `json:"volunteerId"`
	Custom      string `json:"custom"`
	Name        string `json:"name"`
	Source      string `json:"source"`
}

func decodePreallocations(t *testing.T, body []byte) struct {
	Preallocations []preallocationJSON `json:"preallocations"`
} {
	t.Helper()
	var resp struct {
		Preallocations []preallocationJSON `json:"preallocations"`
	}
	require.NoError(t, json.Unmarshal(body, &resp))
	return resp
}

// The endpoint answers with both sources: a config Rota Override pinning
// someone to 11 January shows up beside the manual pin on 18 January, without
// an id, because there is no row to delete.
func TestListPreallocationsEndpoint_ConfigPins(t *testing.T) {
	store := preallocationTestStore()
	store.manualPreallocations = []db.ManualPreallocation{
		{ID: "pin-1", ShiftID: "s2", Role: "Service volunteer", VolunteerID: "bob"},
	}
	cfg := &config.Config{
		RotaOverrides: []config.RotaOverride{{
			RRule: "FREQ=YEARLY;BYMONTH=1;BYMONTHDAY=11",
			Preallocations: []config.Preallocation{
				{VolunteerID: "alice", Role: "Team lead"},
				{Custom: "Scouts", Role: "Service volunteer"},
			},
		}},
	}

	rec := doRequest(t, newTestHandlerWithConfig(store, activeVolunteers(), cfg), http.MethodGet, "/api/preallocations", "", adminCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	resp := decodePreallocations(t, rec.Body.Bytes())
	require.Len(t, resp.Preallocations, 3)

	assert.Equal(t, preallocationJSON{
		Date:        "2026-01-11",
		Role:        "Team lead",
		VolunteerID: "alice",
		Name:        "Alice",
		Source:      "config",
	}, resp.Preallocations[0])
	assert.Equal(t, preallocationJSON{
		Date:   "2026-01-11",
		Role:   "Service volunteer",
		Custom: "Scouts",
		Name:   "Scouts",
		Source: "config",
	}, resp.Preallocations[1])
	assert.Equal(t, "pin-1", resp.Preallocations[2].ID)
	assert.Equal(t, "manual", resp.Preallocations[2].Source)
}

func TestListPreallocationsEndpoint_DateFilter(t *testing.T) {
	store := preallocationTestStore()
	store.manualPreallocations = []db.ManualPreallocation{
		{ID: "pin-1", ShiftID: "s1", Role: "Service volunteer", VolunteerID: "bob"},
		{ID: "pin-2", ShiftID: "s2", Role: "Service volunteer", VolunteerID: "charlie"},
	}

	rec := doRequest(t, newTestHandler(store, activeVolunteers()), http.MethodGet, "/api/preallocations?from=2026-01-12", "", adminCookie())
	require.Equal(t, http.StatusOK, rec.Code)

	resp := decodePreallocations(t, rec.Body.Bytes())
	require.Len(t, resp.Preallocations, 1)
	assert.Equal(t, "pin-2", resp.Preallocations[0].ID)
}

func TestPreallocationsMethodNotAllowed(t *testing.T) {
	handler := newTestHandler(preallocationTestStore(), activeVolunteers())

	rec := doRequest(t, handler, http.MethodPut, "/api/preallocations", "")
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

// TestPreallocationsRequireAdmin proves all three pin endpoints are gated:
// without a session they are rejected, nothing is persisted or deleted, and the
// listing gives nothing away — it names people against dates the rota has not
// published.
func TestPreallocationsRequireAdmin(t *testing.T) {
	store := preallocationTestStore()
	store.manualPreallocations = []db.ManualPreallocation{
		{ID: "pin-1", ShiftID: "s1", Role: "Service volunteer", VolunteerID: "bob"},
	}
	handler := newTestHandler(store, activeVolunteers())

	rec := doRequest(t, handler, http.MethodPost, "/api/preallocations", `{"date":"2026-01-11","volunteerId":"bob"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, store.insertedPreallocations, "an unauthenticated request must not pin anyone")

	rec = doRequest(t, handler, http.MethodDelete, "/api/preallocations/pin-1", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, store.deletedPreallocationIDs, "an unauthenticated request must not delete a pin")

	rec = doRequest(t, handler, http.MethodGet, "/api/preallocations", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.NotContains(t, rec.Body.String(), "bob", "an unauthenticated request must not read who is pinned")
}
