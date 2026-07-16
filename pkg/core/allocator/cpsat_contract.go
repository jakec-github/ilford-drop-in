package allocator

import (
	"fmt"
	"sort"
)

// This file defines the JSON contract between the Go CLI and the Python
// CP-SAT allocator (pyallocator/README.md documents the same contract on
// the Python side), plus the conversions to and from allocator types.
//
// Go owns grouping: volunteer groups and their resolved availability are
// built here via InitVolunteerGroups and sent to Python, which
// only does arithmetic over them.

// CpsatMember is one volunteer inside a group.
type CpsatMember struct {
	ID          string `json:"id"`
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name"`
	DisplayName string `json:"display_name"`
	Gender      string `json:"gender"`
	IsTeamLead  bool   `json:"is_team_lead"`
}

// CpsatGroup is an allocation unit (couples/families allocated together)
// with availability already resolved to shift indices.
type CpsatGroup struct {
	GroupKey                  string        `json:"group_key"`
	Members                   []CpsatMember `json:"members"`
	AvailableShiftIndices     []int         `json:"available_shift_indices"`
	HistoricalAllocationCount int           `json:"historical_allocation_count"`
}

// CpsatShift is an override-resolved shift specification.
type CpsatShift struct {
	Index                    int      `json:"index"`
	Date                     string   `json:"date"`
	Size                     int      `json:"size"`
	Closed                   bool     `json:"closed"`
	CustomPreallocations     []string `json:"custom_preallocations"`
	PreallocatedVolunteerIDs []string `json:"preallocated_volunteer_ids"`
	PreallocatedTeamLeadID   string   `json:"preallocated_team_lead_id"`
}

// CpsatHistoricalShift is a past shift with Go-derived group keys.
type CpsatHistoricalShift struct {
	Date      string   `json:"date"`
	GroupKeys []string `json:"group_keys"`
}

// CpsatInput is the full problem sent to Python on stdin.
type CpsatInput struct {
	MaxAllocationCount int                    `json:"max_allocation_count"`
	Shifts             []CpsatShift           `json:"shifts"`
	Groups             []CpsatGroup           `json:"groups"`
	HistoricalShifts   []CpsatHistoricalShift `json:"historical_shifts"`
}

// CpsatOutputShift is one solved shift. TeamLeadID is "" when the shift
// has no team lead (expected and common; filled in manually later).
type CpsatOutputShift struct {
	Index                int      `json:"index"`
	Date                 string   `json:"date"`
	Size                 int      `json:"size"`
	Closed               bool     `json:"closed"`
	TeamLeadID           string   `json:"team_lead_id"`
	VolunteerIDs         []string `json:"volunteer_ids"`
	CustomPreallocations []string `json:"custom_preallocations"`
	AllocatedGroupKeys   []string `json:"allocated_group_keys"`
}

// CpsatDiagnostics reports solver metadata for logging/inspection.
type CpsatDiagnostics struct {
	SolveTimeSeconds   float64  `json:"solve_time_seconds"`
	NumGroups          int      `json:"num_groups"`
	NumVariables       int      `json:"num_variables"`
	ConstraintsApplied []string `json:"constraints_applied"`
}

// CpsatOutput is the solved rota returned by Python on stdout.
// Success is true iff SolverStatus is OPTIMAL or FEASIBLE; INFEASIBLE is
// a well-formed result (no rota produced), not a subprocess failure.
type CpsatOutput struct {
	SolverStatus   string             `json:"solver_status"`
	Success        bool               `json:"success"`
	Error          string             `json:"error"`
	ObjectiveValue int                `json:"objective_value"`
	Shifts         []CpsatOutputShift `json:"shifts"`
	Diagnostics    CpsatDiagnostics   `json:"diagnostics"`
}

