package services

import (
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/allocator"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services/utils"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// The round answers "who has replied". This answers the question an admin asks
// next: can the rota actually be staffed with what has come in? It is the
// analysis the CLI's viewResponses did, moved onto the availability tab and
// re-expressed against shift ids — the date-string matching it did throughout
// does not survive the move (ADR 0001).
//
// The numbers are only worth anything if they are the numbers the allocator will
// work to, so the seat arithmetic here does not restate the allocator's rules —
// it calls them. [allocator.ShiftShape] says how many Seats of each Role a shift
// has, and the pins are the ones InitShifts will see, resolved by the same
// helpers allocation resolves them with.

// ShiftCoverage is one shift's staffing picture before allocation runs.
//
// Needed is what is left for the allocator to fill: the shift's Seats with the
// already-pinned ones taken out. Available is who could fill them, so Delta —
// the difference — is the number the admin is really after. These four speak for
// the uncapped Role, the one a shift's size is spent on and the one the page is
// mostly asking about; Roles carries the same arithmetic for every configured
// Role, capped ones included. A closed shift carries zeroes throughout: it is not
// a shift that is short of people.
type ShiftCoverage struct {
	ShiftID   string
	Date      string // YYYY-MM-DD
	Closed    bool
	Needed    int
	Pinned    int // uncapped Seats already held by a preallocation
	Available int
	Delta     int            // Available - Needed; negative is understaffed
	Roles     []RoleCoverage // every configured Role, in priority order
}

// RoleCoverage is one Role's part of a shift's picture: the Seats the Shape
// gives it, how many are already spoken for, and how many holders could take
// what is left.
//
// Someone holding two Roles is counted available for both. They can only fill
// one Seat, so the tallies overlap — the question each answers is "could this
// Role be filled at all", which is the one an admin chasing a lead is asking,
// and summing them is not meaningful.
type RoleCoverage struct {
	Role string
	// Capped marks a Role with a ceiling. An empty capped Role is worth calling
	// out on its own — a shift with nobody who can lead it is short in a way no
	// number of ordinary volunteers fixes.
	Capped    bool
	Seats     int // Seats of this Role on the shift
	Pinned    int // of those, already held by a preallocation
	Needed    int // Seats - Pinned, floored at zero
	Available int // holders available for the shift and not already pinned to it
	Delta     int // Available - Needed; negative is understaffed
}

// AvailabilityGroup is a round seen at the grain allocation happens at. Members
// are placed together, so one answer speaks for all of them, and chasing is a
// question about the group rather than about each person in it.
//
// AvailableShiftIDs is the group rule of ADR 0004 applied: the intersection over
// the members who answered. Empty for a group nobody has answered for, which
// Replied is what distinguishes from a group that answered "none of these".
type AvailabilityGroup struct {
	Key               string
	Name              string // the members' names, as the group is addressed on screen
	Replied           bool
	AvailableShiftIDs []string
	Members           []AvailabilityEntry
}

// shiftSeats is what config and pins have already settled about one date, before
// anybody is allocated: the Seats each Role has, and who is already in them.
type shiftSeats struct {
	closed bool
	// seats is the Shape — how many Seats each Role has, by Role name.
	seats map[string]int
	// pinned counts how many of each Role's Seats a preallocation already holds.
	pinned map[string]int
	// pinnedVolunteers are the volunteers in those Seats. One Seat per person
	// per Shift, so anyone here is unavailable for every other Role too.
	pinnedVolunteers map[string]bool
}

// buildShiftSeats resolves every shift's Shape and pins, keyed by shift id.
//
// The pins are resolved through the same two helpers allocation uses —
// buildManualPreallocationOverrides for the manual/config merge (ADR 0003), then
// configPreallocationsForDate over the union, which mirrors InitShifts. That is
// deliberate: the page must show the Seats the solve will actually see, and a
// second implementation of the merge is how the three copies of the group rule
// happened.
//
// An unparseable rrule is warned about and skipped, as everywhere else on a read
// path. Allocation is where it fails hard; refusing to show an admin their round
// over one bad rule in the config would be the wrong trade here.
func buildShiftSeats(
	cfg *config.Config,
	shifts []AvailabilityShift,
	pins []db.ManualPreallocation,
	logger *zap.Logger,
) (map[string]shiftSeats, error) {
	shiftDates := make([]time.Time, 0, len(shifts))
	for _, s := range shifts {
		date, err := time.Parse("2006-01-02", s.Date)
		if err != nil {
			logger.Warn("Skipping shift with unparseable date", zap.String("date", s.Date))
			continue
		}
		shiftDates = append(shiftDates, date)
	}

	overrides := make([]allocator.ShiftOverride, 0, len(cfg.RotaOverrides))
	for i, override := range cfg.RotaOverrides {
		appliesTo, err := utils.NewRRuleMatcher(override.RRule, shiftDates)
		if err != nil {
			logger.Warn("Failed to parse rrule for shift coverage",
				zap.Int("override_index", i),
				zap.String("rrule", override.RRule),
				zap.Error(err))
			continue
		}
		overrides = append(overrides, allocator.ShiftOverride{
			AppliesTo:      appliesTo,
			ShiftSize:      override.ShiftSize,
			Closed:         override.Closed,
			Preallocations: convertConfigPreallocations(override.Preallocations),
		})
	}

	// Manual pins become synthetic overrides appended after the config ones,
	// exactly as allocation composes them, so the union below is the pin list
	// InitShifts would build.
	dateByShiftID := make(map[string]string, len(shifts))
	for _, s := range shifts {
		dateByShiftID[s.ID] = s.Date
	}
	manualOverrides, err := buildManualPreallocationOverrides(pins, dateByShiftID, overrides, cfg.RoleTable())
	if err != nil {
		return nil, err
	}
	overrides = append(overrides, manualOverrides...)

	roles := convertConfigRoles(cfg.Roles)

	seats := make(map[string]shiftSeats, len(shifts))
	for _, shift := range shifts {
		if shift.Closed {
			seats[shift.ID] = shiftSeats{
				closed:           true,
				seats:            map[string]int{},
				pinned:           map[string]int{},
				pinnedVolunteers: map[string]bool{},
			}
			continue
		}

		size := cfg.DefaultShiftSize
		for _, o := range overrides {
			if o.AppliesTo(shift.Date) && o.ShiftSize != nil {
				size = *o.ShiftSize
			}
		}

		shape := make(map[string]int)
		for _, seat := range allocator.ShiftShape(size, roles) {
			shape[seat.Role] = seat.Count
		}

		pinned := make(map[string]int)
		pinnedVolunteers := make(map[string]bool)
		effectivePins, _ := configPreallocationsForDate(shift.Date, overrides)
		for _, pin := range effectivePins {
			pinned[pin.Role]++
			if pin.VolunteerID != "" {
				pinnedVolunteers[pin.VolunteerID] = true
			}
		}

		seats[shift.ID] = shiftSeats{
			seats:            shape,
			pinned:           pinned,
			pinnedVolunteers: pinnedVolunteers,
		}
	}
	return seats, nil
}

// buildCoverage turns the round's groups into the per-shift picture.
//
// Availability is counted a group at a time because that is how it is decided
// (ADR 0004): the members of a group that said yes are all available, including
// the ones who never answered themselves. A volunteer who has stopped
// volunteering since the round was minted still holds a link and still appears
// on the roster, but cannot be allocated, so they are not counted here.
func buildCoverage(
	shifts []AvailabilityShift,
	groups []AvailabilityGroup,
	seats map[string]shiftSeats,
	roles model.Roles,
	volunteersByID map[string]model.Volunteer,
) []ShiftCoverage {
	byPriority := roles.ByPriority()
	uncapped := roles.UncappedName()

	coverage := make([]ShiftCoverage, 0, len(shifts))
	for _, shift := range shifts {
		seat := seats[shift.ID]
		if shift.Closed {
			coverage = append(coverage, ShiftCoverage{
				ShiftID: shift.ID,
				Date:    shift.Date,
				Closed:  true,
				Roles:   []RoleCoverage{},
			})
			continue
		}

		available := make(map[string]int, len(byPriority))
		for _, group := range groups {
			if !group.availableOn(shift.ID) {
				continue
			}
			for _, member := range group.Members {
				volunteer, known := volunteersByID[member.VolunteerID]
				if !known || !utils.IsActive(volunteer) {
					continue
				}
				// A pinned volunteer is already counted on the other side of
				// the sum, as a Seat that has come out of Needed.
				if seat.pinnedVolunteers[volunteer.ID] {
					continue
				}
				for _, role := range byPriority {
					if volunteer.Holds(role.Name) {
						available[role.Name]++
					}
				}
			}
		}

		roleCoverage := make([]RoleCoverage, 0, len(byPriority))
		for _, role := range byPriority {
			needed := seat.seats[role.Name] - seat.pinned[role.Name]
			if needed < 0 {
				needed = 0
			}
			roleCoverage = append(roleCoverage, RoleCoverage{
				Role:      role.Name,
				Capped:    role.Capped(),
				Seats:     seat.seats[role.Name],
				Pinned:    seat.pinned[role.Name],
				Needed:    needed,
				Available: available[role.Name],
				Delta:     available[role.Name] - needed,
			})
		}

		shiftCoverage := ShiftCoverage{
			ShiftID: shift.ID,
			Date:    shift.Date,
			Roles:   roleCoverage,
		}
		// The headline numbers are the uncapped Role's: a shift being "short"
		// has always meant short of the people its size buys.
		for _, rc := range roleCoverage {
			if rc.Role == uncapped {
				shiftCoverage.Needed = rc.Needed
				shiftCoverage.Pinned = rc.Pinned
				shiftCoverage.Available = rc.Available
				shiftCoverage.Delta = rc.Delta
				break
			}
		}
		coverage = append(coverage, shiftCoverage)
	}
	return coverage
}

// buildAvailabilityGroups gathers a round's per-volunteer entries into the
// groups they are allocated in, and settles each group's one answer.
//
// A volunteer no longer on the roster is their own group of one: they keep their
// link and their place in the list, but nothing can be said about who they are
// allocated with.
func buildAvailabilityGroups(
	entries []AvailabilityEntry,
	volunteersByID map[string]model.Volunteer,
	shifts []AvailabilityShift,
) []AvailabilityGroup {
	order := make([]string, 0)
	members := make(map[string][]AvailabilityEntry)
	for _, entry := range entries {
		key := "individual:" + entry.VolunteerID
		if volunteer, known := volunteersByID[entry.VolunteerID]; known {
			key = groupKey(volunteer)
		}
		if _, seen := members[key]; !seen {
			order = append(order, key)
		}
		members[key] = append(members[key], entry)
	}

	groups := make([]AvailabilityGroup, 0, len(order))
	for _, key := range order {
		groups = append(groups, buildAvailabilityGroup(key, members[key], shifts))
	}

	// By name, so the roster reads like the volunteer list beside it.
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Name != groups[j].Name {
			return groups[i].Name < groups[j].Name
		}
		return groups[i].Key < groups[j].Key
	})
	return groups
}

