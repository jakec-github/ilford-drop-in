package services

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/allocator"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services/utils"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// SolveRotaStore is everything assembling and solving the rota in flight reads:
// the rota and its Shifts, what those Shifts ask for, who is available, what the
// previous rota did, and which Seats are already promised.
//
// It reads and nothing else, because a solve writes nothing. What is done with
// the answer is the caller's, and the two callers write to different places —
// allocating commits it as the rota, drafting stores it as a Draft Rota
// Allocation (ADR 0008). Each names its own write below, so no caller holds a
// writer it has no business using: the HTTP API can solve a draft without
// thereby being able to allocate.
type SolveRotaStore interface {
	RotaDefaultsStore
	ShiftShapeStore
	GetRotations(ctx context.Context) ([]db.Rotation, error)
	GetShiftsByRotaID(ctx context.Context, rotaID string) ([]db.Shift, error)
	GetAvailabilityRequestsByRotaID(ctx context.Context, rotaID string) ([]db.AvailabilityRequest, error)
	GetLatestAvailability(ctx context.Context, requestIDs []string, cutoff *time.Time) (map[string]db.AvailabilityGeneration, error)
	GetAllocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.Allocation, error)
	GetAlterationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.Alteration, error)
	GetPreallocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.Preallocation, error)
}

// AllocateRotaStore is what allocating the rota in flight needs: everything
// drafting one needs, plus the write that commits a solve as the rota.
//
// It embeds the draft store rather than only the solve store because allocating
// reads and writes drafts too: it refuses a rota nobody has drafted, and a solve
// it will not commit becomes the draft (ADR 0008). The embedding is deliberately
// one-way — holding a DraftRotaAllocationStore is permission to draft a rota,
// not to allocate it.
type AllocateRotaStore interface {
	DraftRotaAllocationStore
	InsertAllocationsAndSetAllocated(ctx context.Context, allocations []db.Allocation, rotaID string, datetime time.Time) error
}

// fetchGroupAvailability reads a rota's stored availability and settles it into
// the answer the allocator works in: the shift indices each group can work,
// keyed the way the allocator keys its groups.
//
// This is where the two grains meet. Requests are minted per volunteer;
// allocation happens per group. The rule bridging them — available iff at least
// one member answered and every member who answered said yes (ADR 0004) — is
// applied here from the round's own implementation, rather than copied. The
// allocator kept its own version of it until this slice, written against the
// opposite encoding: it unioned unavailability where this intersects
// availability, which is the same rule read backwards.
//
// orderedShiftIDs must be in the solver's shift order, since that is what an
// index means to it.
func fetchGroupAvailability(
	ctx context.Context,
	database SolveRotaStore,
	rotaID string,
	activeVolunteers []allocator.Volunteer,
	orderedShiftIDs []string,
	logger *zap.Logger,
) (map[string][]int, error) {
	requests, err := database.GetAvailabilityRequestsByRotaID(ctx, rotaID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch availability requests: %w", err)
	}
	if len(requests) == 0 {
		// No rota id in it. This is the state every rota is in from the moment
		// it is defined until its round is minted, so the message is read on the
		// Allocation tab as a matter of course rather than in a log — and there
		// is only one rota it could be about (issue #145).
		return nil, wrapf(ErrInvalidInput, "nobody has been asked about this rota yet - start the availability round below, and the draft will solve once answers come in")
	}

	requestIDs := make([]string, 0, len(requests))
	for _, r := range requests {
		requestIDs = append(requestIDs, r.ID)
	}
	// No cutoff: allocation refuses a rota that is already allocated, so every
	// answer on record is an answer that still counts.
	latest, err := database.GetLatestAvailability(ctx, requestIDs, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to read availability: %w", err)
	}

	volunteersByID := make(map[string]allocator.Volunteer, len(activeVolunteers))
	for _, v := range activeVolunteers {
		volunteersByID[v.ID] = v
	}

	entriesByGroup := make(map[string][]AvailabilityEntry)
	for _, request := range requests {
		volunteer, active := volunteersByID[request.VolunteerID]
		if !active {
			// Someone who has gone inactive or left the sheet since the round
			// was minted still holds a link, but cannot be allocated — and so
			// has no group here for their answer to speak for.
			logger.Debug("Skipping availability for a volunteer who is not active",
				zap.String("volunteer_id", request.VolunteerID))
			continue
		}

		generation, replied := latest[request.ID]
		entry := AvailabilityEntry{
			VolunteerID:       request.VolunteerID,
			VolunteerName:     strings.TrimSpace(volunteer.FirstName + " " + volunteer.LastName),
			Replied:           replied,
			AvailableShiftIDs: make([]string, 0, len(generation.Answers)),
		}
		for _, answer := range generation.Answers {
			entry.AvailableShiftIDs = append(entry.AvailableShiftIDs, answer.ShiftID)
		}

		key := allocator.GroupKeyFor(volunteer)
		entriesByGroup[key] = append(entriesByGroup[key], entry)
	}

	// The group rule works in shift ids; the solver works in indices. Closed is
	// left false throughout because the allocator resolves closure itself, from
	// the config overrides — and a volunteer cannot say yes to a closed shift in
	// the first place, so nothing here can leak one in.
	shiftOrder := make([]AvailabilityShift, len(orderedShiftIDs))
	indexByShiftID := make(map[string]int, len(orderedShiftIDs))
	for i, id := range orderedShiftIDs {
		shiftOrder[i] = AvailabilityShift{ID: id}
		indexByShiftID[id] = i
	}

	availability := make(map[string][]int, len(entriesByGroup))
	for key, entries := range entriesByGroup {
		group := buildAvailabilityGroup(key, entries, shiftOrder)
		if !group.Replied {
			// Nobody in the group answered. Absent from the map is how that is
			// told apart from a group that answered "none of these".
			continue
		}
		indices := make([]int, 0, len(group.AvailableShiftIDs))
		for _, shiftID := range group.AvailableShiftIDs {
			indices = append(indices, indexByShiftID[shiftID])
		}
		availability[key] = indices
	}

	return availability, nil
}

