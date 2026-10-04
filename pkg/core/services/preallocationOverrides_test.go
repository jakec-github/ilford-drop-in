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

func TestBuildPreallocationOverrides_VolunteerUnion(t *testing.T) {
	dateByShiftID := map[string]string{"shift-1": "2026-08-02"}
	pins := []db.Preallocation{
		{ID: "p1", ShiftID: "shift-1", RoleID: "role-service-volunteer", VolunteerID: "vol-1"},
	}

	overrides, err := buildPreallocationOverrides(pins, dateByShiftID, testRoles)
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

func TestBuildPreallocationOverrides_TeamLeadAndCustom(t *testing.T) {
	dateByShiftID := map[string]string{"shift-1": "2026-08-02"}
	pins := []db.Preallocation{
		{ID: "p1", ShiftID: "shift-1", RoleID: "role-team-lead", VolunteerID: "tl-1"},
		{ID: "p2", ShiftID: "shift-1", RoleID: "role-service-volunteer", CustomValue: "External Helper"},
	}

	overrides, err := buildPreallocationOverrides(pins, dateByShiftID, testRoles)
	require.NoError(t, err)
	require.Len(t, overrides, 2)
	assert.Equal(t, []allocator.Preallocation{
		{VolunteerID: "tl-1", Role: "Team lead"},
	}, overrides[0].Preallocations)
	assert.Equal(t, []allocator.Preallocation{
		{Custom: "External Helper", Role: "Service volunteer"},
	}, overrides[1].Preallocations)
}

// The pyallocator contract names Roles and a pin references one, so the name
// the solver is given is resolved at solve time. A Role renamed after the pin
// was made reaches it under the name the Shape uses — which is the whole reason
// a pin holds a Role id (issue #195).
func TestBuildPreallocationOverrides_NamesARenamedRoleAsItReadsNow(t *testing.T) {
	renamed := model.NewRoles([]model.Role{
		{ID: "role-team-lead", Name: "Shift lead", Priority: 1, Colour: "violet"},
		{ID: "role-service-volunteer", Name: "Service volunteer", Priority: 2, Colour: "teal"},
	})
	pins := []db.Preallocation{
		{ID: "p1", ShiftID: "shift-1", RoleID: "role-team-lead", VolunteerID: "tl-1"},
	}

	overrides, err := buildPreallocationOverrides(pins, map[string]string{"shift-1": "2026-08-02"}, renamed)
	require.NoError(t, err)
	require.Len(t, overrides, 1)
	assert.Equal(t, []allocator.Preallocation{
		{VolunteerID: "tl-1", Role: "Shift lead"},
	}, overrides[0].Preallocations)
}

// A pin whose Role cannot be named would reach the solver as a Seat no Shape
// has. The foreign key makes it unreachable; it fails loudly rather than
// silently if it ever happens.
func TestBuildPreallocationOverrides_UnknownRoleFails(t *testing.T) {
	pins := []db.Preallocation{
		{ID: "p1", ShiftID: "shift-1", RoleID: "role-ghost", VolunteerID: "vol-1"},
	}
	_, err := buildPreallocationOverrides(pins, map[string]string{"shift-1": "2026-08-02"}, testRoles)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "role-ghost")
}

func TestBuildPreallocationOverrides_UnknownShiftFails(t *testing.T) {
	pins := []db.Preallocation{
		{ID: "p1", ShiftID: "ghost", RoleID: "role-service-volunteer", VolunteerID: "vol-1"},
	}
	_, err := buildPreallocationOverrides(pins, map[string]string{}, testRoles)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ghost")
}

func TestCheckPreallocationsResolve_AllOnTheRoster(t *testing.T) {
	dateByShiftID := map[string]string{"shift-1": "2026-08-02"}
	pins := []db.Preallocation{
		{ID: "p1", ShiftID: "shift-1", RoleID: "role-service-volunteer", VolunteerID: "vol-1"},
		{ID: "p2", ShiftID: "shift-1", RoleID: "role-service-volunteer", VolunteerID: "vol-2"},
		{ID: "p3", ShiftID: "shift-1", RoleID: "role-service-volunteer", CustomValue: "External"},
	}
	// vol-2 has gone inactive since being pinned. That is roster data, not a
	// reason to drop or refuse the pin (ADR 0010).
	rosterIDs := map[string]bool{"vol-1": true, "vol-2": true}

	err := checkPreallocationsResolve(pins, openShiftRows(dateByShiftID), rosterIDs)
	assert.NoError(t, err, "active and inactive volunteers and a custom entry all resolve")
}

