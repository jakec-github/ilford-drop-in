package services

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
)

func TestAggregateByGroup_SingleMemberGroup(t *testing.T) {
	shiftDates := []time.Time{
		time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 1, 12, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 1, 19, 0, 0, 0, 0, time.UTC),
	}

	volunteersByID := map[string]model.Volunteer{
		"v1": {
			ID:        "v1",
			FirstName: "Alice",
			LastName:  "Smith",
			GroupKey:  "",
		},
	}

	responses := []VolunteerResponse{
		{
			VolunteerID:      "v1",
			VolunteerName:    "Alice Smith",
			HasResponded:     true,
			AvailableForAll:  false,
			UnavailableDates: []string{"Sun Jan 12 2025"},
			AvailableDates:   []string{"Sun Jan 5 2025", "Sun Jan 19 2025"},
		},
	}

	result := aggregateByGroup(responses, shiftDates, volunteersByID)

	require.Len(t, result, 1)
	group := result[0]

	assert.Equal(t, "individual_v1", group.GroupKey)
	assert.Equal(t, "Alice Smith", group.GroupName)
	assert.Equal(t, []string{"Alice Smith"}, group.MemberNames)
	assert.True(t, group.HasResponded)
	assert.Contains(t, group.UnavailableDates, "Sun Jan 12 2025")
	assert.Contains(t, group.AvailableDates, "Sun Jan 5 2025")
	assert.Contains(t, group.AvailableDates, "Sun Jan 19 2025")
}

func TestAggregateByGroup_MultiMemberGroup_AllResponded(t *testing.T) {
	shiftDates := []time.Time{
		time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 1, 12, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 1, 19, 0, 0, 0, 0, time.UTC),
	}

	volunteersByID := map[string]model.Volunteer{
		"v1": {
			ID:        "v1",
			FirstName: "Alice",
			LastName:  "Smith",
			GroupKey:  "group_a",
		},
		"v2": {
			ID:        "v2",
			FirstName: "Bob",
			LastName:  "Jones",
			GroupKey:  "group_a",
		},
	}

	responses := []VolunteerResponse{
		{
			VolunteerID:      "v1",
			VolunteerName:    "Alice Smith",
			HasResponded:     true,
			AvailableForAll:  false,
			UnavailableDates: []string{"Sun Jan 12 2025"},
			AvailableDates:   []string{"Sun Jan 5 2025", "Sun Jan 19 2025"},
		},
		{
			VolunteerID:      "v2",
			VolunteerName:    "Bob Jones",
			HasResponded:     true,
			AvailableForAll:  false,
			UnavailableDates: []string{"Sun Jan 5 2025"},
			AvailableDates:   []string{"Sun Jan 12 2025", "Sun Jan 19 2025"},
		},
	}

	result := aggregateByGroup(responses, shiftDates, volunteersByID)

	require.Len(t, result, 1)
	group := result[0]

	assert.Equal(t, "group_a", group.GroupKey)
	assert.Equal(t, "group_a", group.GroupName)
	assert.ElementsMatch(t, []string{"Alice Smith", "Bob Jones"}, group.MemberNames)
	assert.True(t, group.HasResponded)

	// Group should be unavailable on both Jan 5 (Bob) and Jan 12 (Alice)
	assert.Contains(t, group.UnavailableDates, "Sun Jan 5 2025")
	assert.Contains(t, group.UnavailableDates, "Sun Jan 12 2025")

	// Group should only be available on Jan 19 (both available)
	assert.Contains(t, group.AvailableDates, "Sun Jan 19 2025")
	assert.NotContains(t, group.AvailableDates, "Sun Jan 5 2025")
	assert.NotContains(t, group.AvailableDates, "Sun Jan 12 2025")
}

