package services

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/core/services/utils"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// RotaResult represents the result of defining a new rota
type RotaResult struct {
	Rotation   *db.Rotation
	ShiftDates []time.Time
}

// DefineRotaStore defines the database operations needed for defining a rota
type DefineRotaStore interface {
	GetRotations(ctx context.Context) ([]db.Rotation, error)
	InsertRotationAndShifts(ctx context.Context, rotation *db.Rotation, shifts []db.Shift) error
}

// DefineRota creates a new rota with the specified number of shifts
// It finds the latest existing rota, calculates the start date for the new rota,
// creates the rotation record, and calculates all shift dates
func DefineRota(ctx context.Context, database DefineRotaStore, logger *zap.Logger, shiftCount int) (*RotaResult, error) {
	if shiftCount <= 0 {
		return nil, fmt.Errorf("shift count must be positive, got %d", shiftCount)
	}

	logger.Debug("Defining new rota", zap.Int("shift_count", shiftCount))

	// Fetch all existing rotations
	logger.Debug("Fetching existing rotations")
	rotations, err := database.GetRotations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch rotations: %w", err)
	}

	logger.Debug("Found existing rotations", zap.Int("count", len(rotations)))

	// Find latest rota and calculate next start date
	var startDate time.Time
	if len(rotations) == 0 {
		// No existing rotas, start next Sunday
		startDate = nextSunday(time.Now())
		logger.Info("No existing rotations found, starting from next Sunday", zap.Time("start_date", startDate))
	} else {
		// Find the latest rotation
		latestRota := utils.FindLatestRotation(rotations)
		logger.Debug("Latest rotation found",
			zap.String("id", latestRota.ID),
			zap.String("start", latestRota.Start),
			zap.Int("shift_count", latestRota.ShiftCount))

		// Parse the latest rota's end date (its last shift)
		latestEnd, err := time.Parse("2006-01-02", latestRota.End)
		if err != nil {
			return nil, fmt.Errorf("failed to parse latest rota end date: %w", err)
		}

		// New rota starts the Sunday after the latest rota's last shift
		startDate = nextSunday(latestEnd)
		logger.Debug("Calculated start date from latest rotation",
			zap.Time("latest_end", latestEnd),
			zap.Time("new_start", startDate))
	}

	// Create new rotation record
	rotation := &db.Rotation{
		ID:         uuid.New().String(),
		Start:      startDate.Format("2006-01-02"),
		ShiftCount: shiftCount,
	}

	logger.Debug("Creating new rotation", zap.String("id", rotation.ID), zap.String("start", rotation.Start))

	// Mint this rotation's shifts (weekly shifts starting from start date).
	// Rota definition is the sole place shift-date arithmetic lives.
	shiftDates := make([]time.Time, shiftCount)
	shifts := make([]db.Shift, shiftCount)
	for i := 0; i < shiftCount; i++ {
		shiftDate := startDate.AddDate(0, 0, 7*i)
		shiftDates[i] = shiftDate
		shifts[i] = db.Shift{
			ID:     uuid.New().String(),
			RotaID: rotation.ID,
			Date:   shiftDate.Format("2006-01-02"),
		}
	}

	// Insert the rotation and all of its shifts atomically, so a rota can
	// never exist half-formed.
	if err := database.InsertRotationAndShifts(ctx, rotation, shifts); err != nil {
		return nil, fmt.Errorf("failed to insert rotation and shifts: %w", err)
	}

	logger.Debug("Rotation created successfully",
		zap.String("rotation_id", rotation.ID),
		zap.Int("shift_count", shiftCount),
		zap.String("first_shift", shiftDates[0].Format("2006-01-02")),
		zap.String("last_shift", shiftDates[len(shiftDates)-1].Format("2006-01-02")))

	return &RotaResult{
		Rotation:   rotation,
		ShiftDates: shiftDates,
	}, nil
}

// nextSunday returns the next Sunday from the given date
func nextSunday(from time.Time) time.Time {
	// Normalize to start of day to avoid time-of-day issues
	normalized := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)

	// Get days until next Sunday
	// Sunday is 0, so we need (7 - weekday) days, but if already Sunday, add 7
	daysUntilSunday := (7 - int(normalized.Weekday())) % 7
	if daysUntilSunday == 0 {
		// If today is Sunday, use next Sunday
		daysUntilSunday = 7
	}

	return normalized.AddDate(0, 0, daysUntilSunday)
}
