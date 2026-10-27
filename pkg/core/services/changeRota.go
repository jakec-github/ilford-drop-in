package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services/utils"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// ChangeRotaStore defines the database operations needed for changing a rota.
// State reads, validation, and the insert all happen inside WithRotaLock so
// concurrent changes (and allocations) of the same rota serialise instead of
// both validating against the same pre-state (issue #41, hazards H1 and H2).
type ChangeRotaStore interface {
	RoleStore
	GetShiftByID(ctx context.Context, id string) (*db.ShiftInRange, error)
	WithRotaLock(ctx context.Context, rotaIDs []string, fn func(store db.RotaChangeStore) error) error
}

// ChangeRotaParams holds the input parameters for a rota change
type ChangeRotaParams struct {
	ShiftID   string // Id of the shift being changed
	In        string // Volunteer ID to add
	Out       string // Volunteer ID to remove
	InCustom  string // Custom value to add
	OutCustom string // Custom value to remove
	// Optional id of the shift the reverse operation runs on. A Shift is named
	// by id, never by date: a date stops naming one Shift once a day can hold
	// two (issue #293).
	SwapShiftID string
	// Why the change was made. Required only of a simple removal — see
	// isSimpleRemoval. Trimmed on the way in, so whitespace is no reason at
	// all, and an unstated one is stored as NULL rather than an empty string.
	Reason    string
	UserEmail string // Email of the user making the change
	// Role the incoming volunteer takes. Required alongside In, and — on a
	// swap, where Out is also set — refused, since each leg then has its own
	// incoming person and there is no unambiguous one to apply it to. A move
	// (SwapShiftID set, Out empty) has exactly one incoming person, so it is
	// offered there same as any other arrival — see validateRole. Naming the
	// same person in In and Out is a Role switch: they come off the shift and
	// go straight back onto it in this Role, which is the only thing such a
	// change says, so it is the one shape that is nothing without it (issue
	// #147). How many of
	// a Role a shift ends up with is not checked: a change records what
	// happened on the day, and the Shape it would be checked against is
	// frozen the moment the rota is allocated, so refusing would leave an
	// extra pair of hands unrecordable (issue #185).
	//
	// By id, as every reference to a Role is (issue #222): a name would be
	// refused, or land on the wrong Role, the moment somebody renamed one
	// while this change was being made.
	RoleID string
	// The Role of the Seat OutCustom leaves on the shift. A custom entry may hold
	// several Seats on one shift (#195), so its leaving names which, and
	// exactly that one Seat comes off (issue #216). Required whenever the
	// entry holds a Role there; one holding a Seat with no Role — added before
	// #214 — names none. Refused for a volunteer, who holds at most one Seat
	// on a shift and so has none to pick out.
	OutRoleID string
	// The same for whoever leaves the swap shift: InCustom, on a move or a swap,
	// since the swap leg runs the change the other way round.
	SwapOutRoleID string
}

// ChangeRotaResult contains the result of a rota change. Alterations are keyed
// by shift id (ADR 0001); DatesByShiftID maps each shift id touched by this
// change to its date, which the API carries alongside the id for display.
type ChangeRotaResult struct {
	CoverID        string
	Alterations    []db.Alteration
	DatesByShiftID map[string]string
}

