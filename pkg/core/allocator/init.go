package allocator

import (
	"fmt"
	"sort"
)

// GroupKeyFor returns the key binding a volunteer to the people they are
// allocated alongside. A volunteer with no group is their own group of one,
// keyed on their name. "None" may be set in the spreadsheet and does not equate
// to a group.
func GroupKeyFor(volunteer Volunteer) string {
	if volunteer.GroupKey == "" || volunteer.GroupKey == "None" {
		return volunteer.FirstName + " " + volunteer.LastName
	}
	return volunteer.GroupKey
}

// InitVolunteerGroupsInput contains the raw data needed to initialize volunteer groups
type InitVolunteerGroupsInput struct {
	// Volunteers is the list of all volunteers
	Volunteers []Volunteer

	// GroupAvailability is each group's settled answer: the shift indices the
	// group can work, keyed by GroupKeyFor. A group absent from the map has not
	// been answered for by anybody and is discarded.
	//
	// The rule turning per-volunteer answers into this lives with the
	// availability store, not here — a group is available iff at least one
	// member answered and every member who answered said yes (ADR 0004). The
	// allocator used to keep its own copy of that rule, which is how the same
	// logic came to be written three times over.
	GroupAvailability map[string][]int

	// HistoricalShifts for calculating historical frequency per group
	HistoricalShifts []*Shift
}

// InitVolunteerGroups creates and initializes volunteer groups from raw volunteer data
// Groups volunteers by GroupKey, calculates metadata, and filters out invalid groups.
//
// Returns:
//   - A VolunteerState with initialized groups and empty exhaustion map
//   - Error if initialization fails
//
// A group used to be rejected for holding two team leads, because the solver
// then capped leads per group rather than per Seat. Seats do that capping now,
// and a second lead is free to take an ordinary Seat, so the rule is gone (#89).
//
// Invalid groups (discarded):
//   - Groups nobody answered for
//   - Groups with no availability
func InitVolunteerGroups(input InitVolunteerGroupsInput) (*VolunteerState, error) {
	// Step 1: Group volunteers by GroupKey
	groupMap := make(map[string][]Volunteer)

	for _, volunteer := range input.Volunteers {
		groupKey := GroupKeyFor(volunteer)
		groupMap[groupKey] = append(groupMap[groupKey], volunteer)
	}

	// Step 2: Build VolunteerGroup objects
	groups := make([]*VolunteerGroup, 0, len(groupMap))

	for groupKey, members := range groupMap {
		// An absent key is a group nobody answered for; a present but empty one
		// is a group that answered and can work nothing. Both are discarded, but
		// only the first is "no reply".
		availableShiftIndices, answered := input.GroupAvailability[groupKey]
		if !answered || len(availableShiftIndices) == 0 {
			continue
		}

		// Create the volunteer group using the shared builder
		group := BuildVolunteerGroup(groupKey, members)

		// Set context-specific fields
		group.AvailableShiftIndices = availableShiftIndices
		group.AllocatedShiftIndices = []int{}
		group.HistoricalAllocationCount = calculateHistoricalAllocationCount(group.GroupKey, input.HistoricalShifts)

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

	volunteerState := &VolunteerState{
		VolunteerGroups: groups,
	}

	return volunteerState, nil
}

// BuildVolunteerGroup creates a VolunteerGroup from a list of volunteers.
// This function encapsulates the logic for building a group with correct metadata.
// It handles individual volunteers (empty GroupKey) by creating a unique group key.
//
// Parameters:
//   - groupKey: The group key (empty string for individual volunteers)
//   - members: The volunteers in this group
//
// Returns a VolunteerGroup with calculated metadata (HasTeamLead, MaleCount)
// Note: AvailableShiftIndices, AllocatedShiftIndices, and HistoricalAllocationCount
// must be set by the caller as they depend on context.
func BuildVolunteerGroup(groupKey string, members []Volunteer) *VolunteerGroup {
	// For individual volunteers, create a unique group key
	effectiveGroupKey := groupKey
	if effectiveGroupKey == "" && len(members) > 0 {
		effectiveGroupKey = members[0].FirstName + " " + members[0].LastName
	}

	// Calculate group metadata
	hasTeamLead := false
	maleCount := 0

	for _, member := range members {
		if member.IsTeamLead {
			hasTeamLead = true
		}
		if member.Gender == GenderMale {
			maleCount++
		}
	}

	return &VolunteerGroup{
		GroupKey:    effectiveGroupKey,
		Members:     members,
		HasTeamLead: hasTeamLead,
		MaleCount:   maleCount,
		// Note: Caller must set AvailableShiftIndices, AllocatedShiftIndices,
		// and HistoricalAllocationCount based on their context
	}
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

	// Closed indicates whether this shift should be marked as closed (no allocations)
	Closed bool

	// Preallocations are the pins this override contributes, each naming a Role.
	Preallocations []Preallocation
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
//   - Preallocations unioned from every override applying to the date
//   - AvailableGroups populated based on volunteer group availability
//
// Every override applying to a date contributes its pins; a closing override
// wipes the lot. Pins used to be three separate fields with three merge rules —
// the single team lead was last-one-wins where the two lists appended. They are
// one list now, so appending is the only rule, and two overrides pinning the
// same capped Role for one date are both kept rather than one silently
// disappearing. The Role's ceiling is what catches that, in the solver.
func InitShifts(input InitShiftsInput) ([]*Shift, error) {
	shifts := make([]*Shift, len(input.ShiftDates))

	for i, date := range input.ShiftDates {
		// Start with default shift size
		shiftSize := input.DefaultShiftSize

		var preallocations []Preallocation

		// Track if shift is closed
		isClosed := false

		// Apply overrides for this date
		for _, override := range input.Overrides {
			if override.AppliesTo(date) {
				// Override size if specified
				if override.ShiftSize != nil {
					shiftSize = *override.ShiftSize
				}

				// Add pre-allocated volunteers (only if not closed)
				if !override.Closed {
					preallocations = append(preallocations, override.Preallocations...)
				}

				// Mark as closed if any override marks it closed
				if override.Closed {
					isClosed = true
					preallocations = nil
				}
			}
		}

		// Populate available groups for this shift (skip if closed)
		availableGroups := make([]*VolunteerGroup, 0)
		if !isClosed {
			for _, group := range input.VolunteerState.VolunteerGroups {
				if group.IsAvailable(i) {
					availableGroups = append(availableGroups, group)
				}
			}
		}

		shifts[i] = &Shift{
			Date:            date,
			Index:           i,
			Size:            shiftSize,
			AllocatedGroups: []*VolunteerGroup{},
			TeamLead:        nil, // Will be set when a team lead is allocated
			MaleCount:       0,   // Will be updated when groups are allocated
			AvailableGroups: availableGroups,
			Closed:          isClosed,
			Preallocations:  preallocations,
		}
	}

	return shifts, nil
}
