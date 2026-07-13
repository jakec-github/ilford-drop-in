package utils

import (
	"fmt"
	"sort"
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

// FilterSentRequests filters availability requests to only those that were sent
func FilterSentRequests(requests []db.AvailabilityRequest) []db.AvailabilityRequest {
	filtered := []db.AvailabilityRequest{}
	for _, req := range requests {
		if req.FormSent {
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

// ShiftDatesFromShifts extracts the dates of a rota's shifts, sorted ascending.
// This replaces CalculateShiftDates for consumers that now read a rota's shifts
// from the database rather than recomputing them by arithmetic (ADR 0001).
func ShiftDatesFromShifts(shifts []db.Shift) ([]time.Time, error) {
	dates := make([]time.Time, len(shifts))
	for i, s := range shifts {
		date, err := time.Parse("2006-01-02", s.Date)
		if err != nil {
			return nil, fmt.Errorf("invalid shift date %q: %w", s.Date, err)
		}
		dates[i] = date
	}
	sort.Slice(dates, func(i, j int) bool { return dates[i].Before(dates[j]) })
	return dates, nil
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
