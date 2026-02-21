package utils

import (
	"fmt"
	"strings"
	"time"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// FindLatestRotation finds the rotation with the most recent start date
func FindLatestRotation(rotations []db.Rotation) *db.Rotation {
	if len(rotations) == 0 {
		return nil
	}

	latest := &rotations[0]
	latestDate, err := time.Parse("2006-01-02", latest.Start)
	if err != nil {
		return latest
	}

	for i := 1; i < len(rotations); i++ {
		currentDate, err := time.Parse("2006-01-02", rotations[i].Start)
		if err != nil {
			continue
		}

		if currentDate.After(latestDate) {
			latest = &rotations[i]
			latestDate = currentDate
		}
	}

	return latest
}

// FilterRequestsByRotaID filters availability requests to only those for the specified rota
func FilterRequestsByRotaID(requests []db.AvailabilityRequest, rotaID string) []db.AvailabilityRequest {
	filtered := make([]db.AvailabilityRequest, 0)
	for _, req := range requests {
		if req.RotaID == rotaID {
			filtered = append(filtered, req)
		}
	}
	return filtered
}

// FilterSentRequestsByRotaID filters availability requests to only those for a specific rota that were sent
func FilterSentRequestsByRotaID(requests []db.AvailabilityRequest, rotaID string) []db.AvailabilityRequest {
	filtered := []db.AvailabilityRequest{}
	for _, req := range requests {
		if req.RotaID == rotaID && req.FormSent {
			filtered = append(filtered, req)
		}
	}
	return filtered
}

// FilterActiveVolunteers filters volunteers to only those with "Active" status (case-insensitive)
func FilterActiveVolunteers(volunteers []model.Volunteer) []model.Volunteer {
	active := make([]model.Volunteer, 0)
	for _, vol := range volunteers {
		if strings.EqualFold(vol.Status, "Active") {
			active = append(active, vol)
		}
	}
	return active
}

// GetVolunteerIDs extracts volunteer IDs from a list of volunteers (useful for logging)
func GetVolunteerIDs(volunteers []model.Volunteer) []string {
	ids := make([]string, len(volunteers))
	for i, vol := range volunteers {
		ids[i] = vol.ID
	}
	return ids
}

// CalculateShiftDates calculates all shift dates for a rota, starting from the given date
// Shifts occur weekly (every 7 days) for the specified shift count
func CalculateShiftDates(startDateStr string, shiftCount int) ([]time.Time, error) {
	startDate, err := time.Parse("2006-01-02", startDateStr)
	if err != nil {
		return nil, fmt.Errorf("invalid start date format: %w", err)
	}

	dates := make([]time.Time, shiftCount)
	for i := 0; i < shiftCount; i++ {
		dates[i] = startDate.AddDate(0, 0, i*7) // Add i weeks
	}

	return dates, nil
}

// FilterAllocationsByRotaID filters allocations to only those for the specified rota
func FilterAllocationsByRotaID(allocations []db.Allocation, rotaID string) []db.Allocation {
	filtered := make([]db.Allocation, 0)
	for _, allocation := range allocations {
		if allocation.RotaID == rotaID {
			filtered = append(filtered, allocation)
		}
	}
	return filtered
}
