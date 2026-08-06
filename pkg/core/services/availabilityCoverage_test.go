package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
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

// roleCoverageOf picks one Role's numbers out of a shift's picture.
func roleCoverageOf(t *testing.T, shift ShiftCoverage, role string) RoleCoverage {
	t.Helper()
	for _, r := range shift.Roles {
		if r.Role == role {
			return r
		}
	}
	t.Fatalf("no coverage for role %q on %s", role, shift.Date)
	return RoleCoverage{}
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
	store.defaultShape = shapeOfSize(2)
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
	store.defaultShape = shapeOfSize(2)
	volunteers := availabilityVolunteers()
	round := mintRound(t, store, volunteers, cfg)

	answer(t, store, volunteers, cfg, round, "emma", "shift-1")

	updated := readRound(t, store, volunteers, cfg)
	assert.Equal(t, 2, coverageOf(t, updated, "2026-08-02").Available,
		"Michael is opted in by Emma's answer")
	assert.Equal(t, 0, coverageOf(t, updated, "2026-08-09").Available)
}

// TestCoverageReportsTeamLeadCover separates the two questions a shift asks: are
// there enough people, and is one of them allowed to lead. Both are the same
// arithmetic now, run once per Role, so the lead Seat is a Needed and an
// Available like any other rather than a boolean off to one side.
//
// Aaliyah holds both Roles, and is counted under both: she can fill either Seat.
// She is not counted twice for the shift, because she can only take one of them.
func TestCoverageReportsTeamLeadCover(t *testing.T) {
	store, cfg := availabilityFixture()
	store.defaultShape = shapeOfSize(2)
	volunteers := availabilityVolunteers()
	round := mintRound(t, store, volunteers, cfg)

	answer(t, store, volunteers, cfg, round, "michael", "shift-1", "shift-2")
	answer(t, store, volunteers, cfg, round, "aaliyah", "shift-2")

	updated := readRound(t, store, volunteers, cfg)

	first := coverageOf(t, updated, "2026-08-02")
	assert.Equal(t, 0, roleCoverageOf(t, first, "Team lead").Available,
		"the only team lead is not available on the first date")
	assert.Equal(t, 1, roleCoverageOf(t, first, "Team lead").Needed,
		"the lead Seat is still to be filled")
	assert.Equal(t, 2, first.Available)

	second := coverageOf(t, updated, "2026-08-09")
	assert.Equal(t, 1, roleCoverageOf(t, second, "Team lead").Available)
	assert.Equal(t, 3, second.Available,
		"a lead who also holds the uncapped Role could take one of its Seats")
}

// TestCoverageShapesSeatsFromTheRoles: the Seats a shift reports are the Seats
// the solver will be given — a capped Role's ceiling, and the shift's size for
// the uncapped one. If these two ever disagree the page promises staffing the
// solve cannot deliver.
func TestCoverageShapesSeatsFromTheRoles(t *testing.T) {
	store, cfg := availabilityFixture()
	store.defaultShape = shapeOfSize(4)
	volunteers := availabilityVolunteers()
	mintRound(t, store, volunteers, cfg)

	first := coverageOf(t, readRound(t, store, volunteers, cfg), "2026-08-02")

	require.Len(t, first.Roles, 2, "every configured Role is reported")
	assert.Equal(t, "Team lead", first.Roles[0].Role, "priority order")
	assert.Equal(t, 1, first.Roles[0].Seats, "the capped Role gets its ceiling")
	assert.Equal(t, "Service volunteer", first.Roles[1].Role)
	assert.Equal(t, 4, first.Roles[1].Seats, "the uncapped Role takes the shift's size")
}

// TestCoverageReportsTheDelta is the number an admin is really after: how far
// short of a full shift the answers so far leave them.
func TestCoverageReportsTheDelta(t *testing.T) {
	store, cfg := availabilityFixture()
	store.defaultShape = shapeOfSize(3)
	volunteers := availabilityVolunteers()
	round := mintRound(t, store, volunteers, cfg)

	answer(t, store, volunteers, cfg, round, "michael", "shift-1", "shift-2")

	updated := readRound(t, store, volunteers, cfg)
	first := coverageOf(t, updated, "2026-08-02")
	assert.Equal(t, 3, first.Needed)
	assert.Equal(t, 2, first.Available)
	assert.Equal(t, -1, first.Delta, "two people for a shift of three is one short")
}

