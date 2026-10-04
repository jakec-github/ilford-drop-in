package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/rotasheet"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

const (
	leadID    = "role-team-lead"
	serviceID = "role-service-volunteer"
)

// mockPublishRotaStore implements PublishRotaStore for testing
type mockPublishRotaStore struct {
	testRoleStore

	rotations   []db.Rotation
	shifts      []db.Shift
	allocations []db.Allocation
	alterations []db.Alteration

	// published is the record of what Latest holds, nil when there is none.
	published *rotasheet.Layout
	stale     bool

	saved       []savedLayout
	markedStale bool
}

type savedLayout struct {
	rotaID string
	layout rotasheet.Layout
}

func (m *mockPublishRotaStore) GetRotations(ctx context.Context) ([]db.Rotation, error) {
	return m.rotations, nil
}

func (m *mockPublishRotaStore) GetShiftsByRotaID(ctx context.Context, rotaID string) ([]db.Shift, error) {
	var filtered []db.Shift
	for _, s := range m.shifts {
		if s.RotaID == rotaID {
			filtered = append(filtered, s)
		}
	}
	return filtered, nil
}

func (m *mockPublishRotaStore) GetAllocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.Allocation, error) {
	want := idSet(shiftIDs)
	var filtered []db.Allocation
	for _, a := range m.allocations {
		if want[a.ShiftID] {
			filtered = append(filtered, a)
		}
	}
	return filtered, nil
}

func (m *mockPublishRotaStore) GetAlterationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.Alteration, error) {
	want := idSet(shiftIDs)
	var filtered []db.Alteration
	for _, a := range m.alterations {
		if want[a.ShiftID] {
			filtered = append(filtered, a)
		}
	}
	return filtered, nil
}

func (m *mockPublishRotaStore) GetPublishedRotaSheet(ctx context.Context) ([]byte, bool, error) {
	if m.published == nil {
		return nil, false, nil
	}
	raw, err := json.Marshal(m.published)
	return raw, m.stale, err
}

func (m *mockPublishRotaStore) SavePublishedRotaSheet(ctx context.Context, rotaID string, raw []byte) error {
	var layout rotasheet.Layout
	if err := json.Unmarshal(raw, &layout); err != nil {
		return err
	}
	m.saved = append(m.saved, savedLayout{rotaID: rotaID, layout: layout})
	return nil
}

func (m *mockPublishRotaStore) MarkPublishedRotaSheetStale(ctx context.Context) error {
	m.markedStale = true
	return nil
}

// mockSheetsClient records the publish it was asked to carry out.
type mockSheetsClient struct {
	err              error
	latestWasMissing bool

	applied      []rotasheet.Plan
	archiveTitle string
}

func (m *mockSheetsClient) ApplyRotaPlan(spreadsheetID string, plan rotasheet.Plan, archiveTitle string) (bool, error) {
	m.applied = append(m.applied, plan)
	m.archiveTitle = archiveTitle
	return m.latestWasMissing, m.err
}

func (m *mockSheetsClient) plan(t *testing.T) rotasheet.Plan {
	t.Helper()
	require.Len(t, m.applied, 1, "one publish")
	return m.applied[0]
}

var publishVolunteers = &mockVolClient{volunteers: []model.Volunteer{
	{ID: "alice", FirstName: "Alice", LastName: "Smith", DisplayName: "Alice"},
	{ID: "bob", FirstName: "Bob", LastName: "Jones", DisplayName: "Bob"},
	{ID: "charlie", FirstName: "Charlie", LastName: "Brown", DisplayName: "Charlie"},
	{ID: "dave", FirstName: "Dave", LastName: "Wilson", DisplayName: "Dave"},
}}

// rotaOnTheSheet is one allocated rota of two Sundays: Alice leading the first
// with Charlie and Bob, Dave leading the second.
func rotaOnTheSheet() *mockPublishRotaStore {
	return &mockPublishRotaStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2025-01-05", End: "2025-01-12", ShiftCount: 2, AllocatedDatetime: "2024-12-20T10:00:00Z"},
		},
		shifts: sundayShifts("rota-1", "2025-01-05", 2),
		allocations: []db.Allocation{
			{ID: "a1", ShiftID: "2025-01-05", RoleID: "role-team-lead", VolunteerID: "alice"},
			{ID: "a2", ShiftID: "2025-01-05", RoleID: "role-service-volunteer", VolunteerID: "charlie"},
			{ID: "a3", ShiftID: "2025-01-05", RoleID: "role-service-volunteer", VolunteerID: "bob"},
			{ID: "a4", ShiftID: "2025-01-12", RoleID: "role-team-lead", VolunteerID: "dave"},
		},
	}
}

