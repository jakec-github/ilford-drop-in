package allocator

import (
	"fmt"
	"sort"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
)

// This file defines the JSON contract between the Go server and the Python
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

// CpsatRole is one configured Role. It carries no ceiling: a Shift's Shape
// states how many Seats each Role has, and that is the only ceiling there is
// (issue #185).
type CpsatRole struct {
	Name     string `json:"name"`
	Priority int    `json:"priority"`
}

// CpsatSeat is one entry in a Shift's Shape: Count Seats for this Role.
type CpsatSeat struct {
	Role  string `json:"role"`
	Count int    `json:"count"`
}

// CpsatPin pins a volunteer or a custom entry to a Role on a shift.
type CpsatPin struct {
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
	Pins []CpsatPin `json:"pins"`
}

// CpsatHistoricalShift is a past shift with Go-derived group keys.
type CpsatHistoricalShift struct {
	Date      string   `json:"date"`
	GroupKeys []string `json:"group_keys"`
}

// CpsatRule is one Allocation Rule as the solver applies it (ADR 0012).
//
// The Scope is already worked out: VolunteerIDs is every volunteer in the
// problem the rule covers, so Python never needs to know what a Scope is. It is
// always empty for a Shift Rule, which applies to every Shift. Value is the
// kind's one number in the units the solver works in — for max_frequency the
// percentage has already become a count of this rota's shifts.
type CpsatRule struct {
	Kind         string   `json:"kind"`
	VolunteerIDs []string `json:"volunteer_ids"`
	Value        int      `json:"value"`
	// Option is the kind's one choice — the gender "shift_requires" asks for —
	// and empty for a kind that has none.
	Option string `json:"option,omitempty"`
}

// CpsatInput is the full problem sent to Python on stdin.
type CpsatInput struct {
	Roles []CpsatRole `json:"roles"`
	// Rules are the Organiser's Allocation Rules, in the order they were added.
	// Python applies exactly these on top of its fundamentals and holds no
	// default list of its own (ADR 0006); a kind it does not know it ignores.
	Rules            []CpsatRule            `json:"rules"`
	Shifts           []CpsatShift           `json:"shifts"`
	Groups           []CpsatGroup           `json:"groups"`
	HistoricalShifts []CpsatHistoricalShift `json:"historical_shifts"`
}

// CpsatAssignment is one filled Seat: who is in it, and what Role it is.
// Exactly one of VolunteerID and Custom is set, mirroring
// CpsatPin going the other way.
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
	overrides []ShiftOverride,
	historicalShifts []*Shift,
	allocationSettings model.AllocationSettings,
	roles []Role,
) (*CpsatInput, error) {
	// InitShifts resolves each shift's pins from the overrides,
	// carrying its own Shape and Closed through. AvailableGroups isn't part of
	// the contract (Python derives availability from groups), so an empty state
	// suffices.
	//
	// It runs first because the pins it resolves keep their groups in the
	// problem, which InitVolunteerGroups would otherwise discard for having no
	// answer.
	initialised, err := InitShifts(InitShiftsInput{
		Shifts:         shiftSpecs,
		Overrides:      overrides,
		VolunteerState: &VolunteerState{VolunteerGroups: []*VolunteerGroup{}},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialise shifts: %w", err)
	}

	volunteerState := InitVolunteerGroups(InitVolunteerGroupsInput{
		Volunteers:        volunteers,
		GroupAvailability: groupAvailability,
		HistoricalShifts:  historicalShifts,
		Pinned:            pinnedGroupKeys(initialised, volunteers),
	})

	input := &CpsatInput{
		Roles:            contractRoles(roles),
		Rules:            contractRules(allocationSettings, volunteerState.VolunteerGroups, len(shiftSpecs)),
		Shifts:           make([]CpsatShift, len(initialised)),
		Groups:           make([]CpsatGroup, len(volunteerState.VolunteerGroups)),
		HistoricalShifts: make([]CpsatHistoricalShift, len(historicalShifts)),
	}

	for i, shift := range initialised {
		input.Shifts[i] = CpsatShift{
			Index:          shift.Index,
			Date:           shift.Date,
			Shape:          contractShape(shift.Shape),
			Closed:         shift.Closed,
			Pins: contractPins(shift.Pins),
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

// contractRules turns the Allocation Rules this build can apply into what the
// solver is sent, each Scope worked out against the volunteers in the problem,
// in their canonical order.
func contractRules(settings model.AllocationSettings, groups []*VolunteerGroup, shiftCount int) []CpsatRule {
	applicable := settings.Applicable()
	rules := make([]CpsatRule, 0, len(applicable))
	for _, rule := range applicable {
		kind, _ := model.RuleKindByID(rule.Kind)

		covered := []string{}
		if kind.Family == model.VolunteerRuleFamily {
			for _, group := range groups {
				for _, member := range group.Members {
					if rule.Scope.Covers(member.ID) {
						covered = append(covered, member.ID)
					}
				}
			}
		}

		value := rule.Value
		if rule.Kind == model.MaxFrequencyKind {
			value = model.MaxAllocationCount(rule.Value, shiftCount)
		}

		rules = append(rules, CpsatRule{Kind: rule.Kind, VolunteerIDs: covered, Value: value, Option: rule.Option})
	}
	return rules
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

// contractShape renders a Shift's Seats onto the wire, keeping [] rather than
// null: a shift asking for nobody is a well-formed shift, and the Python side
// reads an absent shape as one.
func contractShape(seats []Seat) []CpsatSeat {
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
		out = append(out, CpsatRole{Name: role.Name, Priority: role.Priority})
	}
	return out
}

// contractPins renders a shift's pins, keeping [] rather than null.
func contractPins(pins []Pin) []CpsatPin {
	out := make([]CpsatPin, 0, len(pins))
	for _, pin := range pins {
		out = append(out, CpsatPin{
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
