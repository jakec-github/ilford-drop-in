package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

func TestApplyAlterations_RemoveVolunteer(t *testing.T) {
	allocationsByShiftID := map[string][]db.Allocation{
		"shift-1": {
			{ID: "a1", VolunteerID: "alice", Role: string(model.RoleTeamLead), ShiftID: "shift-1"},
			{ID: "a2", VolunteerID: "bob", Role: string(model.RoleVolunteer), ShiftID: "shift-1"},
			{ID: "a3", VolunteerID: "charlie", Role: string(model.RoleVolunteer), ShiftID: "shift-1"},
		},
	}

	alterations := []db.Alteration{
		{ID: "alt1", ShiftID: "shift-1", Direction: "remove", VolunteerID: "bob", SetTime: "2025-01-01T00:00:00Z"},
	}

	result := ApplyAlterations(allocationsByShiftID, alterations)

	assert.Len(t, result["shift-1"], 2)
	for _, a := range result["shift-1"] {
		assert.NotEqual(t, "bob", a.VolunteerID)
	}
}

func TestApplyAlterations_RemoveCustomEntry(t *testing.T) {
	allocationsByShiftID := map[string][]db.Allocation{
		"shift-1": {
			{ID: "a1", VolunteerID: "alice", Role: string(model.RoleTeamLead), ShiftID: "shift-1"},
			{ID: "a2", CustomEntry: "External John", Role: string(model.RoleVolunteer), ShiftID: "shift-1"},
		},
	}

	alterations := []db.Alteration{
		{ID: "alt1", ShiftID: "shift-1", Direction: "remove", CustomValue: "External John", SetTime: "2025-01-01T00:00:00Z"},
	}

	result := ApplyAlterations(allocationsByShiftID, alterations)

	assert.Len(t, result["shift-1"], 1)
	assert.Equal(t, "alice", result["shift-1"][0].VolunteerID)
}

func TestApplyAlterations_AddVolunteer(t *testing.T) {
	allocationsByShiftID := map[string][]db.Allocation{
		"shift-1": {
			{ID: "a1", VolunteerID: "alice", Role: string(model.RoleTeamLead), ShiftID: "shift-1"},
		},
	}

	alterations := []db.Alteration{
		{ID: "alt1", ShiftID: "shift-1", Direction: "add", VolunteerID: "bob", SetTime: "2025-01-01T00:00:00Z"},
	}

	result := ApplyAlterations(allocationsByShiftID, alterations)

	assert.Len(t, result["shift-1"], 2)
	assert.Equal(t, "bob", result["shift-1"][1].VolunteerID)
	assert.Equal(t, string(model.RoleVolunteer), result["shift-1"][1].Role)
	assert.Equal(t, "shift-1", result["shift-1"][1].ShiftID)
}

func TestApplyAlterations_AddCustomEntry(t *testing.T) {
	allocationsByShiftID := map[string][]db.Allocation{
		"shift-1": {
			{ID: "a1", VolunteerID: "alice", Role: string(model.RoleTeamLead), ShiftID: "shift-1"},
		},
	}

	alterations := []db.Alteration{
		{ID: "alt1", ShiftID: "shift-1", Direction: "add", CustomValue: "External John", SetTime: "2025-01-01T00:00:00Z"},
	}

	result := ApplyAlterations(allocationsByShiftID, alterations)

	assert.Len(t, result["shift-1"], 2)
	assert.Equal(t, "External John", result["shift-1"][1].CustomEntry)
	assert.Equal(t, string(model.RoleVolunteer), result["shift-1"][1].Role)
	assert.Equal(t, "shift-1", result["shift-1"][1].ShiftID)
}

func TestApplyAlterations_AppliedInSetTimeOrder(t *testing.T) {
	allocationsByShiftID := map[string][]db.Allocation{
		"shift-1": {
			{ID: "a1", VolunteerID: "alice", Role: string(model.RoleVolunteer), ShiftID: "shift-1"},
		},
	}

	// Remove alice first, then add bob - order matters
	alterations := []db.Alteration{
		{ID: "alt2", ShiftID: "shift-1", Direction: "add", VolunteerID: "bob", SetTime: "2025-01-01T01:00:00Z"},
		{ID: "alt1", ShiftID: "shift-1", Direction: "remove", VolunteerID: "alice", SetTime: "2025-01-01T00:00:00Z"},
	}

	result := ApplyAlterations(allocationsByShiftID, alterations)

	// Alice should be removed, bob should be added
	assert.Len(t, result["shift-1"], 1)
	assert.Equal(t, "bob", result["shift-1"][0].VolunteerID)
}

func TestApplyAlterations_RemoveNonExistentIsNoOp(t *testing.T) {
	allocationsByShiftID := map[string][]db.Allocation{
		"shift-1": {
			{ID: "a1", VolunteerID: "alice", Role: string(model.RoleVolunteer), ShiftID: "shift-1"},
		},
	}

	alterations := []db.Alteration{
		{ID: "alt1", ShiftID: "shift-1", Direction: "remove", VolunteerID: "nonexistent", SetTime: "2025-01-01T00:00:00Z"},
	}

	result := ApplyAlterations(allocationsByShiftID, alterations)

	// Should be unchanged
	assert.Len(t, result["shift-1"], 1)
	assert.Equal(t, "alice", result["shift-1"][0].VolunteerID)
}

func TestApplyAlterations_MultipleShifts(t *testing.T) {
	allocationsByShiftID := map[string][]db.Allocation{
		"shift-1": {
			{ID: "a1", VolunteerID: "alice", Role: string(model.RoleVolunteer), ShiftID: "shift-1"},
			{ID: "a2", VolunteerID: "bob", Role: string(model.RoleVolunteer), ShiftID: "shift-1"},
		},
		"shift-2": {
			{ID: "a3", VolunteerID: "charlie", Role: string(model.RoleVolunteer), ShiftID: "shift-2"},
		},
	}

	alterations := []db.Alteration{
		{ID: "alt1", ShiftID: "shift-1", Direction: "remove", VolunteerID: "alice", SetTime: "2025-01-01T00:00:00Z"},
		{ID: "alt2", ShiftID: "shift-2", Direction: "add", VolunteerID: "dave", SetTime: "2025-01-01T00:00:00Z"},
	}

	result := ApplyAlterations(allocationsByShiftID, alterations)

	assert.Len(t, result["shift-1"], 1)
	assert.Equal(t, "bob", result["shift-1"][0].VolunteerID)

	assert.Len(t, result["shift-2"], 2)
	assert.Equal(t, "charlie", result["shift-2"][0].VolunteerID)
	assert.Equal(t, "dave", result["shift-2"][1].VolunteerID)
}

func TestApplyAlterations_EmptyAlterations(t *testing.T) {
	allocationsByShiftID := map[string][]db.Allocation{
		"shift-1": {
			{ID: "a1", VolunteerID: "alice", Role: string(model.RoleVolunteer), ShiftID: "shift-1"},
		},
	}

	result := ApplyAlterations(allocationsByShiftID, nil)

	assert.Len(t, result["shift-1"], 1)
	assert.Equal(t, "alice", result["shift-1"][0].VolunteerID)
}
