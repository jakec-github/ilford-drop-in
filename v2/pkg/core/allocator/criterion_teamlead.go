package rotageneration

import "fmt"

// TeamLeadCriterion prevents overallocation of team leads and optimizes for unpopular shifts.
//
// Validity:
//   - Returns false if the shift already has a team lead and this group contains a team lead
//   - This ensures each shift gets at most one team lead
//
// Affinity:
//   - Only applies to groups that contain a team lead
//   - Increases affinity for shifts with fewer available team leads (unpopular for team leads)
//   - Returns 0 if the group doesn't contain a team lead
//
// Promotion:
//   - Promotes groups with team leads to ensure they get allocated early
type TeamLeadCriterion struct {
	groupWeight    float64
	affinityWeight float64
}

// NewTeamLeadCriterion creates a new TeamLeadCriterion with the given weights
func NewTeamLeadCriterion(groupWeight, affinityWeight float64) *TeamLeadCriterion {
	return &TeamLeadCriterion{
		groupWeight:    groupWeight,
		affinityWeight: affinityWeight,
	}
}

func (c *TeamLeadCriterion) Name() string {
	return "TeamLead"
}

func (c *TeamLeadCriterion) PromoteVolunteerGroup(state *RotaState, group *VolunteerGroup) float64 {
	// Promote groups with team leads
	if group.HasTeamLead {
		return 1.0
	}
	return 0
}

func (c *TeamLeadCriterion) IsShiftValid(state *RotaState, group *VolunteerGroup, shift *Shift) bool {
	// If this group has a team lead and the shift already has one, invalid
	if group.HasTeamLead && shift.TeamLead != nil {
		return false
	}
	return true
}

func (c *TeamLeadCriterion) CalculateShiftAffinity(state *RotaState, group *VolunteerGroup, shift *Shift) float64 {
	// Only calculate affinity for groups with team leads
	if !group.HasTeamLead {
		return 0
	}

	// If shift already has a team lead, this shouldn't happen (would be invalid)
	// but return 0 as a safety check
	if shift.TeamLead != nil {
		return 0
	}

	// Count how many team leads are still available for this shift
	remainingTeamLeads := shift.RemainingAvailableTeamLeads(state)

	// Avoid division by zero
	if remainingTeamLeads == 0 {
		return 0
	}

	// Calculate affinity: always 1 (need) / number of available team leads
	// Higher affinity when fewer team leads are available (unpopular shift for team leads)
	// Examples:
	//   - Shift has 10 available team leads → 1/10 = 0.1 (low priority)
	//   - Shift has 5 available team leads → 1/5 = 0.2 (moderate)
	//   - Shift has 1 available team lead → 1/1 = 1.0 (urgent!)
	return 1.0 / float64(remainingTeamLeads)
}

func (c *TeamLeadCriterion) GroupWeight() float64 {
	return c.groupWeight
}

func (c *TeamLeadCriterion) AffinityWeight() float64 {
	return c.affinityWeight
}

func (c *TeamLeadCriterion) ValidateRotaState(state *RotaState) []ShiftValidationError {
	var errors []ShiftValidationError

	for _, shift := range state.Shifts {
		if shift.TeamLead == nil {
			errors = append(errors, ShiftValidationError{
				ShiftIndex:    shift.Index,
				ShiftDate:     shift.Date,
				CriterionName: c.Name(),
				Description:   "Shift has no team lead",
			})
			continue
		}

		// Check that no other volunteers in the shift are team leads
		for _, group := range shift.AllocatedGroups {
			for _, member := range group.Members {
				if member.IsTeamLead && member.ID != shift.TeamLead.ID {
					errors = append(errors, ShiftValidationError{
						ShiftIndex:    shift.Index,
						ShiftDate:     shift.Date,
						CriterionName: c.Name(),
						Description:   fmt.Sprintf("Shift has team lead (%s %s) as ordinary volunteer", member.FirstName, member.LastName),
					})
				}
			}
		}
	}

	return errors
}
