package api

import (
	"sync"
	"time"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/core/services"
)

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

	volunteers, err := c.inner.ListVolunteers(cfg)
	if err != nil {
		return nil, err
	}

	c.cached = volunteers
	c.fetchedAt = time.Now()
	return volunteers, nil
}
