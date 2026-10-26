package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// A store that holds the settings record in memory, so a test can assert on
// what reached the database as well as on what came back.
type stubRotaDefaultsStore struct {
	defaults        db.RotaDefaults
	readErr         error
	writeErr        error
	saved           []db.RotaDefaults
	savedAllocation []string
}

func (s *stubRotaDefaultsStore) UpdateAllocationSettings(_ context.Context, edit func(string) (string, error)) error {
	if s.writeErr != nil {
		return s.writeErr
	}
	next, err := edit(s.defaults.AllocationSettings)
	if err != nil {
		return err
	}
	s.savedAllocation = append(s.savedAllocation, next)
	s.defaults.AllocationSettings = next
	return nil
}

func (s *stubRotaDefaultsStore) GetRotaDefaults(context.Context) (db.RotaDefaults, error) {
	if s.readErr != nil {
		return db.RotaDefaults{}, s.readErr
	}
	return s.defaults, nil
}

func (s *stubRotaDefaultsStore) SaveRotaDefaults(_ context.Context, defaults db.RotaDefaults) error {
	if s.writeErr != nil {
		return s.writeErr
	}
	s.saved = append(s.saved, defaults)
	s.defaults = defaults
	return nil
}

func shiftTimeParams() ShiftTimeParams {
	return ShiftTimeParams{Start: "19:30", End: "21:30", Timezone: "Europe/London"}
}

// A deployment nobody has configured reads as empty settings, not as a failure:
// it is where every deployment starts, and the pages that only render still
// have to render there.
func TestRotaDefaultsUnset(t *testing.T) {
	store := &stubRotaDefaultsStore{}

	defaults, err := RotaDefaults(context.Background(), store)
	require.NoError(t, err)
	assert.Equal(t, model.RotaDefaults{}, defaults)
	assert.False(t, defaults.HasShiftTimes())
}

func TestRotaDefaultsReadsTheRecord(t *testing.T) {
	store := &stubRotaDefaultsStore{defaults: db.RotaDefaults{
		ShiftStartTime: "19:30", ShiftEndTime: "21:30", ShiftTimezone: "Europe/London",
	}}

	defaults, err := RotaDefaults(context.Background(), store)
	require.NoError(t, err)
	assert.Equal(t, model.RotaDefaults{
		ShiftStartTime: "19:30", ShiftEndTime: "21:30", ShiftTimezone: "Europe/London",
	}, defaults)
}

func TestSaveShiftTimeDefaults(t *testing.T) {
	store := &stubRotaDefaultsStore{}

	saved, err := SaveShiftTimeDefaults(context.Background(), store, shiftTimeParams(), zap.NewNop())
	require.NoError(t, err)

	require.Len(t, store.saved, 1)
	assert.Equal(t, db.RotaDefaults{
		ShiftStartTime: "19:30", ShiftEndTime: "21:30", ShiftTimezone: "Europe/London",
	}, store.saved[0])
	assert.Equal(t, "19:30", saved.ShiftStartTime)
	assert.True(t, saved.HasShiftTimes())
}

// An Organiser who never touches the zone field still saves settings that compute a
// time, rather than settings that fall back for the rest of their life.
func TestSaveShiftTimeDefaultsFillsInTheZone(t *testing.T) {
	store := &stubRotaDefaultsStore{}
	params := shiftTimeParams()
	params.Timezone = ""

	saved, err := SaveShiftTimeDefaults(context.Background(), store, params, zap.NewNop())
	require.NoError(t, err)
	assert.Equal(t, model.DefaultShiftTimezone, saved.ShiftTimezone)
	assert.Equal(t, model.DefaultShiftTimezone, store.saved[0].ShiftTimezone)
}

// A shift ends the evening it starts, so a session running past midnight is
// refused rather than stored as one ending before it began.
func TestSaveShiftTimeDefaultsRefusesEndBeforeStart(t *testing.T) {
	store := &stubRotaDefaultsStore{}
	params := shiftTimeParams()
	params.Start, params.End = "22:00", "00:30"

	_, err := SaveShiftTimeDefaults(context.Background(), store, params, zap.NewNop())
	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Empty(t, store.saved)
}

func TestSaveShiftTimeDefaultsRefusesEqualTimes(t *testing.T) {
	store := &stubRotaDefaultsStore{}
	params := shiftTimeParams()
	params.End = params.Start

	_, err := SaveShiftTimeDefaults(context.Background(), store, params, zap.NewNop())
	require.ErrorIs(t, err, ErrInvalidInput)
}

