package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// coverageOf indexes a round's shifts by date, which is how these tests name
// them — a bare index says nothing about which date it is asserting on.
func coverageOf(t *testing.T, round *AvailabilityRound, date string) ShiftCoverage {
	t.Helper()
	for _, s := range round.Shifts {
		if s.Date == date {
			return s
		}
	}
	t.Fatalf("no shift for %s in the round", date)
	return ShiftCoverage{}
}

func groupOf(t *testing.T, round *AvailabilityRound, key string) AvailabilityGroup {
	t.Helper()
	for _, g := range round.Groups {
		if g.Key == key {
			return g
		}
	}
	t.Fatalf("no group %s in the round", key)
	return AvailabilityGroup{}
}

// answer submits one volunteer's whole availability, which is what every
// coverage test is built out of.
func answer(t *testing.T, store *mockAvailabilityStore, volunteers *mockVolunteerClient, cfg *config.Config, round *AvailabilityRound, volunteerID string, shiftIDs ...string) {
	t.Helper()
	_, err := SubmitAvailability(context.Background(), store, volunteers, cfg, zap.NewNop(),
		tokenFor(t, round, volunteerID), shiftIDs)
	require.NoError(t, err)
}

func readRound(t *testing.T, store *mockAvailabilityStore, volunteers *mockVolunteerClient, cfg *config.Config) *AvailabilityRound {
	t.Helper()
	round, err := GetAvailabilityRound(context.Background(), store, volunteers, cfg, zap.NewNop(), "")
	require.NoError(t, err)
	return round
}

// TestCoverageCountsOnlyRespondingGroups is the group rule of ADR 0004 seen from
// the shift's side: a group counts towards a date only once somebody in it has
// answered, and a single member saying no takes the whole group out. Silence is
// not a no — it is simply not an answer.
func TestCoverageCountsOnlyRespondingGroups(t *testing.T) {
	store, cfg := availabilityFixture()
	cfg.DefaultShiftSize = 2
	volunteers := availabilityVolunteers()
	round := mintRound(t, store, volunteers, cfg)

	// Nobody has answered: no group is countable yet, so every date is empty.
	fresh := readRound(t, store, volunteers, cfg)
	assert.Equal(t, 0, coverageOf(t, fresh, "2026-08-02").Available)

	// Michael is available for both open dates; Emma, his group partner, only
	// for the first. Her no on the second takes the pair out of it.
	answer(t, store, volunteers, cfg, round, "michael", "shift-1", "shift-2")
	answer(t, store, volunteers, cfg, round, "emma", "shift-1")

	updated := readRound(t, store, volunteers, cfg)
	assert.Equal(t, 2, coverageOf(t, updated, "2026-08-02").Available,
		"both members of a group that said yes are available")
	assert.Equal(t, 0, coverageOf(t, updated, "2026-08-09").Available,
		"one member's no takes the whole group out")
}

// TestCoverageIgnoresASilentPartner: the group rule takes the intersection over
// the members who answered, not over all of them. A partner who has not replied
// is carried by the one who has, exactly as the allocator will carry them.
func TestCoverageIgnoresASilentPartner(t *testing.T) {
	store, cfg := availabilityFixture()
	cfg.DefaultShiftSize = 2
	volunteers := availabilityVolunteers()
	round := mintRound(t, store, volunteers, cfg)

	answer(t, store, volunteers, cfg, round, "emma", "shift-1")

	updated := readRound(t, store, volunteers, cfg)
	assert.Equal(t, 2, coverageOf(t, updated, "2026-08-02").Available,
		"Michael is opted in by Emma's answer")
	assert.Equal(t, 0, coverageOf(t, updated, "2026-08-09").Available)
}

// TestCoverageReportsTeamLeadCover separates the two questions a shift asks: are
// there enough people, and is one of them allowed to lead. A team lead never
// counts towards the shift's size, so an available lead moves the flag and not
// the count.
func TestCoverageReportsTeamLeadCover(t *testing.T) {
	store, cfg := availabilityFixture()
	cfg.DefaultShiftSize = 2
	volunteers := availabilityVolunteers()
	round := mintRound(t, store, volunteers, cfg)

	answer(t, store, volunteers, cfg, round, "michael", "shift-1", "shift-2")
	answer(t, store, volunteers, cfg, round, "aaliyah", "shift-2")

	updated := readRound(t, store, volunteers, cfg)

	first := coverageOf(t, updated, "2026-08-02")
	assert.False(t, first.HasTeamLead, "the only team lead is not available on the first date")
	assert.Equal(t, 2, first.Available)

	second := coverageOf(t, updated, "2026-08-09")
	assert.True(t, second.HasTeamLead)
	assert.Equal(t, 2, second.Available, "a team lead does not fill an ordinary seat")
}

