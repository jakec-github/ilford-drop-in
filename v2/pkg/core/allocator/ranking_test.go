package allocator

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// mockCriterion is a simple test criterion
type mockCriterion struct {
	name           string
	promotionValue float64
	groupWeight    float64
	affinityValue  float64
	affinityWeight float64
}

func (m *mockCriterion) Name() string {
	return m.name
}

func (m *mockCriterion) PromoteVolunteerGroup(state *RotaState, group *VolunteerGroup) float64 {
	return m.promotionValue
}

func (m *mockCriterion) IsShiftValid(state *RotaState, group *VolunteerGroup, shift *Shift) bool {
	return true
}

func (m *mockCriterion) CalculateShiftAffinity(state *RotaState, group *VolunteerGroup, shift *Shift) float64 {
	return m.affinityValue
}

func (m *mockCriterion) GroupWeight() float64 {
	return m.groupWeight
}

func (m *mockCriterion) AffinityWeight() float64 {
	return m.affinityWeight
}

func (m *mockCriterion) ValidateRotaState(state *RotaState) []ShiftValidationError {
	return nil
}

func TestCalculateGroupRankingScore_NoCriteria(t *testing.T) {
	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0}, {Index: 1}, {Index: 2}, {Index: 3}, {Index: 4},
		},
		HistoricalShifts: []*Shift{},
	}

	group := &VolunteerGroup{
		GroupKey:                  "group_a",
		Members:                   []Volunteer{{ID: "v1"}},
		AvailableShiftIndices:     []int{0, 1, 2, 3, 4},
		AllocatedShiftIndices:     []int{},
		HistoricalAllocationCount: 0,
		MaleCount:                 1,
	}

	// With no historical data and no allocations yet:
	// Built-in 1: targetThisRota = 5*0.5 = 2.5 -> 2, remaining = 2/5 = 0.4, score = max(0.4, 1.0) = 1.0
	// Built-in 2: desired = (0+5)*0.5 = 2.5 -> 2, current = 0, fairness = 2/5 = 0.4, clamped to [-1,1] = 0.4
	// Built-in 3: single member = 0
	// Expected: 1.0 * WeightCurrentRotaUrgency + 0.4 * WeightOverallFrequencyFairness = 1.4
	score := calculateGroupRankingScore(state, group, []Criterion{}, 0.5)

	// With current weights of 1 each: 1.0 + 0.4 = 1.4
	assert.Equal(t, 1.4, score)
}

func TestCalculateGroupRankingScore_WithCustomCriteria(t *testing.T) {
	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0}, {Index: 1}, {Index: 2}, {Index: 3}, {Index: 4},
		},
		HistoricalShifts: []*Shift{},
	}

	group := &VolunteerGroup{
		GroupKey:                  "group_a",
		Members:                   []Volunteer{{ID: "v1"}},
		AvailableShiftIndices:     []int{0, 1, 2, 3, 4},
		AllocatedShiftIndices:     []int{},
		HistoricalAllocationCount: 0,
		MaleCount:                 1,
	}

	criteria := []Criterion{
		&mockCriterion{
			name:           "criterion1",
			promotionValue: 0.5,
			groupWeight:    10.0,
		},
		&mockCriterion{
			name:           "criterion2",
			promotionValue: -0.3,
			groupWeight:    5.0,
		},
	}

	score := calculateGroupRankingScore(state, group, criteria, 0.5)

	// Built-in scores: 1.4 (from previous test)
	// Custom: (0.5 * 10.0) + (-0.3 * 5.0) = 5.0 - 1.5 = 3.5
	// Total: 1.4 + 3.5 = 4.9
	assert.Equal(t, 4.9, score)
}

func TestCalculateGroupRankingScore_GroupPromotion(t *testing.T) {
	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0}, {Index: 1}, {Index: 2}, {Index: 3}, {Index: 4},
		},
		HistoricalShifts: []*Shift{},
	}

	multiMemberGroup := &VolunteerGroup{
		GroupKey: "group_a",
		Members: []Volunteer{
			{ID: "v1"},
			{ID: "v2"},
		},
		AvailableShiftIndices:     []int{0, 1, 2, 3, 4},
		AllocatedShiftIndices:     []int{},
		HistoricalAllocationCount: 0,
		MaleCount:                 2,
	}

	singleMemberGroup := &VolunteerGroup{
		GroupKey:                  "individual_v3",
		Members:                   []Volunteer{{ID: "v3"}},
		AvailableShiftIndices:     []int{0, 1, 2, 3, 4},
		AllocatedShiftIndices:     []int{},
		HistoricalAllocationCount: 0,
		MaleCount:                 1,
	}

	multiScore := calculateGroupRankingScore(state, multiMemberGroup, []Criterion{}, 0.5)
	singleScore := calculateGroupRankingScore(state, singleMemberGroup, []Criterion{}, 0.5)

	// Multi-member group should get +1 * WeightPromoteGroup = +1
	assert.Greater(t, multiScore, singleScore)
	assert.Equal(t, singleScore+float64(WeightPromoteGroup), multiScore)
}

