package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jakechorley/ilford-drop-in/pkg/db"
	"github.com/jakechorley/ilford-drop-in/pkg/db/dbtest"
)

// roundFixture mints a rota with three weekly shifts and returns its id plus the
// shift ids in date order — the state every availability round is minted against.
func roundFixture(t *testing.T, database *db.DB) (rotaID string, shiftIDs []string) {
	t.Helper()
	ctx := context.Background()

	rotaID = uuid.New().String()
	shifts := []db.Shift{
		dbtest.Shift(rotaID, "2026-08-02"),
		dbtest.Shift(rotaID, "2026-08-09"),
		dbtest.Shift(rotaID, "2026-08-16"),
	}
	require.NoError(t, database.InsertDefinedRota(ctx, &db.Rotation{ID: rotaID}, shifts, nil))

	for _, s := range shifts {
		shiftIDs = append(shiftIDs, s.ID)
	}
	return rotaID, shiftIDs
}

func request(rotaID, volunteerID, token string) db.AvailabilityRequest {
	return db.AvailabilityRequest{
		ID:          uuid.New().String(),
		RotaID:      rotaID,
		VolunteerID: volunteerID,
		Token:       token,
	}
}

// TestMintAvailabilityRequestsIsIdempotent pins the acceptance criterion that
// minting a round twice is a no-op rather than a duplicate or a 500: the second
// mint reports nothing inserted and the first round's tokens survive, so links
// already distributed keep working.
func TestMintAvailabilityRequestsIsIdempotent(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	rotaID, _ := roundFixture(t, database)

	first := []db.AvailabilityRequest{
		request(rotaID, "alice", "token-alice"),
		request(rotaID, "bob", "token-bob"),
	}
	inserted, err := database.MintAvailabilityRequests(ctx, first)
	require.NoError(t, err)
	assert.Equal(t, 2, inserted)

	// A second mint over the same volunteers plus a newcomer adds only the
	// newcomer, and does not re-token alice or bob.
	second := []db.AvailabilityRequest{
		request(rotaID, "alice", "token-alice-2"),
		request(rotaID, "bob", "token-bob-2"),
		request(rotaID, "carol", "token-carol"),
	}
	inserted, err = database.MintAvailabilityRequests(ctx, second)
	require.NoError(t, err)
	assert.Equal(t, 1, inserted, "only the volunteer with no request yet is minted")

	requests, err := database.GetAvailabilityRequestsByRotaID(ctx, rotaID)
	require.NoError(t, err)
	require.Len(t, requests, 3)

	tokens := map[string]string{}
	for _, r := range requests {
		tokens[r.VolunteerID] = r.Token
	}
	assert.Equal(t, "token-alice", tokens["alice"], "an existing link must not be replaced")
	assert.Equal(t, "token-bob", tokens["bob"])
	assert.Equal(t, "token-carol", tokens["carol"])
}

// TestGetAvailabilityRequestByToken proves the token resolves to its request and
// that an unknown one is a plain miss rather than an error — the 404 the
// volunteer form depends on.
func TestGetAvailabilityRequestByToken(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	rotaID, _ := roundFixture(t, database)

	_, err := database.MintAvailabilityRequests(ctx, []db.AvailabilityRequest{
		request(rotaID, "alice", "token-alice"),
	})
	require.NoError(t, err)

	found, err := database.GetAvailabilityRequestByToken(ctx, "token-alice")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "alice", found.VolunteerID)
	assert.Equal(t, rotaID, found.RotaID)
	assert.Empty(t, found.SentAt, "minted but not sent")

	missing, err := database.GetAvailabilityRequestByToken(ctx, "nope")
	require.NoError(t, err)
	assert.Nil(t, missing)
}

// TestLatestAvailabilityTakesTheNewestGeneration is the core of ADR 0004:
// resubmitting appends a generation and the latest wins wholesale. The second
// generation drops a shift the first said yes to, which a delta-shaped read
// would keep.
func TestLatestAvailabilityTakesTheNewestGeneration(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	rotaID, shiftIDs := roundFixture(t, database)

	req := request(rotaID, "alice", "token-alice")
	_, err := database.MintAvailabilityRequests(ctx, []db.AvailabilityRequest{req})
	require.NoError(t, err)

	_, err = database.InsertAvailabilityResponse(ctx, req.ID, []db.ShiftAnswer{
		{ShiftID: shiftIDs[0], Answer: db.AnswerYes},
		{ShiftID: shiftIDs[1], Answer: db.AnswerYes},
	})
	require.NoError(t, err)

	second, err := database.InsertAvailabilityResponse(ctx, req.ID, []db.ShiftAnswer{
		{ShiftID: shiftIDs[2], Answer: db.AnswerYes},
	})
	require.NoError(t, err)

	latest, err := database.GetLatestAvailability(ctx, []string{req.ID}, nil)
	require.NoError(t, err)
	require.Contains(t, latest, req.ID)

	generation := latest[req.ID]
	assert.Equal(t, second.ResponseID, generation.ResponseID)
	require.Len(t, generation.Answers, 1, "the latest generation replaces the earlier one wholesale")
	assert.Equal(t, shiftIDs[2], generation.Answers[0].ShiftID)
	assert.Equal(t, db.AnswerYes, generation.Answers[0].Answer)
}