// TestCoverageReportsTheDelta is the number an admin is really after: how far
// short of a full shift the answers so far leave them.
func TestCoverageReportsTheDelta(t *testing.T) {
	store, cfg := availabilityFixture()
	cfg.DefaultShiftSize = 3
	volunteers := availabilityVolunteers()
	round := mintRound(t, store, volunteers, cfg)

	answer(t, store, volunteers, cfg, round, "michael", "shift-1", "shift-2")

	updated := readRound(t, store, volunteers, cfg)
	first := coverageOf(t, updated, "2026-08-02")
	assert.Equal(t, 3, first.Needed)
	assert.Equal(t, 2, first.Available)
	assert.Equal(t, -1, first.Delta, "two people for a shift of three is one short")
}

// TestCoverageTakesShiftSizeFromTheOverrides: a date the config sizes
// differently is a different question, and the last matching override wins —
// which is how InitShifts applies them, so the number an admin reads here is the
// number the allocator will work to.
func TestCoverageTakesShiftSizeFromTheOverrides(t *testing.T) {
	store, cfg := availabilityFixture()
	cfg.DefaultShiftSize = 2
	four, six := 4, 6
	cfg.RotaOverrides = append(cfg.RotaOverrides,
		// 2 August only.
		config.RotaOverride{RRule: "FREQ=YEARLY;BYMONTH=8;BYMONTHDAY=2", ShiftSize: &four},
		config.RotaOverride{RRule: "FREQ=YEARLY;BYMONTH=8;BYMONTHDAY=2", ShiftSize: &six},
	)
	volunteers := availabilityVolunteers()
	mintRound(t, store, volunteers, cfg)

	round := readRound(t, store, volunteers, cfg)
	assert.Equal(t, 6, coverageOf(t, round, "2026-08-02").Needed, "the last matching override wins")
	assert.Equal(t, 2, coverageOf(t, round, "2026-08-09").Needed, "an unmatched date keeps the default")
}

// TestCoverageSubtractsPreallocations: a pinned seat is already filled, from
// both sources (ADR 0003). The seat comes out of what the shift still needs, and
// a pinned volunteer comes out of what is available to fill it — counting them
// on both sides would report the shift as a person better off than it is.
func TestCoverageSubtractsPreallocations(t *testing.T) {
	store, cfg := availabilityFixture()
	cfg.DefaultShiftSize = 4
	cfg.RotaOverrides = append(cfg.RotaOverrides, config.RotaOverride{
		RRule:          "FREQ=YEARLY;BYMONTH=8;BYMONTHDAY=2",
		Preallocations: []config.Preallocation{{Custom: "St John's team", Role: string(model.RoleVolunteer)}},
	})
	volunteers := availabilityVolunteers()
	round := mintRound(t, store, volunteers, cfg)

	// Michael is pinned to the first date by hand.
	store.manualPins = []db.ManualPreallocation{
		{ID: "pin-1", ShiftID: "shift-1", Role: string(model.RoleVolunteer), VolunteerID: "michael"},
	}

	answer(t, store, volunteers, cfg, round, "michael", "shift-1", "shift-2")
	answer(t, store, volunteers, cfg, round, "emma", "shift-1", "shift-2")

	updated := readRound(t, store, volunteers, cfg)

	first := coverageOf(t, updated, "2026-08-02")
	assert.Equal(t, 2, first.Pinned, "the custom entry and Michael each hold a seat")
	assert.Equal(t, 2, first.Needed, "a shift of four with two seats taken needs two more")
	assert.Equal(t, 1, first.Available, "Michael already holds a seat, so only Emma is left to fill one")
	assert.Equal(t, -1, first.Delta)

	second := coverageOf(t, updated, "2026-08-09")
	assert.Equal(t, 0, second.Pinned, "the pins are for the other date")
	assert.Equal(t, 4, second.Needed)
	assert.Equal(t, 2, second.Available)
}

