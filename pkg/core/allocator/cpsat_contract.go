package allocator

import (
	"fmt"
	"sort"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
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
	ID          string   `json:"id"`
	FirstName   string   `json:"first_name"`
	LastName    string   `json:"last_name"`
	DisplayName string   `json:"display_name"`
	Gender      string   `json:"gender"`
	Roles       []string `json:"roles"`
}

// CpsatGroup is an allocation unit (couples/families allocated together)
// with availability already resolved to shift indices.
type CpsatGroup struct {
	GroupKey                  string        `json:"group_key"`
	Members                   []CpsatMember `json:"members"`
	AvailableShiftIndices     []int         `json:"available_shift_indices"`
	HistoricalAllocationCount int           `json:"historical_allocation_count"`
}

// CpsatRole is one configured Role. Max is omitted for an uncapped Role.
type CpsatRole struct {
	Name     string `json:"name"`
	Max      *int   `json:"max"`
	Priority int    `json:"priority"`
}

// CpsatSeat is one entry in a Shift's Shape: Count Seats for this Role.
type CpsatSeat struct {
	Role  string `json:"role"`
	Count int    `json:"count"`
}

// CpsatPreallocation pins a volunteer or a custom entry to a Role on a shift.
type CpsatPreallocation struct {
	VolunteerID string `json:"volunteer_id"`
	Custom      string `json:"custom"`
	Role        string `json:"role"`
}

// CpsatShift is an override-resolved shift specification. Shape is the Seats
// the shift asks for; it replaces a bare size, which could only ever describe a
// rota with one Role.
type CpsatShift struct {
	Index          int                  `json:"index"`
	Date           string               `json:"date"`
	Shape          []CpsatSeat          `json:"shape"`
	Closed         bool                 `json:"closed"`
	Preallocations []CpsatPreallocation `json:"preallocations"`
}

// CpsatHistoricalShift is a past shift with Go-derived group keys.
type CpsatHistoricalShift struct {
	Date      string   `json:"date"`
	GroupKeys []string `json:"group_keys"`
}

// CpsatInput is the full problem sent to Python on stdin.
type CpsatInput struct {
	MaxAllocationCount int         `json:"max_allocation_count"`
	Roles              []CpsatRole `json:"roles"`
	// EnabledConstraints names the optional solver rules this run applies —
	// the admin's Allocation Settings, in registry order. Python applies
	// exactly these on top of its fundamentals and holds no default list of
	// its own (ADR 0006, issue #130); a name it does not know it ignores.
	EnabledConstraints []string               `json:"enabled_constraints"`
	Shifts             []CpsatShift           `json:"shifts"`
	Groups             []CpsatGroup           `json:"groups"`
	HistoricalShifts   []CpsatHistoricalShift `json:"historical_shifts"`
}

// CpsatAssignment is one filled Seat: who is in it, and what Role it is.
// Exactly one of VolunteerID and Custom is set, mirroring
// CpsatPreallocation going the other way.
type CpsatAssignment struct {
	VolunteerID string `json:"volunteer_id"`
	Custom      string `json:"custom"`
	Role        string `json:"role"`
}

