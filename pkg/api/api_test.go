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
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// mockStore implements Store for testing
type mockStore struct {
	shifts               []db.Shift
	shiftsInRange        []db.ShiftInRange
	rotations            []db.Rotation
	allocations          []db.Allocation
	alterations          []db.Alteration
	manualPreallocations []db.Preallocation
	// standingPreallocations are the Rota Defaults' Standing Preallocations,
	// which seed ordinary ones when a rota is defined.
	standingPreallocations []db.StandingPreallocation
	availabilityRequests   []db.AvailabilityRequest
	allocatedRotas         map[string]bool

	insertedCover           *db.Cover
	insertedAlterations     []db.Alteration
	insertedPreallocations  []db.Preallocation
	insertedRotations       []db.Rotation
	insertedShifts          []db.Shift
	deletedPreallocationIDs []string
	insertedStanding        []db.StandingPreallocation
	deletedStandingIDs      []string
	// standingWriteErr is what the database says to a Standing Preallocation
	// write — db.ErrDuplicateStandingPreallocation for a promise already made.
	standingWriteErr error
	insertErr        error
	pingErr          error
	getShiftsErr     error
	getRotationsErr  error

	// roles overrides apiTestRoles for a test that cares which Roles exist;
	// rolesErr makes the read fail.
	roles    []db.Role
	rolesErr error

	// rotaDefaults overrides apiTestRotaDefaults for a test that cares what an
	// admin has set — chiefly one about settings nobody has filled in, which is
	// where every deployment starts. rotaDefaultsErr makes the read fail.
	rotaDefaults            *db.RotaDefaults
	rotaDefaultsErr         error
	savedRotaDefaults       []db.RotaDefaults
	savedAllocationSettings []string
	rotaDefaultsWriteErr    error

	insertedRoles []db.Role
	updatedRoles  []db.Role
	// roleWriteErr is what the database says to a Role write —
	// db.ErrDuplicateRoleName for a name already taken, anything else for a
	// failure. roleMissing makes an update report that no row matched.
	roleWriteErr error
	roleMissing  bool
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
		// A synthesised shift carries the times the settings would have minted
		// it with, as a real one does. An explicit shiftsInRange is left
		// exactly as the test wrote it, so a test can still say "untimed".
		startAt, endAt, err := apiTestDefaults.ShiftTimestamps(date)
		if err != nil {
			startAt, endAt = "", ""
		}
		out = append(out, db.ShiftInRange{
			Shift:     db.Shift{ID: id, Date: date, StartAt: startAt, EndAt: endAt},
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

func (m *mockStore) GetRotations(ctx context.Context) ([]db.Rotation, error) {
	if m.getRotationsErr != nil {
		return nil, m.getRotationsErr
	}
	return m.rotations, nil
}

// InsertDefinedRota records the write and makes it visible to subsequent reads,
// deriving the rotation's span from its shifts as the real store does (ADR 0001)
// so a second define lands after the first.
func (m *mockStore) InsertDefinedRota(ctx context.Context, rotation *db.Rotation, shifts []db.Shift, preallocations []db.Preallocation) error {
	if m.insertErr != nil {
		return m.insertErr
	}
	stored := db.Rotation{
		ID:         rotation.ID,
		Start:      shifts[0].Date,
		End:        shifts[len(shifts)-1].Date,
		ShiftCount: len(shifts),
	}
	m.rotations = append(m.rotations, stored)
	m.shifts = append(m.shifts, shifts...)
	m.insertedRotations = append(m.insertedRotations, stored)
	m.insertedShifts = append(m.insertedShifts, shifts...)
	m.manualPreallocations = append(m.manualPreallocations, preallocations...)
	m.insertedPreallocations = append(m.insertedPreallocations, preallocations...)
	return nil
}

func (m *mockStore) GetStandingPreallocations(context.Context) ([]db.StandingPreallocation, error) {
	return m.standingPreallocations, nil
}

func (m *mockStore) InsertStandingPreallocation(_ context.Context, s db.StandingPreallocation) error {
	if m.standingWriteErr != nil {
		return m.standingWriteErr
	}
	m.standingPreallocations = append(m.standingPreallocations, s)
	m.insertedStanding = append(m.insertedStanding, s)
	return nil
}

func (m *mockStore) DeleteStandingPreallocationByID(_ context.Context, id string) (bool, error) {
	for i := range m.standingPreallocations {
		if m.standingPreallocations[i].ID == id {
			m.standingPreallocations = append(m.standingPreallocations[:i], m.standingPreallocations[i+1:]...)
			m.deletedStandingIDs = append(m.deletedStandingIDs, id)
			return true, nil
		}
	}
	return false, nil
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

// GetPreallocationsByShiftIDs returns the pins on the given shifts.
func (m *mockStore) GetPreallocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.Preallocation, error) {
	want := idSet(shiftIDs)
	var filtered []db.Preallocation
	for _, p := range m.manualPreallocations {
		if want[p.ShiftID] {
			filtered = append(filtered, p)
		}
	}
	return filtered, nil
}

// GetPreallocationByID finds a pin and resolves its shift.
func (m *mockStore) GetPreallocationByID(ctx context.Context, id string) (*db.Preallocation, *db.Shift, error) {
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

// GetShiftByID resolves one shift and its rota's allocation state, from the
// same set GetShiftsInRange serves.
func (m *mockStore) GetShiftByID(ctx context.Context, id string) (*db.ShiftInRange, error) {
	for _, s := range m.allShiftsInRange() {
		if s.ID == id {
			return &s, nil
		}
	}
	return nil, nil
}

// SetShiftClosed writes the flag onto whichever backing slice holds the shift,
// so a later listing reads back what a PATCH just wrote.
func (m *mockStore) SetShiftClosed(ctx context.Context, shiftID string, closed bool) (bool, error) {
	found := false
	for i := range m.shiftsInRange {
		if m.shiftsInRange[i].ID == shiftID || (m.shiftsInRange[i].ID == "" && m.shiftsInRange[i].Date == shiftID) {
			m.shiftsInRange[i].Closed = closed
			found = true
		}
	}
	for i := range m.shifts {
		if m.shifts[i].ID == shiftID {
			m.shifts[i].Closed = closed
			found = true
		}
	}
	return found, nil
}

// WithRotaPreallocationLock hands the mock itself to the callback as the
// transaction-bound store; lock semantics are covered by the db and services
// integration tests.
func (m *mockStore) WithRotaPreallocationLock(ctx context.Context, rotaIDs []string, fn func(store db.PreallocationTxStore) error) error {
	return fn(m)
}

// WithRotaShiftLock likewise, for the per-Shift edits.
func (m *mockStore) WithRotaShiftLock(ctx context.Context, rotaIDs []string, fn func(store db.ShiftTxStore) error) error {
	return fn(m)
}

func (m *mockStore) RotaAllocated(ctx context.Context, rotaID string) (bool, error) {
	return m.allocatedRotas[rotaID], nil
}

func (m *mockStore) InsertPreallocation(ctx context.Context, mp db.Preallocation) error {
	if m.insertErr != nil {
		return m.insertErr
	}
	m.manualPreallocations = append(m.manualPreallocations, mp)
	m.insertedPreallocations = append(m.insertedPreallocations, mp)
	return nil
}

func (m *mockStore) DeletePreallocationByID(ctx context.Context, id string) (bool, error) {
	for i := range m.manualPreallocations {
		if m.manualPreallocations[i].ID == id {
			m.manualPreallocations = append(m.manualPreallocations[:i], m.manualPreallocations[i+1:]...)
			m.deletedPreallocationIDs = append(m.deletedPreallocationIDs, id)
			return true, nil
		}
	}
	return false, nil
}

// GetShiftsByRotaID returns a rota's shifts in date order, as the real store does.
func (m *mockStore) GetShiftsByRotaID(ctx context.Context, rotaID string) ([]db.Shift, error) {
	if m.getShiftsErr != nil {
		return nil, m.getShiftsErr
	}
	var out []db.Shift
	for _, s := range m.shifts {
		if s.RotaID == rotaID {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out, nil
}

// The availability round methods are stubs: this store exists to prove the
// handlers wire up and gate correctly, and the round's behaviour is covered
// against real Postgres (availability_integration_test.go) and by the service's
// own in-memory store. Only the token lookup carries state, because the 404 for
// an unknown link is decided here.
func (m *mockStore) MintAvailabilityRequests(ctx context.Context, requests []db.AvailabilityRequest) (int, error) {
	if m.insertErr != nil {
		return 0, m.insertErr
	}
	m.availabilityRequests = append(m.availabilityRequests, requests...)
	return len(requests), nil
}

func (m *mockStore) GetAvailabilityRequestsByRotaID(ctx context.Context, rotaID string) ([]db.AvailabilityRequest, error) {
	var out []db.AvailabilityRequest
	for _, r := range m.availabilityRequests {
		if r.RotaID == rotaID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *mockStore) GetAvailabilityRequestByToken(ctx context.Context, token string) (*db.AvailabilityRequest, error) {
	for i := range m.availabilityRequests {
		if m.availabilityRequests[i].Token == token {
			return &m.availabilityRequests[i], nil
		}
	}
	return nil, nil
}

func (m *mockStore) MarkAvailabilityRequestSent(ctx context.Context, id string) error {
	for i := range m.availabilityRequests {
		if m.availabilityRequests[i].ID == id {
			m.availabilityRequests[i].SentAt = "2026-08-01T09:00:00Z"
			return nil
		}
	}
	return errors.New("no availability request " + id)
}

func (m *mockStore) GetLatestAvailability(ctx context.Context, requestIDs []string, cutoff *time.Time) (map[string]db.AvailabilityGeneration, error) {
	return map[string]db.AvailabilityGeneration{}, nil
}

func (m *mockStore) InsertAvailabilityResponse(ctx context.Context, requestID string, answers []db.ShiftAnswer) (*db.AvailabilityGeneration, error) {
	if m.insertErr != nil {
		return nil, m.insertErr
	}
	return &db.AvailabilityGeneration{RequestID: requestID, ResponseID: "response-1", SubmittedAt: time.Now(), Answers: answers}, nil
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

func (m *mockStore) Ping(ctx context.Context) error {
	return m.pingErr
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

func (m *mockVolunteerClient) ListVolunteers(cfg *config.Config, roles model.Roles) ([]model.Volunteer, error) {
	m.calls++
	return m.volunteers, m.err
}

// The two Roles S1 ships with. Every endpoint that names a Role resolves it
// against the database, so the mock store serves these and a store that serves
// none is not a usable fixture.
var apiTestRoles = []db.Role{
	{ID: "role-team-lead", Name: "Team lead", Max: intPtr(1), Priority: 1, Colour: "violet"},
	{ID: "role-service-volunteer", Name: "Service volunteer", Priority: 2, Colour: "teal"},
}

// ListRoles is on mockStore rather than a fixture of its own: every store
// interface embeds services.RoleStore, so a mock that cannot answer this
// satisfies none of them.
func (m *mockStore) ListRoles(context.Context) ([]db.Role, error) {
	if m.rolesErr != nil {
		return nil, m.rolesErr
	}
	if m.roles != nil {
		return m.roles, nil
	}
	return apiTestRoles, nil
}

func (m *mockStore) InsertRole(_ context.Context, role db.Role) error {
	if m.roleWriteErr != nil {
		return m.roleWriteErr
	}
	m.insertedRoles = append(m.insertedRoles, role)
	return nil
}

func (m *mockStore) UpdateRole(_ context.Context, role db.Role) (bool, error) {
	if m.roleWriteErr != nil {
		return false, m.roleWriteErr
	}
	m.updatedRoles = append(m.updatedRoles, role)
	return !m.roleMissing, nil
}

func (m *mockStore) GetRotaDefaults(context.Context) (db.RotaDefaults, error) {
	if m.rotaDefaultsErr != nil {
		return db.RotaDefaults{}, m.rotaDefaultsErr
	}
	if m.rotaDefaults != nil {
		return *m.rotaDefaults, nil
	}
	return apiTestRotaDefaults, nil
}

func (m *mockStore) SaveRotaDefaults(_ context.Context, defaults db.RotaDefaults) error {
	if m.rotaDefaultsWriteErr != nil {
		return m.rotaDefaultsWriteErr
	}
	m.savedRotaDefaults = append(m.savedRotaDefaults, defaults)
	m.rotaDefaults = &defaults
	return nil
}

func (m *mockStore) SaveAllocationSettings(_ context.Context, settings string) error {
	if m.rotaDefaultsWriteErr != nil {
		return m.rotaDefaultsWriteErr
	}
	m.savedAllocationSettings = append(m.savedAllocationSettings, settings)

	// Saving a section writes through to what the next read sees, as the real
	// store does — the settings screen reloads nothing after a save.
	updated := apiTestRotaDefaults
	if m.rotaDefaults != nil {
		updated = *m.rotaDefaults
	}
	updated.AllocationSettings = settings
	m.rotaDefaults = &updated
	return nil
}

func intPtr(i int) *int { return &i }

// apiTestRotaDefaults is the settings a configured drop-in has: the evening
// session the real one runs. Every endpoint that renders a time reads these, so
// they are the fixture rather than a per-test setup.
var apiTestRotaDefaults = db.RotaDefaults{
	ShiftStartTime: "19:30",
	ShiftEndTime:   "21:30",
	ShiftTimezone:  "Europe/London",
}

// apiTestDefaults is the same settings as the domain reads them, which is what
// mints a synthesised shift's times.
var apiTestDefaults = model.RotaDefaults{
	ShiftStartTime: apiTestRotaDefaults.ShiftStartTime,
	ShiftEndTime:   apiTestRotaDefaults.ShiftEndTime,
	ShiftTimezone:  apiTestRotaDefaults.ShiftTimezone,
}

var apiTestCfg = &config.Config{}

func testVolunteers() *mockVolunteerClient {
	return &mockVolunteerClient{
		volunteers: []model.Volunteer{
			{ID: "alice", FirstName: "Alice", LastName: "Adams", DisplayName: "Alice", Roles: []string{"Team lead", "Service volunteer"}, Status: "Active"},
			{ID: "bob", FirstName: "Bob", LastName: "Barnes", DisplayName: "Bob", Roles: []string{"Service volunteer"}, Status: "Active"},
			{ID: "charlie", FirstName: "Charlie", LastName: "Cole", DisplayName: "Charlie", Roles: []string{"Service volunteer"}, Status: "Active"},
		},
	}
}

// newTestAuthenticator builds an Authenticator with only the fields the
// non-OAuth paths need. oauth2Config and verifier stay nil: the login and
// callback endpoints are exercised via the live round-trip, not these tests.
func newTestAuthenticator() *Authenticator {
	return &Authenticator{
		secret:      testSecret,
		adminEmails: map[string]struct{}{testAdminEmail: {}},
		logger:      zap.NewNop(),
	}
}

// testAdminEmail is the allowlisted admin newTestAuthenticator recognises.
const testAdminEmail = "admin@example.com"

func newTestHandler(store *mockStore, volunteers *mockVolunteerClient) http.Handler {
	return newTestHandlerWithConfig(store, volunteers, apiTestCfg)
}

// newTestHandlerWithConfig is newTestHandler for the endpoints whose answer
// depends on the config — chiefly the rota overrides, which are where config
// preallocations and closed dates come from.
func newTestHandlerWithConfig(store *mockStore, volunteers *mockVolunteerClient, cfg *config.Config) http.Handler {
	return NewHandler(store, volunteers, cfg, newTestAuthenticator(), nil, nil, zap.NewNop()).Routes()
}

// adminCookie is a valid admin session cookie for testAdminEmail, signed with
// the same secret newTestAuthenticator uses, so requests carrying it pass
// requireAdmin on the gated write endpoints.
func adminCookie() *http.Cookie {
	return &http.Cookie{
		Name:  sessionCookieName,
		Value: signSession(testSecret, testAdminEmail, time.Now().Add(time.Hour)),
	}
}

func doRequest(t *testing.T, handler http.Handler, method, target, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestListShiftsEndpoint(t *testing.T) {
	store := &mockStore{
		allocations: []db.Allocation{
			{ID: "a1", ShiftID: "2026-01-11", Role: "Team lead", VolunteerID: "alice"},
			{ID: "a2", ShiftID: "2026-01-11", Role: "Service volunteer", VolunteerID: "bob"},
			{ID: "a3", ShiftID: "2026-01-18", Role: "Service volunteer", VolunteerID: "bob"},
		},
		alterations: []db.Alteration{
			{ID: "alt1", ShiftID: "2026-01-18", Direction: "remove", VolunteerID: "bob", SetTime: "2026-01-02T10:00:00Z"},
			{ID: "alt2", ShiftID: "2026-01-18", Direction: "add", VolunteerID: "charlie", SetTime: "2026-01-02T10:01:00Z"},
		},
	}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/shifts", "")
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

// A Shift carries its own start and end, and the listing renders those rather
// than recomputing the settings against its date (issue #134, ADR 0007). The
// fixture's Shift runs an hour earlier than the current defaults, so a listing
// still reading the settings gets 19:30 and this catches it.
func TestListShiftsEndpointReadsTheShiftsOwnTimes(t *testing.T) {
	store := &mockStore{
		shiftsInRange: []db.ShiftInRange{{
			Shift: db.Shift{
				ID:      "s1",
				Date:    "2026-01-11",
				RotaID:  "rota-1",
				StartAt: "2026-01-11T18:00:00",
				EndAt:   "2026-01-11T20:00:00",
			},
			Allocated: true,
		}},
	}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/shifts", "")
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Shifts []struct {
			Start string `json:"start"`
			End   string `json:"end"`
		} `json:"shifts"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Shifts, 1)

	// Read in Europe/London, where January is UTC+0.
	assert.Equal(t, "2026-01-11T18:00:00Z", resp.Shifts[0].Start)
	assert.Equal(t, "2026-01-11T20:00:00Z", resp.Shifts[0].End)
}

// A Shift minted before an admin set the shift times has none to render, and
// says so by leaving them out. The day is still known, and a rota that names
// the day but not the hour beats one that will not load (ADR 0006).
func TestListShiftsEndpointUntimedShift(t *testing.T) {
	store := &mockStore{
		shiftsInRange: []db.ShiftInRange{{
			Shift:     db.Shift{ID: "s1", Date: "2026-01-11", RotaID: "rota-1"},
			Allocated: true,
		}},
	}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/shifts", "")
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Shifts []struct {
			Date  string `json:"date"`
			Start string `json:"start"`
			End   string `json:"end"`
		} `json:"shifts"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Shifts, 1)

	assert.Equal(t, "2026-01-11", resp.Shifts[0].Date)
	assert.Empty(t, resp.Shifts[0].Start)
	assert.Empty(t, resp.Shifts[0].End)
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
			{ID: "a1", ShiftID: "2026-01-11", Role: "Service volunteer", VolunteerID: "bob"},
		},
	}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/shifts", "")
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
			{ID: "a1", ShiftID: "2026-01-11", Role: "Service volunteer", VolunteerID: "bob"},
			{ID: "a2", ShiftID: "2026-01-18", Role: "Service volunteer", VolunteerID: "bob"},
		},
	}
	handler := newTestHandler(store, testVolunteers())

	rec := doRequest(t, handler, http.MethodGet, "/api/shifts?from=2026-01-12", "")
	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Shifts []struct {
			Date string `json:"date"`
		} `json:"shifts"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Shifts, 1)
	assert.Equal(t, "2026-01-18", resp.Shifts[0].Date)

	rec = doRequest(t, handler, http.MethodGet, "/api/shifts?from=bogus", "")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestListShiftsEndpoint_StoreError(t *testing.T) {
	store := &mockStore{getShiftsErr: errors.New("connection refused")}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/api/shifts", "")
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
			{ID: "a1", ShiftID: "s1", Role: "Service volunteer", VolunteerID: "bob"},
		},
	}
}

func TestCreateAlterationEndpoint(t *testing.T) {
	store := alterationTestStore()
	body := `{"date":"2026-01-11","out":"bob","in":"charlie","role":"Service volunteer","reason":"Holiday cover"}`

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPost, "/api/alterations", body, adminCookie())
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

	// Proves ChangeRota persisted through the store, attributing the change to
	// the verified admin session rather than any client-supplied field.
	require.NotNil(t, store.insertedCover)
	assert.Equal(t, "Holiday cover", store.insertedCover.Reason)
	assert.Equal(t, testAdminEmail, store.insertedCover.UserEmail)
	assert.Len(t, store.insertedAlterations, 2)
}

// TestCreateAlterationEndpoint_Role proves an admin adding someone says which
// Seat they take — here a team lead, where the roster records them only as a
// service volunteer. The roster is advice, not a gate.
func TestCreateAlterationEndpoint_Role(t *testing.T) {
	store := alterationTestStore()
	body := `{"date":"2026-01-11","in":"charlie","role":"Team lead","reason":"Leading tonight"}`

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPost, "/api/alterations", body, adminCookie())
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())

	require.Len(t, store.insertedAlterations, 1)
	assert.Equal(t, "Team lead", store.insertedAlterations[0].Role)
}

// TestCreateAlterationEndpoint_RequiresAdmin proves the write endpoint is gated:
// no session cookie means no attribution to trust, so the request is rejected
// before any change is attempted.
func TestCreateAlterationEndpoint_RequiresAdmin(t *testing.T) {
	store := alterationTestStore()
	body := `{"date":"2026-01-11","out":"bob","in":"charlie","role":"Service volunteer","reason":"Holiday cover"}`

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPost, "/api/alterations", body)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Nil(t, store.insertedCover, "an unauthenticated request must not persist a change")
}

// TestCreateAlterationEndpoint_RejectsClientUserEmail proves the old trusted
// userEmail field is gone: supplying it is now an unknown field, not an actor
// override.
func TestCreateAlterationEndpoint_RejectsClientUserEmail(t *testing.T) {
	store := alterationTestStore()
	body := `{"date":"2026-01-11","out":"bob","reason":"x","userEmail":"attacker@example.com"}`

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodPost, "/api/alterations", body, adminCookie())
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
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
			body:       `{"date":"2026-01-11","out":"bob","reason":"x","bogus":true}`,
			store:      alterationTestStore(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing reason",
			body:       `{"date":"2026-01-11","out":"bob"}`,
			store:      alterationTestStore(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown volunteer",
			body:       `{"date":"2026-01-11","in":"nobody","role":"Service volunteer","reason":"x"}`,
			store:      alterationTestStore(),
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "volunteer not on shift",
			body:       `{"date":"2026-01-11","out":"charlie","reason":"x"}`,
			store:      alterationTestStore(),
			wantStatus: http.StatusConflict,
		},
		{
			name:       "unknown role",
			body:       `{"date":"2026-01-11","in":"charlie","role":"Supervisor","reason":"x"}`,
			store:      alterationTestStore(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "no role for a volunteer coming in",
			body:       `{"date":"2026-01-11","in":"charlie","reason":"x"}`,
			store:      alterationTestStore(),
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "store insert failure",
			store: func() *mockStore {
				s := alterationTestStore()
				s.insertErr = errors.New("disk full")
				return s
			}(),
			body:       `{"date":"2026-01-11","out":"bob","reason":"x"}`,
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doRequest(t, newTestHandler(tt.store, testVolunteers()), http.MethodPost, "/api/alterations", tt.body, adminCookie())
			assert.Equal(t, tt.wantStatus, rec.Code, rec.Body.String())
		})
	}
}

func TestCalendarEndpoint(t *testing.T) {
	store := &mockStore{
		allocations: []db.Allocation{
			{ID: "a1", ShiftID: "2026-01-11", Role: "Team lead", VolunteerID: "alice"},
			{ID: "a2", ShiftID: "2026-01-11", Role: "Service volunteer", VolunteerID: "bob"},
			{ID: "a3", ShiftID: "2026-01-18", Role: "Service volunteer", VolunteerID: "bob"},
		},
	}

	rec := doRequest(t, newTestHandler(store, testVolunteers()), http.MethodGet, "/calendars/alice.ics", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/calendar")

	body := rec.Body.String()
	assert.Contains(t, body, "UID:alice-2026-01-11@ilford-drop-in")
	assert.Contains(t, body, "SUMMARY:Ilford Drop-In shift (Team lead)")
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

func TestCalendarEndpoint_EmptyFeedIsValid(t *testing.T) {
	// Charlie exists but has no shifts
	rec := doRequest(t, newTestHandler(&mockStore{}, testVolunteers()), http.MethodGet, "/calendars/charlie.ics", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "BEGIN:VCALENDAR")
	assert.NotContains(t, rec.Body.String(), "BEGIN:VEVENT")
}

func TestMethodNotAllowed(t *testing.T) {
	handler := newTestHandler(&mockStore{}, testVolunteers())

	rec := doRequest(t, handler, http.MethodDelete, "/api/shifts", "")
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, "GET", rec.Header().Get("Allow"))

	rec = doRequest(t, handler, http.MethodGet, "/api/alterations", "")
	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "application/json")
}

// newFullStackHandler is the handler as it is deployed: API and frontend in one
// process, which is the only configuration where the two namespaces can collide.
func newFullStackHandler(store *mockStore) http.Handler {
	return NewHandler(store, testVolunteers(), apiTestCfg, newTestAuthenticator(), testFrontend, nil, zap.NewNop()).Routes()
}

// TestUnknownAPIPathIsAJSONNotFound: an endpoint that does not exist has to fail
// as an endpoint. Flat on the mux, an unregistered path fell through to the SPA
// and came back as index.html with a 200, so the caller's own JSON parse was the
// first sign anything was wrong.
func TestUnknownAPIPathIsAJSONNotFound(t *testing.T) {
	handler := newFullStackHandler(&mockStore{})

	for _, path := range []string{"/api/nonsense", "/api/shift", "/api/", "/api/volunteers/alice"} {
		rec := doRequest(t, handler, http.MethodGet, path, "")
		require.Equal(t, http.StatusNotFound, rec.Code, path)
		assert.Contains(t, rec.Header().Get("Content-Type"), "application/json", path)

		var body map[string]string
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), path)
		assert.NotEmpty(t, body["error"], path)
	}
}

// TestFrontendOwnsEverythingOutsideTheAPI: with the data endpoints prefixed, the
// SPA can claim any route it likes — including one named after a resource —
// without a hard navigation and a soft one disagreeing about what lives there.
func TestFrontendOwnsEverythingOutsideTheAPI(t *testing.T) {
	handler := newFullStackHandler(&mockStore{})

	for _, path := range []string{"/", "/admin", "/admin/volunteers", "/availability/some-token", "/volunteers"} {
		rec := doRequest(t, handler, http.MethodGet, path, "")
		require.Equal(t, http.StatusOK, rec.Code, path)
		assert.Equal(t, "<html>app</html>", rec.Body.String(), path)
	}
}

// TestUnprefixedRoutesStayPutBehindTheFrontend: three paths deliberately did not
// move — /health is polled by the deploy tooling, /auth is a browser redirect
// flow tied to a registered OAuth URI, and calendar URLs are subscribed to from
// outside the app. Each has to keep answering rather than fall to the SPA.
func TestUnprefixedRoutesStayPutBehindTheFrontend(t *testing.T) {
	store := &mockStore{
		allocations: []db.Allocation{
			{ID: "a1", ShiftID: "2026-01-11", Role: "Team lead", VolunteerID: "alice"},
		},
	}
	handler := newFullStackHandler(store)

	rec := doRequest(t, handler, http.MethodGet, "/health", "")
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"status":"ok"}`, rec.Body.String())

	rec = doRequest(t, handler, http.MethodGet, "/calendars/alice.ics", "")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/calendar")

	rec = doRequest(t, handler, http.MethodGet, "/auth/me", "")
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}