func TestAggregateByGroup_MultiMemberGroup_OneResponded(t *testing.T) {
	shiftDates := []time.Time{
		time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 1, 12, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 1, 19, 0, 0, 0, 0, time.UTC),
	}

	volunteersByID := map[string]model.Volunteer{
		"v1": {
			ID:        "v1",
			FirstName: "Alice",
			LastName:  "Smith",
			GroupKey:  "group_a",
		},
		"v2": {
			ID:        "v2",
			FirstName: "Bob",
			LastName:  "Jones",
			GroupKey:  "group_a",
		},
	}

	responses := []VolunteerResponse{
		{
			VolunteerID:      "v1",
			VolunteerName:    "Alice Smith",
			HasResponded:     true,
			AvailableForAll:  false,
			UnavailableDates: []string{"Sun Jan 12 2025"},
			AvailableDates:   []string{"Sun Jan 5 2025", "Sun Jan 19 2025"},
		},
		{
			VolunteerID:   "v2",
			VolunteerName: "Bob Jones",
			HasResponded:  false, // Bob hasn't responded
		},
	}

	result := aggregateByGroup(responses, shiftDates, volunteersByID)

	require.Len(t, result, 1)
	group := result[0]

	assert.Equal(t, "group_a", group.GroupKey)
	assert.Equal(t, "group_a", group.GroupName)
	assert.ElementsMatch(t, []string{"Alice Smith", "Bob Jones"}, group.MemberNames)

	// Group has responded because Alice responded
	assert.True(t, group.HasResponded)

	// Group should only be unavailable on Jan 12 (Alice's unavailability)
	// Bob's non-response should NOT make all dates unavailable
	assert.Contains(t, group.UnavailableDates, "Sun Jan 12 2025")
	assert.Len(t, group.UnavailableDates, 1)

	// Group should be available on Jan 5 and Jan 19
	assert.Contains(t, group.AvailableDates, "Sun Jan 5 2025")
	assert.Contains(t, group.AvailableDates, "Sun Jan 19 2025")
}

func TestAggregateByGroup_MultiMemberGroup_NoneResponded(t *testing.T) {
	shiftDates := []time.Time{
		time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 1, 12, 0, 0, 0, 0, time.UTC),
	}

	volunteersByID := map[string]model.Volunteer{
		"v1": {
			ID:        "v1",
			FirstName: "Alice",
			LastName:  "Smith",
			GroupKey:  "group_a",
		},
		"v2": {
			ID:        "v2",
			FirstName: "Bob",
			LastName:  "Jones",
			GroupKey:  "group_a",
		},
	}

	responses := []VolunteerResponse{
		{
			VolunteerID:   "v1",
			VolunteerName: "Alice Smith",
			HasResponded:  false,
		},
		{
			VolunteerID:   "v2",
			VolunteerName: "Bob Jones",
			HasResponded:  false,
		},
	}

	result := aggregateByGroup(responses, shiftDates, volunteersByID)

	require.Len(t, result, 1)
	group := result[0]

	assert.Equal(t, "group_a", group.GroupKey)
	assert.False(t, group.HasResponded)
	assert.ElementsMatch(t, []string{"Alice Smith", "Bob Jones"}, group.MemberNames)

	// Since no one responded, unavailable dates should be empty
	assert.Empty(t, group.UnavailableDates)

	// All dates should be available (no explicit unavailability from responses)
	assert.Contains(t, group.AvailableDates, "Sun Jan 5 2025")
	assert.Contains(t, group.AvailableDates, "Sun Jan 12 2025")
}

