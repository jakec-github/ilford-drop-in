package services

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services/utils"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// SendMode selects which of a round's requests a send covers and what the email
// says. The three differ only in those two things — everything after choosing
// the recipients is one loop — so they are a mode rather than three services.
type SendMode string

const (
	// SendModeRound emails everyone who holds a link they have not been sent.
	// It is the send that turns a minted round into one people know about, and
	// re-running it tops up volunteers minted since, without re-mailing anyone.
	SendModeRound SendMode = "round"
	// SendModeReminder chases the requests that were sent and have gone
	// unanswered, skipping anyone whose group has answered for them.
	SendModeReminder SendMode = "reminder"
	// SendModeResend re-sends one named volunteer's link, sent or not. It is the
	// answer to a bounce or a deleted email, so "already sent" is the normal
	// case rather than a reason to refuse.
	SendModeResend SendMode = "resend"
)

// SendParams is one send. Deadline is the date the email quotes and nothing
// else: it is not stored, not shown on the site, and not enforced — allocation
// is the real cutoff (ADR 0004).
//
// Link turns a request's token into the URL the volunteer is given. It is passed
// in because the address the app answers on is a property of the request being
// served, not of the round.
type SendParams struct {
	RotaID      string // empty means the latest rota
	Mode        SendMode
	Deadline    string
	VolunteerID string // SendModeResend only
	Link        func(token string) string
	// Progress, when set, is called once the recipients are known and again
	// after each email. A send is slow enough — Gmail is throttled to one email
	// every three seconds — that a caller needs to show it moving, and this is
	// the only place that knows both how many were selected and how far it has
	// got.
	Progress func(done, total int)
}

// SentEmail is one email that went out, named the way an admin would recognise
// the person it went to.
type SentEmail struct {
	VolunteerID   string
	VolunteerName string
	Email         string
}

// SendReport is what a send did. Sent and Failed together account for everyone
// the mode selected; a volunteer the mode passed over appears in neither,
// because "we deliberately did not email them" is not a result to report.
type SendReport struct {
	Mode   SendMode
	Sent   []SentEmail
	Failed []FailedEmail
}

// SendAvailabilityEmails emails a round's links as the admin who triggered it,
// through the mailer they are holding a short-lived token for.
//
// A failed address is reported and the batch carries on: one bad mailbox must
// not cost the other twenty-eight volunteers their email. sent_at is stamped per
// volunteer as each send succeeds, never in a batch at the end — the stamp is
// what makes a send resumable, so it has to be true at every point during a
// send that takes a minute and a half, not just after it.
func SendAvailabilityEmails(
	ctx context.Context,
	database AvailabilityStore,
	volunteerClient VolunteerClient,
	mailer GmailClient,
	cfg *config.Config,
	logger *zap.Logger,
	params SendParams,
) (*SendReport, error) {
	if params.Deadline == "" {
		return nil, wrapf(ErrInvalidInput, "a deadline is required: it is quoted in the subject and the body of every email")
	}
	if params.Link == nil {
		return nil, fmt.Errorf("send params carry no link builder")
	}

	rota, err := resolveRota(ctx, database, params.RotaID)
	if err != nil {
		return nil, err
	}
	// Links stop working at allocation, so every email would carry a dead one.
	if rota.AllocatedDatetime != "" {
		return nil, wrapf(ErrConflict, "rota %s is already allocated, so its availability links no longer work", rota.ID)
	}

	requests, err := database.GetAvailabilityRequestsV2ByRotaID(ctx, rota.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch availability requests: %w", err)
	}

	volunteers, err := volunteerClient.ListVolunteers(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch volunteers: %w", err)
	}

	recipients, err := selectRecipients(ctx, database, rota, requests, volunteers, params)
	if err != nil {
		return nil, err
	}

	report := &SendReport{Mode: params.Mode, Sent: []SentEmail{}, Failed: []FailedEmail{}}
	logger.Info("Sending availability emails",
		zap.String("rota_id", rota.ID),
		zap.String("mode", string(params.Mode)),
		zap.Int("recipients", len(recipients)))

	progress := func() {
		if params.Progress != nil {
			params.Progress(len(report.Sent)+len(report.Failed), len(recipients))
		}
	}
	// Reported before the first email, so a caller showing a send knows how many
	// are coming rather than watching a bar with no end.
	progress()

	// Every outcome goes through one of these two, so nothing can be recorded
	// without the progress count moving with it.
	fail := func(f FailedEmail) {
		report.Failed = append(report.Failed, f)
		progress()
	}
	succeed := func(s SentEmail) {
		report.Sent = append(report.Sent, s)
		progress()
	}

	for _, r := range recipients {
		name := volunteerName(r.volunteer)

		if r.volunteer.Email == "" {
			// Nothing to send to. Reported rather than skipped: an admin who
			// saw nothing would believe the round reached everybody.
			fail(FailedEmail{
				VolunteerID:   r.volunteer.ID,
				VolunteerName: name,
				Error:         "no email address on the roster",
			})
			continue
		}

		link := params.Link(r.request.Token)
		subject, body := composeEmail(params.Mode, r.volunteer.FirstName, link, params.Deadline)

		if err := mailer.SendEmail(r.volunteer.Email, subject, body); err != nil {
			logger.Warn("Failed to send availability email",
				zap.String("volunteer_id", r.volunteer.ID),
				zap.Error(err))
			fail(FailedEmail{
				VolunteerID:   r.volunteer.ID,
				VolunteerName: name,
				Email:         r.volunteer.Email,
				Error:         err.Error(),
			})
			continue
		}

		// A reminder does not re-ask, so it leaves the stamp where it is: moving
		// it would make a later round send believe this volunteer's original
		// invitation went out today.
		if params.Mode != SendModeReminder {
			if err := database.MarkAvailabilityRequestSent(ctx, r.request.ID); err != nil {
				// The email is already gone, so this cannot be undone. Report it
				// as a failure so the admin knows the row is out of step: the
				// cost is one duplicate email if they send again, which is far
				// better than a volunteer silently dropping out of the round.
				logger.Error("Sent an availability email but failed to stamp sent_at",
					zap.String("volunteer_id", r.volunteer.ID),
					zap.Error(err))
				fail(FailedEmail{
					VolunteerID:   r.volunteer.ID,
					VolunteerName: name,
					Email:         r.volunteer.Email,
					Error:         "the email sent but could not be recorded, so this volunteer may be emailed again: " + err.Error(),
				})
				continue
			}
		}

		succeed(SentEmail{
			VolunteerID:   r.volunteer.ID,
			VolunteerName: name,
			Email:         r.volunteer.Email,
		})
	}

	logger.Info("Availability send finished",
		zap.String("rota_id", rota.ID),
		zap.String("mode", string(params.Mode)),
		zap.Int("sent", len(report.Sent)),
		zap.Int("failed", len(report.Failed)))

	return report, nil
}

