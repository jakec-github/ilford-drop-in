package api

import (
	"sync"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
)

// volunteerStore holds the volunteer roster in memory. The server no longer
// holds its own Sheets credential: the roster is populated by an admin-triggered
// sync (see sync.go) using the admin's OAuth token, and served verbatim between
// syncs. It never fetches on its own, so a restart leaves it empty until an
// admin syncs — an accepted trade-off (see docs/oidc_admin_sync_plan.md).
type volunteerStore struct {
	mu     sync.RWMutex
	cached []model.Volunteer
}

// NewVolunteerStore returns an empty volunteer store. It satisfies
// services.VolunteerClient, so it can back the read paths, but serves nothing
// until the first sync calls Replace.
func NewVolunteerStore() *volunteerStore {
	return &volunteerStore{}
}

// ListVolunteers returns the roster loaded by the last sync. Before the first
// sync it returns an empty slice (not an error): read paths degrade to no
// volunteers rather than failing. The cfg argument is unused — kept to satisfy
// services.VolunteerClient.
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