func TestAggregateByGroup_MultipleGroups(t *testing.T) {
	shiftDates := []time.Time{
		time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 1, 12, 0, 0, 0, 0, time.UTC),
	}

	volunteersByID := map[string]model.Volunteer{
		"v1": {ID: "v1", FirstName: "Alice", LastName: "Smith", GroupKey: "group_a"},
		"v2": {ID: "v2", FirstName: "Bob", LastName: "Jones", GroupKey: "group_a"},
		"v3": {ID: "v3", FirstName: "Charlie", LastName: "Brown", GroupKey: "group_b"},
		"v4": {ID: "v4", FirstName: "Diana", LastName: "Prince", GroupKey: ""}, // No group
	}

	responses := []VolunteerResponse{
		{
			VolunteerID:      "v1",
			VolunteerName:    "Alice Smith",
			HasResponded:     true,
			UnavailableDates: []string{"Sun Jan 5 2025"},
			AvailableDates:   []string{"Sun Jan 12 2025"},
		},
		{
			VolunteerID:   "v2",
			VolunteerName: "Bob Jones",
			HasResponded:  false,
		},
		{
			VolunteerID:      "v3",
			VolunteerName:    "Charlie Brown",
			HasResponded:     true,
			UnavailableDates: []string{"Sun Jan 12 2025"},
			AvailableDates:   []string{"Sun Jan 5 2025"},
		},
		{
			VolunteerID:    "v4",
			VolunteerName:  "Diana Prince",
			HasResponded:   true,
			AvailableDates: []string{"Sun Jan 5 2025", "Sun Jan 12 2025"},
		},
	}

	result := aggregateByGroup(responses, shiftDates, volunteersByID)

	require.Len(t, result, 3)

	// Find each group in results
	var groupA, groupB, groupDiana *GroupResponse
	for i := range result {
		switch result[i].GroupKey {
		case "group_a":
			groupA = &result[i]
		case "group_b":
			groupB = &result[i]
		case "individual_v4":
			groupDiana = &result[i]
		}
	}

	require.NotNil(t, groupA, "group_a should exist")
	require.NotNil(t, groupB, "group_b should exist")
	require.NotNil(t, groupDiana, "individual group for Diana should exist")

	// Verify group_a
	assert.True(t, groupA.HasResponded)
	assert.Contains(t, groupA.UnavailableDates, "Sun Jan 5 2025")
	assert.Contains(t, groupA.AvailableDates, "Sun Jan 12 2025")

	// Verify group_b
	assert.True(t, groupB.HasResponded)
	assert.Contains(t, groupB.UnavailableDates, "Sun Jan 12 2025")
	assert.Contains(t, groupB.AvailableDates, "Sun Jan 5 2025")

	// Verify Diana (individual)
	assert.Equal(t, "Diana Prince", groupDiana.GroupName)
	assert.True(t, groupDiana.HasResponded)
	assert.Empty(t, groupDiana.UnavailableDates)
	assert.Len(t, groupDiana.AvailableDates, 2)
}

func TestAggregateByGroup_AvailableForAll(t *testing.T) {
	shiftDates := []time.Time{
		time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 1, 12, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 1, 19, 0, 0, 0, 0, time.UTC),
	}

	volunteersByID := map[string]model.Volunteer{
		"v1": {ID: "v1", FirstName: "Alice", LastName: "Smith", GroupKey: "group_a"},
		"v2": {ID: "v2", FirstName: "Bob", LastName: "Jones", GroupKey: "group_a"},
	}

	responses := []VolunteerResponse{
		{
			VolunteerID:     "v1",
			VolunteerName:   "Alice Smith",
			HasResponded:    true,
			AvailableForAll: true,
			AvailableDates:  []string{"Sun Jan 5 2025", "Sun Jan 12 2025", "Sun Jan 19 2025"},
		},
		{
			VolunteerID:      "v2",
			VolunteerName:    "Bob Jones",
			HasResponded:     true,
			AvailableForAll:  false,
			UnavailableDates: []string{"Sun Jan 12 2025"},
			AvailableDates:   []string{"Sun Jan 5 2025", "Sun Jan 19 2025"},
		},
	}

	result := aggregateByGroup(responses, shiftDates, volunteersByID)

	require.Len(t, result, 1)
	group := result[0]

	assert.True(t, group.HasResponded)

	// Even though Alice is available for all, Bob is unavailable on Jan 12
	// So the group should be unavailable on Jan 12
	assert.Contains(t, group.UnavailableDates, "Sun Jan 12 2025")
	assert.Contains(t, group.AvailableDates, "Sun Jan 5 2025")
	assert.Contains(t, group.AvailableDates, "Sun Jan 19 2025")
	assert.NotContains(t, group.AvailableDates, "Sun Jan 12 2025")
}


