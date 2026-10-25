package services

import (
	"context"
	"fmt"
	"sort"
	"time"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services/utils"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// VolunteerRotaStatus represents a volunteer's response status for a single rotation.
//
// The four statuses come straight off the stored round: no request row is
// "no_form"; a request with no generation before the cut-off is "no_response";
// a generation that ticked nothing is "no_availability"; anything else is
// "available". The Forms path had a fifth, "form_error", for a form that could
// not be read — there is no API left to fail, so it is gone.
type VolunteerRotaStatus struct {
	Status         string // "no_form", "no_response", "no_availability", "available"
	AvailableCount int    // number of shifts available (only set when Status == "available")
	ShiftCount     int    // total shifts in the rotation
}

// AvailabilityBand is how much of a rota a volunteer said they could do, read
// as at the moment it was allocated (issue #260).
type AvailabilityBand string

const (
	BandNotSent     AvailabilityBand = "not_sent"
	BandNoResponse  AvailabilityBand = "no_response"
	BandUnavailable AvailabilityBand = "unavailable"
	BandLow         AvailabilityBand = "low"
	BandOK          AvailabilityBand = "ok"
	BandGood        AvailabilityBand = "good"
)

// BandFor places a response in a band. The low/ok/good lines are fractions of
// the rota's open shifts rather than counts, so they mean the same thing on a
// four-shift rota as on a twelve-shift one: under a quarter is low, up to half
// is ok, over half is good.
func BandFor(status VolunteerRotaStatus, openShifts int) AvailabilityBand {
	switch status.Status {
	case "no_form":
		return BandNotSent
	case "no_response":
		return BandNoResponse
	case "no_availability":
		return BandUnavailable
	}
	if openShifts == 0 {
		return BandGood
	}
	switch available := status.AvailableCount; {
	case available*4 < openShifts:
		return BandLow
	case available*2 <= openShifts:
		return BandOK
	default:
		return BandGood
	}
}

// VolunteerHistoryEntry is one volunteer on one past rota. Given is the
// Allocation as it went out; Attended is the rota as it finally stood, after
// Alterations — nothing records who turned up, so the final rota is the
// nearest thing to it.
type VolunteerHistoryEntry struct {
	Band       AvailabilityBand
	Available  int // shifts said yes to; zero unless Band is low, ok or good
	OpenShifts int
	Given      int
	Attended   int
}

type VolunteerHistoryResult struct {
	Rotations  []db.Rotation     // chronological, last N allocated
	Volunteers []model.Volunteer // everyone asked, given or attending in any of them, by name
	Entries    map[string]map[string]VolunteerHistoryEntry
}

type VolunteerHistoryStore interface {
	RoleStore
	GetRotations(ctx context.Context) ([]db.Rotation, error)
	GetAvailabilityRequestsByRotaID(ctx context.Context, rotaID string) ([]db.AvailabilityRequest, error)
	GetLatestAvailability(ctx context.Context, requestIDs []string, cutoff *time.Time) (map[string]db.AvailabilityGeneration, error)
	GetShiftsByRotaID(ctx context.Context, rotaID string) ([]db.Shift, error)
	GetAllocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.Allocation, error)
	GetAlterationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.Alteration, error)
}