// TestLatestAvailabilityRecordsAnEmptyGeneration covers "available for nothing",
// which the Forms encoding could not express: a submission with no ticks is a
// reply, and must not read as silence.
func TestLatestAvailabilityRecordsAnEmptyGeneration(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	rotaID, _ := roundFixture(t, database)

	replied := request(rotaID, "alice", "token-alice")
	silent := request(rotaID, "bob", "token-bob")
	_, err := database.MintAvailabilityRequests(ctx, []db.AvailabilityRequest{replied, silent})
	require.NoError(t, err)

	_, err = database.InsertAvailabilityResponse(ctx, replied.ID, nil)
	require.NoError(t, err)

	latest, err := database.GetLatestAvailability(ctx, []string{replied.ID, silent.ID}, nil)
	require.NoError(t, err)

	generation, ok := latest[replied.ID]
	require.True(t, ok, "a submission with no ticks is still a reply")
	assert.Empty(t, generation.Answers)
	assert.False(t, generation.SubmittedAt.IsZero())

	_, ok = latest[silent.ID]
	assert.False(t, ok, "a volunteer who never replied has no generation at all")
}

// TestLatestAvailabilityRespectsTheCutoff proves the point-in-time read: a
// generation submitted after the rota was allocated is invisible, and the answer
// that was in before allocation is what stands.
func TestLatestAvailabilityRespectsTheCutoff(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	rotaID, shiftIDs := roundFixture(t, database)

	req := request(rotaID, "alice", "token-alice")
	_, err := database.MintAvailabilityRequests(ctx, []db.AvailabilityRequest{req})
	require.NoError(t, err)

	inTime, err := database.InsertAvailabilityResponse(ctx, req.ID, []db.ShiftAnswer{
		{ShiftID: shiftIDs[0], Answer: db.AnswerYes},
	})
	require.NoError(t, err)

	cutoff := inTime.SubmittedAt.Add(time.Millisecond)

	// A later generation exists, but allocation has already happened.
	_, err = database.InsertAvailabilityResponse(ctx, req.ID, []db.ShiftAnswer{
		{ShiftID: shiftIDs[1], Answer: db.AnswerYes},
	})
	require.NoError(t, err)

	latest, err := database.GetLatestAvailability(ctx, []string{req.ID}, &cutoff)
	require.NoError(t, err)
	generation := latest[req.ID]
	assert.Equal(t, inTime.ResponseID, generation.ResponseID)
	require.Len(t, generation.Answers, 1)
	assert.Equal(t, shiftIDs[0], generation.Answers[0].ShiftID)

	// Without the cutoff the later answer wins, so the filter is doing the work
	// rather than the ordering happening to agree.
	latest, err = database.GetLatestAvailability(ctx, []string{req.ID}, nil)
	require.NoError(t, err)
	require.Len(t, latest[req.ID].Answers, 1)
	assert.Equal(t, shiftIDs[1], latest[req.ID].Answers[0].ShiftID)
}

// TestInsertAvailabilityResponseIsAtomic proves a generation cannot land
// half-written: a shift id that is not a shift takes the whole submission down,
// leaving no response row behind. A partial write would silently record
// unavailability for the shifts that did not make it (ADR 0004).
func TestInsertAvailabilityResponseIsAtomic(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	rotaID, shiftIDs := roundFixture(t, database)

	req := request(rotaID, "alice", "token-alice")
	_, err := database.MintAvailabilityRequests(ctx, []db.AvailabilityRequest{req})
	require.NoError(t, err)

	_, err = database.InsertAvailabilityResponse(ctx, req.ID, []db.ShiftAnswer{
		{ShiftID: shiftIDs[0], Answer: db.AnswerYes},
		{ShiftID: uuid.New().String(), Answer: db.AnswerYes},
	})
	require.Error(t, err)

	latest, err := database.GetLatestAvailability(ctx, []string{req.ID}, nil)
	require.NoError(t, err)
	assert.NotContains(t, latest, req.ID, "a rejected submission must not leave a response row")
}

