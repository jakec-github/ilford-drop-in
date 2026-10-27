package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// twoShiftSundays generates a morning and an evening Shift every Sunday.
const twoShiftSundays = "RRULE:FREQ=WEEKLY;BYDAY=SU;BYHOUR=10;BYMINUTE=0\nRRULE:FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=30"

// firstSundayOverride matches every Shift on the first Sunday of a month,
// counted from the rota defaults' own Start from (itself a first Sunday).
func firstSundayOverride(pins ...db.OverridePin) db.Override {
	return db.Override{
		ID: "o-first-sunday", RRule: "FREQ=MONTHLY;BYDAY=1SU", StartFrom: "2026-08-02",
		Granularity: db.OverrideMatchesDate, Pins: pins,
	}
}

func touchedNumbers(shift OverriddenShift) []int {
	numbers := []int{}
	for _, touch := range shift.Touches {
		numbers = append(numbers, touch.Number)
	}
	return numbers
}

// A first-Sunday Override touches the first Sunday of each month and nothing
// else: 2 August and 6 September in a six-week rota.
func TestPlanRota_FirstSundayOverride(t *testing.T) {
	mock := definable()
	mock.overrides = []db.Override{firstSundayOverride(
		db.OverridePin{ID: "op-1", RoleID: "role-team-lead", VolunteerID: "v1"},
	)}

	plan, err := PlanRota(context.Background(), mock, statedRota(6))
	require.NoError(t, err)
	require.NoError(t, plan.Refusal)
	require.Len(t, plan.Overridden, 6)

	var pinned []string
	for i, shift := range plan.Overridden {
		if len(shift.Pins) > 0 {
			pinned = append(pinned, plan.Shifts[i].Date)
		}
	}
	assert.Equal(t, []string{"2026-08-02", "2026-09-06"}, pinned)
	assert.Equal(t, []OverrideReach{{
		Number: 1, Description: plan.Overrides[0].Description, Granularity: db.OverrideMatchesDate, Matched: 2,
	}}, plan.Overrides)
	assert.NotEmpty(t, plan.Overrides[0].Description)
}

// On a day with two Shifts, an Override naming a time touches only the Shift
// generated at that time, and one naming none touches both.
func TestPlanRota_ExactTimeAgainstDate(t *testing.T) {
	mock := withPattern(definable(), twoShiftSundays)
	mock.overrides = []db.Override{
		{ID: "evening", RRule: "FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=30", StartFrom: "2026-08-02", Granularity: db.OverrideMatchesTime},
		{ID: "whole-day", RRule: "FREQ=MONTHLY;BYDAY=1SU", StartFrom: "2026-08-02", Granularity: db.OverrideMatchesDate},
	}

	plan, err := PlanRota(context.Background(), mock, statedRota(4))
	require.NoError(t, err)
	require.Len(t, plan.Shifts, 4)

	assert.Equal(t, "2026-08-02T10:00:00", plan.Shifts[0].StartAt)
	assert.Equal(t, []int{2}, touchedNumbers(plan.Overridden[0]), "the morning is touched by the date Override only")
	assert.Equal(t, "2026-08-02T19:30:00", plan.Shifts[1].StartAt)
	assert.Equal(t, []int{1, 2}, touchedNumbers(plan.Overridden[1]), "the evening by both, in list order")
	assert.Equal(t, []int{}, touchedNumbers(plan.Overridden[2]))
	assert.Equal(t, []int{1}, touchedNumbers(plan.Overridden[3]))

	assert.Equal(t, 2, plan.Overrides[0].Matched)
	assert.Equal(t, 2, plan.Overrides[1].Matched)
}

