package services

import (
	"context"
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

func (s *stubRotaDefaultsStore) SaveAllocationSettings(_ context.Context, settings string) error {
	if s.writeErr != nil {
		return s.writeErr
	}
	s.savedAllocation = append(s.savedAllocation, settings)
	s.defaults.AllocationSettings = settings
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

// An admin who never touches the zone field still saves settings that compute a
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
// a failure: an admin is typing into a form.
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

// A write the database refuses is a failure rather than an admin's mistake, and
// stays one all the way out — the API answers 500, not 400.
func TestSaveShiftTimeDefaultsSurfacesAWriteFailure(t *testing.T) {
	store := &stubRotaDefaultsStore{writeErr: errors.New("connection refused")}

	_, err := SaveShiftTimeDefaults(context.Background(), store, shiftTimeParams(), zap.NewNop())
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrInvalidInput)
}

// The allocation settings are stored as a JSON document, and reading them is
// what turns it back into answers the domain can act on (issue #130).
func TestRotaDefaultsReadsTheAllocationSettings(t *testing.T) {
	store := &stubRotaDefaultsStore{defaults: db.RotaDefaults{
		AllocationSettings: `{"enabled":{"no_back_to_back":true,"male_required":false},"maxFrequency":0.34}`,
	}}

	defaults, err := RotaDefaults(context.Background(), store)
	require.NoError(t, err)

	assert.Equal(t, []string{"no_back_to_back"}, defaults.AllocationSettings.EnabledConstraints())
	assert.Equal(t, 0.34, defaults.AllocationSettings.MaxFrequency)
}

// A document this build cannot read is not allowed to take the settings screen
// down with it: it reads as no answers, which is a state the screen renders and
// an admin can fix by saving the section (ADR 0006 — never an error).
func TestRotaDefaultsToleratesUnreadableAllocationSettings(t *testing.T) {
	store := &stubRotaDefaultsStore{defaults: db.RotaDefaults{
		ShiftStartTime:     "19:30",
		AllocationSettings: `{"enabled":"all of them"}`,
	}}

	defaults, err := RotaDefaults(context.Background(), store)
	require.NoError(t, err)

	assert.Empty(t, defaults.AllocationSettings.EnabledConstraints())
	assert.Equal(t, "19:30", defaults.ShiftStartTime, "the rest of the record still reads")
}

// Saving writes the answers as a document and reports what now stands, so the
// screen shows what was stored rather than what was typed.
func TestSaveAllocationSettings(t *testing.T) {
	store := &stubRotaDefaultsStore{}

	settings, err := SaveAllocationSettings(context.Background(), store, AllocationSettingsParams{
		Enabled:      map[string]bool{"max_frequency": true, "no_back_to_back": true},
		MaxFrequency: 0.5,
	}, zap.NewNop())
	require.NoError(t, err)

	assert.Equal(t, []string{"max_frequency", "no_back_to_back"}, settings.EnabledConstraints())
	assert.Equal(t, 0.5, settings.MaxFrequency)

	require.Len(t, store.savedAllocation, 1)
	assert.JSONEq(t,
		`{"enabled":{"max_frequency":true,"no_back_to_back":true},"maxFrequency":0.5}`,
		store.savedAllocation[0])
}

// An answer for a rule this build does not have is not stored. It would mean
// nothing to anything that later read it, and the reply says what was kept so
// a client working from an older list can see what happened.
func TestSaveAllocationSettingsDropsUnknownRules(t *testing.T) {
	store := &stubRotaDefaultsStore{}

	settings, err := SaveAllocationSettings(context.Background(), store, AllocationSettingsParams{
		Enabled: map[string]bool{"male_required": true, "phase_of_the_moon": true},
	}, zap.NewNop())
	require.NoError(t, err)

	assert.Equal(t, []string{"male_required"}, settings.EnabledConstraints())
	require.Len(t, store.savedAllocation, 1)
	assert.NotContains(t, store.savedAllocation[0], "phase_of_the_moon")
}

// The one rule that carries a value cannot be switched on without one. Refused
// at the point of saving, where an admin can see the box they left empty.
func TestSaveAllocationSettingsRefusesAnEnabledFrequencyWithNoValue(t *testing.T) {
	store := &stubRotaDefaultsStore{}

	_, err := SaveAllocationSettings(context.Background(), store, AllocationSettingsParams{
		Enabled: map[string]bool{"max_frequency": true},
	}, zap.NewNop())

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidInput)
	assert.Empty(t, store.savedAllocation, "a refused save writes nothing")
}

func TestSaveAllocationSettingsRefusesAFrequencyOutOfRange(t *testing.T) {
	store := &stubRotaDefaultsStore{}

	for _, frequency := range []float64{-0.1, 0, 1.5} {
		_, err := SaveAllocationSettings(context.Background(), store, AllocationSettingsParams{
			Enabled:      map[string]bool{"max_frequency": true},
			MaxFrequency: frequency,
		}, zap.NewNop())
		assert.ErrorIs(t, err, ErrInvalidInput, "frequency %v", frequency)
	}
}

// A value left over from when the rule was on is stored as given rather than
// refused: with the rule off it constrains nothing, and blanking it would lose
// what an admin would want back when they switch the rule on again.
func TestSaveAllocationSettingsKeepsTheValueWhenTheRuleIsOff(t *testing.T) {
	store := &stubRotaDefaultsStore{}

	settings, err := SaveAllocationSettings(context.Background(), store, AllocationSettingsParams{
		Enabled:      map[string]bool{"max_frequency": false},
		MaxFrequency: 0.34,
	}, zap.NewNop())
	require.NoError(t, err)

	assert.Empty(t, settings.EnabledConstraints())
	assert.Equal(t, 0.34, settings.MaxFrequency)
}