// Every way of getting a time wrong is an ordinary mistake with a message, not
// a failure: an Organiser is typing into a form.
func TestSaveShiftTimeDefaultsRefusesBadInput(t *testing.T) {
	cases := map[string]ShiftTimeParams{
		"no start":         {Start: "", End: "21:30"},
		"no end":           {Start: "19:30", End: ""},
		"not a time":       {Start: "half seven", End: "21:30"},
		"twelve hour":      {Start: "7:30pm", End: "21:30"},
		"with seconds":     {Start: "19:30:00", End: "21:30"},
		"unknown timezone": {Start: "19:30", End: "21:30", Timezone: "Not/AZone"},
	}

	for name, params := range cases {
		t.Run(name, func(t *testing.T) {
			store := &stubRotaDefaultsStore{}

			_, err := SaveShiftTimeDefaults(context.Background(), store, params, zap.NewNop())
			require.ErrorIs(t, err, ErrInvalidInput)
			assert.Empty(t, store.saved)
		})
	}
}

// A write the database refuses is a failure rather than an Organiser's mistake, and
// stays one all the way out — the API answers 500, not 400.
func TestSaveShiftTimeDefaultsSurfacesAWriteFailure(t *testing.T) {
	store := &stubRotaDefaultsStore{writeErr: errors.New("connection refused")}

	_, err := SaveShiftTimeDefaults(context.Background(), store, shiftTimeParams(), zap.NewNop())
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrInvalidInput)
}

// The rules are stored as a JSON document, and reading them is what turns it
// back into rules the domain can act on (ADR 0012).
func TestRotaDefaultsReadsTheAllocationRules(t *testing.T) {
	store := &stubRotaDefaultsStore{defaults: db.RotaDefaults{
		AllocationSettings: `{"rules":[{"id":"r1","kind":"shift_gap","value":2,"scope":{"exclude":["alice"]},"note":"why"}]}`,
	}}

	defaults, err := RotaDefaults(context.Background(), store)
	require.NoError(t, err)

	assert.Equal(t, []model.AllocationRule{{
		ID: "r1", Kind: "shift_gap", Value: 2, Scope: model.Scope{Exclude: []string{"alice"}}, Note: "why",
	}}, defaults.AllocationSettings.Rules)
}

// A document this build cannot read is not allowed to take the settings screen
// down with it: it reads as no rules (ADR 0006 — never an error).
func TestRotaDefaultsToleratesUnreadableAllocationSettings(t *testing.T) {
	store := &stubRotaDefaultsStore{defaults: db.RotaDefaults{
		ShiftStartTime:     "19:30",
		AllocationSettings: `{"rules":"all of them"}`,
	}}

	defaults, err := RotaDefaults(context.Background(), store)
	require.NoError(t, err)

	assert.Empty(t, defaults.AllocationSettings.Rules)
	assert.Equal(t, "19:30", defaults.ShiftStartTime, "the rest of the record still reads")
}

func storedRules(t *testing.T, store *stubRotaDefaultsStore) []model.AllocationRule {
	t.Helper()
	defaults, err := RotaDefaults(context.Background(), store)
	require.NoError(t, err)
	return defaults.AllocationSettings.Rules
}

// Adding a rule mints its id, stores it cleaned up, and hands back what was
// stored rather than what was typed.
func TestAddAllocationRule(t *testing.T) {
	store := &stubRotaDefaultsStore{}

	added, err := AddAllocationRule(context.Background(), store, model.AllocationRule{
		Kind: "shift_gap", Value: 1, Scope: model.Scope{Exclude: []string{" alice "}}, Note: " rest ",
	}, zap.NewNop())
	require.NoError(t, err)

	assert.NotEmpty(t, added.ID)
	assert.Equal(t, []string{"alice"}, added.Scope.Exclude)
	assert.Equal(t, "rest", added.Note)
	assert.Equal(t, []model.AllocationRule{added}, storedRules(t, store))
}

// Any kind may be added any number of times, Shift Rules included.
func TestAKindCanBeAddedMoreThanOnce(t *testing.T) {
	store := &stubRotaDefaultsStore{}
	for range 2 {
		_, err := AddAllocationRule(context.Background(), store, model.AllocationRule{Kind: "male_required"}, zap.NewNop())
		require.NoError(t, err)
	}

	rules := storedRules(t, store)
	require.Len(t, rules, 2)
	assert.NotEqual(t, rules[0].ID, rules[1].ID)
}

func TestAddAllocationRuleRefusesWhatCannotBeRepresented(t *testing.T) {
	store := &stubRotaDefaultsStore{}

	_, err := AddAllocationRule(context.Background(), store, model.AllocationRule{
		Kind: "monthly_cap", Value: 1,
		Scope: model.Scope{Include: []string{"alice"}, Exclude: []string{"bob"}},
	}, zap.NewNop())

	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Empty(t, store.savedAllocation)
}

