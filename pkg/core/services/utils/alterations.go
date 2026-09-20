package utils

import (
	"sort"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// ApplyAlterations takes allocations grouped by shift id and a list of
// alterations, and returns the modified allocation map. Alterations are applied
// in set_time order. This function is pure (no DB calls) and used by both
// changeRota (validation) and publishRota (output).
//
// An "add" with no Role of its own stays without one. Alterations only gained
// a role column in 004_alteration_role, so historical rows have none; there
// used to be a default here — the uncapped Role — and with that gone (issue
// #185) the honest answer is the one the row actually carries. Every reader
// already handles an allocation whose Role it does not recognise: the
// published sheet gives it a column of its own, and the rota page draws it
// unlabelled.
func ApplyAlterations(
	allocationsByShiftID map[string][]db.Allocation,
	alterations []db.Alteration,
) map[string][]db.Allocation {
	// Sort alterations by set_time to ensure deterministic ordering, with a
	// remove ahead of an add that shares its instant. One cover's rows all
	// carry the transaction's NOW(), so a change that both takes someone off a
	// shift and puts them back on it — switching their Role (issue #147) —
	// gives the two rows the same set_time and nothing but this tie-break says
	// which came first. Applied the other way round the person is added and
	// then removed, and disappears from the shift altogether.
	//
	// Stable, so two rows the comparison cannot separate keep the order the
	// query gave them rather than an arbitrary one.
	sorted := make([]db.Alteration, len(alterations))
	copy(sorted, alterations)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].SetTime != sorted[j].SetTime {
			return sorted[i].SetTime < sorted[j].SetTime
		}
		return sorted[i].Direction == "remove" && sorted[j].Direction != "remove"
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
			newAlloc := db.Allocation{
				ShiftID: shiftID,
				Role:    alt.Role,
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