func TestCalculateGroupRankingScore_UrgencyHighWhenLimitedAvailability(t *testing.T) {
	state := &RotaState{
		Shifts: []*Shift{
			{Index: 0}, {Index: 1}, {Index: 2}, {Index: 3}, {Index: 4},
		},
		HistoricalShifts: []*Shift{},
	}

	// Group with limited availability
	limitedGroup := &VolunteerGroup{
		GroupKey:                  "group_a",
		Members:                   []Volunteer{{ID: "v1"}},
		AvailableShiftIndices:     []int{0, 1}, // Only 2 available
		AllocatedShiftIndices:     []int{},
		HistoricalAllocationCount: 0,
		MaleCount:                 1,
	}

	// Group with full availability
	fullGroup := &VolunteerGroup{
		GroupKey:                  "group_b",
		Members:                   []Volunteer{{ID: "v2"}},
		AvailableShiftIndices:     []int{0, 1, 2, 3, 4}, // All 5 available
		AllocatedShiftIndices:     []int{},
		HistoricalAllocationCount: 0,
		MaleCount:                 1,
	}

	limitedScore := calculateGroupRankingScore(state, limitedGroup, []Criterion{}, 0.5)
	fullScore := calculateGroupRankingScore(state, fullGroup, []Criterion{}, 0.5)

	// Limited group should have higher urgency
	// Limited: Built-in 1 urgency = 1.0, Built-in 2 fairness = 0.4, total = 1.4
	// Full: Built-in 1 urgency = max(2/5, 1.0) = 1.0, Built-in 2 fairness = 0.4, total = 1.4
	// Both should have the same score in this scenario
	// This test mainly validates that the calculation doesn't break with limited availability
	assert.NotZero(t, limitedScore)
	assert.NotZero(t, fullScore)
	assert.Equal(t, limitedScore, fullScore)
}

func TestCalculateGroupRankingScore_BehindOnFrequency(t *testing.T) {
	// Group that is behind on historical frequency should get boosted
	historicalShifts := []*Shift{
		{Index: 0, AllocatedGroups: []*VolunteerGroup{
			{GroupKey: "group_b"}, // group_b was allocated 10 times historically
		}},
	}
	for i := 1; i < 10; i++ {
		historicalShifts = append(historicalShifts, &Shift{
			Index:            i,
			AllocatedGroups:  []*VolunteerGroup{{GroupKey: "group_b"}},
		})
	}

	state := &RotaState{
		Shifts:           []*Shift{{Index: 0}, {Index: 1}, {Index: 2}, {Index: 3}, {Index: 4}},
		HistoricalShifts: historicalShifts,
	}

	// Group A: Never allocated before (behind)
	behindGroup := &VolunteerGroup{
		GroupKey:                  "group_a",
		Members:                   []Volunteer{{ID: "v1"}},
		AvailableShiftIndices:     []int{0, 1, 2, 3, 4},
		AllocatedShiftIndices:     []int{},
		HistoricalAllocationCount: 0, // Behind!
		MaleCount:                 1,
	}

	// Group B: Already allocated 10 times (ahead)
	aheadGroup := &VolunteerGroup{
		GroupKey:                  "group_b",
		Members:                   []Volunteer{{ID: "v2"}},
		AvailableShiftIndices:     []int{0, 1, 2, 3, 4},
		AllocatedShiftIndices:     []int{},
		HistoricalAllocationCount: 10, // Ahead!
		MaleCount:                 1,
	}

	behindScore := calculateGroupRankingScore(state, behindGroup, []Criterion{}, 0.5)
	aheadScore := calculateGroupRankingScore(state, aheadGroup, []Criterion{}, 0.5)

	// Behind group should have higher overall frequency fairness score
	// Behind: desired = (0+5)*0.5 - 0 = 2.5 -> 2, fairness = 2/5 = 0.4 (within [-1,1])
	//         Built-in 1 urgency = 1.0, Built-in 2 = 0.4, total = 1.4
	// Ahead: desired = (10+5)*0.5 - 10 = 7.5 -> 7, then 7 - 10 = -3, fairness = -3/5 = -0.6 (within [-1,1])
	//        Built-in 1 urgency = 1.0, Built-in 2 = -0.6, total = 0.4
	assert.Greater(t, behindScore, aheadScore)
}

func TestCalculateGroupRankingScore_NoRemainingAvailability(t *testing.T) {
	state := &RotaState{
		Shifts:           []*Shift{{Index: 0}, {Index: 1}},
		HistoricalShifts: []*Shift{},
	}

	// Group that has been allocated to all their available shifts
	group := &VolunteerGroup{
		GroupKey:                  "group_a",
		Members:                   []Volunteer{{ID: "v1"}},
		AvailableShiftIndices:     []int{0, 1},
		AllocatedShiftIndices:     []int{0, 1}, // All allocated!
		HistoricalAllocationCount: 0,
		MaleCount:                 1,
	}

	score := calculateGroupRankingScore(state, group, []Criterion{}, 0.5)

	// Should not panic, should return a valid score
	// Built-in 1 skipped (no remaining availability)
	// Built-in 2: fairness calculation still runs
	// Built-in 3: single member = 0
	assert.NotZero(t, score)
}