// buildAvailabilityGroup applies the group rule to one group's members:
// available iff at least one member answered and every member who answered said
// yes. That is the intersection over the responders, and the exact dual of the
// union-of-unavailability the Forms path computed — the semantics are unchanged,
// only the encoding flipped (ADR 0004).
func buildAvailabilityGroup(key string, members []AvailabilityEntry, shifts []AvailabilityShift) AvailabilityGroup {
	sort.Slice(members, func(i, j int) bool {
		if members[i].VolunteerName != members[j].VolunteerName {
			return members[i].VolunteerName < members[j].VolunteerName
		}
		return members[i].VolunteerID < members[j].VolunteerID
	})

	names := make([]string, 0, len(members))
	responders := 0
	saidYes := make(map[string]int, len(shifts))
	for _, member := range members {
		names = append(names, member.VolunteerName)
		if !member.Replied {
			continue
		}
		responders++
		for _, shiftID := range member.AvailableShiftIDs {
			saidYes[shiftID]++
		}
	}

	group := AvailabilityGroup{
		Key:     key,
		Name:    strings.Join(names, " & "),
		Replied: responders > 0,
		Members: members,
		// Never nil: an empty answer and no answer are different things, and
		// both have to serialise as a list rather than a null.
		AvailableShiftIDs: []string{},
	}
	if !group.Replied {
		return group
	}

	// In shift order, so a caller can line the ids up against the dates without
	// sorting them again.
	for _, shift := range shifts {
		if !shift.Closed && saidYes[shift.ID] == responders {
			group.AvailableShiftIDs = append(group.AvailableShiftIDs, shift.ID)
		}
	}
	return group
}

// availableOn reports whether the group can be allocated to a shift.
func (g AvailabilityGroup) availableOn(shiftID string) bool {
	for _, id := range g.AvailableShiftIDs {
		if id == shiftID {
			return true
		}
	}
	return false
}