func TestCalculateShiftAvailability_DefaultShiftSize(t *testing.T) {
	shiftDates := []time.Time{
		time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 1, 12, 0, 0, 0, 0, time.UTC),
	}

	cfg := &config.Config{
		DefaultShiftSize: 5,
		RotaOverrides:    []config.RotaOverride{},
	}

	volunteersByID := map[string]model.Volunteer{
		"v1": {ID: "v1", FirstName: "Alice", LastName: "Smith", Status: "Active", Role: model.RoleVolunteer},
		"v2": {ID: "v2", FirstName: "Bob", LastName: "Jones", Status: "Active", Role: model.RoleVolunteer},
		"v3": {ID: "v3", FirstName: "Charlie", LastName: "Brown", Status: "Active", Role: model.RoleVolunteer},
	}

	responses := []VolunteerResponse{
		{
			VolunteerID:      "v1",
			HasResponded:     true,
			UnavailableDates: []string{},
			AvailableDates:   []string{"Sun Jan 5 2025", "Sun Jan 12 2025"},
		},
		{
			VolunteerID:      "v2",
			HasResponded:     true,
			UnavailableDates: []string{},
			AvailableDates:   []string{"Sun Jan 5 2025", "Sun Jan 12 2025"},
		},
		{
			VolunteerID:      "v3",
			HasResponded:     true,
			UnavailableDates: []string{"Sun Jan 5 2025"},
			AvailableDates:   []string{"Sun Jan 12 2025"},
		},
	}

	logger := zap.NewNop()
	result := calculateShiftAvailability(responses, shiftDates, cfg, volunteersByID, logger)

	require.Len(t, result, 2)

	// First shift: 2 available (v1, v2), need 5, delta = -3, no team lead
	assert.Equal(t, "Sun Jan 5 2025", result[0].Date)
	assert.Equal(t, 5, result[0].ShiftSize)
	assert.Equal(t, 2, result[0].AvailableCount)
	assert.Equal(t, -3, result[0].Delta)
	assert.False(t, result[0].HasTeamLead)

	// Second shift: 3 available (v1, v2, v3), need 5, delta = -2, no team lead
	assert.Equal(t, "Sun Jan 12 2025", result[1].Date)
	assert.Equal(t, 5, result[1].ShiftSize)
	assert.Equal(t, 3, result[1].AvailableCount)
	assert.Equal(t, -2, result[1].Delta)
	assert.False(t, result[1].HasTeamLead)
}