// recipient pairs a request with the volunteer it belongs to, which every send
// needs together: the token comes from one and the address from the other.
type recipient struct {
	request   db.AvailabilityRequestV2
	volunteer model.Volunteer
}

// selectRecipients applies the mode's rule for who gets an email. It is the only
// thing that differs between a round send, a resend and a reminder, so the three
// rules are here side by side where they can be compared.
func selectRecipients(
	ctx context.Context,
	database AvailabilityStore,
	rota *db.Rotation,
	requests []db.AvailabilityRequestV2,
	volunteers []model.Volunteer,
	params SendParams,
) ([]recipient, error) {
	if params.Mode == SendModeResend {
		for _, r := range requests {
			if r.VolunteerID != params.VolunteerID {
				continue
			}
			volunteer, known := findVolunteer(volunteers, params.VolunteerID)
			if !known {
				return nil, wrapf(ErrNotFound, "volunteer %s is not on the roster, so there is no address to send to", params.VolunteerID)
			}
			return []recipient{{request: r, volunteer: volunteer}}, nil
		}
		return nil, wrapf(ErrNotFound, "volunteer %s was not asked for this rota, so there is no link to resend", params.VolunteerID)
	}

	answered, err := groupsThatHaveAnswered(ctx, database, rota, requests, volunteers, params.Mode)
	if err != nil {
		return nil, err
	}

	recipients := make([]recipient, 0, len(requests))
	for _, r := range requests {
		volunteer, known := findVolunteer(volunteers, r.VolunteerID)
		// Someone off the roster has no address, and someone who has stopped
		// volunteering should not be chased for a rota they are not on. Neither
		// is a failure to report — the round simply is not about them.
		if !known || !utils.IsActive(volunteer) {
			continue
		}

		switch params.Mode {
		case SendModeRound:
			// Already sent means they have their link; a round send is not a
			// resend.
			if r.SentAt != "" {
				continue
			}
		case SendModeReminder:
			// Nothing to remind someone of who was never asked.
			if r.SentAt == "" {
				continue
			}
			// A group answers as a unit, so a reply from any member is the
			// answer we were chasing (ADR 0004).
			if answered[groupKey(volunteer)] {
				continue
			}
		default:
			return nil, wrapf(ErrInvalidInput, "unknown send mode %q", params.Mode)
		}

		recipients = append(recipients, recipient{request: r, volunteer: volunteer})
	}
	return recipients, nil
}

// groupsThatHaveAnswered reports which groups already hold a reply, keyed the
// way groupKey keys them. Only reminders ask, so only reminders pay for the
// read.
func groupsThatHaveAnswered(
	ctx context.Context,
	database AvailabilityStore,
	rota *db.Rotation,
	requests []db.AvailabilityRequestV2,
	volunteers []model.Volunteer,
	mode SendMode,
) (map[string]bool, error) {
	answered := make(map[string]bool)
	if mode != SendModeReminder {
		return answered, nil
	}

	requestIDs := make([]string, 0, len(requests))
	for _, r := range requests {
		requestIDs = append(requestIDs, r.ID)
	}
	latest, err := database.GetLatestAvailability(ctx, requestIDs, rotaCutoff(rota))
	if err != nil {
		return nil, fmt.Errorf("failed to read availability: %w", err)
	}

	for _, r := range requests {
		if _, replied := latest[r.ID]; !replied {
			continue
		}
		if volunteer, known := findVolunteer(volunteers, r.VolunteerID); known {
			answered[groupKey(volunteer)] = true
		}
	}
	return answered, nil
}

// composeEmail writes the email for a mode. A reminder differs only in saying it
// is one — the link, the deadline and the promise that answers can be changed
// are the same message either way.
func composeEmail(mode SendMode, firstName, link, deadline string) (subject, body string) {
	if mode == SendModeReminder {
		return fmt.Sprintf("Reminder: Ilford drop-in availability (please complete by %s)", deadline),
			fmt.Sprintf("Hey %s\n\nThis is a reminder to please let us know your availability.\n%s\n\nDeadline for responses is %s when we will create the rota.\nYou can change your response as many times as you like before the deadline.\n\nThanks\nThe Ilford drop-in team\n",
				firstName, link, deadline)
	}
	return fmt.Sprintf("Ilford drop-in availability (please complete by %s)", deadline),
		fmt.Sprintf("Hey %s\n\nPlease use this link to let us know your availability.\n%s\n\nDeadline for responses is %s when we will create the rota.\nYou can change your response as many times as you like before the deadline.\n\nThanks\nThe Ilford drop-in team\n",
			firstName, link, deadline)
}