func publish(t *testing.T, store *mockPublishRotaStore, sheets *mockSheetsClient) error {
	t.Helper()
	_, err := PublishRota(context.Background(), store, sheets, publishVolunteers, &config.Config{RotaSheetID: "sheet"}, zap.NewNop())
	return err
}

// Latest shows who is on each shift: a column per person under their Role,
// alphabetically within it.
func TestPublishRota_LaysOutWhoIsOnEachShift(t *testing.T) {
	sheets := &mockSheetsClient{}
	require.NoError(t, publish(t, rotaOnTheSheet(), sheets))

	sheet := sheets.plan(t).Sheet
	assert.Equal(t, []string{"Date", "Team lead", "Service volunteer 1", "Service volunteer 2"}, sheet.Header)
	assert.Equal(t, [][]string{
		{"Sun Jan 05 2025", "Alice", "Bob", "Charlie"},
		{"Sun Jan 12 2025", "Dave", "", ""},
	}, sheet.Rows)
}

// The sheet shows the rota as it now is: alterations applied, pinned free text
// bracketed, a closed shift saying so, and somebody added before alterations
// had a Role under Unknown role rather than left off.
func TestPublishRota_ShowsTheRotaAsItNowIs(t *testing.T) {
	store := rotaOnTheSheet()
	store.shifts = closeShift(sundayShifts("rota-1", "2025-01-05", 3), "2025-01-19")
	store.allocations = append(store.allocations,
		db.Allocation{ID: "a5", ShiftID: "2025-01-12", RoleID: "role-service-volunteer", CustomEntry: "Rotary Club"},
	)
	store.alterations = []db.Alteration{
		{ID: "x1", ShiftID: "2025-01-05", Direction: "remove", VolunteerID: "charlie"},
		{ID: "x2", ShiftID: "2025-01-05", Direction: "add", VolunteerID: "dave", RoleID: "role-service-volunteer"},
		{ID: "x3", ShiftID: "2025-01-12", Direction: "add", VolunteerID: "bob"},
	}
	sheets := &mockSheetsClient{}

	require.NoError(t, publish(t, store, sheets))

	sheet := sheets.plan(t).Sheet
	assert.Equal(t, []string{"Date", "Team lead", "Service volunteer 1", "Service volunteer 2", "Unknown role"}, sheet.Header)
	assert.Equal(t, [][]string{
		{"Sun Jan 05 2025", "Alice", "Bob", "Dave", ""},
		{"Sun Jan 12 2025", "Dave", "[Rotary Club]", "", "Bob"},
		{"Sun Jan 19 2025", "CLOSED", "", "", ""},
	}, sheet.Rows)
}

// renamedLeadStore is the rota on the sheet after its Team lead was renamed:
// the same Role, read back under its new name.
type renamedLeadStore struct{ *mockPublishRotaStore }

func (renamedLeadStore) ListRoles(context.Context) ([]db.Role, error) {
	return []db.Role{
		{ID: "role-team-lead", Name: "Shift lead", Priority: 1, Colour: "violet"},
		{ID: "role-service-volunteer", Name: "Service volunteer", Priority: 2, Colour: "teal"},
	}, nil
}

// A Role renamed after the rota was allocated keeps its people: they are
// published under its new name, not filed under Unknown role (issue #222).
func TestPublishRota_ARenamedRoleKeepsItsPeople(t *testing.T) {
	sheets := &mockSheetsClient{}
	_, err := PublishRota(context.Background(), renamedLeadStore{rotaOnTheSheet()}, sheets, publishVolunteers, &config.Config{RotaSheetID: "sheet"}, zap.NewNop())
	require.NoError(t, err)

	sheet := sheets.plan(t).Sheet
	assert.Equal(t, []string{"Date", "Shift lead", "Service volunteer 1", "Service volunteer 2"}, sheet.Header)
	assert.Equal(t, []string{"Sun Jan 05 2025", "Alice", "Bob", "Charlie"}, sheet.Rows[0])
}