// TestCoverageCountsAPinnedTeamLeadAsCover: pinning a lead is a commitment the
// allocator has to honour, so the date has cover whether or not that lead
// answered. Without this the view would send an admin hunting for a lead it
// already has.
func TestCoverageCountsAPinnedTeamLeadAsCover(t *testing.T) {
	store, cfg := availabilityFixture()
	cfg.DefaultShiftSize = 2
	volunteers := availabilityVolunteers()
	mintRound(t, store, volunteers, cfg)

	store.manualPins = []db.ManualPreallocation{
		{ID: "pin-1", ShiftID: "shift-1", Role: string(model.RoleTeamLead), VolunteerID: "aaliyah"},
	}

	round := readRound(t, store, volunteers, cfg)
	first := coverageOf(t, round, "2026-08-02")
	assert.True(t, first.HasTeamLead)
	assert.Equal(t, 0, first.Pinned, "a team lead never occupies an ordinary seat")
	assert.Equal(t, 2, first.Needed)
}

// TestCoverageShowsAClosedShiftAsClosed: a date the drop-in is not running is
// not a date that is short of volunteers. Reporting it as understaffed would put
// a permanent red mark on a round that is fine.
func TestCoverageShowsAClosedShiftAsClosed(t *testing.T) {
	store, cfg := availabilityFixture()
	cfg.DefaultShiftSize = 3
	volunteers := availabilityVolunteers()
	round := mintRound(t, store, volunteers, cfg)
	answer(t, store, volunteers, cfg, round, "michael", "shift-1")

	updated := readRound(t, store, volunteers, cfg)
	closed := coverageOf(t, updated, "2026-08-16")
	assert.True(t, closed.Closed)
	assert.Equal(t, 0, closed.Needed, "a closed shift needs nobody")
	assert.Equal(t, 0, closed.Available)
	assert.Equal(t, 0, closed.Delta)
	assert.False(t, closed.HasTeamLead)
}

// TestCoverageSkipsVolunteersWhoHaveStopped: a round is minted against the
// active roster, but somebody can stop volunteering while it is open. Their
// answer must stop counting the moment they do, or the view promises people who
// will not be allocated.
func TestCoverageSkipsVolunteersWhoHaveStopped(t *testing.T) {
	store, cfg := availabilityFixture()
	cfg.DefaultShiftSize = 2
	volunteers := availabilityVolunteers()
	round := mintRound(t, store, volunteers, cfg)
	answer(t, store, volunteers, cfg, round, "michael", "shift-1")

	assert.Equal(t, 2, coverageOf(t, readRound(t, store, volunteers, cfg), "2026-08-02").Available)

	for i := range volunteers.volunteers {
		if volunteers.volunteers[i].ID == "emma" {
			volunteers.volunteers[i].Status = "Inactive"
		}
	}

	assert.Equal(t, 1, coverageOf(t, readRound(t, store, volunteers, cfg), "2026-08-02").Available,
		"a volunteer who has stopped is no longer available to allocate")
}

// TestRoundGroupsTheRoster: the group is the unit of allocation, so it is the
// unit an admin chases. One answer speaks for the pair, and the group's
// availability is the intersection over whoever answered.
func TestRoundGroupsTheRoster(t *testing.T) {
	store, cfg := availabilityFixture()
	volunteers := availabilityVolunteers()
	round := mintRound(t, store, volunteers, cfg)

	answer(t, store, volunteers, cfg, round, "michael", "shift-1", "shift-2")
	answer(t, store, volunteers, cfg, round, "emma", "shift-1")

	updated := readRound(t, store, volunteers, cfg)
	require.Len(t, updated.Groups, 2, "the pair is one group, Aaliyah is a group of one")

	smiths := groupOf(t, updated, "smiths")
	assert.Equal(t, "Emma Williams & Michael Smith", smiths.Name)
	assert.True(t, smiths.Replied)
	assert.Equal(t, []string{"shift-1"}, smiths.AvailableShiftIDs)
	require.Len(t, smiths.Members, 2)

	aaliyah := groupOf(t, updated, "individual:aaliyah")
	assert.False(t, aaliyah.Replied, "silence is not an answer")
	assert.Empty(t, aaliyah.AvailableShiftIDs)
	require.Len(t, aaliyah.Members, 1)
	assert.Equal(t, "Aaliyah Khan", aaliyah.Members[0].VolunteerName)
	assert.NotEmpty(t, aaliyah.Members[0].Token, "every member keeps their own link")
}
