package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// ChangeRotaStore defines the database operations needed for changing a rota
type ChangeRotaStore interface {
	GetRotations(ctx context.Context) ([]db.Rotation, error)
	GetAllocations(ctx context.Context) ([]db.Allocation, error)
	GetAlterations(ctx context.Context) ([]db.Alteration, error)
	InsertCoverAndAlterations(ctx context.Context, cover *db.Cover, alterations []db.Alteration) error
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

// ChangeRotaResult contains the result of a rota change
type ChangeRotaResult struct {
	CoverID     string
	Alterations []db.Alteration
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
		return nil, fmt.Errorf("at least one of --in, --out, --in-custom, or --out-custom must be provided")
	}

	if params.Reason == "" {
		return nil, fmt.Errorf("--reason is required")
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
			return nil, fmt.Errorf("volunteer %s not found", params.In)
		}
	}

	if params.Out != "" {
		if _, ok := volunteersByID[params.Out]; !ok {
			return nil, fmt.Errorf("volunteer %s not found", params.Out)
		}
	}

	// Step 2: Fetch rotations and find the one containing the target date
	rotations, err := database.GetRotations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch rotations: %w", err)
	}

	rota, err := findRotaForDate(rotations, params.Date)
	if err != nil {
		return nil, err
	}

	// If swap_date is provided, find its rota (may be different)
	var swapRota *db.Rotation
	if params.SwapDate != "" {
		swapRota, err = findRotaForDate(rotations, params.SwapDate)
		if err != nil {
			return nil, fmt.Errorf("swap date: %w", err)
		}
	}

	// Step 3: Build current effective state
	allAllocations, err := database.GetAllocations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch allocations: %w", err)
	}

	allAlterations, err := database.GetAlterations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch alterations: %w", err)
	}

	// Build effective state for the primary date's rota
	effectiveState := buildEffectiveState(allAllocations, allAlterations, rota.ID)

	// Step 4: Validate against current effective state
	if err := validateDateChanges(effectiveState, params.Date, params.Out, params.In, params.OutCustom, params.InCustom); err != nil {
		return nil, err
	}

	// Validate swap date (with in/out reversed), using swap rota's effective state
	swapEffectiveState := effectiveState
	if params.SwapDate != "" {
		if swapRota.ID != rota.ID {
			swapEffectiveState = buildEffectiveState(allAllocations, allAlterations, swapRota.ID)
		}
		if err := validateDateChanges(swapEffectiveState, params.SwapDate, params.In, params.Out, params.InCustom, params.OutCustom); err != nil {
			return nil, fmt.Errorf("swap date: %w", err)
		}
	}

	// Step 5: Create cover record
	coverID := uuid.New().String()
	cover := &db.Cover{
		ID:        coverID,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Reason:    params.Reason,
		UserEmail: params.UserEmail,
	}

	// Step 6: Build alterations for the primary date
	var alterations []db.Alteration
	alterations = append(alterations, buildAlterationsForDate(rota.ID, coverID, params.Date, params.Out, params.In, params.OutCustom, params.InCustom, volunteersByID, effectiveState[params.Date])...)

	// Step 7: Build reverse alterations for swap date (may use a different rota ID)
	if params.SwapDate != "" {
		alterations = append(alterations, buildAlterationsForDate(swapRota.ID, coverID, params.SwapDate, params.In, params.Out, params.InCustom, params.OutCustom, volunteersByID, swapEffectiveState[params.SwapDate])...)
	}

	// Step 8: Insert cover and alterations atomically
	if err := database.InsertCoverAndAlterations(ctx, cover, alterations); err != nil {
		return nil, fmt.Errorf("failed to insert cover and alterations: %w", err)
	}

	logger.Info("Rota change recorded",
		zap.String("cover_id", coverID),
		zap.Int("alteration_count", len(alterations)))

	return &ChangeRotaResult{
		CoverID:     coverID,
		Alterations: alterations,
	}, nil
}

