package api

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// gatedPublish is a publish a test controls: each call announces itself on
// started and then waits to be let go, answering with whatever it is handed.
type gatedPublish struct {
	started chan struct{}
	finish  chan error
	running atomic.Int32
	overlap atomic.Bool
}

func newGatedPublish() *gatedPublish {
	return &gatedPublish{started: make(chan struct{}, 10), finish: make(chan error)}
}

func (g *gatedPublish) publish(ctx context.Context) error {
	if g.running.Add(1) > 1 {
		g.overlap.Store(true)
	}
	defer g.running.Add(-1)
	g.started <- struct{}{}
	select {
	case err := <-g.finish:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *gatedPublish) awaitStart(t *testing.T) {
	t.Helper()
	select {
	case <-g.started:
	case <-time.After(2 * time.Second):
		t.Fatal("no publish started")
	}
}

func (g *gatedPublish) assertNoStart(t *testing.T) {
	t.Helper()
	select {
	case <-g.started:
		t.Fatal("a publish started that should not have")
	case <-time.After(50 * time.Millisecond):
	}
}

// A trigger publishes, and returns without waiting for it.
func TestSheetPublisherPublishesWhenTriggered(t *testing.T) {
	gate := newGatedPublish()
	publisher := NewSheetPublisher(t.Context(), gate.publish, zap.NewNop())

	publisher.Trigger()

	gate.awaitStart(t)
	gate.finish <- nil
	gate.assertNoStart(t)
}

// A publish writes the rota as it stands when it starts, so however many
// changes land while one runs, one more publish covers them all — and it waits
// for the one running, so two never edit the sheet at once.
func TestSheetPublisherFoldsTriggersIntoOneMorePublish(t *testing.T) {
	gate := newGatedPublish()
	publisher := NewSheetPublisher(t.Context(), gate.publish, zap.NewNop())

	publisher.Trigger()
	gate.awaitStart(t)
	publisher.Trigger()
	publisher.Trigger()
	publisher.Trigger()
	gate.assertNoStart(t)

	gate.finish <- nil
	gate.awaitStart(t)
	gate.finish <- nil
	gate.assertNoStart(t)

	assert.False(t, gate.overlap.Load(), "two publishes ran at once")
}

// The sheet is best effort: a failed publish is not retried, and the next
// change publishes as usual.
func TestSheetPublisherDoesNotRetryAFailure(t *testing.T) {
	gate := newGatedPublish()
	publisher := NewSheetPublisher(t.Context(), gate.publish, zap.NewNop())

	publisher.Trigger()
	gate.awaitStart(t)
	gate.finish <- errors.New("quota exceeded")
	gate.assertNoStart(t)

	publisher.Trigger()
	gate.awaitStart(t)
	gate.finish <- nil
}

// Once the server is shutting down nothing new is published.
func TestSheetPublisherStopsWithItsContext(t *testing.T) {
	gate := newGatedPublish()
	ctx, cancel := context.WithCancel(t.Context())
	publisher := NewSheetPublisher(ctx, gate.publish, zap.NewNop())

	cancel()
	publisher.Trigger()

	gate.assertNoStart(t)
}

// A server that does not publish — dev mode — has no publisher, and telling it
// about a change is harmless.
func TestNilSheetPublisherIgnoresTriggers(t *testing.T) {
	var publisher *SheetPublisher
	require.NotPanics(t, publisher.Trigger)
}
