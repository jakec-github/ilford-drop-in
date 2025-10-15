package rotageneration

import "sort"

// RankVolunteerGroups calculates and applies ranking scores to volunteer groups
// based on the provided criteria. Groups are sorted in descending order by score
// (higher scores should be allocated first).
//
// The function modifies the VolunteerGroups slice in the RotaState by sorting it in-place.
func RankVolunteerGroups(state *RotaState, criteria []Criterion, targetFrequency float64) {
	// Calculate scores for each group
	groupScores := make(map[*VolunteerGroup]float64)

	for _, group := range state.VolunteerState.VolunteerGroups {
		score := calculateGroupRankingScore(state, group, criteria, targetFrequency)
		groupScores[group] = score
	}

	// Sort groups by score (descending - highest score first)
	sort.Slice(state.VolunteerState.VolunteerGroups, func(i, j int) bool {
		scoreI := groupScores[state.VolunteerState.VolunteerGroups[i]]
		scoreJ := groupScores[state.VolunteerState.VolunteerGroups[j]]
		return scoreI > scoreJ
	})
}

// calculateGroupRankingScore computes the ranking score for a volunteer group
// by running all criterion PromoteVolunteerGroup hooks and summing their weighted results.
//
// The score includes built-in fairness calculations based on target frequency and availability,
// plus all custom criterion promotion values.
//
// Returns a score where higher values indicate the group should be allocated earlier.
func calculateGroupRankingScore(state *RotaState, group *VolunteerGroup, criteria []Criterion, targetFrequency float64) float64 {
	totalScore := 0.0

	remainingAvailability := len(group.AvailableShiftIndices) - len(group.AllocatedShiftIndices)

	// Built-in 1: Current rota urgency
	// Prioritize groups based on how much of this rota's allocation budget they need
	if remainingAvailability > 0 {
		// Calculate how many allocations this group should get in this rota based on target frequency
		targetAllocationsThisRota := int(float64(len(state.Shifts)) * targetFrequency)

		// How many more do they need in this rota?
		remainingNeededThisRota := targetAllocationsThisRota - len(group.AllocatedShiftIndices)

		// Calculate ratio: what they need vs what they have available
		ratio := float64(remainingNeededThisRota) / float64(remainingAvailability)

		// Take max of ratio or 1.0
		urgencyScore := ratio
		if urgencyScore < 1.0 {
			urgencyScore = 1.0
		}

		totalScore += urgencyScore * WeightCurrentRotaUrgency
	}

	// Built-in 2: Overall frequency fairness
	// Prioritize groups that are behind on their target frequency over time
	if len(state.Shifts) > 0 {
		desiredRemaining := group.DesiredRemainingAllocations(
			len(state.HistoricalShifts),
			len(state.Shifts),
			targetFrequency,
		)

		// Normalize by the number of shifts in this rota
		fairnessScore := float64(desiredRemaining) / float64(len(state.Shifts))

		// Clamp to range: minimum 1.0 (groups that need allocations), maximum unbounded negative (over-allocated groups)
		// This ensures groups behind on target get prioritized, while groups ahead are deprioritized proportionally
		if fairnessScore > 1.0 {
			fairnessScore = 1.0
		}
		if fairnessScore < -1.0 {
			fairnessScore = -1.0
		}

		totalScore += fairnessScore * WeightOverallFrequencyFairness
	}

	// Built-in 3: Promote groups over individuals
	// Schedule groups early to make sure there is space
	if len(group.Members) > 1 {
		totalScore += 1 * WeightPromoteGroup
	}

	// Custom criteria promotion values
	for _, criterion := range criteria {
		// Get the promotion value from the criterion (-1.0 to 1.0)
		promotionValue := criterion.PromoteVolunteerGroup(state, group)

		// Multiply by the criterion's group weight and add to total
		weightedValue := promotionValue * criterion.GroupWeight()
		totalScore += weightedValue
	}

	return totalScore
}
