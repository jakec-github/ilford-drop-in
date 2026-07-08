package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// mockStore implements Store for testing
type mockStore struct {
	rotations   []db.Rotation
	allocations []db.Allocation
	alterations []db.Alteration

	insertedCover       *db.Cover
	insertedAlterations []db.Alteration
	insertErr           error
	getAllocationsErr   error
}

func (m *mockStore) GetRotations(ctx context.Context) ([]db.Rotation, error) {
	return m.rotations, nil
}

func (m *mockStore) GetAllocations(ctx context.Context) ([]db.Allocation, error) {
	return m.allocations, m.getAllocationsErr
}

func (m *mockStore) GetAlterations(ctx context.Context) ([]db.Alteration, error) {
	return m.alterations, nil
}

func (m *mockStore) GetAllocationsByRotaID(ctx context.Context, rotaID string) ([]db.Allocation, error) {
	if m.getAllocationsErr != nil {
		return nil, m.getAllocationsErr
	}
	var filtered []db.Allocation
	for _, a := range m.allocations {
		if a.RotaID == rotaID {
			filtered = append(filtered, a)
		}
	}
	return filtered, nil
}

func (m *mockStore) GetAlterationsByRotaID(ctx context.Context, rotaID string) ([]db.Alteration, error) {
	var filtered []db.Alteration
	for _, a := range m.alterations {
		if a.RotaID == rotaID {
			filtered = append(filtered, a)
		}
	}
	return filtered, nil
}

func (m *mockStore) InsertCoverAndAlterations(ctx context.Context, cover *db.Cover, alterations []db.Alteration) error {
	if m.insertErr != nil {
		return m.insertErr
	}
	m.insertedCover = cover
	m.insertedAlterations = alterations
	return nil
}

// mockVolunteerClient implements services.VolunteerClient for testing
type mockVolunteerClient struct {
	volunteers []model.Volunteer
	err        error
	calls      int
}

func (m *mockVolunteerClient) ListVolunteers(cfg *config.Config) ([]model.Volunteer, error) {
	m.calls++
	return m.volunteers, m.err
}

var apiTestCfg = &config.Config{
	ShiftStartTime: "19:30",
	ShiftEndTime:   "21:30",
}

func testVolunteers() *mockVolunteerClient {
	return &mockVolunteerClient{
		volunteers: []model.Volunteer{
			{ID: "alice", DisplayName: "Alice", Role: model.RoleTeamLead},
			{ID: "bob", DisplayName: "Bob", Role: model.RoleVolunteer},
			{ID: "charlie", DisplayName: "Charlie", Role: model.RoleVolunteer},
		},
	}
}

func newTestHandler(store *mockStore, volunteers *mockVolunteerClient) http.Handler {
	return NewHandler(store, volunteers, apiTestCfg, zap.NewNop()).Routes()
}

