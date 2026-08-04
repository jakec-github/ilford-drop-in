package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// mockMailer records what was sent and can be told to fail for one address, so
// a test can exercise the per-volunteer failure path without failing the batch.
type mockMailer struct {
	sent     []sentMail
	failFor  string
	failWith error
}

type sentMail struct {
	to      string
	subject string
	body    string
}

func (m *mockMailer) SendEmail(to, subject, body string) error {
	if to == m.failFor {
		if m.failWith != nil {
			return m.failWith
		}
		return errors.New("mailbox full")
	}
	m.sent = append(m.sent, sentMail{to: to, subject: subject, body: body})
	return nil
}

func (m *mockMailer) recipients() []string {
	to := make([]string, 0, len(m.sent))
	for _, s := range m.sent {
		to = append(to, s.to)
	}
	return to
}

// sendVolunteers is the roster the send tests share: Michael and Emma are a
// group, so a reminder to one is answered by the other; Sara is on her own.
func sendVolunteers() *mockVolunteerClient {
	return &mockVolunteerClient{volunteers: []model.Volunteer{
		{ID: "michael", FirstName: "Michael", LastName: "Smith", Email: "michael@example.com", Status: "Active", GroupKey: "smiths"},
		{ID: "emma", FirstName: "Emma", LastName: "Williams", Email: "emma@example.com", Status: "Active", GroupKey: "smiths"},
		{ID: "sara", FirstName: "Sara", LastName: "Ali", Email: "sara@example.com", Status: "Active"},
	}}
}

// sendStore is a round in which everyone holds a link and nobody has been
// emailed or answered yet. Individual tests move the pieces they care about.
func sendStore() *mockAvailabilityStore {
	return &mockAvailabilityStore{
		rotations: []db.Rotation{{ID: "rota-1", Start: "2026-08-02", End: "2026-08-09", ShiftCount: 2}},
		shifts: []db.Shift{
			{ID: "shift-1", RotaID: "rota-1", Date: "2026-08-02"},
			{ID: "shift-2", RotaID: "rota-1", Date: "2026-08-09"},
		},
		requests: []db.AvailabilityRequest{
			{ID: "req-michael", RotaID: "rota-1", VolunteerID: "michael", Token: "tok-michael"},
			{ID: "req-emma", RotaID: "rota-1", VolunteerID: "emma", Token: "tok-emma"},
			{ID: "req-sara", RotaID: "rota-1", VolunteerID: "sara", Token: "tok-sara"},
		},
	}
}

// sendTestCfg closes no dates: which shifts are open is the form's business,
// not the email's, so these tests have no use for overrides.
var sendTestCfg = &config.Config{}

func sendParams(mode SendMode) SendParams {
	return SendParams{
		Mode:     mode,
		Deadline: "Friday 7 August",
		Link:     func(token string) string { return "https://drop-in.example/availability/" + token },
	}
}

func send(t *testing.T, store *mockAvailabilityStore, mailer *mockMailer, params SendParams) *SendReport {
	t.Helper()
	report, err := SendAvailabilityEmails(context.Background(), store, sendVolunteers(), mailer, sendTestCfg, zap.NewNop(), params)
	require.NoError(t, err)
	return report
}

// TestSendRoundEmailsEveryUnsentRequest: the round send is what turns a minted
// round into one people have actually been asked about, and sent_at is what
// stops it asking twice.
func TestSendRoundEmailsEveryUnsentRequest(t *testing.T) {
	store := sendStore()
	mailer := &mockMailer{}

	report := send(t, store, mailer, sendParams(SendModeRound))

	assert.ElementsMatch(t, []string{"michael@example.com", "emma@example.com", "sara@example.com"}, mailer.recipients())
	assert.Len(t, report.Sent, 3)
	assert.Empty(t, report.Failed)

	for _, req := range store.requests {
		assert.NotEmpty(t, req.SentAt, "every request whose email sent must be stamped: %s", req.VolunteerID)
	}

	// The link is the whole payload of the email; a body without it asks people
	// to answer with no way of doing so.
	require.Len(t, mailer.sent, 3)
	for _, mail := range mailer.sent {
		assert.Contains(t, mail.subject, "Friday 7 August")
		assert.Contains(t, mail.body, "https://drop-in.example/availability/tok-")
		assert.Contains(t, mail.body, "Friday 7 August")
	}
}