// BuildCpsatInput assembles the Python allocator's input, reusing the
// package's model initialisation so grouping, availability resolution and
// override application are never duplicated.
func BuildCpsatInput(
	volunteers []Volunteer,
	availability []VolunteerAvailability,
	shiftDateStrings []string,
	defaultShiftSize int,
	overrides []ShiftOverride,
	historicalShifts []*Shift,
	maxAllocationFrequency float64,
) (*CpsatInput, error) {
	volunteerState, err := InitVolunteerGroups(InitVolunteerGroupsInput{
		Volunteers:       volunteers,
		Availability:     availability,
		TotalShifts:      len(shiftDateStrings),
		HistoricalShifts: historicalShifts,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize volunteer groups: %w", err)
	}

	// InitShifts resolves per-shift size/closed/preallocations from the
	// overrides. AvailableGroups isn't part of the contract (Python
	// derives availability from groups), so an empty state suffices.
	shiftSpecs, err := InitShifts(InitShiftsInput{
		ShiftDates:       shiftDateStrings,
		DefaultShiftSize: defaultShiftSize,
		Overrides:        overrides,
		VolunteerState:   &VolunteerState{VolunteerGroups: []*VolunteerGroup{}},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize shifts: %w", err)
	}

	input := &CpsatInput{
		MaxAllocationCount: int(float64(len(shiftDateStrings)) * maxAllocationFrequency),
		Shifts:             make([]CpsatShift, len(shiftSpecs)),
		Groups:             make([]CpsatGroup, len(volunteerState.VolunteerGroups)),
		HistoricalShifts:   make([]CpsatHistoricalShift, len(historicalShifts)),
	}

	for i, shift := range shiftSpecs {
		input.Shifts[i] = CpsatShift{
			Index:                    shift.Index,
			Date:                     shift.Date,
			Size:                     shift.Size,
			Closed:                   shift.Closed,
			CustomPreallocations:     emptyIfNil(shift.CustomPreallocations),
			PreallocatedVolunteerIDs: emptyIfNil(shift.PreallocatedVolunteerIDs),
			PreallocatedTeamLeadID:   shift.PreallocatedTeamLeadID,
		}
	}

	for i, group := range volunteerState.VolunteerGroups {
		members := make([]CpsatMember, len(group.Members))
		for j, member := range group.Members {
			members[j] = CpsatMember{
				ID:          member.ID,
				FirstName:   member.FirstName,
				LastName:    member.LastName,
				DisplayName: member.DisplayName,
				Gender:      member.Gender,
				IsTeamLead:  member.IsTeamLead,
			}
		}
		input.Groups[i] = CpsatGroup{
			GroupKey:                  group.GroupKey,
			Members:                   members,
			AvailableShiftIndices:     group.AvailableShiftIndices,
			HistoricalAllocationCount: group.HistoricalAllocationCount,
		}
	}

	for i, shift := range historicalShifts {
		groupKeys := make([]string, len(shift.AllocatedGroups))
		for j, group := range shift.AllocatedGroups {
			groupKeys[j] = group.GroupKey
		}
		sort.Strings(groupKeys)
		input.HistoricalShifts[i] = CpsatHistoricalShift{
			Date:      shift.Date,
			GroupKeys: groupKeys,
		}
	}
	// The contract requires historical shifts sorted ascending by date;
	// only the last one matters in v1 (back-to-back boundary).
	sort.Slice(input.HistoricalShifts, func(i, j int) bool {
		return input.HistoricalShifts[i].Date < input.HistoricalShifts[j].Date
	})

	return input, nil
}

// CpsatOutputToShifts rebuilds Shift values from the solver output so
// persistence (convertToDBAllocations) and printing reuse the existing
// code paths.
func CpsatOutputToShifts(output *CpsatOutput, volunteers []Volunteer) ([]*Shift, error) {
	volunteersByID := make(map[string]Volunteer, len(volunteers))
	for _, vol := range volunteers {
		volunteersByID[vol.ID] = vol
	}

	shifts := make([]*Shift, len(output.Shifts))
	for i, outShift := range output.Shifts {
		var teamLead *Volunteer
		memberIDs := outShift.VolunteerIDs
		if outShift.TeamLeadID != "" {
			vol, exists := volunteersByID[outShift.TeamLeadID]
			if !exists {
				return nil, fmt.Errorf("solver returned unknown team lead ID %s for shift %s", outShift.TeamLeadID, outShift.Date)
			}
			teamLead = &vol
			memberIDs = append(append([]string{}, memberIDs...), outShift.TeamLeadID)
		}

		// Regroup members by GroupKey (individuals keyed by name, as in
		// InitVolunteerGroups) and rebuild groups with the shared helper.
		membersByGroup := make(map[string][]Volunteer)
		groupOrder := []string{}
		for _, id := range memberIDs {
			vol, exists := volunteersByID[id]
			if !exists {
				return nil, fmt.Errorf("solver returned unknown volunteer ID %s for shift %s", id, outShift.Date)
			}
			groupKey := vol.GroupKey
			if groupKey == "" || groupKey == "None" {
				groupKey = vol.FirstName + " " + vol.LastName
			}
			if _, seen := membersByGroup[groupKey]; !seen {
				groupOrder = append(groupOrder, groupKey)
			}
			membersByGroup[groupKey] = append(membersByGroup[groupKey], vol)
		}

		allocatedGroups := make([]*VolunteerGroup, 0, len(groupOrder))
		maleCount := 0
		for _, groupKey := range groupOrder {
			group := BuildVolunteerGroup(groupKey, membersByGroup[groupKey])
			group.AllocatedShiftIndices = []int{outShift.Index}
			allocatedGroups = append(allocatedGroups, group)
			maleCount += group.MaleCount
		}

		shifts[i] = &Shift{
			Date:                 outShift.Date,
			Index:                outShift.Index,
			Size:                 outShift.Size,
			Closed:               outShift.Closed,
			AllocatedGroups:      allocatedGroups,
			CustomPreallocations: outShift.CustomPreallocations,
			TeamLead:             teamLead,
			MaleCount:            maleCount,
		}
	}

	return shifts, nil
}

// emptyIfNil keeps the JSON contract's arrays as [] rather than null.
func emptyIfNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
