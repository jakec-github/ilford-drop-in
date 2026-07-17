package services

import (
	"context"
	"fmt"
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
	GetShiftByDate(ctx context.Context, date time.Time) (*db.Shift, error)
	WithRotaLock(ctx context.Context, rotaIDs []string, fn func(store db.RotaChangeStore) error) error
}

// ChangeRotaParams holds the input parameters for a rota change
type ChangeRotaParams struct {
	Date      string // Target shift date (YYYY-MM-DD)
	In        string // Volunteer ID to add
	Out       string // Volunteer ID to remove
	InCustom  string // Custom value to add
	OutCustom string // Custom value to remove
	SwapDate  string // Optional date for reverse operation (YYYY-MM-DD)
	Reason    string // Required reason for the change
	UserEmail string // Email of the user making the change
}

// ChangeRotaResult contains the result of a rota change. Alterations are keyed
// by shift id (ADR 0001); DatesByShiftID maps each shift id touched by this
// change back to its date so the API and CLI can still render dates without
// re-resolving them.
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
	cfg *config.Config,
	params ChangeRotaParams,
	logger *zap.Logger,
) (*ChangeRotaResult, error) {
	logger.Debug("Starting changeRota",
		zap.String("date", params.Date),
		zap.String("in", params.In),
		zap.String("out", params.Out),
		zap.String("in_custom", params.InCustom),
		zap.String("out_custom", params.OutCustom),
		zap.String("swap_date", params.SwapDate),
		zap.String("reason", params.Reason))

	// Step 1: Input validation
	if params.In == "" && params.Out == "" && params.InCustom == "" && params.OutCustom == "" {
		return nil, wrapf(ErrInvalidInput, "at least one of --in, --out, --in-custom, or --out-custom must be provided")
	}

	if params.Reason == "" {
		return nil, wrapf(ErrInvalidInput, "--reason is required")
	}

	// Step 1b: Fetch volunteers and validate IDs
	volunteers, err := volunteerClient.ListVolunteers(cfg)
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
	}

	if params.Out != "" {
		if _, ok := volunteersByID[params.Out]; !ok {
			return nil, wrapf(ErrNotFound, "volunteer %s not found", params.Out)
		}
	}

	// Step 2: Resolve the target date to its shift — the id keys allocations and
	// alterations, the rota id is what the change locks, and the date is echoed
	// back in the result. Shifts are immutable once minted, so resolving before
	// the lock is safe.
	shift, err := resolveShift(ctx, database, params.Date)
	if err != nil {
		return nil, err
	}

	// If swap_date is provided, resolve its shift (may be in a different rota).
	// A swap onto the same date reuses the primary shift.
	swapShift := shift
	if params.SwapDate != "" && params.SwapDate != params.Date {
		swapShift, err = resolveShift(ctx, database, params.SwapDate)
		if err != nil {
			return nil, fmt.Errorf("swap date: %w", err)
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
	if params.SwapDate != "" {
		lockRotaIDs = append(lockRotaIDs, swapShift.RotaID)
	}

	coverID := uuid.New().String()
	var alterations []db.Alteration
	err = database.WithRotaLock(ctx, lockRotaIDs, func(store db.RotaChangeStore) error {
		// Build effective state for the primary shift and validate against it
		effectiveState, err := buildEffectiveState(ctx, store, shift)
		if err != nil {
			return err
		}

		if err := validateDateChanges(effectiveState, params.Date, params.Out, params.In, params.OutCustom, params.InCustom); err != nil {
			return err
		}

		// Validate swap date (with in/out reversed), using its own effective state
		swapEffectiveState := effectiveState
		if params.SwapDate != "" {
			if params.SwapDate != params.Date {
				swapEffectiveState, err = buildEffectiveState(ctx, store, swapShift)
				if err != nil {
					return err
				}
			}
			if err := validateDateChanges(swapEffectiveState, params.SwapDate, params.In, params.Out, params.InCustom, params.OutCustom); err != nil {
				return fmt.Errorf("swap date: %w", err)
			}
		}

		// Create cover record
		cover := &db.Cover{
			ID:        coverID,
			CreatedAt: time.Now().UTC().Format(time.RFC3339),
			Reason:    params.Reason,
			UserEmail: params.UserEmail,
		}

		// Build alterations for the primary shift, then reverse alterations for
		// the swap shift (which may belong to a different rota)
		alterations = append(alterations, buildAlterationsForShift(shift.ID, coverID, params.Out, params.In, params.OutCustom, params.InCustom, volunteersByID, effectiveState)...)
		if params.SwapDate != "" {
			alterations = append(alterations, buildAlterationsForShift(swapShift.ID, coverID, params.In, params.Out, params.InCustom, params.OutCustom, volunteersByID, swapEffectiveState)...)
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
	if params.SwapDate != "" {
		datesByShiftID[swapShift.ID] = swapShift.Date
	}

	return &ChangeRotaResult{
		CoverID:        coverID,
		Alterations:    alterations,
		DatesByShiftID: datesByShiftID,
	}, nil
}

// buildEffectiveState computes the current effective allocations for a single
// shift by applying that shift's existing alterations to its base allocations.
// It reads through the lock-holding transaction's store, so the state cannot
// change between this read and the caller's insert. The shift is a single date,
// so the result is a flat allocation slice rather than a per-shift map.
func buildEffectiveState(ctx context.Context, store db.RotaChangeStore, shift *db.Shift) ([]db.Allocation, error) {
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

// resolveShift looks up the shift on the given date, rejecting dates with no
// shift. This replaces recomputing rota arithmetic: a date now resolves to what
// actually exists in the shift table (ADR 0001).
func resolveShift(ctx context.Context, database ChangeRotaStore, dateStr string) (*db.Shift, error) {
	date, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return nil, wrapf(ErrInvalidInput, "invalid date format %q: expected YYYY-MM-DD", dateStr)
	}

	shift, err := database.GetShiftByDate(ctx, date)
	if err != nil {
		return nil, fmt.Errorf("failed to look up shift for date %s: %w", dateStr, err)
	}
	if shift == nil {
		return nil, wrapf(ErrNotFound, "date %s is not in any rota", dateStr)
	}

	return shift, nil
}

// validateDateChanges validates that the proposed changes are consistent with
// the shift's current effective allocations. dateStr is used only for error
// messages.
func validateDateChanges(allocations []db.Allocation, dateStr, outVol, inVol, outCustom, inCustom string) error {
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
			return wrapf(ErrConflict, "volunteer %s is not on the shift for %s", outVol, dateStr)
		}
	}

	// Validate inVol: must NOT be currently on the shift
	if inVol != "" {
		for _, a := range allocations {
			if a.VolunteerID == inVol {
				return wrapf(ErrConflict, "volunteer %s is already on the shift for %s", inVol, dateStr)
			}
		}
	}

	// Validate outCustom: must exist on the shift
	if outCustom != "" {
		found := false
		for _, a := range allocations {
			if a.CustomEntry == outCustom {
				found = true
				break
			}
		}
		if !found {
			return wrapf(ErrConflict, "custom entry %q is not on the shift for %s", outCustom, dateStr)
		}
	}

	// No validation for inCustom: custom entries can be duplicated on a shift
	// (e.g. multiple people from the same organisation)

	return nil
}

// buildAlterationsForShift creates alteration records for a single shift.
// dateAllocations is the shift's effective state, used to infer the role for
// "add" alterations. Each alteration references the shift by id (ADR 0001).
func buildAlterationsForShift(shiftID, coverID, outVol, inVol, outCustom, inCustom string, volunteersByID map[string]model.Volunteer, dateAllocations []db.Allocation) []db.Alteration {
	var alterations []db.Alteration

	if outVol != "" {
		alterations = append(alterations, db.Alteration{
			ID:          uuid.New().String(),
			ShiftID:     shiftID,
			Direction:   "remove",
			VolunteerID: outVol,
			CoverID:     coverID,
		})
	}

	if inVol != "" {
		role := inferRole(inVol, outVol, volunteersByID, dateAllocations)
		alterations = append(alterations, db.Alteration{
			ID:          uuid.New().String(),
			ShiftID:     shiftID,
			Direction:   "add",
			VolunteerID: inVol,
			CoverID:     coverID,
			Role:        role,
		})
	}

	if outCustom != "" {
		alterations = append(alterations, db.Alteration{
			ID:          uuid.New().String(),
			ShiftID:     shiftID,
			Direction:   "remove",
			CustomValue: outCustom,
			CoverID:     coverID,
		})
	}

	if inCustom != "" {
		alterations = append(alterations, db.Alteration{
			ID:          uuid.New().String(),
			ShiftID:     shiftID,
			Direction:   "add",
			CustomValue: inCustom,
			CoverID:     coverID,
		})
	}

	return alterations
}

// inferRole determines the role for an incoming volunteer.
// 1. Replacement: inherit the outgoing volunteer's role from the shift.
// 2. Otherwise use the volunteer's own role, but downgrade team lead if one already exists.
func inferRole(inVol, outVol string, volunteersByID map[string]model.Volunteer, dateAllocations []db.Allocation) string {
	if outVol != "" {
		for _, a := range dateAllocations {
			if a.VolunteerID == outVol {
				return a.Role
			}
		}
	}

	role := string(model.RoleVolunteer)
	if v, ok := volunteersByID[inVol]; ok {
		role = string(v.Role)
	}

	if role == string(model.RoleTeamLead) {
		for _, a := range dateAllocations {
			if a.Role == string(model.RoleTeamLead) {
				return string(model.RoleVolunteer)
			}
		}
	}

	return role
}