func TestCalculateShiftAvailability_WithRRuleOverrides(t *testing.T) {
	shiftDates := []time.Time{
		time.Date(2025, 1, 4, 0, 0, 0, 0, time.UTC),  // Saturday
		time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC),  // Sunday
		time.Date(2025, 1, 11, 0, 0, 0, 0, time.UTC), // Saturday
	}

	shiftSize3 := 3
	cfg := &config.Config{
		DefaultShiftSize: 5, // Default is 5
		RotaOverrides: []config.RotaOverride{
			{
				RRule:     "FREQ=WEEKLY;BYDAY=SA", // Saturdays have different shift size
				ShiftSize: &shiftSize3,             // Override to 3 for Saturdays
			},
		},
	}

	volunteersByID := map[string]model.Volunteer{
		"v1": {ID: "v1", Status: "Active", Role: model.RoleVolunteer},
		"v2": {ID: "v2", Status: "Active", Role: model.RoleVolunteer},
		"v3": {ID: "v3", Status: "Active", Role: model.RoleVolunteer},
		"v4": {ID: "v4", Status: "Active", Role: model.RoleVolunteer},
	}

	responses := []VolunteerResponse{
		{VolunteerID: "v1", HasResponded: true, UnavailableDates: []string{}},
		{VolunteerID: "v2", HasResponded: true, UnavailableDates: []string{}},
		{VolunteerID: "v3", HasResponded: true, UnavailableDates: []string{}},
		{VolunteerID: "v4", HasResponded: true, UnavailableDates: []string{}},
	}

	logger := zap.NewNop()
	result := calculateShiftAvailability(responses, shiftDates, cfg, volunteersByID, logger)

	require.Len(t, result, 3)

	// Saturday Jan 4: 4 available, need 3 (override), delta = +1, no team lead
	assert.Equal(t, "Sat Jan 4 2025", result[0].Date)
	assert.Equal(t, 3, result[0].ShiftSize)
	assert.Equal(t, 4, result[0].AvailableCount)
	assert.Equal(t, 1, result[0].Delta)
	assert.False(t, result[0].HasTeamLead)

	// Sunday Jan 5: 4 available, need 5 (default), delta = -1, no team lead
	assert.Equal(t, "Sun Jan 5 2025", result[1].Date)
	assert.Equal(t, 5, result[1].ShiftSize)
	assert.Equal(t, 4, result[1].AvailableCount)
	assert.Equal(t, -1, result[1].Delta)
	assert.False(t, result[1].HasTeamLead)

	// Saturday Jan 11: 4 available, need 3 (override), delta = +1, no team lead
	assert.Equal(t, "Sat Jan 11 2025", result[2].Date)
	assert.Equal(t, 3, result[2].ShiftSize)
	assert.Equal(t, 4, result[2].AvailableCount)
	assert.Equal(t, 1, result[2].Delta)
	assert.False(t, result[2].HasTeamLead)
}

func TestCalculateShiftAvailability_ExcludesTeamLeadsFromCount(t *testing.T) {
	shiftDates := []time.Time{
		time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC),
	}

	cfg := &config.Config{
		DefaultShiftSize: 3,
	}

	volunteersByID := map[string]model.Volunteer{
		"v1": {ID: "v1", Status: "Active", Role: model.RoleTeamLead}, // Team lead - should not be counted but HasTeamLead should be true
		"v2": {ID: "v2", Status: "Active", Role: model.RoleVolunteer},
		"v3": {ID: "v3", Status: "Active", Role: model.RoleVolunteer},
	}

	responses := []VolunteerResponse{
		{VolunteerID: "v1", HasResponded: true, UnavailableDates: []string{}}, // Available team lead
		{VolunteerID: "v2", HasResponded: true, UnavailableDates: []string{}},
		{VolunteerID: "v3", HasResponded: true, UnavailableDates: []string{}},
	}

	logger := zap.NewNop()
	result := calculateShiftAvailability(responses, shiftDates, cfg, volunteersByID, logger)

	require.Len(t, result, 1)

	// Only count volunteers, not team leads: 2 available, need 3, delta = -1
	// But HasTeamLead should be true
	assert.Equal(t, 3, result[0].ShiftSize)
	assert.Equal(t, 2, result[0].AvailableCount)
	assert.Equal(t, -1, result[0].Delta)
	assert.True(t, result[0].HasTeamLead, "Should have team lead available")
}

func TestCalculateShiftAvailability_ExcludesInactive(t *testing.T) {
	shiftDates := []time.Time{
		time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC),
	}

	cfg := &config.Config{
		DefaultShiftSize: 3,
	}

	volunteersByID := map[string]model.Volunteer{
		"v1": {ID: "v1", Status: "Inactive", Role: model.RoleVolunteer}, // Inactive
		"v2": {ID: "v2", Status: "Active", Role: model.RoleVolunteer},
		"v3": {ID: "v3", Status: "Active", Role: model.RoleVolunteer},
	}

	responses := []VolunteerResponse{
		{VolunteerID: "v1", HasResponded: true, UnavailableDates: []string{}},
		{VolunteerID: "v2", HasResponded: true, UnavailableDates: []string{}},
		{VolunteerID: "v3", HasResponded: true, UnavailableDates: []string{}},
	}

	logger := zap.NewNop()
	result := calculateShiftAvailability(responses, shiftDates, cfg, volunteersByID, logger)

	require.Len(t, result, 1)

	// Only count active volunteers: 2 available, need 3, delta = -1, no team lead
	assert.Equal(t, 3, result[0].ShiftSize)
	assert.Equal(t, 2, result[0].AvailableCount)
	assert.Equal(t, -1, result[0].Delta)
	assert.False(t, result[0].HasTeamLead)
}