// TestCoverageReadsTheDefaultShape: what a shift needs is the Shape an admin
// stated in the settings, Role by Role — the same Shape allocation sends the
// solver. A config file has nothing left to say about it: `defaultShiftSize` and
// the `shiftSize` on a rota override both left in issue #129.
func TestCoverageReadsTheDefaultShape(t *testing.T) {
	store, cfg := availabilityFixture()
	store.defaultShape = []db.DefaultShapeSeat{
		{RoleID: "role-team-lead", Seats: 1},
		{RoleID: "role-service-volunteer", Seats: 6},
	}
	volunteers := availabilityVolunteers()
	mintRound(t, store, volunteers, cfg)

	round := readRound(t, store, volunteers, cfg)
	first := coverageOf(t, round, "2026-08-02")
	assert.Equal(t, 6, first.Needed, "the uncapped Role's Seats are the headline number")
	assert.Equal(t, 1, roleCoverageOf(t, first, "Team lead").Seats)
	assert.Equal(t, 6, coverageOf(t, round, "2026-08-09").Needed,
		"every shift of a rota asks for the same thing")
}

// A Role the Shape does not name has no Seats, so nobody is chased for it. That
// is a Shape an admin can state now — the derivation this replaced gave every
// capped Role its ceiling whether it was wanted or not.
func TestCoverageGivesNoSeatsToARoleTheShapeOmits(t *testing.T) {
	store, cfg := availabilityFixture()
	store.defaultShape = []db.DefaultShapeSeat{
		{RoleID: "role-service-volunteer", Seats: 3},
	}
	volunteers := availabilityVolunteers()
	mintRound(t, store, volunteers, cfg)

	round := readRound(t, store, volunteers, cfg)
	first := coverageOf(t, round, "2026-08-02")
	assert.Equal(t, 0, roleCoverageOf(t, first, "Team lead").Seats)
	assert.Equal(t, 0, roleCoverageOf(t, first, "Team lead").Needed)
	assert.Equal(t, 3, first.Needed)
}

