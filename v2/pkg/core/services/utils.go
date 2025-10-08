package services

import (
	"fmt"
	"strings"
	"time"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// findLatestRotation finds the rotation with the most recent start date
func findLatestRotation(rotations []db.Rotation) *db.Rotation {
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

// filterRequestsByRotaID filters availability requests to only those for the specified rota
func filterRequestsByRotaID(requests []db.AvailabilityRequest, rotaID string) []db.AvailabilityRequest {
	filtered := make([]db.AvailabilityRequest, 0)
	for _, req := range requests {
		if req.RotaID == rotaID {
			filtered = append(filtered, req)
		}
	}
	return filtered
}

// filterActiveVolunteers filters volunteers to only those with "Active" status (case-insensitive)
func filterActiveVolunteers(volunteers []model.Volunteer) []model.Volunteer {
	active := make([]model.Volunteer, 0)
	for _, vol := range volunteers {
		if strings.EqualFold(vol.Status, "Active") {
			active = append(active, vol)
		}
	}
	return active
}

// getVolunteerIDs extracts volunteer IDs from a list of volunteers (useful for logging)
func getVolunteerIDs(volunteers []model.Volunteer) []string {
	ids := make([]string, len(volunteers))
	for i, vol := range volunteers {
		ids[i] = vol.ID
	}
	return ids
}

// calculateShiftDates calculates all shift dates for a rota, starting from the given date
// Shifts occur weekly (every 7 days) for the specified shift count
func calculateShiftDates(startDateStr string, shiftCount int) ([]time.Time, error) {
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