// convertToAllocatorVolunteers converts model.Volunteer to allocator.Volunteer
func convertToAllocatorVolunteers(volunteers []model.Volunteer) []allocator.Volunteer {
	result := make([]allocator.Volunteer, len(volunteers))
	for i, vol := range volunteers {

		result[i] = allocator.Volunteer{
			ID:          vol.ID,
			FirstName:   vol.FirstName,
			LastName:    vol.LastName,
			DisplayName: vol.DisplayName,
			Gender:      vol.Gender,
			Roles:       vol.Roles,
			GroupKey:    vol.GroupKey,
		}
	}
	return result
}

// convertToDBAllocations converts allocator shifts to database allocation
// records, resolving each solver-output date to its minted shift id via
// shiftIDByDate. A date with no minted shift is a broken invariant (the solver
// only ever sees minted dates); it fails loudly here rather than tripping the
// shift_id FK on insert (ADR 0001).
//
// One filled Seat is one row, whatever Role it is: the solver decided the
// Role, so there is nothing to work out here.
func convertToDBAllocations(shiftIDByDate map[string]string, shifts []*allocator.Shift) ([]db.Allocation, error) {
	allocations := make([]db.Allocation, 0)

	for _, shift := range shifts {
		shiftID, ok := shiftIDByDate[shift.Date]
		if !ok {
			return nil, fmt.Errorf("solver produced an allocation for date %s with no minted shift", shift.Date)
		}

		for _, assignment := range shift.Assignments {
			volunteerID := ""
			if assignment.Volunteer != nil {
				volunteerID = assignment.Volunteer.ID
			}
			allocations = append(allocations, db.Allocation{
				ID:          uuid.New().String(),
				ShiftID:     shiftID,
				Role:        assignment.Role,
				VolunteerID: volunteerID,
				CustomEntry: assignment.Custom,
			})
		}
	}

	return allocations, nil
}

