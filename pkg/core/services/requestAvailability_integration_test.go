package services

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/pkg/clients/formsclient"
	"github.com/jakechorley/ilford-drop-in/pkg/core/model"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
	"github.com/jakechorley/ilford-drop-in/pkg/db/dbtest"
)

// barrierFormsClient holds every CreateAvailabilityForm call until all
// expected callers have arrived. Form creation sits between a run's read of
// the existing requests and its insert, so the barrier guarantees both
// concurrent runs validate against the same empty request set — the widest
// possible H3 race window.
type barrierFormsClient struct {
	arrivals sync.WaitGroup
}

func (b *barrierFormsClient) CreateAvailabilityForm(volunteerName string, shiftDates []time.Time, closedDates []time.Time) (*formsclient.AvailabilityFormResult, error) {
	b.arrivals.Done()
	b.arrivals.Wait()
	formID := uuid.New().String()
	return &formsclient.AvailabilityFormResult{
		FormID:       formID,
		ResponderURI: "https://forms.google.com/" + formID,
	}, nil
}

// concurrentGmailClient records sent emails behind a mutex so racing runs can
// share it.
type concurrentGmailClient struct {
	mu   sync.Mutex
	sent []string
}

func (c *concurrentGmailClient) SendEmail(to, subject, body string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, to)
	return nil
}

// TestRequestAvailabilityConcurrentRuns covers hazard H3 (issue #41): two
// concurrent RequestAvailability runs both see a volunteer as unrequested and
// both create a form, but the unique constraint on availability_request
// (rota_id, volunteer_id) sinks the losing run's insert wholesale — it
// surfaces ErrConflict, records nothing, and never reaches its email loop, so
// the volunteer gets exactly one request row and one email.
func TestRequestAvailabilityConcurrentRuns(t *testing.T) {
	database, _ := dbtest.New(t)
	ctx := context.Background()

	rotaID := uuid.New().String()
	require.NoError(t, database.InsertRotationAndShifts(ctx, &db.Rotation{ID: rotaID}, []db.Shift{
		{ID: uuid.New().String(), Date: "2026-08-02", RotaID: rotaID},
	}))

	volunteers := &mockVolunteerClient{
		volunteers: []model.Volunteer{
			{ID: "vol-1", FirstName: "John", LastName: "Doe", Email: "john@example.com", Status: "Active"},
		},
	}
	forms := &barrierFormsClient{}
	forms.arrivals.Add(2)
	gmail := &concurrentGmailClient{}

	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _, errs[i] = RequestAvailability(ctx, database, volunteers, forms, gmail, testCfg, zap.NewNop(), "2026-07-31", false)
		}(i)
	}
	wg.Wait()

	var successes, conflicts int
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case assert.ErrorIs(t, err, ErrConflict):
			conflicts++
		}
	}
	assert.Equal(t, 1, successes, "exactly one run must complete")
	assert.Equal(t, 1, conflicts, "the losing run must fail with ErrConflict")

	requests, err := database.GetAvailabilityRequestsByRotaID(ctx, rotaID)
	require.NoError(t, err)
	require.Len(t, requests, 1, "the volunteer must have exactly one request")
	assert.Equal(t, "vol-1", requests[0].VolunteerID)
	assert.True(t, requests[0].FormSent, "the winning run must mark its request sent")

	assert.Equal(t, []string{"john@example.com"}, gmail.sent, "the volunteer must be emailed exactly once")
}