// buildEffectiveState computes the current effective allocations for a rota
// by applying all existing alterations to the base allocations
func buildEffectiveState(allAllocations []db.Allocation, allAlterations []db.Alteration, rotaID string) map[string][]db.Allocation {
	rotaAllocations := filterAllocationsByRotaID(allAllocations, rotaID)

	var rotaAlterations []db.Alteration
	for _, a := range allAlterations {
		if a.RotaID == rotaID {
			rotaAlterations = append(rotaAlterations, a)
		}
	}

	allocationsByDate := make(map[string][]db.Allocation)
	for _, a := range rotaAllocations {
		allocationsByDate[a.ShiftDate] = append(allocationsByDate[a.ShiftDate], a)
	}

	return ApplyAlterations(allocationsByDate, rotaAlterations)
}

// findRotaForDate finds the rotation that contains the given date
func findRotaForDate(rotations []db.Rotation, dateStr string) (*db.Rotation, error) {
	targetDate, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return nil, fmt.Errorf("invalid date format %q: expected YYYY-MM-DD", dateStr)
	}

	for i := range rotations {
		rota := &rotations[i]
		shiftDates, err := calculateShiftDates(rota.Start, rota.ShiftCount)
		if err != nil {
			continue
		}
		for _, sd := range shiftDates {
			if sd.Equal(targetDate) {
				return rota, nil
			}
		}
	}

	return nil, fmt.Errorf("date %s is not in any rota", dateStr)
}

// validateDateChanges validates that the proposed changes are consistent with the current state
func validateDateChanges(effectiveState map[string][]db.Allocation, dateStr, outVol, inVol, outCustom, inCustom string) error {
	allocations := effectiveState[dateStr]

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
			return fmt.Errorf("volunteer %s is not on the shift for %s", outVol, dateStr)
		}
	}

	// Validate inVol: must NOT be currently on the shift
	if inVol != "" {
		for _, a := range allocations {
			if a.VolunteerID == inVol {
				return fmt.Errorf("volunteer %s is already on the shift for %s", inVol, dateStr)
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
			return fmt.Errorf("custom entry %q is not on the shift for %s", outCustom, dateStr)
		}
	}

	// No validation for inCustom: custom entries can be duplicated on a shift
	// (e.g. multiple people from the same organisation)

	return nil
}

// buildAlterationsForDate creates alteration records for a single date.
// dateAllocations is the effective state for the date, used to infer the role for "add" alterations.
func buildAlterationsForDate(rotaID, coverID, dateStr, outVol, inVol, outCustom, inCustom string, volunteersByID map[string]model.Volunteer, dateAllocations []db.Allocation) []db.Alteration {
	var alterations []db.Alteration

	if outVol != "" {
		alterations = append(alterations, db.Alteration{
			ID:          uuid.New().String(),
			ShiftDate:   dateStr,
			RotaID:      rotaID,
			Direction:   "remove",
			VolunteerID: outVol,
			CoverID:     coverID,
		})
	}

	if inVol != "" {
		role := inferRole(inVol, outVol, volunteersByID, dateAllocations)
		alterations = append(alterations, db.Alteration{
			ID:          uuid.New().String(),
			ShiftDate:   dateStr,
			RotaID:      rotaID,
			Direction:   "add",
			VolunteerID: inVol,
			CoverID:     coverID,
			Role:        role,
		})
	}

	if outCustom != "" {
		alterations = append(alterations, db.Alteration{
			ID:          uuid.New().String(),
			ShiftDate:   dateStr,
			RotaID:      rotaID,
			Direction:   "remove",
			CustomValue: outCustom,
			CoverID:     coverID,
		})
	}

	if inCustom != "" {
		alterations = append(alterations, db.Alteration{
			ID:          uuid.New().String(),
			ShiftDate:   dateStr,
			RotaID:      rotaID,
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