// buildHistoricalShifts fetches allocations from the previous rota, applies that
// rota's alterations (covers/swaps) so history reflects who actually worked, and
// builds historical shift objects sorted ascending by date. Only includes Date
// and AllocatedGroups fields. Callers pass ALL volunteers (inactive included) so
// shifts worked by now-inactive volunteers keep their groups — dropping them
// would shift the back-to-back boundary onto an earlier date. Allocations whose
// volunteer id is unknown (deleted from the sheet) and custom entries are
// skipped; a date is still emitted even if no groups remain.
func buildHistoricalShifts(
	ctx context.Context,
	database SolveRotaStore,
	allRotations []db.Rotation,
	targetRota *db.Rotation,
	volunteers []allocator.Volunteer,
	logger *zap.Logger,
) ([]*allocator.Shift, error) {
	// Find the previous rota (the one before the target rota)
	previousRota := findPreviousRotation(allRotations, targetRota)
	if previousRota == nil {
		logger.Info("No previous rota found, historical shifts will be empty")
		return []*allocator.Shift{}, nil
	}

	logger.Debug("Found previous rota",
		zap.String("id", previousRota.ID),
		zap.String("start", previousRota.Start))

	// Read the previous rota's shifts to scope its allocations/alterations by id
	// and to recover each shift's date for the historical output (ADR 0001).
	previousRotaShifts, err := database.GetShiftsByRotaID(ctx, previousRota.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch shifts: %w", err)
	}
	shiftIDs := make([]string, len(previousRotaShifts))
	dateByShiftID := make(map[string]string, len(previousRotaShifts))
	for i, s := range previousRotaShifts {
		shiftIDs[i] = s.ID
		dateByShiftID[s.ID] = s.Date
	}

	// Fetch the previous rota's allocations
	previousRotaAllocations, err := database.GetAllocationsByShiftIDs(ctx, shiftIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch allocations: %w", err)
	}
	logger.Debug("Fetched allocations from previous rota", zap.Int("count", len(previousRotaAllocations)))

	if len(previousRotaAllocations) == 0 {
		logger.Info("No allocations found in previous rota")
		return []*allocator.Shift{}, nil
	}

	// Group allocations by shift id, custom entries included so
	// alterations that remove them can match.
	allocationsByShiftID := make(map[string][]db.Allocation)
	for _, allocation := range previousRotaAllocations {
		allocationsByShiftID[allocation.ShiftID] = append(allocationsByShiftID[allocation.ShiftID], allocation)
	}

	// Apply the previous rota's alterations so history reflects who
	// actually worked (covers and swaps), not the rota as first published.
	previousRotaAlterations, err := database.GetAlterationsByShiftIDs(ctx, shiftIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch alterations: %w", err)
	}
	logger.Debug("Applying alterations to historical shifts", zap.Int("count", len(previousRotaAlterations)))
	allocationsByShiftID = utils.ApplyAlterations(allocationsByShiftID, previousRotaAlterations)

	// Build a map of volunteers by ID for quick lookup
	volunteersByID := make(map[string]allocator.Volunteer)
	for _, vol := range volunteers {
		volunteersByID[vol.ID] = vol
	}

	// Build historical shifts
	historicalShifts := make([]*allocator.Shift, 0, len(allocationsByShiftID))
	for shiftID, allocations := range allocationsByShiftID {
		// Group volunteers by allocator.GroupKeyFor to reconstruct volunteer
		// groups, skipping custom entries and unknown volunteer ids. Only the
		// key travels into the solver, and it is matched against the keys of
		// the rota being allocated — so history must be grouped by the same
		// rule, under which an ungrouped volunteer is their own group of one.
		volunteersByGroup := make(map[string][]allocator.Volunteer)
		for _, allocation := range allocations {
			if allocation.VolunteerID == "" {
				continue
			}
			volunteer, exists := volunteersByID[allocation.VolunteerID]
			if !exists {
				continue
			}
			groupKey := allocator.GroupKeyFor(volunteer)
			volunteersByGroup[groupKey] = append(volunteersByGroup[groupKey], volunteer)
		}

		// Build AllocatedGroups for this shift using the allocator's BuildVolunteerGroup helper
		allocatedGroups := make([]*allocator.VolunteerGroup, 0, len(volunteersByGroup))
		for _, members := range volunteersByGroup {
			group := allocator.BuildVolunteerGroup(members)
			allocatedGroups = append(allocatedGroups, group)
		}

		// Create the historical shift with only Date and AllocatedGroups
		historicalShifts = append(historicalShifts, &allocator.Shift{
			Date:            dateByShiftID[shiftID],
			AllocatedGroups: allocatedGroups,
		})
	}

	// Consumers treat the last element as the boundary shift (and measure
	// index distances), so the order must be by date, not map iteration.
	sort.Slice(historicalShifts, func(i, j int) bool {
		return historicalShifts[i].Date < historicalShifts[j].Date
	})

	logger.Debug("Built historical shifts", zap.Int("shift_count", len(historicalShifts)))

	return historicalShifts, nil
}

// convertRoles lifts the Roles into the allocator's own type, in priority
// order, the same way volunteers and pins are lifted: the allocator keeps its
// own vocabulary and does not import the domain package.
func convertRoles(roles model.Roles) []allocator.Role {
	ordered := roles.ByPriority()
	converted := make([]allocator.Role, 0, len(ordered))
	for _, role := range ordered {
		converted = append(converted, allocator.Role{
			Name:     role.Name,
			Priority: role.Priority,
		})
	}
	return converted
}

