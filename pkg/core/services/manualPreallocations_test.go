package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/allocator"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// configOverride builds an allocator.ShiftOverride that applies to exactly the
// given dates, so the union/dedupe helpers can be unit-tested without going
// through rrule parsing.
func configOverride(dates []string, o allocator.ShiftOverride) allocator.ShiftOverride {
	want := make(map[string]bool, len(dates))
	for _, d := range dates {
		want[d] = true
	}
	o.AppliesTo = func(d string) bool { return want[d] }
	return o
}

func TestBuildManualPreallocationOverrides_VolunteerUnion(t *testing.T) {
	dateByShiftID := map[string]string{"shift-1": "2026-08-02"}
	pins := []db.ManualPreallocation{
		{ID: "p1", ShiftID: "shift-1", Role: "Service volunteer", VolunteerID: "vol-1"},
	}

	overrides, err := buildManualPreallocationOverrides(pins, dateByShiftID, nil, testRoles)
	require.NoError(t, err)
	require.Len(t, overrides, 1)

	got := overrides[0]
	assert.Equal(t, []allocator.Preallocation{
		{VolunteerID: "vol-1", Role: "Service volunteer"},
	}, got.Preallocations)
	// Exact-date matcher only matches its own date.
	assert.True(t, got.AppliesTo("2026-08-02"))
	assert.False(t, got.AppliesTo("2026-08-09"))
}

func TestBuildManualPreallocationOverrides_DedupesAgainstConfig(t *testing.T) {
	dateByShiftID := map[string]string{
		"shift-1": "2026-08-02",
		"shift-2": "2026-08-09",
	}
	configOverrides := []allocator.ShiftOverride{
		configOverride([]string{"2026-08-02"}, allocator.ShiftOverride{
			Preallocations: []allocator.Preallocation{
				{VolunteerID: "vol-1", Role: "Service volunteer"},
				{Custom: "Cover Team", Role: "Service volunteer"},
				{VolunteerID: "tl-config", Role: "Team lead"},
			},
		}),
	}
	pins := []db.ManualPreallocation{
		// Duplicates config volunteer on the same date → skipped.
		{ID: "p1", ShiftID: "shift-1", Role: "Service volunteer", VolunteerID: "vol-1"},
		// Duplicates config custom entry on the same date → skipped.
		{ID: "p2", ShiftID: "shift-1", Role: "Service volunteer", CustomValue: "Cover Team"},
		// Team lead but config already pins one on that date → skipped.
		{ID: "p3", ShiftID: "shift-1", Role: "Team lead", VolunteerID: "tl-manual"},
		// Same volunteer id but a different date, where config does not pin it → kept.
		{ID: "p4", ShiftID: "shift-2", Role: "Service volunteer", VolunteerID: "vol-1"},
	}

	overrides, err := buildManualPreallocationOverrides(pins, dateByShiftID, configOverrides, testRoles)
	require.NoError(t, err)
	require.Len(t, overrides, 1, "only the pin on a date config does not already cover survives")
	assert.Equal(t, []allocator.Preallocation{
		{VolunteerID: "vol-1", Role: "Service volunteer"},
	}, overrides[0].Preallocations)
	assert.True(t, overrides[0].AppliesTo("2026-08-09"))
}

func TestBuildManualPreallocationOverrides_TeamLeadAndCustom(t *testing.T) {
	dateByShiftID := map[string]string{"shift-1": "2026-08-02"}
	pins := []db.ManualPreallocation{
		{ID: "p1", ShiftID: "shift-1", Role: "Team lead", VolunteerID: "tl-1"},
		{ID: "p2", ShiftID: "shift-1", Role: "Service volunteer", CustomValue: "External Helper"},
	}

	overrides, err := buildManualPreallocationOverrides(pins, dateByShiftID, nil, testRoles)
	require.NoError(t, err)
	require.Len(t, overrides, 2)
	assert.Equal(t, []allocator.Preallocation{
		{VolunteerID: "tl-1", Role: "Team lead"},
	}, overrides[0].Preallocations)
	assert.Equal(t, []allocator.Preallocation{
		{Custom: "External Helper", Role: "Service volunteer"},
	}, overrides[1].Preallocations)
}

func TestBuildManualPreallocationOverrides_UnknownShiftFails(t *testing.T) {
	pins := []db.ManualPreallocation{
		{ID: "p1", ShiftID: "ghost", Role: "Service volunteer", VolunteerID: "vol-1"},
	}
	_, err := buildManualPreallocationOverrides(pins, map[string]string{}, nil, testRoles)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ghost")
}