// TestGetLatestAvailabilityHandlesNoRequests guards the batch read's empty case:
// a round nobody has been minted into is a legitimate state, not an error.
func TestGetLatestAvailabilityHandlesNoRequests(t *testing.T) {
	database, _ := dbtest.New(t)

	latest, err := database.GetLatestAvailability(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Empty(t, latest)
}

// TestMarkAvailabilityRequestSentStampsOneRequest pins what sending records: a
// stamp on the volunteer who was emailed and on nobody else. It is the whole
// basis of a round send being repeatable — an unstamped request is one still to
// email, so a stamp landing on the wrong row silently drops a volunteer from the
// round.
func TestMarkAvailabilityRequestSentStampsOneRequest(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	rotaID, _ := roundFixture(t, database)

	requests := []db.AvailabilityRequest{
		request(rotaID, "alice", "token-alice"),
		request(rotaID, "bob", "token-bob"),
	}
	_, err := database.MintAvailabilityRequests(ctx, requests)
	require.NoError(t, err)

	before := time.Now().Add(-time.Second)
	require.NoError(t, database.MarkAvailabilityRequestSent(ctx, requests[0].ID))

	stored, err := database.GetAvailabilityRequestsByRotaID(ctx, rotaID)
	require.NoError(t, err)
	require.Len(t, stored, 2)

	byVolunteer := make(map[string]db.AvailabilityRequest, len(stored))
	for _, r := range stored {
		byVolunteer[r.VolunteerID] = r
	}

	require.NotEmpty(t, byVolunteer["alice"].SentAt)
	sentAt, err := time.Parse(time.RFC3339, byVolunteer["alice"].SentAt)
	require.NoError(t, err, "sent_at must come back as a timestamp the API can serialise")
	assert.True(t, sentAt.After(before))

	assert.Empty(t, byVolunteer["bob"].SentAt, "bob was not emailed, so he is still to send")
}

// TestMarkAvailabilityRequestSentRejectsAnUnknownID: the stamp is the record
// that an email went out, so silently stamping nothing would leave a send
// reporting success over a row that does not exist.
func TestMarkAvailabilityRequestSentRejectsAnUnknownID(t *testing.T) {
	database, _ := dbtest.New(t)

	err := database.MarkAvailabilityRequestSent(context.Background(), uuid.New().String())

	assert.Error(t, err)
}

// TestBackfilledResponseKeepsItsOriginalTimestamp: the whole point of the
// backfill's own writer is that a Forms answer lands at the moment it was given,
// not the moment it was imported. Reading it back at a cut-off just after that
// moment must find it — with NOW() it would sit years in the future and every
// historical read would miss it.
func TestBackfilledResponseKeepsItsOriginalTimestamp(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()
	rotaID, shiftIDs := roundFixture(t, database)

	req := request(rotaID, "alice", "token-alice")
	_, err := database.MintAvailabilityRequests(ctx, []db.AvailabilityRequest{req})
	require.NoError(t, err)

	answered := time.Date(2025, 3, 4, 9, 30, 0, 0, time.UTC)
	inserted, err := database.InsertBackfilledAvailabilityResponse(ctx, req.ID, answered, []db.ShiftAnswer{
		{ShiftID: shiftIDs[0], Answer: db.AnswerYes},
	})
	require.NoError(t, err)
	assert.True(t, inserted)

	cutoff := answered.Add(time.Second)
	latest, err := database.GetLatestAvailability(ctx, []string{req.ID}, &cutoff)
	require.NoError(t, err)
	require.Contains(t, latest, req.ID)
	assert.True(t, answered.Equal(latest[req.ID].SubmittedAt))
	require.Len(t, latest[req.ID].Answers, 1)
	assert.Equal(t, shiftIDs[0], latest[req.ID].Answers[0].ShiftID)
}

// TestBackfilledResponseIsIdempotent is the ticket's acceptance criterion:
// running the backfill twice must not double the generations. The second offer
// of the same (request, submitted_at) writes nothing at all — not the response
// row, and not its shift rows.
func TestBackfilledResponseIsIdempotent(t *testing.T) {
	database, dbURL := dbtest.New(t)
	ctx := context.Background()
	rotaID, shiftIDs := roundFixture(t, database)

	req := request(rotaID, "alice", "token-alice")
	_, err := database.MintAvailabilityRequests(ctx, []db.AvailabilityRequest{req})
	require.NoError(t, err)

	// Nanoseconds Postgres cannot store: the second run must still recognise the
	// row it wrote, so the comparison has to happen at the stored precision.
	answered := time.Date(2025, 3, 4, 9, 30, 0, 123456789, time.UTC)
	answers := []db.ShiftAnswer{{ShiftID: shiftIDs[0], Answer: db.AnswerYes}}

	inserted, err := database.InsertBackfilledAvailabilityResponse(ctx, req.ID, answered, answers)
	require.NoError(t, err)
	require.True(t, inserted)

	inserted, err = database.InsertBackfilledAvailabilityResponse(ctx, req.ID, answered, answers)
	require.NoError(t, err)
	assert.False(t, inserted, "the same Forms response must not be imported twice")

	conn, err := pgx.Connect(ctx, dbURL)
	require.NoError(t, err)
	defer conn.Close(ctx)

	var responses, shiftRows int
	require.NoError(t, conn.QueryRow(ctx,
		`SELECT COUNT(*) FROM availability_response WHERE availability_request_id = $1`, req.ID).Scan(&responses))
	require.NoError(t, conn.QueryRow(ctx,
		`SELECT COUNT(*) FROM shift_availability`).Scan(&shiftRows))
	assert.Equal(t, 1, responses)
	assert.Equal(t, 1, shiftRows)
}