// preallocationsForDate collects the preallocations that apply to a single date,
// mirroring InitShifts exactly: every matching override appends its pins in
// order.
//
// Whether the shift is closed plays no part here. Closure is a field on the
// Shift now, so the shifts a caller is iterating already carry it, and
// InitShifts strips the pins of a closed one.
func preallocationsForDate(date string, overrides []allocator.ShiftOverride) []allocator.Preallocation {
	var pins []allocator.Preallocation
	for _, o := range overrides {
		if !o.AppliesTo(date) {
			continue
		}
		pins = append(pins, o.Preallocations...)
	}
	return pins
}

// exactDateMatcher returns an AppliesTo predicate matching exactly one date, so
// a synthetic preallocation override touches only its own shift.
func exactDateMatcher(date string) func(string) bool {
	return func(d string) bool { return d == date }
}

// buildPreallocationOverrides turns each pin into a synthetic, exact-date
// allocator.ShiftOverride so InitShifts applies them through its existing append
// semantics — no new merge logic in the solver (issue #39 / ADR 0003).
//
// There is nothing to merge against any more: Config Preallocations were deleted
// in issue #131, so the `preallocation` table is the whole set of pins a rota
// has and the rules that kept the two sources apart went with the second source.
// A pin on a closed Shift is left alone here and stripped by InitShifts.
func buildPreallocationOverrides(
	pins []db.Preallocation,
	dateByShiftID map[string]string,
) ([]allocator.ShiftOverride, error) {
	overrides := make([]allocator.ShiftOverride, 0, len(pins))

	for _, pin := range pins {
		date, ok := dateByShiftID[pin.ShiftID]
		if !ok {
			return nil, fmt.Errorf("preallocation %s references shift %s with no minted date", pin.ID, pin.ShiftID)
		}
		if pin.VolunteerID == "" && pin.CustomValue == "" {
			return nil, fmt.Errorf("preallocation %s has neither a volunteer nor a custom value", pin.ID)
		}

		overrides = append(overrides, allocator.ShiftOverride{
			AppliesTo: exactDateMatcher(date),
			Preallocations: []allocator.Preallocation{{
				VolunteerID: pin.VolunteerID,
				Custom:      pin.CustomValue,
				Role:        pin.Role,
			}},
		})
	}

	return overrides, nil
}

// checkPreallocationsResolve verifies, before the solver runs, that every
// preallocated volunteer still resolves to an active volunteer. A pin whose
// volunteer has gone inactive or been deleted would otherwise surface as the
// solver's opaque ProblemError; here it fails loudly, naming the offending
// pin(s). Custom (non-volunteer) pins carry no id and are not checked.
//
// It matters most for the pins nobody typed recently: a Standing Preallocation
// seeds one at definition and an admin may not look at it again before
// allocating, by which time the person can have left.
//
// Closed shifts are skipped: InitShifts strips their pins, so a stale one there
// reaches neither the solver nor anything else, and reporting it would block a
// rota over a pin that has no effect.
func checkPreallocationsResolve(
	pins []db.Preallocation,
	shifts []db.Shift,
	activeIDs map[string]bool,
) error {
	var offenders []string

	openByShiftID := make(map[string]string, len(shifts))
	for _, s := range shifts {
		if !s.Closed {
			openByShiftID[s.ID] = s.Date
		}
	}

	for _, pin := range pins {
		if pin.VolunteerID == "" {
			continue
		}
		date, open := openByShiftID[pin.ShiftID]
		if !open {
			continue
		}
		if !activeIDs[pin.VolunteerID] {
			offenders = append(offenders, fmt.Sprintf("pin for %s: volunteer %s is not active", date, pin.VolunteerID))
		}
	}

	if len(offenders) > 0 {
		return wrapf(ErrInvalidInput, "preallocated volunteers are no longer active: %s", strings.Join(offenders, "; "))
	}
	return nil
}

// findPreviousRotation finds the rotation immediately before the target rotation
func findPreviousRotation(rotations []db.Rotation, targetRota *db.Rotation) *db.Rotation {
	targetDate, err := time.Parse("2006-01-02", targetRota.Start)
	if err != nil {
		return nil
	}

	var previousRota *db.Rotation
	var previousDate time.Time

	for i := range rotations {
		rota := &rotations[i]
		if rota.ID == targetRota.ID {
			continue
		}

		rotaDate, err := time.Parse("2006-01-02", rota.Start)
		if err != nil {
			continue
		}

		// Only consider rotas that start before the target rota
		if rotaDate.Before(targetDate) {
			// If this is our first match or it's more recent than our current previous
			if previousRota == nil || rotaDate.After(previousDate) {
				previousRota = rota
				previousDate = rotaDate
			}
		}
	}

	return previousRota
}