func TestCheckPreallocationsResolve_OffTheRoster(t *testing.T) {
	dateByShiftID := map[string]string{"shift-1": "2026-08-02"}
	pins := []db.Preallocation{
		{ID: "p1", ShiftID: "shift-1", RoleID: "role-service-volunteer", VolunteerID: "gone"},
	}
	rosterIDs := map[string]bool{"vol-1": true}

	err := checkPreallocationsResolve(pins, openShiftRows(dateByShiftID), rosterIDs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pin for")
	assert.Contains(t, err.Error(), "2026-08-02")
	assert.Contains(t, err.Error(), "gone")
	assert.Contains(t, err.Error(), "not on the roster")
}

// Who the solver is sent: everyone active, plus anyone inactive a pin on an
// open shift names. The inactive are sent on their own — not in their group —
// so the pin places them and nobody else, and nothing pulls them in elsewhere.
func TestSolverVolunteers(t *testing.T) {
	roster := []model.Volunteer{
		{ID: "ada", FirstName: "Ada", LastName: "A", GroupKey: "Group A", Status: "Active"},
		{ID: "bea", FirstName: "Bea", LastName: "B", GroupKey: "Group A", Status: "Inactive"},
		{ID: "cal", FirstName: "Cal", LastName: "C", Status: "Inactive"},
		{ID: "dot", FirstName: "Dot", LastName: "D", Status: "Inactive"},
	}
	shifts := []db.Shift{
		{ID: "open", Date: "2026-08-02"},
		{ID: "shut", Date: "2026-08-09", Closed: true},
	}
	pins := []db.Preallocation{
		{ID: "p1", ShiftID: "open", VolunteerID: "bea"},
		// Pinned only to a closed shift: InitShifts strips that pin, so Dot
		// has nothing to be placed on and is not sent.
		{ID: "p2", ShiftID: "shut", VolunteerID: "dot"},
		{ID: "p3", ShiftID: "open", CustomValue: "Scouts"},
	}

	got := solverVolunteers(roster, pins, shifts)

	ids := make([]string, len(got))
	for i, v := range got {
		ids[i] = v.ID
	}
	assert.Equal(t, []string{"ada", "bea"}, ids, "Cal holds no pin, Dot's is on a closed shift")
	assert.Equal(t, "Group A", got[0].GroupKey)
	assert.Empty(t, got[1].GroupKey, "an inactive pinned volunteer is sent as a group of one")
}

// A stale pin on a closed shift is not reported: InitShifts strips it, so it
// reaches nothing, and failing the whole rota over a pin with no effect would
// leave an Organiser with nothing to fix but a shut date.
func TestCheckPreallocationsResolve_SkipsClosedShifts(t *testing.T) {
	shifts := []db.Shift{{ID: "shift-1", Date: "2026-08-02", Closed: true}}
	pins := []db.Preallocation{
		{ID: "p1", ShiftID: "shift-1", RoleID: "role-service-volunteer", VolunteerID: "gone"},
	}

	err := checkPreallocationsResolve(pins, shifts, map[string]bool{"vol-1": true})
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

// TestAllocateRotaFailsOnStalePin covers the pre-solve stale-pin guard end to
// end: a pin whose volunteer has left the roster makes allocating fail before
// the solver runs, naming the pin, and writes nothing.
func TestAllocateRotaFailsOnStalePin(t *testing.T) {
	store := &mockAllocateRotaStore{
		rotations: []db.Rotation{
			{ID: "rota-1", Start: "2026-08-02", ShiftCount: 1},
		},
		// A draft exists, so the refusal is the stale pin rather than the gate
		// that stops a rota nobody has drafted.
		storedDrafts: []db.DraftRotaAllocation{{RotaID: "rota-1"}},
		shifts:       sundayShifts("rota-1", "2026-08-02", 1),
		availabilityRequests: []db.AvailabilityRequest{
			{ID: "req-1", RotaID: "rota-1", VolunteerID: "vol-1", Token: "tok-1"},
		},
		generations: map[string]db.AvailabilityGeneration{
			"req-1": {RequestID: "req-1", ResponseID: "gen-1", Answers: []db.ShiftAnswer{
				{ShiftID: "2026-08-02", Answer: db.AnswerYes},
			}},
		},
		manualPreallocations: []db.Preallocation{
			{ID: "pin-1", ShiftID: "2026-08-02", RoleID: "role-service-volunteer", VolunteerID: "gone"},
		},
	}

	volClient := &mockVolClient{
		volunteers: []model.Volunteer{
			{ID: "vol-1", FirstName: "Ada", LastName: "Active", Roles: []string{"Service volunteer"}, Status: "Active"},
			// "gone" is deliberately absent from the roster.
		},
	}

	result, err := AllocateRotaInFlight(
		context.Background(),
		store,
		volClient,
		testCfg,
		zap.NewNop(),
		"a-hash", // the draft being confirmed; never reached
		"",       // pythonFlag
	)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "gone", "error should name the stale pin's volunteer")
	assert.Contains(t, err.Error(), "not on the roster")
	assert.Empty(t, store.insertedAllocations, "nothing should be written when a pin is stale")
}