func TestCalculateShiftAvailability_ExcludesNonResponders(t *testing.T) {
	shiftDates := []time.Time{
		time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC),
	}

	cfg := &config.Config{
		DefaultShiftSize: 3,
	}

	volunteersByID := map[string]model.Volunteer{
		"v1": {ID: "v1", Status: "Active", Role: model.RoleVolunteer},
		"v2": {ID: "v2", Status: "Active", Role: model.RoleVolunteer},
		"v3": {ID: "v3", Status: "Active", Role: model.RoleVolunteer},
	}

	responses := []VolunteerResponse{
		{VolunteerID: "v1", HasResponded: false}, // Hasn't responded
		{VolunteerID: "v2", HasResponded: true, UnavailableDates: []string{}},
		{VolunteerID: "v3", HasResponded: true, UnavailableDates: []string{}},
	}

	logger := zap.NewNop()
	result := calculateShiftAvailability(responses, shiftDates, cfg, volunteersByID, logger)

	require.Len(t, result, 1)

	// Only count responders: 2 available, need 3, delta = -1, no team lead
	assert.Equal(t, 3, result[0].ShiftSize)
	assert.Equal(t, 2, result[0].AvailableCount)
	assert.Equal(t, -1, result[0].Delta)
	assert.False(t, result[0].HasTeamLead)
}

func TestCalculateShiftAvailability_TeamLeadAvailability(t *testing.T) {
	shiftDates := []time.Time{
		time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 1, 12, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 1, 19, 0, 0, 0, 0, time.UTC),
	}

	cfg := &config.Config{
		DefaultShiftSize: 2,
	}

	volunteersByID := map[string]model.Volunteer{
		"tl1": {ID: "tl1", Role: model.RoleTeamLead, Status: "Active"},
		"tl2": {ID: "tl2", Role: model.RoleTeamLead, Status: "Active"},
		"v1":  {ID: "v1", Role: model.RoleVolunteer, Status: "Active"},
		"v2":  {ID: "v2", Role: model.RoleVolunteer, Status: "Active"},
		"v3":  {ID: "v3", Role: model.RoleVolunteer, Status: "Active"},
		"v4":  {ID: "v4", Role: model.RoleVolunteer, Status: "Active"},
	}

	responses := []VolunteerResponse{
		// tl1: Available for Jan 5 only
		{VolunteerID: "tl1", HasResponded: true, UnavailableDates: []string{"Sun Jan 12 2025", "Sun Jan 19 2025"}},
		// tl2: Available for Jan 12 only
		{VolunteerID: "tl2", HasResponded: true, UnavailableDates: []string{"Sun Jan 5 2025", "Sun Jan 19 2025"}},
		// v1: Available for Jan 5 and Jan 19
		{VolunteerID: "v1", HasResponded: true, UnavailableDates: []string{"Sun Jan 12 2025"}},
		// v2: Available for Jan 5 and Jan 19
		{VolunteerID: "v2", HasResponded: true, UnavailableDates: []string{"Sun Jan 12 2025"}},
		// v3: Available for Jan 12 only
		{VolunteerID: "v3", HasResponded: true, UnavailableDates: []string{"Sun Jan 5 2025", "Sun Jan 19 2025"}},
		// v4: Available for Jan 12 only
		{VolunteerID: "v4", HasResponded: true, UnavailableDates: []string{"Sun Jan 5 2025", "Sun Jan 19 2025"}},
	}

	logger := zap.NewNop()
	result := calculateShiftAvailability(responses, shiftDates, cfg, volunteersByID, logger)

	require.Len(t, result, 3)

	// Jan 5: Has team lead (tl1), 2 volunteers (v1, v2) available
	assert.Equal(t, "Sun Jan 5 2025", result[0].Date)
	assert.Equal(t, 2, result[0].ShiftSize)
	assert.Equal(t, 2, result[0].AvailableCount)
	assert.Equal(t, 0, result[0].Delta)
	assert.True(t, result[0].HasTeamLead)

	// Jan 12: Has team lead (tl2), 2 volunteers (v3, v4) available
	assert.Equal(t, "Sun Jan 12 2025", result[1].Date)
	assert.Equal(t, 2, result[1].ShiftSize)
	assert.Equal(t, 2, result[1].AvailableCount)
	assert.Equal(t, 0, result[1].Delta)
	assert.True(t, result[1].HasTeamLead)

	// Jan 19: No team lead, 2 volunteers (v1, v2) available
	assert.Equal(t, "Sun Jan 19 2025", result[2].Date)
	assert.Equal(t, 2, result[2].ShiftSize)
	assert.Equal(t, 2, result[2].AvailableCount)
	assert.Equal(t, 0, result[2].Delta)
	assert.False(t, result[2].HasTeamLead)
}

