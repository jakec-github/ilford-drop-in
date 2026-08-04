package services

import (
	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
)

// mockVolunteerClient stands in for the roster. Nearly every service in this
// package takes one, so it lives next to the interface rather than in whichever
// test file happened to need it first — it used to sit in the Forms
// availability tests, and outlived them (issue #80).
type mockVolunteerClient struct {
	volunteers []model.Volunteer
	err        error
}

func (m *mockVolunteerClient) ListVolunteers(cfg *config.Config) ([]model.Volunteer, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.volunteers, nil
}