// A volunteer since removed from the roster still worked the shift. The row
// says somebody was there, and the rest of the rota is published.
func TestPublishRota_ShowsAVolunteerMissingFromTheRosterAsAPlaceholder(t *testing.T) {
	store := rotaOnTheSheet()
	store.allocations = append(store.allocations, db.Allocation{ID: "a9", ShiftID: "2025-01-12", RoleID: "role-service-volunteer", VolunteerID: "long-gone"})
	sheets := &mockSheetsClient{}

	require.NoError(t, publish(t, store, sheets))

	assert.Equal(t, []string{"Sun Jan 12 2025", "Dave", "[unknown volunteer]", ""}, sheets.plan(t).Sheet.Rows[1])
}

// Latest holds the rota allocated most recently, whatever prompted the publish
// and whatever the rotas' dates: never one still being planned, and never an
// older one somebody has just changed.
func TestPublishRota_PublishesTheRotaAllocatedMostRecently(t *testing.T) {
	store := rotaOnTheSheet()
	store.rotations = []db.Rotation{
		{ID: "older", Start: "2024-10-06", End: "2024-12-29", AllocatedDatetime: "2024-09-20T10:00:00Z"},
		{ID: "rota-1", Start: "2025-01-05", End: "2025-01-12", AllocatedDatetime: "2024-12-20T10:00:00Z"},
		{ID: "in-flight", Start: "2025-03-30", End: "2025-06-22"},
	}
	store.shifts = append(store.shifts, sundayShifts("in-flight", "2025-03-30", 2)...)
	sheets := &mockSheetsClient{}

	require.NoError(t, publish(t, store, sheets))

	assert.Equal(t, "rota-1", sheets.plan(t).Sheet.Layout.RotaID)
}

// Until something has been allocated there is nothing to publish, and the sheet
// is left alone.
func TestPublishRota_PublishesNothingBeforeAnythingIsAllocated(t *testing.T) {
	store := rotaOnTheSheet()
	store.rotations[0].AllocatedDatetime = ""
	sheets := &mockSheetsClient{}

	sheet, err := PublishRota(context.Background(), store, sheets, publishVolunteers, &config.Config{}, zap.NewNop())

	require.NoError(t, err)
	assert.Nil(t, sheet)
	assert.Empty(t, sheets.applied)
	assert.Empty(t, store.saved)
}

// What was published is remembered, so the next publish can edit Latest rather
// than rebuild it.
func TestPublishRota_RemembersWhatItPublished(t *testing.T) {
	store := rotaOnTheSheet()
	sheets := &mockSheetsClient{}

	require.NoError(t, publish(t, store, sheets))

	require.Len(t, store.saved, 1)
	assert.Equal(t, "rota-1", store.saved[0].rotaID)
	assert.Equal(t, sheets.plan(t).Sheet.Layout, store.saved[0].layout)
}

// A change to the rota already on Latest edits it in place, from the record of
// what it holds.
func TestPublishRota_EditsTheRotaAlreadyOnLatest(t *testing.T) {
	store := rotaOnTheSheet()
	store.published = &rotasheet.Layout{
		RotaID:   "rota-1",
		ShiftIDs: []string{"2025-01-05", "2025-01-12"},
		Groups:   []rotasheet.Group{{Key: leadID, Width: 1}, {Key: serviceID, Width: 1}},
	}
	sheets := &mockSheetsClient{}

	require.NoError(t, publish(t, store, sheets))

	plan := sheets.plan(t)
	assert.False(t, plan.Archive)
	assert.False(t, plan.Rebuild)
	assert.Equal(t, []rotasheet.Op{{Kind: rotasheet.InsertColumns, Index: 3, Count: 1}}, plan.Ops, "Service volunteer grew a column")
}

// A newly allocated rota archives the one Latest was showing, under that
// rota's dates.
func TestPublishRota_ArchivesTheRotaLatestWasShowing(t *testing.T) {
	store := rotaOnTheSheet()
	store.rotations = append(store.rotations, db.Rotation{ID: "older", Start: "2024-10-06", End: "2024-12-29", AllocatedDatetime: "2024-09-20T10:00:00Z"})
	store.published = &rotasheet.Layout{RotaID: "older"}
	sheets := &mockSheetsClient{}

	require.NoError(t, publish(t, store, sheets))

	assert.True(t, sheets.plan(t).Archive)
	assert.Equal(t, "Oct 06 - Dec 29", sheets.archiveTitle)
}