// Editing changes a rule's value, Scope and note in place. Its id and kind
// are what it is, so they stay.
func TestEditAllocationRule(t *testing.T) {
	store := &stubRotaDefaultsStore{}
	added, err := AddAllocationRule(context.Background(), store, model.AllocationRule{Kind: "shift_gap", Value: 1}, zap.NewNop())
	require.NoError(t, err)
	other, err := AddAllocationRule(context.Background(), store, model.AllocationRule{Kind: "male_required"}, zap.NewNop())
	require.NoError(t, err)

	edited, err := EditAllocationRule(context.Background(), store, added.ID, model.AllocationRule{
		Kind: "monthly_cap", Value: 3, Scope: model.Scope{Include: []string{"bob"}}, Note: "asked",
	}, zap.NewNop())
	require.NoError(t, err)

	want := model.AllocationRule{
		ID: added.ID, Kind: "shift_gap", Value: 3, Scope: model.Scope{Include: []string{"bob"}}, Note: "asked",
	}
	assert.Equal(t, want, edited)
	assert.Equal(t, []model.AllocationRule{want, other}, storedRules(t, store), "in place, the order kept")
}

func TestEditAllocationRuleRefusesAnInvalidEdit(t *testing.T) {
	store := &stubRotaDefaultsStore{}
	added, err := AddAllocationRule(context.Background(), store, model.AllocationRule{Kind: "shift_gap", Value: 1}, zap.NewNop())
	require.NoError(t, err)

	_, err = EditAllocationRule(context.Background(), store, added.ID, model.AllocationRule{Value: 0}, zap.NewNop())

	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Equal(t, []model.AllocationRule{added}, storedRules(t, store))
}

func TestEditingOrRemovingAMissingRuleIsNotFound(t *testing.T) {
	store := &stubRotaDefaultsStore{}

	_, err := EditAllocationRule(context.Background(), store, "nope", model.AllocationRule{Value: 1}, zap.NewNop())
	require.ErrorIs(t, err, ErrNotFound)

	err = RemoveAllocationRule(context.Background(), store, "nope", zap.NewNop())
	require.ErrorIs(t, err, ErrNotFound)
}

func TestRemoveAllocationRule(t *testing.T) {
	store := &stubRotaDefaultsStore{}
	first, err := AddAllocationRule(context.Background(), store, model.AllocationRule{Kind: "male_required"}, zap.NewNop())
	require.NoError(t, err)
	second, err := AddAllocationRule(context.Background(), store, model.AllocationRule{Kind: "shift_gap", Value: 2}, zap.NewNop())
	require.NoError(t, err)

	require.NoError(t, RemoveAllocationRule(context.Background(), store, first.ID, zap.NewNop()))

	assert.Equal(t, []model.AllocationRule{second}, storedRules(t, store))
}

// What this build does not understand is left exactly as it was: a rule of a
// kind it does not know, a field on a rule it does not know, and anything
// else in the document. Editing one rule must not lose what a newer build
// wrote beside it (ADR 0006).
func TestEditsLeaveWhatThisBuildDoesNotUnderstandAlone(t *testing.T) {
	store := &stubRotaDefaultsStore{defaults: db.RotaDefaults{
		AllocationSettings: `{"weights":{"spread_males":3},"rules":[` +
			`{"id":"future","kind":"phase_of_the_moon","value":3,"colour":"blue"},` +
			`{"id":"gap","kind":"shift_gap","value":1,"scope":{}}]}`,
	}}

	_, err := EditAllocationRule(context.Background(), store, "gap", model.AllocationRule{Value: 2}, zap.NewNop())
	require.NoError(t, err)
	_, err = AddAllocationRule(context.Background(), store, model.AllocationRule{Kind: "male_required"}, zap.NewNop())
	require.NoError(t, err)

	var doc struct {
		Weights json.RawMessage   `json:"weights"`
		Rules   []json.RawMessage `json:"rules"`
	}
	require.NoError(t, json.Unmarshal([]byte(store.defaults.AllocationSettings), &doc))
	assert.JSONEq(t, `{"spread_males":3}`, string(doc.Weights))
	require.Len(t, doc.Rules, 3)
	assert.JSONEq(t, `{"id":"future","kind":"phase_of_the_moon","value":3,"colour":"blue"}`, string(doc.Rules[0]))
}

// A stored rule this build cannot apply is not refused at allocation: it is
// left out, and the solve goes ahead (ADR 0006).
func TestSettingsForAllocationIgnoresRulesItCannotApply(t *testing.T) {
	store := &stubRotaDefaultsStore{defaults: db.RotaDefaults{
		ShiftStartTime:     "19:30",
		ShiftEndTime:       "21:30",
		AllocationSettings: `{"rules":[{"id":"a","kind":"phase_of_the_moon"},{"id":"b","kind":"shift_gap","value":0}]}`,
	}}

	defaults, err := settingsForAllocation(context.Background(), store, zap.NewNop())
	require.NoError(t, err)
	assert.Empty(t, defaults.AllocationSettings.Applicable())
}
