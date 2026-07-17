package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
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
	shifts               []db.Shift
	shiftsInRange        []db.ShiftInRange
	allocations          []db.Allocation
	alterations          []db.Alteration
	manualPreallocations []db.ManualPreallocation
	allocatedRotas       map[string]bool

	insertedCover           *db.Cover
	insertedAlterations     []db.Alteration
	insertedPreallocations  []db.ManualPreallocation
	deletedPreallocationIDs []string
	insertErr               error
	getShiftsErr            error
}

// allShiftsInRange is the canonical shift set the store would hold, each with an
// id. Explicit shiftsInRange without an id default to date-as-id; otherwise one
// allocated shift is synthesised per distinct allocation (or shift) shift id, so
// tests that only populate allocations keep enumerating the same shifts.
// Fixtures use the date string as the shift id, so a synthesised shift's id
// doubles as its date.
func (m *mockStore) allShiftsInRange() []db.ShiftInRange {
	if m.shiftsInRange != nil {
		out := make([]db.ShiftInRange, len(m.shiftsInRange))
		for i, s := range m.shiftsInRange {
			if s.ID == "" {
				s.ID = s.Date
			}
			out[i] = s
		}
		return out
	}

	seen := make(map[string]bool)
	var out []db.ShiftInRange
	add := func(id, date string) {
		if id == "" {
			id = date
		}
		if seen[id] {
			return
		}
		seen[id] = true
		out = append(out, db.ShiftInRange{
			Shift:     db.Shift{ID: id, Date: date},
			Allocated: true,
		})
	}
	for _, s := range m.shifts {
		add(s.ID, s.Date)
	}
	for _, a := range m.allocations {
		add(a.ShiftID, a.ShiftID)
	}
	return out
}

// GetShiftsInRange returns the minted shifts in range.
func (m *mockStore) GetShiftsInRange(ctx context.Context, from, to time.Time) ([]db.ShiftInRange, error) {
	if m.getShiftsErr != nil {
		return nil, m.getShiftsErr
	}
	var filtered []db.ShiftInRange
	for _, s := range m.allShiftsInRange() {
		if shiftDateInRange(s.Date, from, to) {
			filtered = append(filtered, s)
		}
	}
	// Mirror the DB's ORDER BY date: production trusts this ordering.
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].Date < filtered[j].Date })
	return filtered, nil
}

func (m *mockStore) GetAllocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.Allocation, error) {
	want := idSet(shiftIDs)
	var filtered []db.Allocation
	for _, a := range m.allocations {
		if want[a.ShiftID] {
			filtered = append(filtered, a)
		}
	}
	return filtered, nil
}

func (m *mockStore) GetAlterationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.Alteration, error) {
	want := idSet(shiftIDs)
	var filtered []db.Alteration
	for _, a := range m.alterations {
		if want[a.ShiftID] {
			filtered = append(filtered, a)
		}
	}
	return filtered, nil
}

func (m *mockStore) GetShiftByDate(ctx context.Context, date time.Time) (*db.Shift, error) {
	dateStr := date.Format("2006-01-02")
	for i := range m.shifts {
		if m.shifts[i].Date == dateStr {
			return &m.shifts[i], nil
		}
	}
	return nil, nil
}

// GetManualPreallocationsByShiftIDs returns the pins on the given shifts.
func (m *mockStore) GetManualPreallocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.ManualPreallocation, error) {
	want := idSet(shiftIDs)
	var filtered []db.ManualPreallocation
	for _, p := range m.manualPreallocations {
		if want[p.ShiftID] {
			filtered = append(filtered, p)
		}
	}
	return filtered, nil
}

// GetManualPreallocationByID finds a pin and resolves its shift.
func (m *mockStore) GetManualPreallocationByID(ctx context.Context, id string) (*db.ManualPreallocation, *db.Shift, error) {
	for i := range m.manualPreallocations {
		if m.manualPreallocations[i].ID != id {
			continue
		}
		p := m.manualPreallocations[i]
		for j := range m.shifts {
			if m.shifts[j].ID == p.ShiftID {
				return &p, &m.shifts[j], nil
			}
		}
		return &p, nil, nil
	}
	return nil, nil, nil
}

// WithRotaPreallocationLock hands the mock itself to the callback as the
// transaction-bound store; lock semantics are covered by the db and services
// integration tests.
func (m *mockStore) WithRotaPreallocationLock(ctx context.Context, rotaIDs []string, fn func(store db.PreallocationTxStore) error) error {
	return fn(m)
}

func (m *mockStore) RotaAllocated(ctx context.Context, rotaID string) (bool, error) {
	return m.allocatedRotas[rotaID], nil
}

