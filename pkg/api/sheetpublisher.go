package api

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// RotaPublishFunc brings the rota sheet up to date with the latest allocated
// rota. Injected by the composition root, which owns the Google credentials.
type RotaPublishFunc func(ctx context.Context) error

// publishRetryDelays is how long to wait before each retry of a failed publish.
// Sheets failures are usually quota or a blip, and a publish is always of the
// rota as it stands, so a late one loses nothing.
var publishRetryDelays = []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute}

// SheetPublisher publishes the rota to its sheet in the background after every
// change that could show there (issue #191).
//
// Publishes run one at a time on a single goroutine, so two quick changes cannot
// interleave their edits on the sheet. Triggers fold together: a publish reads
// the rota as it is when it starts, so any number of changes landing while one
// runs need one more publish between them, not one each.
//
// A failure is logged and retried, and never reaches the request that triggered
// it — allocating or changing the rota has already succeeded by then, and the
// sheet is a copy of it for people who prefer one.
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
			p.publishWithRetry(ctx)
		}
	}
}

func (p *SheetPublisher) publishWithRetry(ctx context.Context) {
	for attempt := 0; ; attempt++ {
		err := p.publish(ctx)
		if err == nil {
			return
		}
		if attempt == len(publishRetryDelays) {
			p.logger.Error("Publishing the rota to the sheet failed; giving up until the next change",
				zap.Int("attempts", attempt+1), zap.Error(err))
			return
		}
		p.logger.Warn("Publishing the rota to the sheet failed; retrying",
			zap.Int("attempt", attempt+1), zap.Duration("retry_in", publishRetryDelays[attempt]), zap.Error(err))

		select {
		case <-ctx.Done():
			return
		case <-time.After(publishRetryDelays[attempt]):
		}
	}
}
