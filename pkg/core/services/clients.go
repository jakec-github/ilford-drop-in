package services

import (
	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
)

// The outside world this package talks to, narrowed to what it actually uses.
// Both were declared alongside the Google Forms availability flow until that
// flow was removed (issue #80); they outlived it because neither has anything
// to do with Forms.

// VolunteerClient reads the volunteer roster. It is the Google Sheet in
// production, and the one piece of Google every command still depends on.
//
// The Roles table is a parameter because the roster names the Roles a volunteer
// holds as strings, and only the Roles the app knows are kept. Roles live in the
// database now (ADR 0006), so the caller reads them and passes them in rather
// than the roster reader reaching for a store of its own.
type VolunteerClient interface {
	ListVolunteers(cfg *config.Config, roles model.Roles) ([]model.Volunteer, error)
}

// GmailClient sends one email. The server builds one per send from the token an
// admin has just granted, so the interface is deliberately smaller than a mail
// client: nothing here outlives the request that made it.
type GmailClient interface {
	SendEmail(to, subject, body string) error
}