// CpsatOutputShift is one solved shift. Assignments are the Seats that
// ended up filled; a Seat nobody filled is simply absent, which is how
// "this shift has no team lead" is said (expected and common; filled in
// manually later).
type CpsatOutputShift struct {
	Index              int               `json:"index"`
	Date               string            `json:"date"`
	Size               int               `json:"size"`
	Closed             bool              `json:"closed"`
	Assignments        []CpsatAssignment `json:"assignments"`
	AllocatedGroupKeys []string          `json:"allocated_group_keys"`
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
	groupAvailability map[string][]int,
	shiftSpecs []ShiftSpec,
	defaultShiftSize int,
	overrides []ShiftOverride,
	historicalShifts []*Shift,
	allocationSettings model.AllocationSettings,
	roles []Role,
) (*CpsatInput, error) {
	if _, ok := uncappedRole(roles); !ok {
		return nil, fmt.Errorf("no uncapped role configured: a shift's size has no Seats to be spent on")
	}

	// InitShifts resolves per-shift size and preallocations from the overrides,
	// carrying each shift's own Closed through. AvailableGroups isn't part of
	// the contract (Python derives availability from groups), so an empty state
	// suffices.
	//
	// It runs first because the pins it resolves settle availability for the
	// shifts they name, and InitVolunteerGroups discards a group with none.
	initialised, err := InitShifts(InitShiftsInput{
		Shifts:           shiftSpecs,
		DefaultShiftSize: defaultShiftSize,
		Overrides:        overrides,
		VolunteerState:   &VolunteerState{VolunteerGroups: []*VolunteerGroup{}},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize shifts: %w", err)
	}

	volunteerState, err := InitVolunteerGroups(InitVolunteerGroupsInput{
		Volunteers:        volunteers,
		GroupAvailability: withPreallocatedAvailability(groupAvailability, initialised, volunteers),
		HistoricalShifts:  historicalShifts,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize volunteer groups: %w", err)
	}

	input := &CpsatInput{
		MaxAllocationCount: allocationSettings.MaxAllocationCount(len(shiftSpecs)),
		Roles:              contractRoles(roles),
		EnabledConstraints: allocationSettings.EnabledConstraints(),
		Shifts:             make([]CpsatShift, len(initialised)),
		Groups:             make([]CpsatGroup, len(volunteerState.VolunteerGroups)),
		HistoricalShifts:   make([]CpsatHistoricalShift, len(historicalShifts)),
	}

	for i, shift := range initialised {
		input.Shifts[i] = CpsatShift{
			Index:          shift.Index,
			Date:           shift.Date,
			Shape:          shiftShape(shift.Size, roles),
			Closed:         shift.Closed,
			Preallocations: contractPreallocations(shift.Preallocations),
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
				Roles:       emptyIfNil(member.Roles),
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
		// Regroup members by GroupKey (individuals keyed by name, as in
		// InitVolunteerGroups) and rebuild groups with the shared helper.
		assignments := make([]Assignment, 0, len(outShift.Assignments))
		membersByGroup := make(map[string][]Volunteer)
		groupOrder := []string{}
		for _, assigned := range outShift.Assignments {
			if assigned.VolunteerID == "" {
				assignments = append(assignments, Assignment{
					Custom: assigned.Custom,
					Role:   assigned.Role,
				})
				continue
			}

			vol, exists := volunteersByID[assigned.VolunteerID]
			if !exists {
				return nil, fmt.Errorf("solver returned unknown volunteer ID %s for shift %s", assigned.VolunteerID, outShift.Date)
			}
			assignments = append(assignments, Assignment{
				Volunteer: &vol,
				Role:      assigned.Role,
			})

			groupKey := GroupKeyFor(vol)
			if _, seen := membersByGroup[groupKey]; !seen {
				groupOrder = append(groupOrder, groupKey)
			}
			membersByGroup[groupKey] = append(membersByGroup[groupKey], vol)
		}

		allocatedGroups := make([]*VolunteerGroup, 0, len(groupOrder))
		maleCount := 0
		for _, groupKey := range groupOrder {
			group := BuildVolunteerGroup(membersByGroup[groupKey])
			group.AllocatedShiftIndices = []int{outShift.Index}
			allocatedGroups = append(allocatedGroups, group)
			maleCount += group.MaleCount
		}

		shifts[i] = &Shift{
			Date:            outShift.Date,
			Index:           outShift.Index,
			Size:            outShift.Size,
			Closed:          outShift.Closed,
			AllocatedGroups: allocatedGroups,
			Assignments:     assignments,
			MaleCount:       maleCount,
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

// uncappedRole finds the single Role with no ceiling — the one a shift's size
// buys Seats in. Config validation guarantees exactly one.
func uncappedRole(roles []Role) (Role, bool) {
	for _, role := range roles {
		if role.Max == nil {
			return role, true
		}
	}
	return Role{}, false
}

// shiftShape renders [ShiftShape] onto the wire.
func shiftShape(size int, roles []Role) []CpsatSeat {
	seats := ShiftShape(size, roles)
	shape := make([]CpsatSeat, 0, len(seats))
	for _, seat := range seats {
		shape = append(shape, CpsatSeat{Role: seat.Role, Count: seat.Count})
	}
	return shape
}

// contractRoles renders the configured Roles in priority order.
func contractRoles(roles []Role) []CpsatRole {
	out := make([]CpsatRole, 0, len(roles))
	for _, role := range sortedByPriority(roles) {
		out = append(out, CpsatRole{Name: role.Name, Max: role.Max, Priority: role.Priority})
	}
	return out
}

// contractPreallocations renders a shift's pins, keeping [] rather than null.
func contractPreallocations(pins []Preallocation) []CpsatPreallocation {
	out := make([]CpsatPreallocation, 0, len(pins))
	for _, pin := range pins {
		out = append(out, CpsatPreallocation{
			VolunteerID: pin.VolunteerID,
			Custom:      pin.Custom,
			Role:        pin.Role,
		})
	}
	return out
}

func sortedByPriority(roles []Role) []Role {
	ordered := make([]Role, len(roles))
	copy(ordered, roles)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Priority < ordered[j].Priority })
	return ordered
}
