package llm

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Gap is the least time between the starts of two calls to one model. Most
// Mistral models allow one request a second, and one rate for every provider
// keeps them alike.
const Gap = time.Second

// retries is how many more times a call refused for now is asked.
const retries = 3

// firstBackoff is the wait before the first retry when the API says none;
// each later one doubles it.
const firstBackoff = time.Second

// RefusedForNow is a call the API turned away for the moment — rate limited,
// overloaded, or a fault on its side — that may answer if asked again. After
// is how long the API asked for before then, or zero where it did not say.
type RefusedForNow struct {
	Err   error
	After time.Duration
}

func (e *RefusedForNow) Error() string { return e.Err.Error() }
func (e *RefusedForNow) Unwrap() error { return e.Err }

// Refusal is err as a RefusedForNow where an API answering status, with
// header, has refused for now; otherwise err as it is.
func Refusal(err error, status int, header http.Header) error {
	switch {
	case status == http.StatusRequestTimeout, status == http.StatusConflict,
		status == http.StatusTooManyRequests, status >= 500:
		return &RefusedForNow{Err: err, After: retryAfter(header)}
	}
	return err
}

// retryAfter is the wait an API asked for, in retry-after-ms or retry-after
// (seconds or a date); zero if it asked for none.
func retryAfter(h http.Header) time.Duration {
	if ms, err := strconv.ParseFloat(h.Get("Retry-After-Ms"), 64); err == nil && ms > 0 {
		return time.Duration(ms * float64(time.Millisecond))
	}
	v := h.Get("Retry-After")
	if s, err := strconv.ParseFloat(v, 64); err == nil && s > 0 {
		return time.Duration(s * float64(time.Second))
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(time.Until(t), 0)
	}
	return 0
}

// steady is a Model asked no more often than its pace allows, and asked again
// when refused for now.
type steady struct {
	model Model
	pace  *pace
	// sleep waits d, or until ctx ends; a test's waits nothing.
	sleep func(ctx context.Context, d time.Duration) error
}

func (s *steady) Reply(ctx context.Context, prompt *Prompt, turns []Turn) (Response, error) {
	for attempt := 0; ; attempt++ {
		if err := s.sleep(ctx, s.pace.slot()); err != nil {
			return Response{}, err
		}
		resp, err := s.model.Reply(ctx, prompt, turns)
		var refused *RefusedForNow
		if !errors.As(err, &refused) || attempt == retries {
			return resp, err
		}
		wait := refused.After
		if wait == 0 {
			// Full jitter, so callers refused together don't come back together.
			wait = time.Duration(rand.Int64N(int64(firstBackoff<<attempt))) + 1
		}
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < wait {
			return resp, err
		}
		if err := s.sleep(ctx, wait); err != nil {
			return resp, fmt.Errorf("%w (and %w while waiting to ask again)", refused, err)
		}
	}
}

// pace hands out start times at least Gap apart.
type pace struct {
	mu   sync.Mutex
	gap  time.Duration
	next time.Time
}

// slot books the next start and is how long until it.
func (p *pace) slot() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	at := now
	if p.next.After(now) {
		at = p.next
	}
	p.next = at.Add(p.gap)
	return at.Sub(now)
}

// paces is each model's pace, by provider and model id, shared by every Model
// asking it in this process.
var paces sync.Map

func paceOf(provider, id string) *pace {
	p, _ := paces.LoadOrStore(provider+"/"+id, &pace{gap: Gap})
	return p.(*pace)
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