// The later Override wins: its Shape replaces an earlier one, and its Pin for a
// person replaces theirs. Reordering the same two changes the result.
func TestPlanRota_LaterOverrideWins(t *testing.T) {
	small := db.Override{
		ID: "small", RRule: "FREQ=WEEKLY;BYDAY=SU", StartFrom: "2026-08-02", Granularity: db.OverrideMatchesDate,
		SetsShape: true, Seats: []db.OverrideSeat{{RoleID: "role-service-volunteer", Seats: 2}},
		Pins: []db.OverridePin{
			{ID: "p-small-alice", RoleID: "role-service-volunteer", VolunteerID: "v1"},
			{ID: "p-small-team", RoleID: "role-service-volunteer", CustomValue: "Youth group"},
		},
	}
	large := db.Override{
		ID: "large", RRule: "FREQ=WEEKLY;BYDAY=SU", StartFrom: "2026-08-02", Granularity: db.OverrideMatchesDate,
		SetsShape: true, Seats: []db.OverrideSeat{{RoleID: "role-team-lead", Seats: 1}, {RoleID: "role-service-volunteer", Seats: 6}},
		Pins: []db.OverridePin{
			{ID: "p-large-alice", RoleID: "role-team-lead", VolunteerID: "v1"},
			{ID: "p-large-team", RoleID: "role-service-volunteer", CustomValue: "Youth group"},
		},
	}

	mock := definable()
	mock.overrides = []db.Override{small, large}
	plan, err := PlanRota(context.Background(), mock, statedRota(1))
	require.NoError(t, err)
	shift := plan.Overridden[0]
	assert.True(t, shift.SetsShape)
	assert.Equal(t, large.Seats, shift.Seats)
	assert.Equal(t, []db.OverridePin{small.Pins[1], large.Pins[0], large.Pins[1]}, shift.Pins,
		"Alice keeps only the later Pin; custom entries add up")

	mock.overrides = []db.Override{large, small}
	plan, err = PlanRota(context.Background(), mock, statedRota(1))
	require.NoError(t, err)
	shift = plan.Overridden[0]
	assert.Equal(t, small.Seats, shift.Seats)
	assert.Equal(t, []db.OverridePin{large.Pins[1], small.Pins[0], small.Pins[1]}, shift.Pins)
}

// An exact-time Override that matches nothing, though the rota has Shifts on
// its dates, is aimed at the right days at a time the pattern no longer
// generates. One that misses on date too is simply out of reach.
func TestPlanRota_ReportsAnOverrideThatMatchesNothing(t *testing.T) {
	mock := definable()
	mock.overrides = []db.Override{
		{ID: "old-time", RRule: "FREQ=WEEKLY;BYDAY=SU;BYHOUR=18;BYMINUTE=0", StartFrom: "2026-08-02", Granularity: db.OverrideMatchesTime},
		{ID: "saturdays", RRule: "FREQ=WEEKLY;BYDAY=SA", StartFrom: "2026-08-02", Granularity: db.OverrideMatchesDate},
	}

	plan, err := PlanRota(context.Background(), mock, statedRota(4))
	require.NoError(t, err)
	require.Len(t, plan.Overrides, 2)
	assert.Equal(t, 0, plan.Overrides[0].Matched)
	assert.True(t, plan.Overrides[0].MissedByTime)
	assert.Equal(t, 0, plan.Overrides[1].Matched)
	assert.False(t, plan.Overrides[1].MissedByTime)
}

// Nothing before an Override's own Start from is touched.
func TestPlanRota_OverrideCountsFromItsStartFrom(t *testing.T) {
	mock := definable()
	mock.overrides = []db.Override{
		{ID: "later", RRule: "FREQ=WEEKLY;BYDAY=SU", StartFrom: "2026-08-16", Granularity: db.OverrideMatchesDate},
	}

	plan, err := PlanRota(context.Background(), mock, statedRota(4))
	require.NoError(t, err)
	assert.Equal(t, []int{}, touchedNumbers(plan.Overridden[0]))
	assert.Equal(t, []int{}, touchedNumbers(plan.Overridden[1]))
	assert.Equal(t, []int{1}, touchedNumbers(plan.Overridden[2]))
	assert.Equal(t, 2, plan.Overrides[0].Matched)
}

// A stored rule this build cannot read refuses the rota rather than minting
// one quietly missing what it promised, and says which Override to correct.
func TestPlanRota_RefusesAnUnreadableOverride(t *testing.T) {
	mock := definable()
	mock.overrides = []db.Override{
		firstSundayOverride(),
		{ID: "broken", RRule: "FREQ=SOMETIMES", StartFrom: "2026-08-02", Granularity: db.OverrideMatchesDate},
	}

	plan, err := PlanRota(context.Background(), mock, statedRota(4))
	require.NoError(t, err)
	require.ErrorIs(t, plan.Refusal, ErrInvalidInput)
	assert.Contains(t, plan.Refusal.Error(), "override 2 cannot be used")

	_, err = DefineRota(context.Background(), mock, definableRoster(), nil, zap.NewNop(), statedRota(4))
	require.ErrorIs(t, err, ErrInvalidInput)
	assert.Empty(t, mock.insertedRotas)
}
