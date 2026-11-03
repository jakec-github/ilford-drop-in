package mistral

import (
	"context"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// gap is the least time between the starts of two calls to one model: most
// Mistral models allow one request a second.
const gap = time.Second

// retries is how many more times a call Mistral turned away for now is asked.
const retries = 3

// firstBackoff is the wait before the first retry when Mistral says none;
// each later one doubles it.
const firstBackoff = time.Second

// busy is a call Mistral turned away for the moment — rate limited, or a
// fault on its side — that may answer if asked again. after is how long it
// asked for before then, or zero where it did not say.
type busy struct {
	err   error
	after time.Duration
}

func (e *busy) Error() string { return e.err.Error() }
func (e *busy) Unwrap() error { return e.err }

// refusal is err as a busy where Mistral answered status, with header, and
// may answer if asked again; otherwise err as it is.
func refusal(err error, status int, header http.Header) error {
	if status == http.StatusTooManyRequests || status >= 500 {
		return &busy{err: err, after: retryAfter(header)}
	}
	return err
}

// retryAfter is the wait asked for in Retry-After, in seconds or as a date;
// zero if none.
func retryAfter(h http.Header) time.Duration {
	v := h.Get("Retry-After")
	if s, err := strconv.ParseFloat(v, 64); err == nil && s > 0 {
		return time.Duration(s * float64(time.Second))
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(time.Until(t), 0)
	}
	return 0
}

// backoff is the wait before retry attempt+1 when Mistral asked for none:
// full jitter, so calls turned away together don't come back together.
func backoff(attempt int) time.Duration {
	return time.Duration(rand.Int64N(int64(firstBackoff<<attempt))) + 1
}

// pace hands out start times at least its gap apart.
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

// paces is each model's pace, by model id, shared by every Model asking it in
// this process: Mistral counts its limits per model.
var paces sync.Map

func paceOf(id string) *pace {
	p, _ := paces.LoadOrStore(id, &pace{gap: gap})
	return p.(*pace)
}

// sleep waits d, or until ctx ends.
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
