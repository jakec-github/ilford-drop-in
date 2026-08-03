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
// work to, so the seat arithmetic here deliberately mirrors InitShifts and the
// solver's capacity constraint: the last matching override sets the size, pins
// from config and from manual rows both hold seats, and a team lead never holds
// an ordinary one.

// ShiftCoverage is one shift's staffing picture before allocation runs.
//
// Needed is what is left for the allocator to fill: the shift's size with the
// already-pinned seats taken out. Available is who could fill them, so Delta —
// the difference — is the number the admin is really after. A closed shift
// carries zeroes throughout: it is not a shift that is short of people.
type ShiftCoverage struct {
	ShiftID     string
	Date        string // YYYY-MM-DD
	Closed      bool
	Needed      int
	Pinned      int // ordinary seats already held by a preallocation
	Available   int
	Delta       int  // Available - Needed; negative is understaffed
	HasTeamLead bool // a lead is available for this date, or already pinned to it
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
// anybody is allocated: how big the shift is, who holds one of its ordinary
// seats, and whether the team-lead slot is spoken for.
type shiftSeats struct {
	size       int
	closed     bool
	volunteers map[string]bool // volunteer ids holding an ordinary seat
	customs    int             // non-volunteer entries holding one
	teamLeadID string
}

// pinnedSeats is how many of the shift's seats are already committed. Team leads
// are excluded because they never count towards a shift's size — the solver's
// capacity constraint gives them a seat cost of zero.
func (s shiftSeats) pinnedSeats() int {
	return len(s.volunteers) + s.customs
}

// buildShiftSeats resolves every shift's size and pins, keyed by shift id.
//
// It mirrors InitShifts rather than reimplementing it: the last matching
// override with an explicit size wins, a closed date carries no pins at all, and
// manual pins union with the config ones with the same de-duplication the
// allocator applies (ADR 0003) — config stays authoritative for the
// single-valued team-lead slot, and a pin that repeats a config one is one seat,
// not two.
//
// An unparseable rrule is warned about and skipped, as everywhere else on a read
// path. Allocation is where it fails hard; refusing to show an admin their round
// over one bad rule in the config would be the wrong trade here.
func buildShiftSeats(
	cfg *config.Config,
	shifts []AvailabilityShift,
	pins []db.ManualPreallocation,
	logger *zap.Logger,
) map[string]shiftSeats {
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
			AppliesTo:                appliesTo,
			ShiftSize:                override.ShiftSize,
			CustomPreallocations:     override.CustomPreallocations,
			Closed:                   override.Closed,
			PreallocatedVolunteerIDs: override.PreallocatedVolunteerIDs,
			PreallocatedTeamLeadID:   override.PreallocatedTeamLeadID,
		})
	}

	pinsByShiftID := make(map[string][]db.ManualPreallocation, len(pins))
	for _, p := range pins {
		pinsByShiftID[p.ShiftID] = append(pinsByShiftID[p.ShiftID], p)
	}

	seats := make(map[string]shiftSeats, len(shifts))
	for _, shift := range shifts {
		if shift.Closed {
			seats[shift.ID] = shiftSeats{closed: true, volunteers: map[string]bool{}}
			continue
		}

		size := cfg.DefaultShiftSize
		for _, o := range overrides {
			if o.AppliesTo(shift.Date) && o.ShiftSize != nil {
				size = *o.ShiftSize
			}
		}

		volunteerIDs, teamLeadID, customs, _ := configPreallocationsForDate(shift.Date, overrides)
		for _, pin := range pinsByShiftID[shift.ID] {
			switch {
			case pin.Role == string(model.RoleTeamLead):
				// Config is authoritative for the team-lead slot, so a manual
				// pin only fills it when config has left it empty.
				if teamLeadID == "" {
					teamLeadID = pin.VolunteerID
				}
			case pin.VolunteerID != "":
				volunteerIDs[pin.VolunteerID] = true
			case pin.CustomValue != "":
				customs[pin.CustomValue] = true
			}
		}
		// Someone pinned as both the lead and an ordinary volunteer still holds
		// one seat, and it is the lead's, which costs the shift nothing.
		delete(volunteerIDs, teamLeadID)

		seats[shift.ID] = shiftSeats{
			size:       size,
			volunteers: volunteerIDs,
			customs:    len(customs),
			teamLeadID: teamLeadID,
		}
	}
	return seats
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
	volunteersByID map[string]model.Volunteer,
) []ShiftCoverage {
	coverage := make([]ShiftCoverage, 0, len(shifts))
	for _, shift := range shifts {
		seat := seats[shift.ID]
		if shift.Closed {
			coverage = append(coverage, ShiftCoverage{ShiftID: shift.ID, Date: shift.Date, Closed: true})
			continue
		}

		available := 0
		hasTeamLead := seat.teamLeadID != ""
		for _, group := range groups {
			if !group.availableOn(shift.ID) {
				continue
			}
			for _, member := range group.Members {
				volunteer, known := volunteersByID[member.VolunteerID]
				if !known || !utils.IsActive(volunteer) {
					continue
				}
				if volunteer.Role == model.RoleTeamLead {
					hasTeamLead = true
					continue
				}
				// A pinned volunteer is already counted on the other side of
				// the sum, as a seat that has come out of Needed.
				if seat.volunteers[volunteer.ID] {
					continue
				}
				available++
			}
		}

		needed := seat.size - seat.pinnedSeats()
		if needed < 0 {
			needed = 0
		}

		coverage = append(coverage, ShiftCoverage{
			ShiftID:     shift.ID,
			Date:        shift.Date,
			Needed:      needed,
			Pinned:      seat.pinnedSeats(),
			Available:   available,
			Delta:       available - needed,
			HasTeamLead: hasTeamLead,
		})
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
