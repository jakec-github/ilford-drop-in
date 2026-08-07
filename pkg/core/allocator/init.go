package allocator

import (
	"fmt"
	"slices"
	"sort"
)

// GroupKeyFor returns the key binding a volunteer to the people they are
// allocated alongside. A volunteer with no group is their own group of one,
// keyed on their name.
func GroupKeyFor(volunteer Volunteer) string {
	if volunteer.GroupKey == "" {
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
		group := BuildVolunteerGroup(members)

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

// withPreallocatedAvailability returns groupAvailability with every pinned
// group marked available for the shift it is pinned to, leaving the caller's
// map untouched.
//
// A pin is a decision already taken: someone has been asked to work that shift,
// so the availability question for it is settled and an answer that never
// arrived cannot unsettle it. Without this, InitVolunteerGroups discards the
// group of anyone who did not reply — including the person who was pinned —
// and the solver then fails on a pin naming somebody who is not in the problem.
//
// The grant is per shift, so a pinned group is available where it is pinned and
// nowhere else it did not claim. It is group-atomic to match the solver, which
// forces every member of a pinned group onto the shift. Pins are read from
// resolved shift specs rather than raw overrides so that a closed shift, whose
// pins InitShifts strips, implies nothing.
func withPreallocatedAvailability(groupAvailability map[string][]int, shifts []*Shift, volunteers []Volunteer) map[string][]int {
	groupKeyByVolunteerID := make(map[string]string, len(volunteers))
	for _, volunteer := range volunteers {
		groupKeyByVolunteerID[volunteer.ID] = GroupKeyFor(volunteer)
	}

	augmented := make(map[string][]int, len(groupAvailability))
	for key, indices := range groupAvailability {
		augmented[key] = append([]int(nil), indices...)
	}

	granted := make(map[string]bool)
	for _, shift := range shifts {
		for _, pin := range shift.Preallocations {
			if pin.VolunteerID == "" {
				// A custom entry is not a person, so there is nobody whose
				// availability it could settle.
				continue
			}
			key, known := groupKeyByVolunteerID[pin.VolunteerID]
			if !known {
				// A pin naming nobody on the roster is reported by the
				// pre-solve check and by the solver, each of which can name it.
				// Inventing a group for it here would only hide that.
				continue
			}
			if slices.Contains(augmented[key], shift.Index) {
				continue
			}
			augmented[key] = append(augmented[key], shift.Index)
			granted[key] = true
		}
	}

	// Shift indices are read as a set everywhere, but sorted is what an answer
	// from the store looks like, and a stable input is worth the sort.
	for key := range granted {
		slices.Sort(augmented[key])
	}

	return augmented
}

// BuildVolunteerGroup creates a VolunteerGroup from a list of volunteers.
// This function encapsulates the logic for building a group with correct metadata.
//
// The key is derived — GroupKeyFor(members[0]) — rather than passed in, so
// GroupKeyFor stays the single rule for what a group key is and no caller can
// hold a different one. Every caller buckets by GroupKeyFor already, so any
// member yields the same key. An empty member list has no key.
//
// Returns a VolunteerGroup with calculated metadata (MaleCount)
// Note: AvailableShiftIndices, AllocatedShiftIndices, and HistoricalAllocationCount
// must be set by the caller as they depend on context.
func BuildVolunteerGroup(members []Volunteer) *VolunteerGroup {
	groupKey := ""
	if len(members) > 0 {
		groupKey = GroupKeyFor(members[0])
	}

	// Calculate group metadata
	maleCount := 0

	for _, member := range members {
		if member.Gender == GenderMale {
			maleCount++
		}
	}

	return &VolunteerGroup{
		GroupKey:  groupKey,
		Members:   members,
		MaleCount: maleCount,
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

// ShiftOverride allows customizing specific shifts based on date patterns.
//
// All it can say now is who is pinned: it carried a shift size until the Shape
// stopped being a number (issue #129), and closures and config pins left before
// that. Every override that reaches InitShifts is a Preallocation wearing a
// date matcher.
type ShiftOverride struct {
	// AppliesTo is a function that returns true if this override applies to the given shift date
	AppliesTo func(date string) bool

	// Preallocations are the pins this override contributes, each naming a Role.
	Preallocations []Preallocation
}

// ShiftSpec is one minted shift as the solver's model receives it, before
// anything is resolved: which date it falls on, what it asks for, and whether
// the drop-in runs that day. Closed arrives here rather than being derived from
// an override because it is a field on the Shift, set by hand (issue #132).
type ShiftSpec struct {
	Date string

	// Shape is the Seats this shift asks for: which Roles, and how many of
	// each, already resolved to Role names. It is per shift rather than one
	// Shape for the rota because a Shift owns its Shape — stored when the rota
	// was defined, and untouched by any later edit to the settings (issue
	// #137). An empty Shape is a shift asking for nobody, which allocation
	// refuses before it gets here.
	Shape []Seat

	Closed bool
}

// InitShiftsInput contains the data needed to initialize shifts
type InitShiftsInput struct {
	// Shifts is the current rota's minted shifts, in date order
	Shifts []ShiftSpec

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
//   - The Shape each shift arrived asking for (issue #137)
//   - Preallocations unioned from every override applying to the date
//   - AvailableGroups populated based on volunteer group availability
//
// Every override applying to a date contributes its pins. Pins used to be three
// separate fields with three merge rules — the single team lead was
// last-one-wins where the two lists appended. They are one list now, so
// appending is the only rule, and two overrides pinning the same capped Role
// for one date are both kept rather than one silently disappearing. The Role's
// ceiling is what catches that, in the solver.
//
// A closed shift keeps neither pins nor available groups: nobody works a day
// the drop-in is shut, whichever of the two sources promised them. Stripping
// the pins here is also what the Python side's contract rests on — it refuses
// an input pinning anyone to a closed shift.
func InitShifts(input InitShiftsInput) ([]*Shift, error) {
	shifts := make([]*Shift, len(input.Shifts))

	for i, spec := range input.Shifts {
		var preallocations []Preallocation

		// Apply overrides for this date
		for _, override := range input.Overrides {
			if override.AppliesTo(spec.Date) {
				preallocations = append(preallocations, override.Preallocations...)
			}
		}

		// Populate available groups for this shift (skip if closed)
		availableGroups := make([]*VolunteerGroup, 0)
		if spec.Closed {
			preallocations = nil
		} else {
			for _, group := range input.VolunteerState.VolunteerGroups {
				if group.IsAvailable(i) {
					availableGroups = append(availableGroups, group)
				}
			}
		}

		shifts[i] = &Shift{
			Date:            spec.Date,
			Index:           i,
			Shape:           spec.Shape,
			AllocatedGroups: []*VolunteerGroup{},
			MaleCount:       0, // Will be updated when groups are allocated
			AvailableGroups: availableGroups,
			Closed:          spec.Closed,
			Preallocations:  preallocations,
		}
	}

	return shifts, nil
}
