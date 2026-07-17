package api

import (
	"sync"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
)

// volunteerStore holds the volunteer roster in memory. The roster is populated
// from the volunteer sheet using the server's service account (see sync.go and
// cmd/server/main.go): once at startup and again on each admin-triggered sync,
// and served verbatim in between. It never fetches on its own, so between syncs
// a volunteer added to the sheet 404s until an admin syncs — an accepted
// trade-off (see docs/oidc_admin_sync_plan.md).
type volunteerStore struct {
	mu     sync.RWMutex
	cached []model.Volunteer
}

// NewVolunteerStore returns an empty volunteer store. It satisfies
// services.VolunteerClient, so it can back the read paths, but serves nothing
// until the startup populate (or a sync) calls Replace.
func NewVolunteerStore() *volunteerStore {
	return &volunteerStore{}
}

// ListVolunteers returns the roster loaded by the last sync (or the startup
// populate). Before either has run it returns an empty slice (not an error):
// read paths degrade to no volunteers rather than failing. The cfg argument is
// unused — kept to satisfy services.VolunteerClient.
func (s *volunteerStore) ListVolunteers(_ *config.Config) ([]model.Volunteer, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cached, nil
}

// Replace swaps the whole roster for the freshly synced one. It is the only way
// data enters the store.
func (s *volunteerStore) Replace(volunteers []model.Volunteer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cached = volunteers
}
