package services

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
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
		{ID: "p1", ShiftID: "shift-1", Role: string(model.RoleVolunteer), VolunteerID: "vol-1"},
	}

	overrides, err := buildManualPreallocationOverrides(pins, dateByShiftID, nil)
	require.NoError(t, err)
	require.Len(t, overrides, 1)

	got := overrides[0]
	assert.Equal(t, []string{"vol-1"}, got.PreallocatedVolunteerIDs)
	assert.Empty(t, got.CustomPreallocations)
	assert.Empty(t, got.PreallocatedTeamLeadID)
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
			PreallocatedVolunteerIDs: []string{"vol-1"},
			CustomPreallocations:     []string{"Cover Team"},
			PreallocatedTeamLeadID:   "tl-config",
		}),
	}
	pins := []db.ManualPreallocation{
		// Duplicates config volunteer on the same date → skipped.
		{ID: "p1", ShiftID: "shift-1", Role: string(model.RoleVolunteer), VolunteerID: "vol-1"},
		// Duplicates config custom entry on the same date → skipped.
		{ID: "p2", ShiftID: "shift-1", Role: string(model.RoleVolunteer), CustomValue: "Cover Team"},
		// Team lead but config already pins one on that date → skipped.
		{ID: "p3", ShiftID: "shift-1", Role: string(model.RoleTeamLead), VolunteerID: "tl-manual"},
		// Same volunteer id but a different date, where config does not pin it → kept.
		{ID: "p4", ShiftID: "shift-2", Role: string(model.RoleVolunteer), VolunteerID: "vol-1"},
	}

	overrides, err := buildManualPreallocationOverrides(pins, dateByShiftID, configOverrides)
	require.NoError(t, err)
	require.Len(t, overrides, 1, "only the pin on a date config does not already cover survives")
	assert.Equal(t, []string{"vol-1"}, overrides[0].PreallocatedVolunteerIDs)
	assert.True(t, overrides[0].AppliesTo("2026-08-09"))
}

func TestBuildManualPreallocationOverrides_TeamLeadAndCustom(t *testing.T) {
	dateByShiftID := map[string]string{"shift-1": "2026-08-02"}
	pins := []db.ManualPreallocation{
		{ID: "p1", ShiftID: "shift-1", Role: string(model.RoleTeamLead), VolunteerID: "tl-1"},
		{ID: "p2", ShiftID: "shift-1", Role: string(model.RoleVolunteer), CustomValue: "External Helper"},
	}

	overrides, err := buildManualPreallocationOverrides(pins, dateByShiftID, nil)
	require.NoError(t, err)
	require.Len(t, overrides, 2)
	assert.Equal(t, "tl-1", overrides[0].PreallocatedTeamLeadID)
	assert.Equal(t, []string{"External Helper"}, overrides[1].CustomPreallocations)
}

func TestBuildManualPreallocationOverrides_ClosedByConfigDropsPin(t *testing.T) {
	dateByShiftID := map[string]string{"shift-1": "2026-08-02"}
	configOverrides := []allocator.ShiftOverride{
		configOverride([]string{"2026-08-02"}, allocator.ShiftOverride{Closed: true}),
	}
	pins := []db.ManualPreallocation{
		{ID: "p1", ShiftID: "shift-1", Role: string(model.RoleVolunteer), VolunteerID: "vol-1"},
	}

	overrides, err := buildManualPreallocationOverrides(pins, dateByShiftID, configOverrides)
	require.NoError(t, err)
	assert.Empty(t, overrides, "a manual pin cannot reopen a config-closed date")
}

func TestBuildManualPreallocationOverrides_UnknownShiftFails(t *testing.T) {
	pins := []db.ManualPreallocation{
		{ID: "p1", ShiftID: "ghost", Role: string(model.RoleVolunteer), VolunteerID: "vol-1"},
	}
	_, err := buildManualPreallocationOverrides(pins, map[string]string{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ghost")
}

func TestCheckPreallocationsResolve_AllActive(t *testing.T) {
	dateByShiftID := map[string]string{"shift-1": "2026-08-02"}
	pins := []db.ManualPreallocation{
		{ID: "p1", ShiftID: "shift-1", Role: string(model.RoleVolunteer), VolunteerID: "vol-1"},
		{ID: "p2", ShiftID: "shift-1", Role: string(model.RoleVolunteer), CustomValue: "External"},
	}
	activeIDs := map[string]bool{"vol-1": true}

	err := checkPreallocationsResolve(pins, dateByShiftID, nil, nil, activeIDs)
	assert.NoError(t, err, "active volunteer and a custom entry both resolve")
}

func TestCheckPreallocationsResolve_InactiveManualPin(t *testing.T) {
	dateByShiftID := map[string]string{"shift-1": "2026-08-02"}
	pins := []db.ManualPreallocation{
		{ID: "p1", ShiftID: "shift-1", Role: string(model.RoleVolunteer), VolunteerID: "gone"},
	}
	activeIDs := map[string]bool{"vol-1": true}

	err := checkPreallocationsResolve(pins, dateByShiftID, nil, nil, activeIDs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "manual pin")
	assert.Contains(t, err.Error(), "2026-08-02")
	assert.Contains(t, err.Error(), "gone")
}

func TestCheckPreallocationsResolve_InactiveConfigPin(t *testing.T) {
	shiftDates := []time.Time{time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)}
	configOverrides := []allocator.ShiftOverride{
		configOverride([]string{"2026-08-02"}, allocator.ShiftOverride{
			PreallocatedVolunteerIDs: []string{"stale"},
		}),
	}
	activeIDs := map[string]bool{"vol-1": true}

	err := checkPreallocationsResolve(nil, nil, configOverrides, shiftDates, activeIDs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config pin")
	assert.Contains(t, err.Error(), "stale")
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
			{ID: "req-1", RotaID: "rota-1", VolunteerID: "vol-1", FormID: "form-1", FormSent: true},
		},
		manualPreallocations: []db.ManualPreallocation{
			{ID: "pin-1", ShiftID: "2026-08-02", Role: string(model.RoleVolunteer), VolunteerID: "gone"},
		},
	}

	volClient := &mockVolClient{
		volunteers: []model.Volunteer{
			{ID: "vol-1", FirstName: "Ada", LastName: "Active", Role: model.RoleVolunteer, Status: "Active"},
			// "gone" is deliberately absent / inactive — it is not in the active set.
		},
	}

	result, err := AllocateRota(
		context.Background(),
		store,
		volClient,
		&mockFormsClientWithResponses{},
		&config.Config{},
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
