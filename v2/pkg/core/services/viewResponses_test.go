package services

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