// TestSendRoundSkipsAlreadySentRequests: sending twice is how an admin tops up a
// round after new volunteers join, so it must not re-mail everyone who already
// has their link.
func TestSendRoundSkipsAlreadySentRequests(t *testing.T) {
	store := sendStore()
	store.requests[0].SentAt = "2026-07-30T09:00:00Z"
	mailer := &mockMailer{}

	report := send(t, store, mailer, sendParams(SendModeRound))

	assert.ElementsMatch(t, []string{"emma@example.com", "sara@example.com"}, mailer.recipients())
	assert.Len(t, report.Sent, 2)
}

// TestSendRoundReportsAFailureWithoutStampingOrStopping: one bad address must
// not cost everyone else their email, and the volunteer it failed for has to
// stay resendable — which is exactly what an unstamped sent_at means.
func TestSendRoundReportsAFailureWithoutStampingOrStopping(t *testing.T) {
	store := sendStore()
	mailer := &mockMailer{failFor: "emma@example.com"}

	report := send(t, store, mailer, sendParams(SendModeRound))

	assert.ElementsMatch(t, []string{"michael@example.com", "sara@example.com"}, mailer.recipients())
	require.Len(t, report.Failed, 1)
	assert.Equal(t, "emma", report.Failed[0].VolunteerID)
	assert.Equal(t, "Emma Williams", report.Failed[0].VolunteerName)
	assert.Contains(t, report.Failed[0].Error, "mailbox full")

	emma, ok := store.findRequest("rota-1", "emma")
	require.True(t, ok)
	assert.Empty(t, emma.SentAt, "a failed send must leave the request unsent so it can be retried")
}

// TestSendRoundSkipsVolunteersWhoHaveGoneInactive: a request minted while
// someone was active outlives their leaving, and mailing them afterwards is
// contacting a volunteer who has stopped volunteering.
func TestSendRoundSkipsVolunteersWhoHaveGoneInactive(t *testing.T) {
	store := sendStore()
	volunteers := sendVolunteers()
	volunteers.volunteers[1].Status = "Inactive"
	mailer := &mockMailer{}

	report, err := SendAvailabilityEmails(context.Background(), store, volunteers, mailer, sendTestCfg, zap.NewNop(), sendParams(SendModeRound))
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"michael@example.com", "sara@example.com"}, mailer.recipients())
	assert.Len(t, report.Sent, 2)
	assert.Empty(t, report.Failed, "skipping someone who has left is not a failure to report")
}

// TestSendRoundReportsAVolunteerWithNoEmailAddress: there is nothing to send to,
// and silently dropping them would leave an admin believing the round went out
// to everyone.
func TestSendRoundReportsAVolunteerWithNoEmailAddress(t *testing.T) {
	store := sendStore()
	volunteers := sendVolunteers()
	volunteers.volunteers[2].Email = ""
	mailer := &mockMailer{}

	report, err := SendAvailabilityEmails(context.Background(), store, volunteers, mailer, sendTestCfg, zap.NewNop(), sendParams(SendModeRound))
	require.NoError(t, err)

	assert.Len(t, report.Sent, 2)
	require.Len(t, report.Failed, 1)
	assert.Equal(t, "sara", report.Failed[0].VolunteerID)
	assert.Contains(t, report.Failed[0].Error, "no email address")
}

// TestResendEmailsOneVolunteerWhoAlreadyHasTheirLink: the resend exists for the
// email that bounced or was deleted, so "already sent" is the normal case rather
// than a reason to refuse.
func TestResendEmailsOneVolunteerWhoAlreadyHasTheirLink(t *testing.T) {
	store := sendStore()
	store.requests[2].SentAt = "2026-07-30T09:00:00Z"
	mailer := &mockMailer{}

	params := sendParams(SendModeResend)
	params.VolunteerID = "sara"
	report := send(t, store, mailer, params)

	assert.Equal(t, []string{"sara@example.com"}, mailer.recipients())
	assert.Len(t, report.Sent, 1)
}

// TestResendRejectsAVolunteerWithNoRequest: they were never part of this round,
// so there is no link to resend and no row to stamp.
func TestResendRejectsAVolunteerWithNoRequest(t *testing.T) {
	store := sendStore()
	store.requests = store.requests[:1]

	params := sendParams(SendModeResend)
	params.VolunteerID = "sara"
	_, err := SendAvailabilityEmails(context.Background(), store, sendVolunteers(), &mockMailer{}, sendTestCfg, zap.NewNop(), params)

	assert.ErrorIs(t, err, ErrNotFound)
}