func TestCheckPreallocationsResolve_AllActive(t *testing.T) {
	dateByShiftID := map[string]string{"shift-1": "2026-08-02"}
	pins := []db.ManualPreallocation{
		{ID: "p1", ShiftID: "shift-1", Role: "Service volunteer", VolunteerID: "vol-1"},
		{ID: "p2", ShiftID: "shift-1", Role: "Service volunteer", CustomValue: "External"},
	}
	activeIDs := map[string]bool{"vol-1": true}

	err := checkPreallocationsResolve(pins, openShiftRows(dateByShiftID), nil, activeIDs)
	assert.NoError(t, err, "active volunteer and a custom entry both resolve")
}

func TestCheckPreallocationsResolve_InactiveManualPin(t *testing.T) {
	dateByShiftID := map[string]string{"shift-1": "2026-08-02"}
	pins := []db.ManualPreallocation{
		{ID: "p1", ShiftID: "shift-1", Role: "Service volunteer", VolunteerID: "gone"},
	}
	activeIDs := map[string]bool{"vol-1": true}

	err := checkPreallocationsResolve(pins, openShiftRows(dateByShiftID), nil, activeIDs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manual pin")
	assert.Contains(t, err.Error(), "2026-08-02")
	assert.Contains(t, err.Error(), "gone")
}

func TestCheckPreallocationsResolve_InactiveConfigPin(t *testing.T) {
	configOverrides := []allocator.ShiftOverride{
		configOverride([]string{"2026-08-02"}, allocator.ShiftOverride{
			Preallocations: []allocator.Preallocation{
				{VolunteerID: "stale", Role: "Service volunteer"},
			},
		}),
	}
	activeIDs := map[string]bool{"vol-1": true}

	err := checkPreallocationsResolve(nil, openShiftRows(map[string]string{"shift-1": "2026-08-02"}), configOverrides, activeIDs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config pin")
	assert.Contains(t, err.Error(), "stale")
}

// A stale pin on a closed shift is not reported: InitShifts strips it, so it
// reaches nothing, and failing the whole rota over a pin with no effect would
// leave an admin with nothing to fix but a shut date.
func TestCheckPreallocationsResolve_SkipsClosedShifts(t *testing.T) {
	shifts := []db.Shift{{ID: "shift-1", Date: "2026-08-02", Closed: true}}
	pins := []db.ManualPreallocation{
		{ID: "p1", ShiftID: "shift-1", Role: "Service volunteer", VolunteerID: "gone"},
	}
	configOverrides := []allocator.ShiftOverride{
		configOverride([]string{"2026-08-02"}, allocator.ShiftOverride{
			Preallocations: []allocator.Preallocation{
				{VolunteerID: "stale", Role: "Service volunteer"},
			},
		}),
	}

	err := checkPreallocationsResolve(pins, shifts, configOverrides, map[string]bool{"vol-1": true})
	assert.NoError(t, err)
}

// openShiftRows turns a test's id→date map into open shift rows, which is what
// checkPreallocationsResolve scopes itself by.
func openShiftRows(dateByShiftID map[string]string) []db.Shift {
	shifts := make([]db.Shift, 0, len(dateByShiftID))
	for id, date := range dateByShiftID {
		shifts = append(shifts, db.Shift{ID: id, Date: date})
	}
	return shifts
}

// TestAllocateRotaFailsOnStaleManualPin covers the pre-solve stale-pin guard end
// to end: a manual pin whose volunteer has gone inactive makes AllocateRota fail
// before the solver runs, naming the pin, and writes nothing.
func TestAllocateRotaFailsOnStaleManualPin(t *testing.T) {
	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2026-08-02", ShiftCount: 1},
		},
		shifts: sundayShifts("rota-1", "2026-08-02", 1),
		availabilityRequests: []db.AvailabilityRequest{
			{ID: "req-1", RotaID: "rota-1", VolunteerID: "vol-1", Token: "tok-1"},
		},
		generations: map[string]db.AvailabilityGeneration{
			"req-1": {RequestID: "req-1", ResponseID: "gen-1", Answers: []db.ShiftAnswer{
				{ShiftID: "2026-08-02", Answer: db.AnswerYes},
			}},
		},
		manualPreallocations: []db.ManualPreallocation{
			{ID: "pin-1", ShiftID: "2026-08-02", Role: "Service volunteer", VolunteerID: "gone"},
		},
	}

	volClient := &mockVolClient{
		volunteers: []model.Volunteer{
			{ID: "vol-1", FirstName: "Ada", LastName: "Active", Roles: []string{"Service volunteer"}, Status: "Active"},
			// "gone" is deliberately absent / inactive — it is not in the active set.
		},
	}

	result, err := AllocateRota(
		context.Background(),
		store,
		volClient,
		testCfg,
		zap.NewNop(),
		false, // dryRun
		false, // forceCommit
		"",    // pythonFlag
	)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "gone", "error should name the stale pin's volunteer")
	assert.Contains(t, err.Error(), "not active")
	assert.Empty(t, store.insertedAllocations, "nothing should be written when a pin is stale")
}