// VolunteerHistory reports, per volunteer, the last count allocated rotas:
// what they said, what they were given and what they ended up on.
func VolunteerHistory(
	ctx context.Context,
	database VolunteerHistoryStore,
	volunteerClient VolunteerClient,
	organisation *config.Organisation,
	logger *zap.Logger,
	count int,
) (*VolunteerHistoryResult, error) {
	rotations, err := database.GetRotations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch rotations: %w", err)
	}
	var allocated []db.Rotation
	for _, r := range rotations {
		if r.AllocatedDatetime != "" {
			allocated = append(allocated, r)
		}
	}
	sort.Slice(allocated, func(i, j int) bool { return allocated[i].Start < allocated[j].Start })
	if count < len(allocated) {
		allocated = allocated[len(allocated)-count:]
	}

	entries := make(map[string]map[string]VolunteerHistoryEntry)
	entry := func(volunteerID, rotaID string) *VolunteerHistoryEntry {
		if entries[volunteerID] == nil {
			entries[volunteerID] = make(map[string]VolunteerHistoryEntry)
		}
		e := entries[volunteerID][rotaID]
		return &e
	}
	set := func(volunteerID, rotaID string, e *VolunteerHistoryEntry) {
		entries[volunteerID][rotaID] = *e
	}

	openByRota := make(map[string]int, len(allocated))
	for _, rota := range allocated {
		statuses, err := rotaResponseStatuses(ctx, database, rota)
		if err != nil {
			return nil, fmt.Errorf("rota %s: %w", rota.ID, err)
		}
		shifts, err := database.GetShiftsByRotaID(ctx, rota.ID)
		if err != nil {
			return nil, fmt.Errorf("rota %s: failed to fetch shifts: %w", rota.ID, err)
		}
		// A Closed Shift is nobody's, so neither its allocations nor its
		// alterations count.
		var openIDs []string
		for _, s := range shifts {
			if !s.Closed {
				openIDs = append(openIDs, s.ID)
			}
		}
		openByRota[rota.ID] = len(openIDs)

		allocations, err := database.GetAllocationsByShiftIDs(ctx, openIDs)
		if err != nil {
			return nil, fmt.Errorf("rota %s: failed to fetch allocations: %w", rota.ID, err)
		}
		alterations, err := database.GetAlterationsByShiftIDs(ctx, openIDs)
		if err != nil {
			return nil, fmt.Errorf("rota %s: failed to fetch alterations: %w", rota.ID, err)
		}

		for volunteerID, status := range statuses {
			e := entry(volunteerID, rota.ID)
			e.Band = BandFor(status, len(openIDs))
			e.Available = status.AvailableCount
			set(volunteerID, rota.ID, e)
		}

		byShift := make(map[string][]db.Allocation)
		for _, a := range allocations {
			if a.VolunteerID == "" {
				continue
			}
			byShift[a.ShiftID] = append(byShift[a.ShiftID], a)
			e := entry(a.VolunteerID, rota.ID)
			e.Given++
			set(a.VolunteerID, rota.ID, e)
		}
		for _, final := range utils.ApplyAlterations(byShift, alterations) {
			for _, a := range final {
				if a.VolunteerID == "" {
					continue
				}
				e := entry(a.VolunteerID, rota.ID)
				e.Attended++
				set(a.VolunteerID, rota.ID, e)
			}
		}
	}

	roles, err := RoleTable(ctx, database)
	if err != nil {
		return nil, err
	}
	allVolunteers, err := volunteerClient.ListVolunteers(organisation, roles)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch volunteers: %w", err)
	}
	volunteers := []model.Volunteer{}
	for _, v := range allVolunteers {
		if entries[v.ID] != nil {
			volunteers = append(volunteers, v)
		}
	}
	sort.Slice(volunteers, func(i, j int) bool { return volunteers[i].DisplayName < volunteers[j].DisplayName })

	// Fill the gaps: someone absent from a rota's round was not asked.
	for _, v := range volunteers {
		for _, rota := range allocated {
			e := entry(v.ID, rota.ID)
			if e.Band == "" {
				e.Band = BandNotSent
			}
			e.OpenShifts = openByRota[rota.ID]
			set(v.ID, rota.ID, e)
		}
	}

	logger.Debug("Volunteer history", zap.Int("rotations", len(allocated)), zap.Int("volunteers", len(volunteers)))
	return &VolunteerHistoryResult{Rotations: allocated, Volunteers: volunteers, Entries: entries}, nil
}

// rotaResponseStatuses reads one allocated rota's round and returns a status per
// volunteer who was asked, keyed by volunteer id. Volunteers who were not asked
// are simply absent — the caller decides what that means.
func rotaResponseStatuses(
	ctx context.Context,
	database VolunteerHistoryStore,
	rota db.Rotation,
) (map[string]VolunteerRotaStatus, error) {
	cutoff, err := time.Parse(time.RFC3339, rota.AllocatedDatetime)
	if err != nil {
		return nil, fmt.Errorf("failed to parse allocated_datetime: %w", err)
	}

	requests, err := database.GetAvailabilityRequestsByRotaID(ctx, rota.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch availability requests: %w", err)
	}

	requestIDs := make([]string, 0, len(requests))
	for _, r := range requests {
		requestIDs = append(requestIDs, r.ID)
	}
	latest, err := database.GetLatestAvailability(ctx, requestIDs, &cutoff)
	if err != nil {
		return nil, fmt.Errorf("failed to read availability: %w", err)
	}

	statuses := make(map[string]VolunteerRotaStatus, len(requests))
	for _, request := range requests {
		status := VolunteerRotaStatus{ShiftCount: rota.ShiftCount}
		generation, replied := latest[request.ID]
		switch {
		case !replied:
			status.Status = "no_response"
		case len(generation.Answers) == 0:
			// Available for nothing: a real answer, and one the Forms encoding
			// could not tell apart from silence.
			status.Status = "no_availability"
		default:
			status.Status = "available"
			status.AvailableCount = len(generation.Answers)
		}
		statuses[request.VolunteerID] = status
	}

	return statuses, nil
}
