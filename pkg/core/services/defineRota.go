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

// RotaResult represents the result of defining a new rota: the rotation, the
// shifts it minted, in date order, and the Preallocations its Standing
// Preallocations seeded. Callers get the shifts themselves rather than bare
// dates, because a shift's id is its identity everywhere downstream (ADR 0001).
type RotaResult struct {
	Rotation *db.Rotation
	Shifts   []db.Shift
	// Preallocations are ordinary Preallocations from the moment they are
	// written: nothing marks them as having been seeded, and an admin may
	// remove any of them (issue #131).
	Preallocations []db.Preallocation
}

// DefineRotaStore defines the database operations needed for defining a rota.
// Defining a rota is where the Rota Defaults are spent, so it reads all of them:
// the shift times, because minting a Shift means deciding when it runs (ADR
// 0007), the default Shape, which each Shift is minted with a copy of (issue
// #137), and the Standing Preallocations, which name a Role by id while the pins
// they seed record its name.
type DefineRotaStore interface {
	RotaDefaultsStore
	DefaultShapeStore
	GetRotations(ctx context.Context) ([]db.Rotation, error)
	GetStandingPreallocations(ctx context.Context) ([]db.StandingPreallocation, error)
	InsertDefinedRota(ctx context.Context, rotation *db.Rotation, shifts []db.Shift, preallocations []db.Preallocation, requirements []db.ShiftRequirement) error
}

// DefineRota creates a new rota with the specified number of shifts
// It finds the latest existing rota, calculates the start date for the new rota,
// creates the rotation record, and calculates all shift dates
func DefineRota(ctx context.Context, database DefineRotaStore, logger *zap.Logger, shiftCount int) (*RotaResult, error) {
	if shiftCount <= 0 {
		return nil, wrapf(ErrInvalidInput, "shift count must be positive, got %d", shiftCount)
	}

	logger.Debug("Defining new rota", zap.Int("shift_count", shiftCount))

	// The times each minted shift will run at come from the settings. Unset
	// settings are not a refusal here: incomplete settings block allocation and
	// nothing else (ADR 0006), so the shifts are minted without times. Such a
	// shift still has its date, from the column the migrate phase keeps for
	// exactly these rows, and renders as a day with no hours until #135.
	defaults, err := RotaDefaults(ctx, database)
	if err != nil {
		return nil, err
	}

	// The Shape each minted Shift will ask for. Unlike the times, an unset one
	// *is* a refusal here: a Shift's times stay editable after it is minted,
	// but its Shape is fixed at define until #138, so a rota minted without one
	// could never be allocated and could never be fixed either. The one moment
	// the Shape is spent is the one moment worth stopping at.
	shape, err := DefaultShape(ctx, database)
	if err != nil {
		return nil, err
	}
	if len(shape) == 0 {
		return nil, wrapf(ErrInvalidInput,
			"the default shape has not been set, so there is nothing for these shifts to ask for; state it on the settings screen before defining a rota")
	}

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
	shifts := make([]db.Shift, shiftCount)
	for i := 0; i < shiftCount; i++ {
		date := startDate.AddDate(0, 0, 7*i).Format("2006-01-02")

		// Both times or neither: a start with no end describes nothing, and
		// the database refuses it.
		var startAt, endAt string
		if defaults.HasShiftTimes() {
			startAt, endAt, err = defaults.ShiftTimestamps(date)
			if err != nil {
				return nil, fmt.Errorf("failed to derive shift times for %s: %w", date, err)
			}
		}

		shifts[i] = db.Shift{
			ID:      uuid.New().String(),
			RotaID:  rotation.ID,
			Date:    date,
			StartAt: startAt,
			EndAt:   endAt,
		}
	}

	// The rotation's span is its shifts' span (ADR 0001): setting End here means
	// the returned rotation reads the same as one fetched back from the store,
	// which derives both ends from the shift rows.
	rotation.End = shifts[len(shifts)-1].Date

	// Copy the default Shape onto every Shift. From here it is this Shift's
	// Shape and nothing else's: editing the setting changes what the *next* rota
	// starts from, and cannot reach back into a rota already defined. That is
	// the whole of issue #137 in three lines, and the reason the copy is made
	// here rather than being resolved from the settings on every read.
	requirements := make([]db.ShiftRequirement, 0, len(shifts)*len(shape))
	for _, s := range shifts {
		for _, seat := range shape {
			requirements = append(requirements, db.ShiftRequirement{
				ShiftID: s.ID,
				RoleID:  seat.Role.ID,
				Seats:   seat.Count,
			})
		}
	}

	// Spend the Rota Defaults: the Standing Preallocations become ordinary
	// Preallocations on the Shifts their rules land on. Defining is the only
	// moment they are read, which is what makes them a convenience rather than a
	// standing fact — editing one later changes what the next rota starts from,
	// never what this one holds.
	roles, err := RoleTable(ctx, database)
	if err != nil {
		return nil, err
	}
	standing, err := database.GetStandingPreallocations(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch standing preallocations: %w", err)
	}
	preallocations, err := seedPreallocations(standing, shifts, roles, logger)
	if err != nil {
		return nil, err
	}

	// Insert the rotation, its shifts, their Shapes and its seeded pins
	// atomically, so a rota can never exist half-formed.
	if err := database.InsertDefinedRota(ctx, rotation, shifts, preallocations, requirements); err != nil {
		return nil, fmt.Errorf("failed to insert rotation and shifts: %w", err)
	}

	logger.Debug("Rotation created successfully",
		zap.String("rotation_id", rotation.ID),
		zap.Int("shift_count", shiftCount),
		zap.Int("preallocation_count", len(preallocations)),
		zap.String("first_shift", shifts[0].Date),
		zap.String("last_shift", shifts[len(shifts)-1].Date))

	return &RotaResult{
		Rotation:       rotation,
		Shifts:         shifts,
		Preallocations: preallocations,
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
