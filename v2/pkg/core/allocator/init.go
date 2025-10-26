package allocator

import (
	"fmt"
	"sort"
)

// VolunteerAvailability represents a volunteer's availability response
type VolunteerAvailability struct {
	VolunteerID  string
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
//   - A VolunteerState with initialized groups and empty exhaustion map
//   - Error if initialization fails
//
// Invalid groups (errors returned):
//   - Groups with more than one team lead
//
// Invalid groups (discarded):
//   - Groups where no members have responded
//   - Groups with no availability
func InitVolunteerGroups(input InitVolunteerGroupsInput) (*VolunteerState, error) {
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
			if member.Gender == GenderMale {
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

	// Sort groups deterministically by GroupKey to ensure consistent ordering
	// This prevents flaky tests due to Go's randomized map iteration order
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].GroupKey < groups[j].GroupKey
	})

	// Create VolunteerState with empty exhaustion map
	volunteerState := &VolunteerState{
		VolunteerGroups:          groups,
		ExhaustedVolunteerGroups: make(map[*VolunteerGroup]bool),
	}

	return volunteerState, nil
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

	// CustomPreallocations are volunteers manually assigned to this shift.
	CustomPreallocations []string
}

// InitShiftsInput contains the data needed to initialize shifts
type InitShiftsInput struct {
	// ShiftDates is the list of dates for shifts in the current rota
	ShiftDates []string

	// DefaultShiftSize is the default number of volunteers per shift
	DefaultShiftSize int

	// Overrides allow customizing specific shifts
	Overrides []ShiftOverride

	// VolunteerState contains the initialized volunteer groups
	// Used to populate each shift's AvailableGroups
	VolunteerState *VolunteerState
}

// InitShifts creates and initializes shifts for the rota
//
// Returns a slice of initialized Shift objects with:
//   - Sequential indices
//   - Applied size overrides
//   - Pre-allocated volunteer IDs (metadata flags start at false/0)
//   - AvailableGroups populated based on volunteer group availability
func InitShifts(input InitShiftsInput) ([]*Shift, error) {
	shifts := make([]*Shift, len(input.ShiftDates))

	for i, date := range input.ShiftDates {
		// Start with default shift size
		shiftSize := input.DefaultShiftSize

		// Track pre-allocated volunteers
		var customPreallocations []string

		// Apply overrides for this date
		for _, override := range input.Overrides {
			if override.AppliesTo(date) {
				// Override size if specified
				if override.ShiftSize != nil {
					shiftSize = *override.ShiftSize
				}

				// Add pre-allocated volunteers
				customPreallocations = append(customPreallocations, override.CustomPreallocations...)
			}
		}

		// Populate available groups for this shift
		availableGroups := make([]*VolunteerGroup, 0)
		for _, group := range input.VolunteerState.VolunteerGroups {
			if group.IsAvailable(i) {
				availableGroups = append(availableGroups, group)
			}
		}

		shifts[i] = &Shift{
			Date:                 date,
			Index:                i,
			Size:                 shiftSize,
			AllocatedGroups:      []*VolunteerGroup{},
			CustomPreallocations: customPreallocations,
			TeamLead:             nil, // Will be set when a team lead is allocated
			MaleCount:            0,   // Will be updated when groups are allocated
			AvailableGroups:      availableGroups,
		}
	}

	return shifts, nil
}

func InitAllocation(config AllocationConfig) (Allocator, error) {
	// Validate config
	if len(config.ShiftDates) == 0 {
		return Allocator{}, fmt.Errorf("no shift dates provided")
	}
	if len(config.Volunteers) == 0 {
		return Allocator{}, fmt.Errorf("no volunteers provided")
	}
	if config.DefaultShiftSize < 0 {
		return Allocator{}, fmt.Errorf("default shift size must be non-negative, got %d", config.DefaultShiftSize)
	}
	if config.MaxAllocationFrequency <= 0 || config.MaxAllocationFrequency > 1 {
		return Allocator{}, fmt.Errorf("max allocation frequency must be between 0 and 1, got %.2f", config.MaxAllocationFrequency)
	}

	volunteerState, err := InitVolunteerGroups(
		InitVolunteerGroupsInput{
			Volunteers:       config.Volunteers,
			Availability:     config.Availability,
			TotalShifts:      len(config.ShiftDates),
			HistoricalShifts: config.HistoricalShifts,
		},
	)
	if err != nil {
		return Allocator{}, err
	}

	shifts, err := InitShifts(
		InitShiftsInput{
			ShiftDates:       config.ShiftDates,
			DefaultShiftSize: config.DefaultShiftSize,
			Overrides:        config.Overrides,
			VolunteerState:   volunteerState,
		},
	)
	if err != nil {
		return Allocator{}, err
	}

	// Create initial rota state
	state := &RotaState{
		Shifts:                         shifts,
		VolunteerState:                 volunteerState,
		HistoricalShifts:               config.HistoricalShifts,
		MaxAllocationFrequency:         config.MaxAllocationFrequency,
		WeightCurrentRotaUrgency:       config.WeightCurrentRotaUrgency,
		WeightOverallFrequencyFairness: config.WeightOverallFrequencyFairness,
		WeightPromoteGroup:             config.WeightPromoteGroup,
	}

	RankVolunteerGroups(state, config.Criteria, config.MaxAllocationFrequency)

	return Allocator{
		criteria: config.Criteria,
		state:    state,
	}, nil
}