// TestRemindersGoOnlyToVolunteersWhoseGroupHasNotAnswered: a group answers as a
// unit, so chasing Michael when Emma has already replied is chasing an answer
// we hold (ADR 0004).
func TestRemindersGoOnlyToVolunteersWhoseGroupHasNotAnswered(t *testing.T) {
	store := sendStore()
	for i := range store.requests {
		store.requests[i].SentAt = "2026-07-30T09:00:00Z"
	}
	store.generations = []db.AvailabilityGeneration{{
		RequestID:   "req-emma",
		ResponseID:  "gen-1",
		SubmittedAt: time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC),
		Answers:     []db.ShiftAnswer{{ShiftID: "shift-1", Answer: db.AnswerYes}},
	}}
	mailer := &mockMailer{}

	report := send(t, store, mailer, sendParams(SendModeReminder))

	assert.Equal(t, []string{"sara@example.com"}, mailer.recipients(),
		"Emma answered for the group, so neither she nor Michael needs chasing")
	assert.Len(t, report.Sent, 1)

	require.Len(t, mailer.sent, 1)
	assert.Contains(t, mailer.sent[0].subject, "Reminder")
}

// TestRemindersSkipRequestsThatWereNeverSent: a reminder is a second email, and
// there is nothing to remind someone of who was never asked in the first place.
func TestRemindersSkipRequestsThatWereNeverSent(t *testing.T) {
	store := sendStore()
	store.requests[0].SentAt = "2026-07-30T09:00:00Z"
	mailer := &mockMailer{}

	report := send(t, store, mailer, sendParams(SendModeReminder))

	assert.Equal(t, []string{"michael@example.com"}, mailer.recipients())
	assert.Len(t, report.Sent, 1)
}

// TestRemindersDoNotRestampSentAt: sent_at records when someone was asked, which
// a reminder does not change — and moving it would make the round send think
// they had been covered by a different email.
func TestRemindersDoNotRestampSentAt(t *testing.T) {
	store := sendStore()
	for i := range store.requests {
		store.requests[i].SentAt = "2026-07-30T09:00:00Z"
	}

	send(t, store, &mockMailer{}, sendParams(SendModeReminder))

	for _, req := range store.requests {
		assert.Equal(t, "2026-07-30T09:00:00Z", req.SentAt, "%s", req.VolunteerID)
	}
}

// TestSendRefusesWithoutADeadline: the deadline is the whole point of the email
// — it says when answering stops mattering — and it appears in both the subject
// and the body, so an empty one ships a sentence with a hole in it.
func TestSendRefusesWithoutADeadline(t *testing.T) {
	params := sendParams(SendModeRound)
	params.Deadline = ""

	_, err := SendAvailabilityEmails(context.Background(), sendStore(), sendVolunteers(), &mockMailer{}, sendTestCfg, zap.NewNop(), params)

	assert.ErrorIs(t, err, ErrInvalidInput)
}

// TestSendRefusesOnceTheRotaIsAllocated: the links stopped working at
// allocation, so every email would carry a dead one.
func TestSendRefusesOnceTheRotaIsAllocated(t *testing.T) {
	store := sendStore()
	store.rotations[0].AllocatedDatetime = "2026-08-01T09:00:00Z"
	mailer := &mockMailer{}

	_, err := SendAvailabilityEmails(context.Background(), store, sendVolunteers(), mailer, sendTestCfg, zap.NewNop(), sendParams(SendModeRound))

	assert.ErrorIs(t, err, ErrConflict)
	assert.Empty(t, mailer.sent)
}

// TestSendReportsNobodyToEmailWithoutFailing: a round already fully sent, or one
// where everybody has answered, is a no-op an admin should see as "nothing to
// do" rather than an error.
func TestSendReportsNobodyToEmailWithoutFailing(t *testing.T) {
	store := sendStore()
	for i := range store.requests {
		store.requests[i].SentAt = "2026-07-30T09:00:00Z"
	}
	mailer := &mockMailer{}

	report := send(t, store, mailer, sendParams(SendModeRound))

	assert.Empty(t, report.Sent)
	assert.Empty(t, report.Failed)
	assert.Empty(t, mailer.sent)
}