// TestCoverageSubtractsPreallocations: a pinned seat is already filled. The seat
// comes out of what the shift still needs, and a pinned volunteer comes out of
// what is available to fill it — counting them on both sides would report the
// shift as a person better off than it is.
func TestCoverageSubtractsPreallocations(t *testing.T) {
	store, cfg := availabilityFixture()
	store.defaultShape = shapeOfSize(4)
	volunteers := availabilityVolunteers()
	round := mintRound(t, store, volunteers, cfg)

	// A custom entry and Michael are both pinned to the first date.
	store.pins = []db.Preallocation{
		{ID: "pin-0", ShiftID: "shift-1", Role: "Service volunteer", CustomValue: "St John's team"},
		{ID: "pin-1", ShiftID: "shift-1", Role: "Service volunteer", VolunteerID: "michael"},
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
	store.defaultShape = shapeOfSize(2)
	volunteers := availabilityVolunteers()
	mintRound(t, store, volunteers, cfg)

	store.pins = []db.Preallocation{
		{ID: "pin-1", ShiftID: "shift-1", Role: "Team lead", VolunteerID: "aaliyah"},
	}

	round := readRound(t, store, volunteers, cfg)
	first := coverageOf(t, round, "2026-08-02")
	lead := roleCoverageOf(t, first, "Team lead")
	assert.Equal(t, 1, lead.Pinned)
	assert.Equal(t, 0, lead.Needed, "the lead Seat is spoken for")
	assert.Equal(t, 0, first.Pinned, "a Seat in a capped Role is not one of the uncapped Role's")
	assert.Equal(t, 2, first.Needed)
}

// TestCoverageShowsAClosedShiftAsClosed: a date the drop-in is not running is
// not a date that is short of volunteers. Reporting it as understaffed would put
// a permanent red mark on a round that is fine.
func TestCoverageShowsAClosedShiftAsClosed(t *testing.T) {
	store, cfg := availabilityFixture()
	store.defaultShape = shapeOfSize(3)
	volunteers := availabilityVolunteers()
	round := mintRound(t, store, volunteers, cfg)
	answer(t, store, volunteers, cfg, round, "michael", "shift-1")

	updated := readRound(t, store, volunteers, cfg)
	closed := coverageOf(t, updated, "2026-08-16")
	assert.True(t, closed.Closed)
	assert.Equal(t, 0, closed.Needed, "a closed shift needs nobody")
	assert.Equal(t, 0, closed.Available)
	assert.Equal(t, 0, closed.Delta)
	assert.Empty(t, closed.Roles, "a closed shift has no Seats of any Role")
}

// TestCoverageSkipsVolunteersWhoHaveStopped: a round is minted against the
// active roster, but somebody can stop volunteering while it is open. Their
// answer must stop counting the moment they do, or the view promises people who
// will not be allocated.
func TestCoverageSkipsVolunteersWhoHaveStopped(t *testing.T) {
	store, cfg := availabilityFixture()
	store.defaultShape = shapeOfSize(2)
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
	assert.Equal(t, "Emma & Michael", smiths.Name)
	assert.True(t, smiths.Replied)
	assert.Equal(t, []string{"shift-1"}, smiths.AvailableShiftIDs)
	require.Len(t, smiths.Members, 2)

	aaliyah := groupOf(t, updated, "individual:aaliyah")
	assert.False(t, aaliyah.Replied, "silence is not an answer")
	assert.Empty(t, aaliyah.AvailableShiftIDs)
	require.Len(t, aaliyah.Members, 1)
	assert.Equal(t, "Aaliyah", aaliyah.Members[0].VolunteerName)
	assert.NotEmpty(t, aaliyah.Members[0].Token, "every member keeps their own link")
}

// TestRoundEntriesCarryTheRolesTheyHold: the responses grid filters its rows by
// Role, and a Role is a fact about the roster, not about the round. Carrying it
// on the entry is what keeps the server the authority on who holds what — the
// alternative is every client fetching the roster and joining it back.
//
// A volunteer no longer on the roster holds no Roles here, which is the same
// answer allocation gives: there is nobody to ask.
func TestRoundEntriesCarryTheRolesTheyHold(t *testing.T) {
	store, cfg := availabilityFixture()
	volunteers := availabilityVolunteers()
	round := mintRound(t, store, volunteers, cfg)

	aaliyah := groupOf(t, round, "individual:aaliyah")
	assert.Equal(t, []string{"Team lead", "Service volunteer"}, aaliyah.Members[0].Roles,
		"in the roster's own order, which is priority order")

	smiths := groupOf(t, round, "smiths")
	for _, member := range smiths.Members {
		assert.Equal(t, []string{"Service volunteer"}, member.Roles)
	}

	// Dropping someone from the roster mid-round leaves their link working and
	// their row in place, so the round must still build — with no Roles on it.
	volunteers.volunteers = volunteers.volunteers[:2]
	updated := readRound(t, store, volunteers, cfg)
	assert.Empty(t, groupOf(t, updated, "individual:aaliyah").Members[0].Roles)
}

// TestRoundNamesPeopleAsTheRotaDoes: the round is an admin screen, and every
// other admin screen — the rota, the volunteer list — calls someone by their
// display name. A grid of full names is a grid whose first column is wider than
// the dates it exists to show.
//
// The volunteer's own form and the emails keep the full name: a wrong name is
// how somebody notices a forwarded link, and the shortest unambiguous form is a
// weaker signal than the whole thing.
func TestRoundNamesPeopleAsTheRotaDoes(t *testing.T) {
	store, cfg := availabilityFixture()
	volunteers := availabilityVolunteers()
	round := mintRound(t, store, volunteers, cfg)

	assert.Equal(t, "Emma & Michael", groupOf(t, round, "smiths").Name)
	assert.Equal(t, "Aaliyah", groupOf(t, round, "individual:aaliyah").Members[0].VolunteerName)

	// Covered-by names the partner who answered, in the same breath as the
	// members around it, so it is named the same way.
	answer(t, store, volunteers, cfg, round, "michael", "shift-1")
	updated := readRound(t, store, volunteers, cfg)
	for _, member := range groupOf(t, updated, "smiths").Members {
		if member.VolunteerName == "Emma" {
			assert.Equal(t, []string{"Michael"}, member.CoveredBy)
		}
	}

	// The form still says the whole thing.
	form, err := GetAvailabilityForm(context.Background(), store, volunteers, cfg, zap.NewNop(),
		tokenFor(t, round, "michael"))
	require.NoError(t, err)
	assert.Equal(t, "Michael Smith", form.VolunteerName)
	assert.Equal(t, []string{"Emma Williams"}, form.GroupMembers)
}