func TestCalculateShiftAvailability_GroupBasedCounting(t *testing.T) {
	shiftDates := []time.Time{
		time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC),
		time.Date(2025, 1, 12, 0, 0, 0, 0, time.UTC),
	}

	cfg := &config.Config{
		DefaultShiftSize: 2,
	}

	volunteersByID := map[string]model.Volunteer{
		// Group "couple1" - Alice and Bob
		"alice": {ID: "alice", Role: model.RoleVolunteer, Status: "Active", GroupKey: "couple1"},
		"bob":   {ID: "bob", Role: model.RoleVolunteer, Status: "Active", GroupKey: "couple1"},
		// Individual volunteer
		"charlie": {ID: "charlie", Role: model.RoleVolunteer, Status: "Active", GroupKey: ""},
	}

	responses := []VolunteerResponse{
		// Only Alice from couple1 responds - but Bob should still be counted as available
		{VolunteerID: "alice", HasResponded: true, UnavailableDates: []string{"Sun Jan 12 2025"}},
		// Bob hasn't responded, but should be counted because Alice (his group member) responded
		{VolunteerID: "bob", HasResponded: false, UnavailableDates: []string{}},
		// Charlie responds individually
		{VolunteerID: "charlie", HasResponded: true, UnavailableDates: []string{}},
	}

	logger := zap.NewNop()
	result := calculateShiftAvailability(responses, shiftDates, cfg, volunteersByID, logger)

	require.Len(t, result, 2)

	// Jan 5: Should count Alice (responded), Bob (group member), and Charlie (responded) = 3 volunteers
	assert.Equal(t, "Sun Jan 5 2025", result[0].Date)
	assert.Equal(t, 2, result[0].ShiftSize)
	assert.Equal(t, 3, result[0].AvailableCount) // Alice + Bob + Charlie
	assert.Equal(t, 1, result[0].Delta)
	assert.False(t, result[0].HasTeamLead)

	// Jan 12: Alice (group member) marked unavailable, so entire couple1 group is unavailable
	// Should only count Charlie = 1 volunteer
	assert.Equal(t, "Sun Jan 12 2025", result[1].Date)
	assert.Equal(t, 2, result[1].ShiftSize)
	assert.Equal(t, 1, result[1].AvailableCount) // Only Charlie
	assert.Equal(t, -1, result[1].Delta)
	assert.False(t, result[1].HasTeamLead)
}

