package rotageneration

import (
	"fmt"
)

// VolunteerAvailability represents a volunteer's availability response
type VolunteerAvailability struct {
	VolunteerID string
	HasResponded bool
	// UnavailableShiftIndices contains shift indices this volunteer marked as unavailable
	// Only meaningful if HasResponded is true
	UnavailableShiftIndices []int
}

// InitVolunteerGroupsInput contains the raw data needed to initialize volunteer groups
type InitVolunteerGroupsInput struct {
	// Volunteers is the list of all volunteers
	Volunteers []Volunteer

	// Availability is the list of availability responses from volunteers
	Availability []VolunteerAvailability

	// TotalShifts is the total number of shifts in the current rota
	TotalShifts int

	// HistoricalShifts for calculating historical frequency per group
	HistoricalShifts []*Shift
}

// InitVolunteerGroups creates and initializes volunteer groups from raw volunteer data
// Groups volunteers by GroupKey, calculates metadata, and filters out invalid groups.
//
// Availability logic (matching ViewResponses):
//   - A group has responded if ANY member has responded
//   - A group is unavailable on a date if ANY responding member marked it unavailable
//   - Non-responding members don't affect availability
//
// Returns:
//   - A slice of initialized VolunteerGroups
//   - Error if initialization fails
//
// Invalid groups (discarded):
//   - Groups where no members have responded
//   - Groups with more than one team lead
func InitVolunteerGroups(input InitVolunteerGroupsInput) ([]*VolunteerGroup, error) {
	// Build availability lookup map
	availabilityMap := make(map[string]VolunteerAvailability)
	for _, avail := range input.Availability {
		availabilityMap[avail.VolunteerID] = avail
	}

	// Step 1: Group volunteers by GroupKey
	groupMap := make(map[string][]Volunteer)

	for _, volunteer := range input.Volunteers {
		groupKey := volunteer.GroupKey
		if groupKey == "" {
			// Individual volunteer - create unique group key
			groupKey = "individual_" + volunteer.ID
		}
		groupMap[groupKey] = append(groupMap[groupKey], volunteer)
	}

	// Step 2: Build VolunteerGroup objects
	groups := make([]*VolunteerGroup, 0, len(groupMap))

	for groupKey, members := range groupMap {
		// Validate: No group can have more than 1 team lead
		teamLeadCount := 0
		maleCount := 0
		hasTeamLead := false

		for _, member := range members {
			if member.IsTeamLead {
				teamLeadCount++
				hasTeamLead = true
			}
			if member.Gender == "Male" {
				maleCount++
			}
		}

		if teamLeadCount > 1 {
			// Invalid group - skip it
			continue
		}

		// Calculate availability for the group
		// Group has responded if ANY member responded
		groupHasResponded := false
		unavailableSet := make(map[int]bool)

		for _, member := range members {
			memberAvail, exists := availabilityMap[member.ID]
			if !exists {
				continue
			}

			if memberAvail.HasResponded {
				groupHasResponded = true

				// Add this member's unavailable dates to the group's unavailable set
				for _, shiftIdx := range memberAvail.UnavailableShiftIndices {
					unavailableSet[shiftIdx] = true
				}
			}
			// If member hasn't responded, they don't affect availability
		}

		// Discard groups where no one has responded
		if !groupHasResponded {
			continue
		}

		// Calculate available shift indices
		// All shifts EXCEPT those in the unavailable set
		availableShiftIndices := make([]int, 0)
		for shiftIdx := 0; shiftIdx < input.TotalShifts; shiftIdx++ {
			if !unavailableSet[shiftIdx] {
				availableShiftIndices = append(availableShiftIndices, shiftIdx)
			}
		}

		// Discard groups with no availability
		if len(availableShiftIndices) == 0 {
			continue
		}

		// Calculate historical allocation count for this group
		historicalAllocationCount := calculateHistoricalAllocationCount(groupKey, input.HistoricalShifts)

		// Create the volunteer group
		group := &VolunteerGroup{
			GroupKey:                  groupKey,
			Members:                   members,
			AvailableShiftIndices:     availableShiftIndices,
			AllocatedShiftIndices:     []int{},
			HistoricalAllocationCount: historicalAllocationCount,
			HasTeamLead:               hasTeamLead,
			MaleCount:                 maleCount,
		}

		groups = append(groups, group)
	}

	if len(groups) == 0 {
		return nil, fmt.Errorf("no valid volunteer groups after initialization")
	}

	return groups, nil
}

// calculateHistoricalAllocationCount counts how many times a group was allocated in historical shifts
func calculateHistoricalAllocationCount(groupKey string, historicalShifts []*Shift) int {
	count := 0

	for _, shift := range historicalShifts {
		for _, allocatedGroup := range shift.AllocatedGroups {
			if allocatedGroup.GroupKey == groupKey {
				count++
				break // Count each shift only once per group
			}
		}
	}

	return count
}
