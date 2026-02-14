package services

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

func TestApplyAlterations_RemoveVolunteer(t *testing.T) {
	allocationsByDate := map[string][]db.Allocation{
		"2025-01-05": {
			{ID: "a1", VolunteerID: "alice", Role: string(model.RoleTeamLead), ShiftDate: "2025-01-05"},
			{ID: "a2", VolunteerID: "bob", Role: string(model.RoleVolunteer), ShiftDate: "2025-01-05"},
			{ID: "a3", VolunteerID: "charlie", Role: string(model.RoleVolunteer), ShiftDate: "2025-01-05"},
		},
	}

	alterations := []db.Alteration{
		{ID: "alt1", ShiftDate: "2025-01-05", Direction: "remove", VolunteerID: "bob", SetTime: "2025-01-01T00:00:00Z"},
	}

	result := ApplyAlterations(allocationsByDate, alterations)

	assert.Len(t, result["2025-01-05"], 2)
	for _, a := range result["2025-01-05"] {
		assert.NotEqual(t, "bob", a.VolunteerID)
	}
}

func TestApplyAlterations_RemoveCustomEntry(t *testing.T) {
	allocationsByDate := map[string][]db.Allocation{
		"2025-01-05": {
			{ID: "a1", VolunteerID: "alice", Role: string(model.RoleTeamLead), ShiftDate: "2025-01-05"},
			{ID: "a2", CustomEntry: "External John", Role: string(model.RoleVolunteer), ShiftDate: "2025-01-05"},
		},
	}

	alterations := []db.Alteration{
		{ID: "alt1", ShiftDate: "2025-01-05", Direction: "remove", CustomValue: "External John", SetTime: "2025-01-01T00:00:00Z"},
	}

	result := ApplyAlterations(allocationsByDate, alterations)

	assert.Len(t, result["2025-01-05"], 1)
	assert.Equal(t, "alice", result["2025-01-05"][0].VolunteerID)
}

func TestApplyAlterations_AddVolunteer(t *testing.T) {
	allocationsByDate := map[string][]db.Allocation{
		"2025-01-05": {
			{ID: "a1", VolunteerID: "alice", Role: string(model.RoleTeamLead), ShiftDate: "2025-01-05"},
		},
	}

	alterations := []db.Alteration{
		{ID: "alt1", ShiftDate: "2025-01-05", Direction: "add", VolunteerID: "bob", SetTime: "2025-01-01T00:00:00Z"},
	}

	result := ApplyAlterations(allocationsByDate, alterations)

	assert.Len(t, result["2025-01-05"], 2)
	assert.Equal(t, "bob", result["2025-01-05"][1].VolunteerID)
	assert.Equal(t, string(model.RoleVolunteer), result["2025-01-05"][1].Role)
}

func TestApplyAlterations_AddCustomEntry(t *testing.T) {
	allocationsByDate := map[string][]db.Allocation{
		"2025-01-05": {
			{ID: "a1", VolunteerID: "alice", Role: string(model.RoleTeamLead), ShiftDate: "2025-01-05"},
		},
	}

	alterations := []db.Alteration{
		{ID: "alt1", ShiftDate: "2025-01-05", Direction: "add", CustomValue: "External John", SetTime: "2025-01-01T00:00:00Z"},
	}

	result := ApplyAlterations(allocationsByDate, alterations)

	assert.Len(t, result["2025-01-05"], 2)
	assert.Equal(t, "External John", result["2025-01-05"][1].CustomEntry)
	assert.Equal(t, string(model.RoleVolunteer), result["2025-01-05"][1].Role)
}

func TestApplyAlterations_AppliedInSetTimeOrder(t *testing.T) {
	allocationsByDate := map[string][]db.Allocation{
		"2025-01-05": {
			{ID: "a1", VolunteerID: "alice", Role: string(model.RoleVolunteer), ShiftDate: "2025-01-05"},
		},
	}

	// Remove alice first, then add bob - order matters
	alterations := []db.Alteration{
		{ID: "alt2", ShiftDate: "2025-01-05", Direction: "add", VolunteerID: "bob", SetTime: "2025-01-01T01:00:00Z"},
		{ID: "alt1", ShiftDate: "2025-01-05", Direction: "remove", VolunteerID: "alice", SetTime: "2025-01-01T00:00:00Z"},
	}

	result := ApplyAlterations(allocationsByDate, alterations)

	// Alice should be removed, bob should be added
	assert.Len(t, result["2025-01-05"], 1)
	assert.Equal(t, "bob", result["2025-01-05"][0].VolunteerID)
}

func TestApplyAlterations_RemoveNonExistentIsNoOp(t *testing.T) {
	allocationsByDate := map[string][]db.Allocation{
		"2025-01-05": {
			{ID: "a1", VolunteerID: "alice", Role: string(model.RoleVolunteer), ShiftDate: "2025-01-05"},
		},
	}

	alterations := []db.Alteration{
		{ID: "alt1", ShiftDate: "2025-01-05", Direction: "remove", VolunteerID: "nonexistent", SetTime: "2025-01-01T00:00:00Z"},
	}

	result := ApplyAlterations(allocationsByDate, alterations)

	// Should be unchanged
	assert.Len(t, result["2025-01-05"], 1)
	assert.Equal(t, "alice", result["2025-01-05"][0].VolunteerID)
}

func TestApplyAlterations_MultipleDates(t *testing.T) {
	allocationsByDate := map[string][]db.Allocation{
		"2025-01-05": {
			{ID: "a1", VolunteerID: "alice", Role: string(model.RoleVolunteer), ShiftDate: "2025-01-05"},
			{ID: "a2", VolunteerID: "bob", Role: string(model.RoleVolunteer), ShiftDate: "2025-01-05"},
		},
		"2025-01-12": {
			{ID: "a3", VolunteerID: "charlie", Role: string(model.RoleVolunteer), ShiftDate: "2025-01-12"},
		},
	}

	alterations := []db.Alteration{
		{ID: "alt1", ShiftDate: "2025-01-05", Direction: "remove", VolunteerID: "alice", SetTime: "2025-01-01T00:00:00Z"},
		{ID: "alt2", ShiftDate: "2025-01-12", Direction: "add", VolunteerID: "dave", SetTime: "2025-01-01T00:00:00Z"},
	}

	result := ApplyAlterations(allocationsByDate, alterations)

	assert.Len(t, result["2025-01-05"], 1)
	assert.Equal(t, "bob", result["2025-01-05"][0].VolunteerID)

	assert.Len(t, result["2025-01-12"], 2)
	assert.Equal(t, "charlie", result["2025-01-12"][0].VolunteerID)
	assert.Equal(t, "dave", result["2025-01-12"][1].VolunteerID)
}

func TestApplyAlterations_EmptyAlterations(t *testing.T) {
	allocationsByDate := map[string][]db.Allocation{
		"2025-01-05": {
			{ID: "a1", VolunteerID: "alice", Role: string(model.RoleVolunteer), ShiftDate: "2025-01-05"},
		},
	}

	result := ApplyAlterations(allocationsByDate, nil)

	assert.Len(t, result["2025-01-05"], 1)
	assert.Equal(t, "alice", result["2025-01-05"][0].VolunteerID)
}
