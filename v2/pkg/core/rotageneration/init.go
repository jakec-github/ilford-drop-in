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
// Invalid groups (errors returned):
//   - Groups with more than one team lead
//
// Invalid groups (discarded):
//   - Groups where no members have responded
//   - Groups with no availability
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
			// Invalid group - return error with details
			memberNames := make([]string, len(members))
			for i, member := range members {
				memberNames[i] = member.FirstName + " " + member.LastName
			}
			return nil, fmt.Errorf("group '%s' has %d team leads (max 1 allowed): %v",
				groupKey, teamLeadCount, memberNames)
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

// ShiftOverride allows customizing specific shifts based on date patterns
type ShiftOverride struct {
	// AppliesTo is a function that returns true if this override applies to the given shift date
	AppliesTo func(date string) bool

	// ShiftSize overrides the default shift size (if set)
	ShiftSize *int

	// PreAllocatedVolunteers are volunteers manually assigned to this shift.
	PreAllocatedVolunteers []string
}

// InitShiftsInput contains the data needed to initialize shifts
type InitShiftsInput struct {
	// ShiftDates is the list of dates for shifts in the current rota
	ShiftDates []string

	// DefaultShiftSize is the default number of volunteers per shift
	DefaultShiftSize int

	// Overrides allow customizing specific shifts
	Overrides []ShiftOverride

	// VolunteerGroups is the list of initialized volunteer groups
	// Used to populate each shift's AvailableGroupIndices
	VolunteerGroups []*VolunteerGroup
}

// InitShifts creates and initializes shifts for the rota
//
// Returns a slice of initialized Shift objects with:
//   - Sequential indices
//   - Applied size overrides
//   - Pre-allocated volunteer IDs (metadata flags start at false/0)
//   - AvailableGroupIndices populated based on volunteer group availability
func InitShifts(input InitShiftsInput) ([]*Shift, error) {
	shifts := make([]*Shift, len(input.ShiftDates))

	for i, date := range input.ShiftDates {
		// Start with default shift size
		shiftSize := input.DefaultShiftSize

		// Track pre-allocated volunteers
		var preAllocatedVolunteers []string

		// Apply overrides for this date
		for _, override := range input.Overrides {
			if override.AppliesTo(date) {
				// Override size if specified
				if override.ShiftSize != nil {
					shiftSize = *override.ShiftSize
				}

				// Add pre-allocated volunteers
				preAllocatedVolunteers = append(preAllocatedVolunteers, override.PreAllocatedVolunteers...)
			}
		}

		// Populate available group indices for this shift
		availableGroupIndices := make([]int, 0)
		for groupIdx, group := range input.VolunteerGroups {
			if group.IsAvailable(i) {
				availableGroupIndices = append(availableGroupIndices, groupIdx)
			}
		}

		shifts[i] = &Shift{
			Date:                   date,
			Index:                  i,
			Size:                   shiftSize,
			AllocatedGroups:        []*VolunteerGroup{},
			PreAllocatedVolunteers: preAllocatedVolunteers,
			TeamLead:               nil, // Will be set when a team lead is allocated
			MaleCount:              0,   // Will be updated when groups are allocated
			AvailableGroupIndices:  availableGroupIndices,
		}
	}

	return shifts, nil
}
