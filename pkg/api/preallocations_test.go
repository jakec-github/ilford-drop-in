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

// activeVolunteers is testVolunteers with statuses set, since preallocation
// set-time validation requires an active volunteer (unlike alterations).
func activeVolunteers() *mockVolunteerClient {
	return &mockVolunteerClient{
		volunteers: []model.Volunteer{
			{ID: "alice", DisplayName: "Alice", Role: model.RoleTeamLead, Status: "Active"},
			{ID: "bob", DisplayName: "Bob", Role: model.RoleVolunteer, Status: "Active"},
			{ID: "charlie", DisplayName: "Charlie", Role: model.RoleVolunteer, Status: "Active"},
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
	body := `{"date":"2026-01-11","volunteerId":"bob"}`

	rec := doRequest(t, newTestHandler(store, activeVolunteers()), http.MethodPost, "/preallocations", body, adminCookie())
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
	assert.Equal(t, string(model.RoleVolunteer), resp.Role)
	assert.Equal(t, "bob", resp.VolunteerID)

	// Proves the pin persisted through the store, on the right shift.
	require.Len(t, store.insertedPreallocations, 1)
	assert.Equal(t, "s1", store.insertedPreallocations[0].ShiftID)
	assert.Equal(t, "bob", store.insertedPreallocations[0].VolunteerID)
}

func TestCreatePreallocationEndpoint_TeamLead(t *testing.T) {
	store := preallocationTestStore()
	body := `{"date":"2026-01-11","volunteerId":"alice","teamLead":true}`

	rec := doRequest(t, newTestHandler(store, activeVolunteers()), http.MethodPost, "/preallocations", body, adminCookie())
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Len(t, store.insertedPreallocations, 1)
	assert.Equal(t, string(model.RoleTeamLead), store.insertedPreallocations[0].Role)
}

func TestCreatePreallocationEndpoint_Errors(t *testing.T) {
	seeded := func() *mockStore {
		s := preallocationTestStore()
		s.manualPreallocations = []db.ManualPreallocation{
			{ID: "existing", ShiftID: "s1", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
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
			body:       `{"date":"2026-01-11"}`,
			store:      preallocationTestStore(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "both volunteer and custom",
			body:       `{"date":"2026-01-11","volunteerId":"bob","custom":"Helper"}`,
			store:      preallocationTestStore(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "team lead on a non-team-lead volunteer",
			body:       `{"date":"2026-01-11","volunteerId":"bob","teamLead":true}`,
			store:      preallocationTestStore(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown volunteer",
			body:       `{"date":"2026-01-11","volunteerId":"nobody"}`,
			store:      preallocationTestStore(),
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "unknown date",
			body:       `{"date":"2026-02-01","volunteerId":"bob"}`,
			store:      preallocationTestStore(),
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "duplicate assignee",
			body:       `{"date":"2026-01-11","volunteerId":"bob"}`,
			store:      seeded(),
			wantStatus: http.StatusConflict,
		},
		{
			name: "already allocated",
			body: `{"date":"2026-01-11","volunteerId":"bob"}`,
			store: func() *mockStore {
				s := preallocationTestStore()
				s.allocatedRotas = map[string]bool{"rota-1": true}
				return s
			}(),
			wantStatus: http.StatusConflict,
		},
		{
			name: "store insert failure",
			body: `{"date":"2026-01-11","volunteerId":"bob"}`,
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
			rec := doRequest(t, newTestHandler(tt.store, activeVolunteers()), http.MethodPost, "/preallocations", tt.body, adminCookie())
			assert.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
		})
	}
}

func TestDeletePreallocationEndpoint(t *testing.T) {
	store := preallocationTestStore()
	store.manualPreallocations = []db.ManualPreallocation{
		{ID: "pin-1", ShiftID: "s1", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
	}

	rec := doRequest(t, newTestHandler(store, activeVolunteers()), http.MethodDelete, "/preallocations/pin-1", "", adminCookie())
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	assert.Equal(t, []string{"pin-1"}, store.deletedPreallocationIDs)
}

func TestDeletePreallocationEndpoint_NotFound(t *testing.T) {
	rec := doRequest(t, newTestHandler(preallocationTestStore(), activeVolunteers()), http.MethodDelete, "/preallocations/ghost", "", adminCookie())
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestDeletePreallocationEndpoint_FrozenRota(t *testing.T) {
	store := preallocationTestStore()
	store.manualPreallocations = []db.ManualPreallocation{
		{ID: "pin-1", ShiftID: "s1", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
	}
	store.allocatedRotas = map[string]bool{"rota-1": true}

	rec := doRequest(t, newTestHandler(store, activeVolunteers()), http.MethodDelete, "/preallocations/pin-1", "", adminCookie())
	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Empty(t, store.deletedPreallocationIDs, "a frozen rota must not delete the pin")
}

func TestListPreallocationsEndpoint(t *testing.T) {
	store := preallocationTestStore()
	store.manualPreallocations = []db.ManualPreallocation{
		{ID: "pin-1", ShiftID: "s1", Role: string(model.RoleTeamLead), VolunteerID: "alice"},
		{ID: "pin-2", ShiftID: "s2", Role: string(model.RoleVolunteer), CustomValue: "External Helper"},
	}

	rec := doRequest(t, newTestHandler(store, activeVolunteers()), http.MethodGet, "/preallocations", "")
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Preallocations []struct {
			ID          string `json:"id"`
			Date        string `json:"date"`
			Role        string `json:"role"`
			VolunteerID string `json:"volunteerId"`
			Custom      string `json:"custom"`
		} `json:"preallocations"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Preallocations, 2)

	byID := map[string]string{}
	for _, p := range resp.Preallocations {
		byID[p.ID] = p.Date
	}
	assert.Equal(t, "2026-01-11", byID["pin-1"])
	assert.Equal(t, "2026-01-18", byID["pin-2"])
}

func TestListPreallocationsEndpoint_DateFilter(t *testing.T) {
	store := preallocationTestStore()
	store.manualPreallocations = []db.ManualPreallocation{
		{ID: "pin-1", ShiftID: "s1", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
		{ID: "pin-2", ShiftID: "s2", Role: string(model.RoleVolunteer), VolunteerID: "charlie"},
	}

	rec := doRequest(t, newTestHandler(store, activeVolunteers()), http.MethodGet, "/preallocations?from=2026-01-12", "")
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Preallocations []struct {
			ID string `json:"id"`
		} `json:"preallocations"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Preallocations, 1)
	assert.Equal(t, "pin-2", resp.Preallocations[0].ID)
}

func TestPreallocationsMethodNotAllowed(t *testing.T) {
	handler := newTestHandler(preallocationTestStore(), activeVolunteers())

	rec := doRequest(t, handler, http.MethodPut, "/preallocations", "")
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

// TestPreallocationWritesRequireAdmin proves both mutating pin endpoints are
// gated: without a session they are rejected and nothing is persisted or
// deleted. Reads (GET) stay open, so they are not exercised here.
func TestPreallocationWritesRequireAdmin(t *testing.T) {
	store := preallocationTestStore()
	store.manualPreallocations = []db.ManualPreallocation{
		{ID: "pin-1", ShiftID: "s1", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
	}
	handler := newTestHandler(store, activeVolunteers())

	rec := doRequest(t, handler, http.MethodPost, "/preallocations", `{"date":"2026-01-11","volunteerId":"bob"}`)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, store.insertedPreallocations, "an unauthenticated request must not pin anyone")

	rec = doRequest(t, handler, http.MethodDelete, "/preallocations/pin-1", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Empty(t, store.deletedPreallocationIDs, "an unauthenticated request must not delete a pin")
}