func doRequest(t *testing.T, handler http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestListShiftsEndpoint(t *testing.T) {
	store := &mockStore{
		allocations: []db.Allocation{
			{ID: "a1", RotaID: "rota-1", ShiftDate: "2026-01-11", Role: string(model.RoleTeamLead), VolunteerID: "alice"},
			{ID: "a2", RotaID: "rota-1", ShiftDate: "2026-01-11", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
			{ID: "a3", RotaID: "rota-1", ShiftDate: "2026-01-18", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
		},
		alterations: []db.Alteration{
			{ID: "alt1", RotaID: "rota-1", ShiftDate: "2026-01-18", Direction: "remove", VolunteerID: "bob", SetTime: "2026-01-02T10:00:00Z"},
			{ID: "alt2", RotaID: "rota-1", ShiftDate: "2026-01-18", Direction: "add", VolunteerID: "charlie", SetTime: "2026-01-02T10:01:00Z"},
		},
	}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/shifts", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")

	var resp struct {
		Shifts []struct {
			Date      string `json:"date"`
			Start     string `json:"start"`
			End       string `json:"end"`
			Closed    bool   `json:"closed"`
			Assignees []struct {
				VolunteerID string `json:"volunteerId"`
				Name        string `json:"name"`
				Role        string `json:"role"`
			} `json:"assignees"`
		} `json:"shifts"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Shifts, 2)

	first := resp.Shifts[0]
	assert.Equal(t, "2026-01-11", first.Date)
	// 19:30 Europe/London in January is 19:30 UTC
	start, err := time.Parse(time.RFC3339, first.Start)
	require.NoError(t, err)
	assert.Equal(t, "2026-01-11T19:30:00Z", start.UTC().Format(time.RFC3339))
	require.Len(t, first.Assignees, 2)
	assert.Equal(t, "alice", first.Assignees[0].VolunteerID)

	// Alterations applied: bob swapped for charlie on the second shift
	second := resp.Shifts[1]
	require.Len(t, second.Assignees, 1)
	assert.Equal(t, "charlie", second.Assignees[0].VolunteerID)
}

func TestListShiftsEndpoint_DateFilters(t *testing.T) {
	store := &mockStore{
		allocations: []db.Allocation{
			{ID: "a1", ShiftDate: "2026-01-11", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
			{ID: "a2", ShiftDate: "2026-01-18", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
		},
	}
	handler := newTestHandler(store, testVolunteers())

	rec := doRequest(t, handler, http.MethodGet, "/shifts?from=2026-01-12", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Shifts []struct {
			Date string `json:"date"`
		} `json:"shifts"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Shifts, 1)
	assert.Equal(t, "2026-01-18", resp.Shifts[0].Date)

	rec = doRequest(t, handler, http.MethodGet, "/shifts?from=bogus", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestListShiftsEndpoint_StoreError(t *testing.T) {
	store := &mockStore{getAllocationsErr: errors.New("connection refused")}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/shifts", "")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "internal server error")
	assert.NotContains(t, rec.Body.String(), "connection refused")
}

func alterationTestStore() *mockStore {
	return &mockStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2026-01-11", ShiftCount: 2},
		},
		allocations: []db.Allocation{
			{ID: "a1", RotaID: "rota-1", ShiftDate: "2026-01-11", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
		},
	}
}

func TestCreateAlterationEndpoint(t *testing.T) {
	store := alterationTestStore()
	body := `{"date":"2026-01-11","out":"bob","in":"charlie","reason":"Holiday cover","userEmail":"jane@example.com"}`

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPost, "/alterations", body)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	var resp struct {
		CoverID     string `json:"coverId"`
		Alterations []struct {
			Direction   string `json:"direction"`
			VolunteerID string `json:"volunteerId"`
			ShiftDate   string `json:"shiftDate"`
		} `json:"alterations"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp.CoverID)
	require.Len(t, resp.Alterations, 2)

	// Proves ChangeRota persisted through the store
	require.NotNil(t, store.insertedCover)
	assert.Equal(t, "Holiday cover", store.insertedCover.Reason)
	assert.Equal(t, "jane@example.com", store.insertedCover.UserEmail)
	assert.Len(t, store.insertedAlterations, 2)
}

func TestCreateAlterationEndpoint_Errors(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		store      *mockStore
		wantStatus int
	}{
		{
			name:       "malformed JSON",
			body:       `{"date":`,
			store:      alterationTestStore(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown field",
			body:       `{"date":"2026-01-11","out":"bob","reason":"x","userEmail":"a@b.c","bogus":true}`,
			store:      alterationTestStore(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing userEmail",
			body:       `{"date":"2026-01-11","out":"bob","reason":"x"}`,
			store:      alterationTestStore(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing reason",
			body:       `{"date":"2026-01-11","out":"bob","userEmail":"a@b.c"}`,
			store:      alterationTestStore(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown volunteer",
			body:       `{"date":"2026-01-11","in":"nobody","reason":"x","userEmail":"a@b.c"}`,
			store:      alterationTestStore(),
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "volunteer not on shift",
			body:       `{"date":"2026-01-11","out":"charlie","reason":"x","userEmail":"a@b.c"}`,
			store:      alterationTestStore(),
			wantStatus: http.StatusConflict,
		},
		{
			name: "store insert failure",
			store: func() *mockStore {
				s := alterationTestStore()
				s.insertErr = errors.New("disk full")
				return s
			}(),
			body:       `{"date":"2026-01-11","out":"bob","reason":"x","userEmail":"a@b.c"}`,
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doRequest(t, newTestHandler(tt.store, testVolunteers()), http.MethodPost, "/alterations", tt.body)
			assert.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
		})
	}
}

func TestCalendarEndpoint(t *testing.T) {
	store := &mockStore{
		allocations: []db.Allocation{
			{ID: "a1", ShiftDate: "2026-01-11", Role: string(model.RoleTeamLead), VolunteerID: "alice"},
			{ID: "a2", ShiftDate: "2026-01-11", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
			{ID: "a3", ShiftDate: "2026-01-18", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
		},
	}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/calendars/alice.ics", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/calendar")

	body := rec.Body.String()
	assert.Contains(t, body, "UID:alice-2026-01-11@ilford-drop-in")
	assert.Contains(t, body, "SUMMARY:Ilford Drop-In shift (team lead)")
	// Only alice's shifts appear
	assert.NotContains(t, body, "2026-01-18")
}

func TestCalendarEndpoint_NotFound(t *testing.T) {
	handler := newTestHandler(&mockStore{}, testVolunteers())

	rec := doRequest(t, handler, http.MethodGet, "/calendars/nobody.ics", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)

	// Missing .ics suffix
	rec = doRequest(t, handler, http.MethodGet, "/calendars/alice", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestCalendarEndpoint_VolunteerAddedAfterCacheFill(t *testing.T) {
	inner := testVolunteers()
	cached := NewCachingVolunteerClient(inner, time.Hour)
	handler := NewHandler(&mockStore{}, cached, apiTestCfg, zap.NewNop()).Routes()

	// Warm the cache, as unattended calendar polling does continuously
	rec := doRequest(t, handler, http.MethodGet, "/calendars/alice.ics", "")
	require.Equal(t, http.StatusOK, rec.Code)

	// Dana joins after the cache was filled, then requests her calendar
	inner.volunteers = append(inner.volunteers, model.Volunteer{ID: "dana", DisplayName: "Dana", Role: model.RoleVolunteer})
	backdateCacheFill(t, cached, minRefreshInterval)

	rec = doRequest(t, handler, http.MethodGet, "/calendars/dana.ics", "")
	assert.Equal(t, http.StatusOK, rec.Code, "just-added volunteer must trigger a cache refresh, not a 404")

	// A genuinely unknown ID still 404s, and the rate limit stops it from
	// forcing another Sheets fetch
	callsBefore := inner.calls
	rec = doRequest(t, handler, http.MethodGet, "/calendars/nobody.ics", "")
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, callsBefore, inner.calls, "miss within the rate limit must not refetch")
}

func TestCalendarEndpoint_EmptyFeedIsValid(t *testing.T) {
	// Charlie exists but has no shifts
	rec := doRequest(t, newTestHandler(&mockStore{}, testVolunteers()), http.MethodGet, "/calendars/charlie.ics", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "BEGIN:VCALENDAR")
	assert.NotContains(t, rec.Body.String(), "BEGIN:VEVENT")
}

func TestMethodNotAllowed(t *testing.T) {
	handler := newTestHandler(&mockStore{}, testVolunteers())

	rec := doRequest(t, handler, http.MethodDelete, "/shifts", "")
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)

	rec = doRequest(t, handler, http.MethodGet, "/alterations", "")
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestCachingVolunteerClient(t *testing.T) {
	inner := testVolunteers()
	cached := NewCachingVolunteerClient(inner, time.Minute)

	first, err := cached.ListVolunteers(apiTestCfg)
	require.NoError(t, err)
	second, err := cached.ListVolunteers(apiTestCfg)
	require.NoError(t, err)

	assert.Equal(t, first, second)
	assert.Equal(t, 1, inner.calls, "second call within TTL must be served from cache")
}

func TestCachingVolunteerClient_ExpiredTTL(t *testing.T) {
	inner := testVolunteers()
	cached := NewCachingVolunteerClient(inner, time.Nanosecond)

	_, err := cached.ListVolunteers(apiTestCfg)
	require.NoError(t, err)
	time.Sleep(time.Millisecond)
	_, err = cached.ListVolunteers(apiTestCfg)
	require.NoError(t, err)

	assert.Equal(t, 2, inner.calls, "expired cache must refetch")
}

func TestCachingVolunteerClient_RefreshBypassesTTL(t *testing.T) {
	inner := testVolunteers()
	cached := NewCachingVolunteerClient(inner, time.Hour)

	_, err := cached.ListVolunteers(apiTestCfg)
	require.NoError(t, err)

	// A volunteer joins after the cache was filled
	inner.volunteers = append(inner.volunteers, model.Volunteer{ID: "dana", DisplayName: "Dana", Role: model.RoleVolunteer})
	backdateCacheFill(t, cached, minRefreshInterval)

	volunteers, err := cached.(VolunteerRefresher).RefreshVolunteers(apiTestCfg)
	require.NoError(t, err)
	assert.Len(t, volunteers, 4, "refresh must bypass the TTL and see the new volunteer")
	assert.Equal(t, 2, inner.calls)
}

func TestCachingVolunteerClient_RefreshRateLimited(t *testing.T) {
	inner := testVolunteers()
	cached := NewCachingVolunteerClient(inner, time.Hour)

	_, err := cached.ListVolunteers(apiTestCfg)
	require.NoError(t, err)

	volunteers, err := cached.(VolunteerRefresher).RefreshVolunteers(apiTestCfg)
	require.NoError(t, err)
	assert.Len(t, volunteers, 3)
	assert.Equal(t, 1, inner.calls, "refresh just after a fill must be served from cache")
}

// backdateCacheFill ages the cache past the forced-refresh rate limit while
// staying inside the TTL
func backdateCacheFill(t *testing.T, client services.VolunteerClient, age time.Duration) {
	t.Helper()
	c, ok := client.(*cachingVolunteerClient)
	require.True(t, ok)
	c.mu.Lock()
	c.fetchedAt = c.fetchedAt.Add(-age)
	c.mu.Unlock()
}

func TestCachingVolunteerClient_ErrorNotCached(t *testing.T) {
	inner := &mockVolunteerClient{err: errors.New("sheets unavailable")}
	cached := NewCachingVolunteerClient(inner, time.Minute)

	_, err := cached.ListVolunteers(apiTestCfg)
	require.Error(t, err)

	inner.err = nil
	inner.volunteers = []model.Volunteer{{ID: "alice"}}
	volunteers, err := cached.ListVolunteers(apiTestCfg)
	require.NoError(t, err)
	assert.Len(t, volunteers, 1)
}