// The first publish after this was deployed has no record of what Latest holds.
// It is treated as a new rota, and Latest is archived under the rota before
// this one, which is what the command that used to publish left there.
func TestPublishRota_FirstPublishArchivesUnderThePreviousRota(t *testing.T) {
	store := rotaOnTheSheet()
	store.rotations = append(store.rotations,
		db.Rotation{ID: "much-older", Start: "2024-07-07", End: "2024-09-29", AllocatedDatetime: "2024-06-20T10:00:00Z"},
		db.Rotation{ID: "older", Start: "2024-10-06", End: "2024-12-29", AllocatedDatetime: "2024-09-20T10:00:00Z"},
	)
	sheets := &mockSheetsClient{}

	require.NoError(t, publish(t, store, sheets))

	plan := sheets.plan(t)
	assert.True(t, plan.Archive)
	assert.True(t, plan.Rebuild)
	assert.Equal(t, "Oct 06 - Dec 29", sheets.archiveTitle)
}

// A failed publish may still have landed — Google can apply the edits and lose
// the answer — so the record is no longer to be trusted. It is marked, and left
// as it was rather than claiming the publish that failed.
func TestPublishRota_AFailedPublishMarksTheRecordStale(t *testing.T) {
	store := rotaOnTheSheet()
	sheets := &mockSheetsClient{err: errors.New("deadline exceeded")}

	err := publish(t, store, sheets)

	require.Error(t, err)
	assert.True(t, store.markedStale)
	assert.Empty(t, store.saved)
}

// A record gone stale is not edited from: Latest is archived and rebuilt, which
// keeps whatever people typed in the archive and cannot misalign it.
func TestPublishRota_RebuildsFromAStaleRecord(t *testing.T) {
	store := rotaOnTheSheet()
	store.published = &rotasheet.Layout{
		RotaID:   "rota-1",
		ShiftIDs: []string{"2025-01-05", "2025-01-12"},
		Groups:   []rotasheet.Group{{Key: leadID, Width: 1}, {Key: serviceID, Width: 2}},
	}
	store.stale = true
	sheets := &mockSheetsClient{}

	require.NoError(t, publish(t, store, sheets))

	plan := sheets.plan(t)
	assert.True(t, plan.Archive)
	assert.True(t, plan.Rebuild)
	assert.Equal(t, "Jan 05 - Jan 12", sheets.archiveTitle, "archived under the rota the record says Latest holds")
}

// A Latest somebody deleted or renamed is rebuilt, and the warning says so.
func TestPublishRota_WarnsWhenLatestWasMissing(t *testing.T) {
	core, logs := observer.New(zapcore.WarnLevel)
	store := rotaOnTheSheet()
	sheets := &mockSheetsClient{latestWasMissing: true}

	_, err := PublishRota(context.Background(), store, sheets, publishVolunteers, &config.Config{}, zap.New(core))

	require.NoError(t, err)
	assert.Equal(t, 1, logs.FilterMessageSnippet("Latest tab was missing").Len())
	assert.Len(t, store.saved, 1, "what was rebuilt is what Latest now holds")
}

// closeShift marks one of a fixture's shifts closed by date.
func closeShift(shifts []db.Shift, date string) []db.Shift {
	for i := range shifts {
		if shifts[i].Date == date {
			shifts[i].Closed = true
		}
	}
	return shifts
}

// An empty roster is a server whose roster has not synced yet, not a drop-in
// with nobody in it. Publishing then would name everybody on the sheet as
// unknown, so it refuses and leaves the sheet as it was.
func TestPublishRota_RefusesBeforeTheRosterHasLoaded(t *testing.T) {
	store := rotaOnTheSheet()
	sheets := &mockSheetsClient{}

	_, err := PublishRota(context.Background(), store, sheets, &mockVolClient{}, &config.Config{}, zap.NewNop())

	require.ErrorContains(t, err, "roster")
	assert.Empty(t, sheets.applied)
	assert.False(t, store.markedStale, "nothing was sent, so the record still describes Latest")
}
