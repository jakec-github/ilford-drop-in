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
// Publishes run one at a time on a single goroutine, so two quick changes cannot
// interleave their edits on the sheet. Triggers fold together: a publish reads
// the rota as it is when it starts, so any number of changes landing while one
// runs need one more publish between them, not one each.
//
// Best effort: a failure is logged and not retried. It never reaches the
// request that triggered it — the rota is the database, which has already
// changed by then, and the sheet is a copy of it for the few who prefer one.
// Every publish writes the whole rota as it stands, so the next change, or the
// next restart, puts right whatever a failed one missed.
type SheetPublisher struct {
	publish RotaPublishFunc
	logger  *zap.Logger
	wake    chan struct{}
}

// NewSheetPublisher starts the publisher, which runs until ctx is done.
func NewSheetPublisher(ctx context.Context, publish RotaPublishFunc, logger *zap.Logger) *SheetPublisher {
	p := &SheetPublisher{publish: publish, logger: logger, wake: make(chan struct{}, 1)}
	go p.run(ctx)
	return p
}

// Trigger asks for a publish and returns at once. Safe to call on a nil
// publisher, which is a server that does not publish — dev mode, where there is
// no sheet to write.
func (p *SheetPublisher) Trigger() {
	if p == nil {
		return
	}
	select {
	case p.wake <- struct{}{}:
	default:
		// A publish is already pending, and it will read this change too.
	}
}

func (p *SheetPublisher) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-p.wake:
			p.publishOnce(ctx)
		}
	}
}

func (p *SheetPublisher) publishOnce(ctx context.Context) {
	if err := p.publish(ctx); err != nil {
		p.logger.Warn("Publishing the rota to the sheet failed; it will be brought up to date by the next change", zap.Error(err))
	}
}
