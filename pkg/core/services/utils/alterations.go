package utils

import (
	"sort"

	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// ApplyAlterations takes allocations grouped by shift id and a list of
// alterations, and returns the modified allocation map. Alterations are applied
// in set_time order. This function is pure (no DB calls) and used by both
// changeRota (validation) and publishRota (output).
func ApplyAlterations(
	allocationsByShiftID map[string][]db.Allocation,
	alterations []db.Alteration,
) map[string][]db.Allocation {
	// Sort alterations by set_time to ensure deterministic ordering
	sorted := make([]db.Alteration, len(alterations))
	copy(sorted, alterations)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].SetTime < sorted[j].SetTime
	})

	for _, alt := range sorted {
		shiftID := alt.ShiftID

		switch alt.Direction {
		case "remove":
			allocations := allocationsByShiftID[shiftID]
			filtered := make([]db.Allocation, 0, len(allocations))
			for _, a := range allocations {
				if alt.VolunteerID != "" && a.VolunteerID == alt.VolunteerID {
					continue // Remove this volunteer
				}
				if alt.CustomValue != "" && a.CustomEntry == alt.CustomValue {
					continue // Remove this custom entry
				}
				filtered = append(filtered, a)
			}
			allocationsByShiftID[shiftID] = filtered

		case "add":
			role := alt.Role
			if role == "" {
				role = string(model.RoleVolunteer)
			}
			newAlloc := db.Allocation{
				ShiftID: shiftID,
				Role:    role,
			}
			if alt.VolunteerID != "" {
				newAlloc.VolunteerID = alt.VolunteerID
			}
			if alt.CustomValue != "" {
				newAlloc.CustomEntry = alt.CustomValue
			}
			allocationsByShiftID[shiftID] = append(allocationsByShiftID[shiftID], newAlloc)
		}
	}

	return allocationsByShiftID
}
