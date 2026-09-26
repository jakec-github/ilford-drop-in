package api

import (
	"context"

	"go.uber.org/zap"
)

// RotaPublishFunc brings the rota sheet up to date with the latest allocated
// rota. Injected by the composition root, which owns the Google credentials.
type RotaPublishFunc func(ctx context.Context) error

// SheetPublisher publishes the rota to its sheet in the background after every
// change that could show there (issue #191).
//
// Publishes run one at a time on one goroutine, so two changes in quick
// succession cannot interleave their edits on the sheet. Triggers fold together:
// a publish writes the rota as it stands when it starts, so any number of
// changes landing while one runs need one more publish between them, not one
// each.
//
// Best effort. The rota is the database, which has already changed by the time
// anything is published; the sheet is a copy of it for the few who prefer one.
// A failure is logged and not retried, and never reaches the request that
// triggered it. Every publish writes the whole rota, so the next change, or the
// next restart, puts right whatever a failed one missed.
type SheetPublisher struct {
	ctx     context.Context
	publish RotaPublishFunc
	logger  *zap.Logger
	// wake holds at most one pending publish. A trigger finding it full has
	// nothing to add: the publish already waiting will read its change too.
	wake chan struct{}
}

// NewSheetPublisher starts the publisher, which runs until ctx is done.
func NewSheetPublisher(ctx context.Context, publish RotaPublishFunc, logger *zap.Logger) *SheetPublisher {
	p := &SheetPublisher{ctx: ctx, publish: publish, logger: logger, wake: make(chan struct{}, 1)}
	go p.run()
	return p
}

// Trigger asks for a publish and returns at once. Safe on a nil publisher,
// which is a server that does not publish.
func (p *SheetPublisher) Trigger() {
	if p == nil {
		return
	}
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *SheetPublisher) run() {
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-p.wake:
			// A trigger and shutdown can both be ready at once, and select
			// picks between them at random.
			if p.ctx.Err() != nil {
				return
			}
			if err := p.publish(p.ctx); err != nil {
				p.logger.Warn("Publishing the rota to the sheet failed; the next change will try again", zap.Error(err))
			}
		}
	}
}
