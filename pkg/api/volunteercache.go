package api

import (
	"sync"
	"time"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

// minRefreshInterval limits how often RefreshVolunteers may bypass the TTL,
// so requests probing unknown volunteer IDs can't turn every miss into a
// Google Sheets round trip.
const minRefreshInterval = 10 * time.Second

// VolunteerRefresher is optionally implemented by volunteer clients whose
// results may be stale, to fetch a fresh roster when a lookup misses.
type VolunteerRefresher interface {
	RefreshVolunteers(cfg *config.Config) ([]model.Volunteer, error)
}

// cachingVolunteerClient wraps a VolunteerClient with a TTL cache. Calendar
// clients poll unattended, and without this every poll would cost a Google
// Sheets round trip. The volunteer roster changes rarely, so brief staleness
// is harmless. Server-only concern: the CLI keeps fetching fresh.
type cachingVolunteerClient struct {
	inner services.VolunteerClient
	ttl   time.Duration

	mu        sync.Mutex
	cached    []model.Volunteer
	fetchedAt time.Time
}

// NewCachingVolunteerClient wraps inner with a TTL cache
func NewCachingVolunteerClient(inner services.VolunteerClient, ttl time.Duration) services.VolunteerClient {
	return &cachingVolunteerClient{inner: inner, ttl: ttl}
}

func (c *cachingVolunteerClient) ListVolunteers(cfg *config.Config) ([]model.Volunteer, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cached != nil && time.Since(c.fetchedAt) < c.ttl {
		return c.cached, nil
	}
	return c.fetchLocked(cfg)
}

// RefreshVolunteers bypasses the TTL so a lookup miss can be retried against
// the source of truth (e.g. a volunteer added since the cache was filled),
// but never refetches more than once per minRefreshInterval.
func (c *cachingVolunteerClient) RefreshVolunteers(cfg *config.Config) ([]model.Volunteer, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cached != nil && time.Since(c.fetchedAt) < minRefreshInterval {
		return c.cached, nil
	}
	return c.fetchLocked(cfg)
}

// fetchLocked fetches from the inner client and fills the cache. Callers must
// hold c.mu.
func (c *cachingVolunteerClient) fetchLocked(cfg *config.Config) ([]model.Volunteer, error) {
	volunteers, err := c.inner.ListVolunteers(cfg)
	if err != nil {
		return nil, err
	}

	c.cached = volunteers
	c.fetchedAt = time.Now()
	return volunteers, nil
}