func (m *mockStore) InsertManualPreallocation(ctx context.Context, mp db.ManualPreallocation) error {
	if m.insertErr != nil {
		return m.insertErr
	}
	m.manualPreallocations = append(m.manualPreallocations, mp)
	m.insertedPreallocations = append(m.insertedPreallocations, mp)
	return nil
}

func (m *mockStore) DeleteManualPreallocationByID(ctx context.Context, id string) (bool, error) {
	for i := range m.manualPreallocations {
		if m.manualPreallocations[i].ID == id {
			m.manualPreallocations = append(m.manualPreallocations[:i], m.manualPreallocations[i+1:]...)
			m.deletedPreallocationIDs = append(m.deletedPreallocationIDs, id)
			return true, nil
		}
	}
	return false, nil
}

// idSet turns a shift id slice into a lookup set.
func idSet(ids []string) map[string]bool {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}

// shiftDateInRange mimics the DB's inclusive shift_date bounds, with zero
// times leaving the corresponding bound open
func shiftDateInRange(dateStr string, from, to time.Time) bool {
	if !from.IsZero() && dateStr < from.Format("2006-01-02") {
		return false
	}
	if !to.IsZero() && dateStr > to.Format("2006-01-02") {
		return false
	}
	return true
}

// WithRotaLock hands the mock itself to the callback as the transaction-bound
// store; lock semantics are covered by the db and services integration tests.
func (m *mockStore) WithRotaLock(ctx context.Context, rotaIDs []string, fn func(store db.RotaChangeStore) error) error {
	return fn(m)
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
			{ID: "a1", ShiftID: "2026-01-11", Role: string(model.RoleTeamLead), VolunteerID: "alice"},
			{ID: "a2", ShiftID: "2026-01-11", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
			{ID: "a3", ShiftID: "2026-01-18", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
		},
		alterations: []db.Alteration{
			{ID: "alt1", ShiftID: "2026-01-18", Direction: "remove", VolunteerID: "bob", SetTime: "2026-01-02T10:00:00Z"},
			{ID: "alt2", ShiftID: "2026-01-18", Direction: "add", VolunteerID: "charlie", SetTime: "2026-01-02T10:01:00Z"},
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
			Allocated bool   `json:"allocated"`
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
	assert.True(t, first.Allocated)
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

func TestListShiftsEndpoint_UnallocatedShift(t *testing.T) {
	// rota-2's shift is minted but its rota is unallocated; the endpoint must
	// surface it with allocated=false and no assignees.
	store := &mockStore{
		shiftsInRange: []db.ShiftInRange{
			{Shift: db.Shift{Date: "2026-01-11", RotaID: "rota-1"}, Allocated: true},
			{Shift: db.Shift{Date: "2026-01-18", RotaID: "rota-2"}, Allocated: false},
		},
		allocations: []db.Allocation{
			{ID: "a1", ShiftID: "2026-01-11", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
		},
	}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/shifts", "")
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Shifts []struct {
			Date      string `json:"date"`
			Allocated bool   `json:"allocated"`
			Assignees []struct {
				VolunteerID string `json:"volunteerId"`
			} `json:"assignees"`
		} `json:"shifts"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Shifts, 2)

	assert.Equal(t, "2026-01-11", resp.Shifts[0].Date)
	assert.True(t, resp.Shifts[0].Allocated)
	require.Len(t, resp.Shifts[0].Assignees, 1)

	assert.Equal(t, "2026-01-18", resp.Shifts[1].Date)
	assert.False(t, resp.Shifts[1].Allocated)
	assert.Empty(t, resp.Shifts[1].Assignees)
}

func TestListShiftsEndpoint_DateFilters(t *testing.T) {
	store := &mockStore{
		allocations: []db.Allocation{
			{ID: "a1", ShiftID: "2026-01-11", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
			{ID: "a2", ShiftID: "2026-01-18", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
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
	store := &mockStore{getShiftsErr: errors.New("connection refused")}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/shifts", "")
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "internal server error")
	assert.NotContains(t, rec.Body.String(), "connection refused")
}

func alterationTestStore() *mockStore {
	return &mockStore{
		shifts: []db.Shift{
			{ID: "s1", RotaID: "rota-1", Date: "2026-01-11"},
			{ID: "s2", RotaID: "rota-1", Date: "2026-01-18"},
		},
		allocations: []db.Allocation{
			{ID: "a1", ShiftID: "s1", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
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
			{ID: "a1", ShiftID: "2026-01-11", Role: string(model.RoleTeamLead), VolunteerID: "alice"},
			{ID: "a2", ShiftID: "2026-01-11", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
			{ID: "a3", ShiftID: "2026-01-18", Role: string(model.RoleVolunteer), VolunteerID: "bob"},
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
