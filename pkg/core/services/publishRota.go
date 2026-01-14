package services

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/teambition/rrule-go"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/clients/sheetsclient"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// PublishRotaStore defines the database operations needed for publishing a rota
type PublishRotaStore interface {
	GetRotations(ctx context.Context) ([]db.Rotation, error)
	GetAllocations(ctx context.Context) ([]db.Allocation, error)
}

// SheetsClient defines the sheets operations needed for publishing a rota
type SheetsClient interface {
	PublishRota(spreadsheetID string, publishedRota *sheetsclient.PublishedRota) error
}

// PublishRota publishes a rota to Google Sheets
// It fetches the rota, allocations, and volunteer information, then constructs
// the rows with formatted dates, team leads, and volunteers, and publishes to sheets
// If rotaID is empty, it defaults to the latest rota
func PublishRota(
	ctx context.Context,
	database PublishRotaStore,
	sheetsClient SheetsClient,
	volunteerClient VolunteerClient,
	cfg *config.Config,
	logger *zap.Logger,
	rotaID string,
) (*sheetsclient.PublishedRota, error) {
	logger.Debug("Starting publishRota", zap.String("rota_id", rotaID))

	// Step 1: Fetch the target rota
	logger.Debug("Fetching rotations")
	rotations, err := database.GetRotations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch rotations: %w", err)
	}

	if len(rotations) == 0 {
		return nil, fmt.Errorf("no rotations found")
	}

	// Find the target rota (or default to latest if rotaID is empty)
	var targetRota *db.Rotation
	if rotaID == "" {
		// Default to latest rota
		targetRota = findLatestRotation(rotations)
		logger.Debug("No rota ID provided, using latest rota", zap.String("id", targetRota.ID))
	} else {
		// Find specific rota by ID
		for i := range rotations {
			if rotations[i].ID == rotaID {
				targetRota = &rotations[i]
				break
			}
		}

		if targetRota == nil {
			return nil, fmt.Errorf("rota not found: %s", rotaID)
		}
	}

	logger.Debug("Found target rota",
		zap.String("id", targetRota.ID),
		zap.String("start", targetRota.Start),
		zap.Int("shift_count", targetRota.ShiftCount))

	// Step 2: Calculate shift dates
	shiftDates, err := calculateShiftDates(targetRota.Start, targetRota.ShiftCount)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate shift dates: %w", err)
	}

	// Step 3: Fetch all allocations
	logger.Debug("Fetching allocations")
	allAllocations, err := database.GetAllocations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch allocations: %w", err)
	}

	// Filter to allocations for this rota only
	rotaAllocations := filterAllocationsByRotaID(allAllocations, targetRota.ID)
	logger.Debug("Filtered allocations for rota", zap.Int("count", len(rotaAllocations)))

	// Step 4: Fetch volunteers
	logger.Debug("Fetching volunteers")
	volunteers, err := volunteerClient.ListVolunteers(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch volunteers: %w", err)
	}

	// Build volunteer lookup map
	volunteersByID := make(map[string]model.Volunteer)
	for _, vol := range volunteers {
		volunteersByID[vol.ID] = vol
	}

	// Step 5: Group allocations by shift date
	allocationsByDate := make(map[string][]db.Allocation)
	for _, allocation := range rotaAllocations {
		allocationsByDate[allocation.ShiftDate] = append(allocationsByDate[allocation.ShiftDate], allocation)
	}

	// Step 6: Build the published rota rows
	rows := make([]sheetsclient.PublishedRotaRow, 0, len(shiftDates))

	for _, shiftDate := range shiftDates {
		dateStr := shiftDate.Format("2006-01-02")
		allocations := allocationsByDate[dateStr]

		// Check if this shift is closed
		isClosed := isShiftClosed(dateStr, cfg.RotaOverrides, shiftDates, logger)

		row := sheetsclient.PublishedRotaRow{
			Date:       shiftDate.Format("Mon Jan 02 2006"),
			TeamLead:   "",
			Volunteers: []string{},
			HotFood:    "",
			Collection: "",
		}

		// For closed shifts, display "CLOSED" instead of processing allocations
		if isClosed {
			row.TeamLead = "CLOSED"
			rows = append(rows, row)
			continue
		}

		// Process allocations for this shift
		for _, allocation := range allocations {
			// Handle custom entries (pre-allocated volunteers)
			if allocation.VolunteerID == "" && allocation.CustomEntry != "" {
				row.Volunteers = append(row.Volunteers, allocation.CustomEntry)
				continue
			}

			// Look up the volunteer
			volunteer, exists := volunteersByID[allocation.VolunteerID]
			if !exists {
				return nil, fmt.Errorf("volunteer not found: %s (allocation %s, shift %s)",
					allocation.VolunteerID, allocation.ID, dateStr)
			}

			fullName := fmt.Sprintf("%s %s", volunteer.FirstName, volunteer.LastName)

			// Check if this is a team lead allocation
			if allocation.Role == string(model.RoleTeamLead) {
				row.TeamLead = fullName
			} else {
				row.Volunteers = append(row.Volunteers, fullName)
			}
		}

		// Sort volunteers alphabetically for consistency
		sort.Strings(row.Volunteers)

		rows = append(rows, row)
	}

	publishedRota := &sheetsclient.PublishedRota{
		StartDate:  targetRota.Start,
		ShiftCount: targetRota.ShiftCount,
		Rows:       rows,
	}

	logger.Info("Published rota built successfully",
		zap.String("rota_id", targetRota.ID),
		zap.Int("shift_count", len(rows)))

	// Step 7: Publish to Google Sheets
	logger.Debug("Publishing to Google Sheets", zap.String("spreadsheet_id", cfg.RotaSheetID))
	err = sheetsClient.PublishRota(cfg.RotaSheetID, publishedRota)
	if err != nil {
		return nil, fmt.Errorf("failed to publish to Google Sheets: %w", err)
	}

	logger.Info("Rota published successfully to Google Sheets",
		zap.String("rota_id", targetRota.ID))

	return publishedRota, nil
}

// isShiftClosed checks if a shift is marked as closed by any matching RotaOverride
func isShiftClosed(dateStr string, overrides []config.RotaOverride, shiftDates []time.Time, logger *zap.Logger) bool {
	// Determine the date range for RRule generation
	var rotaStart, rotaEnd time.Time
	if len(shiftDates) > 0 {
		rotaStart = shiftDates[0]
		rotaEnd = shiftDates[len(shiftDates)-1]
	}

	for _, override := range overrides {
		// Skip if not marked as closed
		if !override.Closed {
			continue
		}

		// Parse the RRule
		rule, err := rrule.StrToRRule(override.RRule)
		if err != nil {
			logger.Warn("Failed to parse rrule for closed check",
				zap.String("rrule", override.RRule),
				zap.Error(err))
			continue
		}

		// Check if this date matches the RRule
		searchStart := rotaStart.AddDate(0, 0, -7)
		searchEnd := rotaEnd.AddDate(0, 0, 7)
		rule.DTStart(searchStart)
		occurrences := rule.Between(searchStart, searchEnd, true)
		for _, occurrence := range occurrences {
			if occurrence.Format("2006-01-02") == dateStr {
				return true
			}
		}
	}

	return false
}
