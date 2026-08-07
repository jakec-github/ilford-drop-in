package services

import (
	"context"
	"fmt"
	"sort"
	"time"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// DraftRotaAllocationReadStore is what reading a Draft Rota Allocation needs:
// the rota in flight, the Shifts and Shapes that say what it is asking for, and
// the draft solved against them.
//
// Deliberately read-only, and deliberately not embedding
// DraftRotaAllocationStore. Holding one of these is permission to look at a
// draft, not to solve one — a solve is a thirty-second subprocess, and the two
// reach the API through different gates.
type DraftRotaAllocationReadStore interface {
	ShiftShapeStore
	GetRotaInFlight(ctx context.Context) (*db.RotaInFlight, error)
	GetShiftsByRotaID(ctx context.Context, rotaID string) ([]db.Shift, error)
	GetDraftRotaAllocation(ctx context.Context, rotaID string) (*db.DraftRotaAllocation, error)
	GetDraftAllocationsByShiftIDs(ctx context.Context, shiftIDs []string) ([]db.DraftAllocation, error)
}

// DraftRotaAllocationView is the rota in flight and whatever has been drafted
// for it, as an admin reads it on the rota page.
//
// The two levels are nullable independently because there are three states and
// the screen says something different in each: nothing in flight (this whole
// value is nil), a rota nobody has solved for yet (Draft is nil), and a rota
// with a draft. An infeasible solve is the third of those, not the second — it
// stores no Seats but it is an answer, and telling the two apart is why the
// outcome is stored at all (ADR 0008).
type DraftRotaAllocationView struct {
	RotaID string
	// SeatsAsked is what the rota is asking for *now* — every Seat of every open
	// Shift's Shape — rather than what it asked when the draft was solved. It is
	// the denominator of "two Seats unfilled", and reading it live is the
	// truthful half of that fraction: a Shape edited since the solve is exactly
	// the kind of change that makes a draft worth solving again.
	SeatsAsked int
	// Draft is the solve, or nil for a rota nobody has solved for yet.
	Draft *SolvedDraft
}

// SolvedDraft is one stored solve: what it concluded, and the rota it drafted.
type SolvedDraft struct {
	SolvedAt     time.Time
	Success      bool
	SolverStatus string
	// SeatsFilled is how many Seats this solve put somebody in, counted from the
	// Seats themselves rather than stored: they are right there, and a count that
	// could disagree with the rows beside it would be worse than no count.
	SeatsFilled int
	// Shifts carries only the Shifts the draft placed anybody on, in date order.
	// A Shift the solver left empty is absent rather than present and empty —
	// there is nothing to say about it that the rota page does not already say.
	Shifts []DraftShift
}

// DraftShift is one Shift's draft Seats. Keyed by Shift id rather than date,
// because the caller already holds the Shifts and the Shift is the authority on
// its own date (ADR 0001).
type DraftShift struct {
	ShiftID   string
	Assignees []ShiftAssignee
}

// ReadDraftRotaAllocation reads the rota in flight's Draft Rota Allocation, or
// nil when no rota is in flight.
//
// Every caller of this must be admin-gated. A draft names people against Shifts
// on a rota nobody has decided yet and is replaced wholesale every few hours;
// publishing one would tell a volunteer they are working a shift they may well
// not be. That is the whole reason drafts live in tables of their own (ADR
// 0008), and this is the only read that reaches them outside a solve.
//
// It reads through the rota in flight rather than the latest Rotation, so a
// draft left behind on a Rotation that has since been allocated is unreachable
// here: the allocation is the rota, and a draft beside it could only contradict
// it.
func ReadDraftRotaAllocation(
	ctx context.Context,
	database DraftRotaAllocationReadStore,
	volunteerClient VolunteerClient,
	cfg *config.Config,
	logger *zap.Logger,
) (*DraftRotaAllocationView, error) {
	inFlight, err := database.GetRotaInFlight(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read the rota in flight: %w", err)
	}
	if inFlight == nil {
		return nil, nil
	}

	shifts, err := database.GetShiftsByRotaID(ctx, inFlight.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch the rota's shifts: %w", err)
	}
	shiftIDs := make([]string, 0, len(shifts))
	for _, shift := range shifts {
		shiftIDs = append(shiftIDs, shift.ID)
	}

	// Unlike the solve, a Shift asking for nobody is not refused here: this
	// reports on the rota rather than acting on it, and "asks for nobody" is
	// already said on the row itself.
	shapes, err := ShiftShapes(ctx, database, shiftIDs)
	if err != nil {
		return nil, err
	}

	view := &DraftRotaAllocationView{
		RotaID:     inFlight.ID,
		SeatsAsked: countSeatsAsked(shifts, shapes),
	}

	draft, err := database.GetDraftRotaAllocation(ctx, inFlight.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to read the draft for rota %s: %w", inFlight.ID, err)
	}
	if draft == nil {
		// Nothing has been solved, so nothing is read: the roster behind the
		// names is a Google Sheet, and fetching it to name nobody would put a
		// network call on every load of a rota nobody has drafted.
		return view, nil
	}

	seats, err := database.GetDraftAllocationsByShiftIDs(ctx, shiftIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to read the draft's seats: %w", err)
	}

	roles, err := RoleTable(ctx, database)
	if err != nil {
		return nil, err
	}
	volunteers, err := volunteerClient.ListVolunteers(cfg, roles)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch volunteers: %w", err)
	}
	volunteersByID := make(map[string]model.Volunteer, len(volunteers))
	for _, v := range volunteers {
		volunteersByID[v.ID] = v
	}

	// A draft Seat is exactly an allocation row — it becomes one when the rota is
	// allocated (ADR 0008) — so it goes through the one converter rather than
	// growing a second rule about how a Seat becomes a name in an order.
	seatsByShiftID := make(map[string][]db.Allocation, len(seats))
	for _, seat := range seats {
		seatsByShiftID[seat.ShiftID] = append(seatsByShiftID[seat.ShiftID], db.Allocation{
			ID:          seat.ID,
			ShiftID:     seat.ShiftID,
			Role:        seat.Role,
			VolunteerID: seat.VolunteerID,
			CustomEntry: seat.CustomEntry,
		})
	}

	// In date order, which is the order the rota is read in. GetShiftsByRotaID
	// makes no promise about ordering, so the Shifts are sorted here rather than
	// relied on.
	sorted := make([]db.Shift, len(shifts))
	copy(sorted, shifts)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Date < sorted[j].Date })

	solved := &SolvedDraft{
		SolvedAt:     draft.SolvedAt,
		Success:      draft.Success,
		SolverStatus: draft.SolverStatus,
		SeatsFilled:  len(seats),
	}
	for _, shift := range sorted {
		placed := seatsByShiftID[shift.ID]
		if len(placed) == 0 {
			continue
		}
		solved.Shifts = append(solved.Shifts, DraftShift{
			ShiftID:   shift.ID,
			Assignees: buildAssignees(placed, volunteersByID, roles, logger),
		})
	}
	view.Draft = solved

	logger.Debug("Read the draft rota allocation",
		zap.String("rota_id", inFlight.ID),
		zap.String("solver_status", draft.SolverStatus),
		zap.Int("seats_filled", solved.SeatsFilled),
		zap.Int("seats_asked", view.SeatsAsked))

	return view, nil
}