// ChangeRota records a cover and its alterations for modifying a published rota.
// It validates the change against the current effective state (allocations + existing alterations).
func ChangeRota(
	ctx context.Context,
	database ChangeRotaStore,
	volunteerClient VolunteerClient,
	organisation *config.Organisation,
	params ChangeRotaParams,
	logger *zap.Logger,
) (*ChangeRotaResult, error) {
	// Trimmed before anything reads it, so whitespace is an absent reason
	// rather than a present one: the check below and the stored Cover then
	// agree on what "no reason" is, whatever a client posts (issue #148).
	params.Reason = strings.TrimSpace(params.Reason)

	logger.Debug("Starting changeRota",
		zap.String("shift_id", params.ShiftID),
		zap.String("in", params.In),
		zap.String("out", params.Out),
		zap.String("in_custom", params.InCustom),
		zap.String("out_custom", params.OutCustom),
		zap.String("swap_shift_id", params.SwapShiftID),
		zap.String("reason", params.Reason))

	// Step 1: Input validation
	if params.In == "" && params.Out == "" && params.InCustom == "" && params.OutCustom == "" {
		return nil, wrapf(ErrInvalidInput, "at least one of --in, --out, --in-custom, or --out-custom must be provided")
	}

	if params.Reason == "" && isSimpleRemoval(params) {
		return nil, wrapf(ErrInvalidInput, "a reason is required to take someone off a shift with nobody in their place")
	}

	if err := validateSwitch(params); err != nil {
		return nil, err
	}

	roles, err := RoleTable(ctx, database)
	if err != nil {
		return nil, err
	}
	if err := validateRole(params, roles); err != nil {
		return nil, err
	}
	if err := validateLeavingRoles(params, roles); err != nil {
		return nil, err
	}

	// Step 1b: Fetch volunteers and validate IDs
	volunteers, err := volunteerClient.ListVolunteers(organisation, roles)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch volunteers: %w", err)
	}

	volunteersByID := make(map[string]model.Volunteer, len(volunteers))
	for _, v := range volunteers {
		volunteersByID[v.ID] = v
	}

	if params.In != "" {
		if _, ok := volunteersByID[params.In]; !ok {
			return nil, wrapf(ErrNotFound, "volunteer %s not found", params.In)
		}
		warnUnheldRole(params, volunteersByID, roles, logger)
	}

	if params.Out != "" {
		if _, ok := volunteersByID[params.Out]; !ok {
			return nil, wrapf(ErrNotFound, "volunteer %s not found", params.Out)
		}
	}

	// Step 2: Resolve the target shift — the id keys allocations and
	// alterations, the rota id is what the change locks, and the date and start
	// name it in a refusal. Resolving before the lock is safe: a change only
	// reaches an allocated rota, whose shifts no longer move.
	shift, err := resolveShift(ctx, database, params.ShiftID)
	if err != nil {
		return nil, err
	}

	// If a swap shift is named, resolve it (it may be in a different rota). A
	// swap onto the same shift reuses the primary one.
	swapShift := shift
	if params.SwapShiftID != "" && params.SwapShiftID != params.ShiftID {
		swapShift, err = resolveShift(ctx, database, params.SwapShiftID)
		if err != nil {
			return nil, fmt.Errorf("swap shift: %w", err)
		}
	}

	// Steps 3-8 run under the rotation-row lock: the effective-state reads,
	// the validation against them, and the insert must be one atomic span, or
	// a concurrent change or allocation could commit between validation and
	// insert and invalidate what was just checked (issue #41, H1 and H2).
	// Locking both rotas (deduplicated, in consistent order) covers a swap
	// that crosses rotas. External calls (volunteer fetch above) stay outside
	// so no network I/O happens while rows are locked.
	lockRotaIDs := []string{shift.RotaID}
	if params.SwapShiftID != "" {
		lockRotaIDs = append(lockRotaIDs, swapShift.RotaID)
	}

	// Each shift's half of the change. The swap leg runs it the other way
	// round: whoever leaves the primary shift joins this one. Only the Role of
	// whoever arrives is left to settle, once the shifts have been read.
	primary := shiftChange{
		shiftID:       shift.ID,
		out:           params.Out,
		in:            params.In,
		outCustom:     params.OutCustom,
		inCustom:      params.InCustom,
		outCustomRole: params.OutRoleID,
		roleID:        params.RoleID,
	}
	swapLeg := shiftChange{
		shiftID:       swapShift.ID,
		out:           params.In,
		in:            params.Out,
		outCustom:     params.InCustom,
		inCustom:      params.OutCustom,
		outCustomRole: params.SwapOutRoleID,
	}

	coverID := uuid.New().String()
	var alterations []db.Alteration
	err = database.WithRotaLock(ctx, lockRotaIDs, func(store db.RotaChangeStore) error {
		// Build effective state for the primary shift and validate against it
		effectiveState, err := buildEffectiveState(ctx, store, shift, roles)
		if err != nil {
			return err
		}

		if err := validateShiftChanges(effectiveState, primary, volunteersByID, roles, shiftLabel(*shift)); err != nil {
			return err
		}

		// Validate the swap shift (with in/out reversed), using its own
		// effective state
		swapEffectiveState := effectiveState
		if params.SwapShiftID != "" {
			if params.SwapShiftID != params.ShiftID {
				swapEffectiveState, err = buildEffectiveState(ctx, store, swapShift, roles)
				if err != nil {
					return err
				}
			}
			if err := validateShiftChanges(swapEffectiveState, swapLeg, volunteersByID, roles, shiftLabel(*swapShift)); err != nil {
				return fmt.Errorf("swap shift: %w", err)
			}
		}

		// Create cover record. An unstated reason is NULL, not an empty
		// string: the column is the audit trail's account of why a rota
		// stopped matching its allocation, and it should say when there was
		// no account rather than leaving a reader to decode '' (issue #148).
		var reason *string
		if params.Reason != "" {
			reason = &params.Reason
		}
		cover := &db.Cover{
			ID:        coverID,
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
			Reason:    reason,
			UserEmail: params.UserEmail,
		}

		// Build alterations for the primary shift, then reverse alterations for
		// the swap shift (which may belong to a different rota)
		arriving := person{volunteerID: params.In, custom: params.InCustom, customRole: params.SwapOutRoleID}
		leaving := person{volunteerID: params.Out, custom: params.OutCustom, customRole: params.OutRoleID}
		if (primary.in != "" || primary.inCustom != "") && primary.roleID == "" {
			primary.roleID = roleForIncoming(arriving, leaving, effectiveState, swapEffectiveState)
		}
		alterations = append(alterations, buildAlterationsForShift(primary, coverID)...)
		if params.SwapShiftID != "" {
			// A swap names no Role for whoever arrives — there is no
			// unambiguous person to apply one to — so both legs work theirs
			// out from the shifts.
			if swapLeg.in != "" || swapLeg.inCustom != "" {
				swapLeg.roleID = roleForIncoming(leaving, arriving, swapEffectiveState, effectiveState)
			}
			alterations = append(alterations, buildAlterationsForShift(swapLeg, coverID)...)
		}

		if err := store.InsertCoverAndAlterations(ctx, cover, alterations); err != nil {
			return fmt.Errorf("failed to insert cover and alterations: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	logger.Info("Rota change recorded",
		zap.String("cover_id", coverID),
		zap.Int("alteration_count", len(alterations)))

	datesByShiftID := map[string]string{shift.ID: shift.Date}
	if params.SwapShiftID != "" {
		datesByShiftID[swapShift.ID] = swapShift.Date
	}

	return &ChangeRotaResult{
		CoverID:        coverID,
		Alterations:    alterations,
		DatesByShiftID: datesByShiftID,
	}, nil
}

// isSimpleRemoval reports whether the change only takes people off the rota:
// nobody arrives on the shift, and there is no swap shift to put whoever
// leaves on another. That one shape is the only one asked for a reason (issue
// #148). Every other change accounts for itself — a replacement or a swap names
// who fills the place, an add makes the shift better off — while a removal
// leaves a gap that the cover record is the only place to explain.
//
// A swap shift is enough to disqualify it even with nothing coming in, because
// the swap leg reverses the change: whoever leaves the shift joins that one.
//
// Only asked after the check that the change does something at all, so nobody
// arriving does mean somebody leaving.
func isSimpleRemoval(params ChangeRotaParams) bool {
	return params.In == "" && params.InCustom == "" && params.SwapShiftID == ""
}

// switchingRole reports whether this change names the same person on both
// sides of one shift — the shape that moves somebody between Roles without
// moving them anywhere else (issue #147). They come off the shift and go
// straight back onto it in the Role the caller named, which is two alterations
// like a replacement, and a replacement of somebody by themselves is exactly
// what it is.
func switchingRole(outVol, inVol, outCustom, inCustom string) bool {
	if outVol != "" && outVol == inVol {
		return true
	}
	return outCustom != "" && outCustom == inCustom
}

// validateSwitch refuses the one nonsense a Role switch can be asked for: the
// same person on both sides of a *swap*. A swap reverses the change on the
// second shift, so naming one person means taking them off each shift and
// putting them back on it — two no-ops, recorded as four alterations, and on a
// reading of the result indistinguishable from a switch that went through.
// Nothing offers it; saying so beats storing it.
func validateSwitch(params ChangeRotaParams) error {
	if params.SwapShiftID == "" {
		return nil
	}
	if switchingRole(params.Out, params.In, params.OutCustom, params.InCustom) {
		return wrapf(ErrInvalidInput, "the same person cannot be both sides of a swap")
	}
	return nil
}

// buildEffectiveState computes the current effective allocations for a single
// shift by applying that shift's existing alterations to its base allocations.
// It reads through the lock-holding transaction's store, so the state cannot
// change between this read and the caller's insert. It is a single shift, so
// the result is a flat allocation slice rather than a per-shift map.
func buildEffectiveState(ctx context.Context, store db.RotaChangeStore, shift *db.Shift, roles model.Roles) ([]db.Allocation, error) {
	allocations, err := store.GetAllocationsByShiftIDs(ctx, []string{shift.ID})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch allocations: %w", err)
	}

	alterations, err := store.GetAlterationsByShiftIDs(ctx, []string{shift.ID})
	if err != nil {
		return nil, fmt.Errorf("failed to fetch alterations: %w", err)
	}

	byShiftID := utils.ApplyAlterations(map[string][]db.Allocation{shift.ID: allocations}, alterations)
	return byShiftID[shift.ID], nil
}

// resolveShift looks up the shift with the given id, rejecting an id no shift
// has (ADR 0001).
func resolveShift(ctx context.Context, database ChangeRotaStore, shiftID string) (*db.Shift, error) {
	if shiftID == "" {
		return nil, wrapf(ErrInvalidInput, "shiftId is required")
	}
	found, err := database.GetShiftByID(ctx, shiftID)
	if err != nil {
		return nil, fmt.Errorf("failed to look up shift %s: %w", shiftID, err)
	}
	if found == nil {
		return nil, wrapf(ErrNotFound, "shift %s not found", shiftID)
	}

	return &found.Shift, nil
}

// validateShiftChanges validates that one shift's half of a change is
// consistent with the shift's current effective allocations. label names the
// shift in error messages, and volunteersByID and roles only name people and
// Roles in them.
func validateShiftChanges(allocations []db.Allocation, change shiftChange, volunteersByID map[string]model.Volunteer, roles model.Roles, label string) error {
	outVol, inVol, outCustom, inCustom := change.out, change.in, change.outCustom, change.inCustom

	// A Role switch names the same person on both sides: they are meant to be
	// on the shift already, and to stay there. The two checks below are about
	// somebody arriving who was not there and somebody leaving who was, so
	// neither says anything here — the "already on the shift" one would refuse
	// the change outright.
	switching := switchingRole(outVol, inVol, outCustom, inCustom)

	// Validate outVol: must be currently on the shift
	if outVol != "" {
		found := false
		for _, a := range allocations {
			if a.VolunteerID == outVol {
				found = true
				break
			}
		}
		if !found {
			return wrapf(ErrConflict, "%s is not on the shift on %s", volunteerLabel(outVol, volunteersByID), label)
		}
	}

	// Validate inVol: must NOT be currently on the shift
	if inVol != "" && !switching {
		for _, a := range allocations {
			if a.VolunteerID == inVol {
				return wrapf(ErrConflict, "%s is already on the shift on %s", volunteerLabel(inVol, volunteersByID), label)
			}
		}
	}

	// Validate outCustom: must hold the Seat it names on the shift
	if outCustom != "" {
		if err := validateLeavingSeat(allocations, outCustom, change.outCustomRole, roles, label); err != nil {
			return err
		}
	}

	// No validation for inCustom: custom entries can be duplicated on a shift
	// (e.g. multiple people from the same organisation)

	return nil
}

// validateLeavingSeat checks that a custom entry leaving a shift holds the
// Seat it names there. The entry may hold several (#195), and the Role is what
// says which one goes (issue #216), so a Role it is not in on this shift is a
// change against a state that is not the shift's.
//
// Naming no Role is how an entry whose Seat has none leaves — one added before
// #214. Where every Seat it holds has a Role, naming none leaves it unsaid
// which goes, and that is the caller's to say rather than this to guess.
func validateLeavingSeat(allocations []db.Allocation, custom, roleID string, roles model.Roles, label string) error {
	onShift := false
	for _, a := range allocations {
		if a.CustomEntry != custom {
			continue
		}
		if a.RoleID == roleID {
			return nil
		}
		onShift = true
	}
	if !onShift {
		return wrapf(ErrConflict, "custom entry %q is not on the shift on %s", custom, label)
	}
	if roleID == "" {
		return wrapf(ErrInvalidInput, "say which Role custom entry %q is leaving the shift on %s in", custom, label)
	}
	role, _ := roles.ByID(roleID)
	return wrapf(ErrConflict, "custom entry %q is not on the shift on %s as %s", custom, label, role.Name)
}

// volunteerLabel names a volunteer for an error message a human reads — the
// rota UI shows these inline against the shift, where an opaque id says
// nothing. Falls back to the id for anyone the roster does not know, which
// cannot happen on the validation path but keeps the helper total.
func volunteerLabel(id string, volunteersByID map[string]model.Volunteer) string {
	if v, ok := volunteersByID[id]; ok && v.DisplayName != "" {
		return v.DisplayName
	}
	return id
}

// shiftChange is one shift's half of a rota change: who leaves it, who joins
// it, and the Role whoever joins takes when whoever made the change named one. A swap
// is two of these — the second with in and out exchanged.
type shiftChange struct {
	shiftID       string
	out           string // Volunteer ID leaving
	in            string // Volunteer ID joining
	outCustom     string // Custom entry leaving
	inCustom      string // Custom entry joining
	outCustomRole string // Role of the Seat outCustom leaves, or empty for a Role-less one
	roleID        string // Role for whoever joins, or empty to inherit it
}

// buildAlterationsForShift creates alteration records for a single shift. The
// Role is settled by the caller, which is the only place both shifts of a swap
// are in scope. Whoever arrives takes it, volunteer or custom entry alike: a
// Seat is a Seat in a Role, and being off the roster changes nothing about that
// (issue #214). A custom entry's remove states the Role of the Seat it leaves,
// which is what makes it take that one Seat (issue #216); a volunteer's states
// none, having only the one Seat on the shift to leave. Each alteration
// references the shift by id (ADR 0001).
func buildAlterationsForShift(change shiftChange, coverID string) []db.Alteration {
	var alterations []db.Alteration

	if change.out != "" {
		alterations = append(alterations, db.Alteration{
			ID:          uuid.New().String(),
			ShiftID:     change.shiftID,
			Direction:   "remove",
			VolunteerID: change.out,
			CoverID:     coverID,
		})
	}

	if change.in != "" {
		alterations = append(alterations, db.Alteration{
			ID:          uuid.New().String(),
			ShiftID:     change.shiftID,
			Direction:   "add",
			VolunteerID: change.in,
			CoverID:     coverID,
			RoleID:      change.roleID,
		})
	}

	if change.outCustom != "" {
		alterations = append(alterations, db.Alteration{
			ID:          uuid.New().String(),
			ShiftID:     change.shiftID,
			Direction:   "remove",
			CustomValue: change.outCustom,
			CoverID:     coverID,
			RoleID:      change.outCustomRole,
		})
	}

	if change.inCustom != "" {
		alterations = append(alterations, db.Alteration{
			ID:          uuid.New().String(),
			ShiftID:     change.shiftID,
			Direction:   "add",
			CustomValue: change.inCustom,
			CoverID:     coverID,
			RoleID:      change.roleID,
		})
	}

	return alterations
}

// validateRole checks the caller-supplied Role. It names whoever is coming in
// on the shift, so anybody coming in must have one: the Role is what the Shape has a
// Seat for, and guessing it from the roster is exactly what #89 takes away.
// "Anybody" is the whole of the rule — a custom entry fills a Seat as much as a
// volunteer does, and whether somebody is on the roster is no business of
// whether their Seat has a Role (issue #214).
//
// A change with nobody coming in has none to set, and a swap has two incoming
// people — one on each shift — so a single Role would land on whichever the
// caller did not mean; there, each leg inherits the Role of the person it
// replaces instead.
func validateRole(params ChangeRotaParams, roles model.Roles) error {
	arriving := params.In != "" || params.InCustom != ""
	leaving := params.Out != "" || params.OutCustom != ""
	if params.RoleID != "" {
		if _, ok := roles.ByID(params.RoleID); !ok {
			return wrapf(ErrInvalidInput, "no role has the id %q", params.RoleID)
		}
		if !arriving {
			return wrapf(ErrInvalidInput, "a role can only be set for someone coming in")
		}
		if params.SwapShiftID != "" && leaving {
			return wrapf(ErrInvalidInput, "a role cannot be set on a swap: each shift has its own incoming person")
		}
		return nil
	}
	if arriving && params.SwapShiftID == "" {
		return wrapf(ErrInvalidInput, "a role is required for someone coming in")
	}
	return nil
}

// validateLeavingRoles checks the Roles a change names for custom entries
// leaving: each is a Role that exists, and names somebody who is leaving and
// is off the roster. A volunteer holds one Seat on a shift at most, so there is
// none to pick out; whether the entry holds the Seat is checked against the
// shift itself, under the lock.
func validateLeavingRoles(params ChangeRotaParams, roles model.Roles) error {
	for _, leaving := range []struct {
		roleID, custom, volunteer, what string
		leaves                          bool
	}{
		{params.OutRoleID, params.OutCustom, params.Out, "the shift", true},
		{params.SwapOutRoleID, params.InCustom, params.In, "the swap shift", params.SwapShiftID != ""},
	} {
		if leaving.roleID == "" {
			continue
		}
		if _, ok := roles.ByID(leaving.roleID); !ok {
			return wrapf(ErrInvalidInput, "no role has the id %q", leaving.roleID)
		}
		if !leaving.leaves || leaving.custom == "" {
			if leaving.volunteer != "" && leaving.leaves {
				return wrapf(ErrInvalidInput, "a leaving role is only named for a custom entry: a volunteer holds one Seat on a shift")
			}
			return wrapf(ErrInvalidInput, "a leaving role was named for %s, which no custom entry is leaving", leaving.what)
		}
	}
	return nil
}

// warnUnheldRole notes someone placing someone in a Role the roster does not
// record them as holding. It proceeds: the structure (one Seat, one person) is
// enforced, but who is up to the job on the day is the call of whoever is changing the rota, and the
// roster is standing advice rather than a gate on a cover (ADR 0005).
//
// The roster spells a Role by name, so that is what the id is read as here.
func warnUnheldRole(params ChangeRotaParams, volunteersByID map[string]model.Volunteer, roles model.Roles, logger *zap.Logger) {
	if params.RoleID == "" || params.In == "" {
		return
	}
	role, ok := roles.ByID(params.RoleID)
	if !ok {
		return
	}
	volunteer, known := volunteersByID[params.In]
	if !known || volunteer.Holds(role.Name) {
		return
	}
	logger.Warn("Placing a volunteer in a role they do not hold",
		zap.String("volunteer_id", params.In),
		zap.String("role", role.Name),
		zap.String("shift_id", params.ShiftID))
}

// allocationLabel names whoever holds an allocation for an error message a
// human reads: a volunteer by display name, a custom entry by its text.
func allocationLabel(a db.Allocation, volunteersByID map[string]model.Volunteer) string {
	if a.VolunteerID == "" {
		return a.CustomEntry
	}
	return volunteerLabel(a.VolunteerID, volunteersByID)
}

// person names one side of a change: a volunteer by id, or somebody off the
// roster by the text they are recorded under. At most one is set, and neither
// is where a change only has the other side. A custom entry also carries the
// Role of the Seat it leaves, since its text alone does not say which of its
// Seats that is (issue #216).
type person struct {
	volunteerID string
	custom      string
	customRole  string
}

// roleForIncoming settles the Role somebody joining a shift takes where the
// caller named none — the swap path, which is the one path that cannot name
// one. In order:
//
//  1. The Role of the person they replace on the shift they are joining. A
//     swap hands each Seat over intact.
//  2. The Role they held on the shift they are leaving in the same change, so
//     a move carries someone's Role across rather than resetting it. A move is
//     a swap with nobody coming back, and so has nobody to replace.
//  3. No Role at all, which is what a pre-004_alteration_role row says and
//     what every reader already handles: the published sheet gives it a
//     column of its own, the rota page draws it unlabelled. It used to be the
//     uncapped Role, and with that gone there is nothing left to guess with
//     (issue #185). Refusing is not the alternative it looks like — a swap is
//     the one change that may not name a Role, so there would be no way
//     through.
//
// All three are rules about the shifts, not guesses about the person, which is
// what makes them survivable after inferRole's deletion — and what makes them
// apply unchanged to somebody off the roster, who now holds a Role like anybody
// else and would otherwise lose it to a swap (issue #214).
func roleForIncoming(in, out person, joining, leaving []db.Allocation) string {
	if role, ok := roleOnShift(out, joining); ok {
		return role
	}
	if role, ok := roleOnShift(in, leaving); ok {
		return role
	}
	return ""
}

// roleOnShift is the Role somebody currently holds among these allocations. An
// allocation with no Role — written before alterations had a column for one —
// names nothing to inherit, so it reports none rather than an empty Role.
//
// A custom entry is not looked up at all. It may hold several Seats on one
// shift (#195), and is only ever asked about on the shift it is leaving, where
// it has named the Seat it leaves (issue #216) — and validation has already
// checked it holds it. That Role is the answer, and none is none.
func roleOnShift(p person, allocations []db.Allocation) (string, bool) {
	if p.custom != "" {
		return p.customRole, p.customRole != ""
	}
	if p.volunteerID == "" {
		return "", false
	}
	for _, a := range allocations {
		if a.RoleID == "" {
			continue
		}
		if a.VolunteerID == p.volunteerID {
			return a.RoleID, true
		}
	}
	return "", false
}