func TestCalculateShiftAvailability_WithPreallocations(t *testing.T) {
	shiftDates := []time.Time{
		time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC),  // Sunday
		time.Date(2025, 1, 12, 0, 0, 0, 0, time.UTC), // Sunday
		time.Date(2025, 1, 18, 0, 0, 0, 0, time.UTC), // Saturday
	}

	// Config with multiple overrides:
	// Rule 1: Sundays have shift size 5
	// Rule 2: Sundays have 2 preallocations (John, Jane)
	// Rule 3: First Sunday of month has 1 additional preallocation (Bob)
	// So Jan 5 (first Sunday): size 5, preallocations 3 (John + Jane + Bob) = effective 2
	// Jan 12 (regular Sunday): size 5, preallocations 2 (John + Jane) = effective 3
	// Jan 18 (Saturday): default size 4, no preallocations = effective 4
	cfg := &config.Config{
		DefaultShiftSize: 4,
		RotaOverrides: []config.RotaOverride{
			{
				// All Sundays: shift size 5
				RRule:     "DTSTART:20250105T000000Z\nRRULE:FREQ=WEEKLY;BYDAY=SU",
				ShiftSize: intPtr(5),
			},
			{
				// All Sundays: preallocate John and Jane
				RRule:                "DTSTART:20250105T000000Z\nRRULE:FREQ=WEEKLY;BYDAY=SU",
				CustomPreallocations: []string{"External John", "External Jane"},
			},
			{
				// First Sunday of month: additional preallocation Bob
				RRule:                "DTSTART:20250105T000000Z\nRRULE:FREQ=MONTHLY;BYDAY=1SU",
				CustomPreallocations: []string{"External Bob"},
			},
		},
	}

	volunteersByID := map[string]model.Volunteer{
		"v1": {ID: "v1", Role: model.RoleVolunteer, Status: "Active"},
		"v2": {ID: "v2", Role: model.RoleVolunteer, Status: "Active"},
		"v3": {ID: "v3", Role: model.RoleVolunteer, Status: "Active"},
		"v4": {ID: "v4", Role: model.RoleVolunteer, Status: "Active"},
	}

	responses := []VolunteerResponse{
		{VolunteerID: "v1", HasResponded: true, UnavailableDates: []string{}},
		{VolunteerID: "v2", HasResponded: true, UnavailableDates: []string{}},
		{VolunteerID: "v3", HasResponded: true, UnavailableDates: []string{}},
		{VolunteerID: "v4", HasResponded: true, UnavailableDates: []string{}},
	}

	logger := zap.NewNop()
	result := calculateShiftAvailability(responses, shiftDates, cfg, volunteersByID, logger)

	require.Len(t, result, 3)

	// Jan 5 (first Sunday): shift size 5, preallocations 3 (John+Jane from rule 2, Bob from rule 3) = effective 2
	// Have 4 volunteers available, so delta = +2
	assert.Equal(t, "Sun Jan 5 2025", result[0].Date)
	assert.Equal(t, 2, result[0].ShiftSize) // 5 - 3 preallocations
	assert.Equal(t, 4, result[0].AvailableCount)
	assert.Equal(t, 2, result[0].Delta)

	// Jan 12 (regular Sunday): shift size 5, preallocations 2 (John+Jane from rule 2) = effective 3
	// Have 4 volunteers available, so delta = +1
	assert.Equal(t, "Sun Jan 12 2025", result[1].Date)
	assert.Equal(t, 3, result[1].ShiftSize) // 5 - 2 preallocations
	assert.Equal(t, 4, result[1].AvailableCount)
	assert.Equal(t, 1, result[1].Delta)

	// Jan 18 (Saturday): default size 4, no preallocations = effective 4
	// Have 4 volunteers available, so delta = 0
	assert.Equal(t, "Sat Jan 18 2025", result[2].Date)
	assert.Equal(t, 4, result[2].ShiftSize) // default, no preallocations
	assert.Equal(t, 4, result[2].AvailableCount)
	assert.Equal(t, 0, result[2].Delta)
}

// Helper function to create int pointer
func intPtr(i int) *int {
	return &i
}
