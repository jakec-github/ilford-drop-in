package llm

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

// script is a Model that answers each call with the next of its errors, and
// nil once they run out.
type script struct {
	errs  []error
	calls int
}

func (s *script) Reply(context.Context, *Prompt, []Turn) (Response, error) {
	s.calls++
	if len(s.errs) == 0 {
		return Response{Text: "{}"}, nil
	}
	err := s.errs[0]
	s.errs = s.errs[1:]
	return Response{}, err
}

// waits is a sleep that waits for nothing and records what it was asked to.
type waits []time.Duration

func (w *waits) sleep(_ context.Context, d time.Duration) error {
	*w = append(*w, d)
	return nil
}

// retryWaits is the waits other than the pace's, which are zero here.
func (w waits) retryWaits() []time.Duration {
	var out []time.Duration
	for _, d := range w {
		if d > 0 {
			out = append(out, d)
		}
	}
	return out
}

func refused(after time.Duration) error {
	return &RefusedForNow{Err: errors.New("429 Too Many Requests"), After: after}
}

func newSteady(m Model, w *waits) *steady {
	return &steady{model: m, pace: &pace{}, sleep: w.sleep}
}

func TestSteady_AsksAgainAfterARefusalForNow(t *testing.T) {
	m := &script{errs: []error{refused(3 * time.Second), refused(0)}}
	var w waits
	resp, err := newSteady(m, &w).Reply(context.Background(), &Prompt{}, nil)
	if err != nil || resp.Text != "{}" {
		t.Fatalf("got %q, %v; want the third call's reply", resp.Text, err)
	}
	if m.calls != 3 {
		t.Errorf("calls = %d, want 3", m.calls)
	}
	got := w.retryWaits()
	if len(got) != 2 || got[0] != 3*time.Second || got[1] <= 0 || got[1] > 2*firstBackoff {
		t.Errorf("waits = %v, want the 3s the API asked for, then a backoff of up to %v", got, 2*firstBackoff)
	}
}

func TestSteady_GivesUpAfterThreeRetries(t *testing.T) {
	last := refused(0)
	m := &script{errs: []error{refused(0), refused(0), refused(0), last}}
	var w waits
	_, err := newSteady(m, &w).Reply(context.Background(), &Prompt{}, nil)
	if err != last {
		t.Fatalf("err = %v, want the last refusal", err)
	}
	if m.calls != 1+retries {
		t.Errorf("calls = %d, want %d", m.calls, 1+retries)
	}
}

func TestSteady_ReturnsOtherErrorsAtOnce(t *testing.T) {
	for _, err := range []error{ErrUnusableReply, errors.New("400 Bad Request"), context.Canceled} {
		m := &script{errs: []error{err}}
		var w waits
		if _, got := newSteady(m, &w).Reply(context.Background(), &Prompt{}, nil); got != err {
			t.Errorf("err = %v, want %v", got, err)
		}
		if m.calls != 1 {
			t.Errorf("%v: calls = %d, want 1", err, m.calls)
		}
	}
}

func TestSteady_DoesNotWaitPastTheDeadline(t *testing.T) {
	first := refused(time.Minute)
	m := &script{errs: []error{first}}
	var w waits
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := newSteady(m, &w).Reply(ctx, &Prompt{}, nil); err != first {
		t.Fatalf("err = %v, want the refusal, returned at once", err)
	}
	if m.calls != 1 || len(w.retryWaits()) != 0 {
		t.Errorf("calls = %d, waits = %v; want one call and no wait", m.calls, w.retryWaits())
	}
}

func TestPace_SpacesCallsToOneModelAndNotToAnother(t *testing.T) {
	p := &pace{gap: Gap}
	if d := p.slot(); d != 0 {
		t.Errorf("first slot waits %v, want none", d)
	}
	if d := p.slot(); d < Gap-50*time.Millisecond || d > Gap {
		t.Errorf("second slot waits %v, want about %v", d, Gap)
	}
	if d := p.slot(); d < 2*Gap-50*time.Millisecond || d > 2*Gap {
		t.Errorf("third slot waits %v, want about %v", d, 2*Gap)
	}

	if paceOf("mistral", "a") != paceOf("mistral", "a") {
		t.Error("one model has two paces")
	}
	if paceOf("mistral", "a") == paceOf("mistral", "b") || paceOf("mistral", "a") == paceOf("anthropic", "a") {
		t.Error("two models share a pace")
	}
}

func TestRefusal_IsForNowOnlyForStatusesWorthAskingAgain(t *testing.T) {
	base := errors.New("api error")
	for status, want := range map[int]bool{
		400: false, 401: false, 403: false, 404: false, 422: false,
		408: true, 409: true, 429: true, 500: true, 503: true, 529: true,
	} {
		var r *RefusedForNow
		got := errors.As(Refusal(base, status, nil), &r)
		if got != want {
			t.Errorf("%d: refused for now = %v, want %v", status, got, want)
		}
		if !errors.Is(Refusal(base, status, nil), base) {
			t.Errorf("%d: the API's error is lost", status)
		}
	}
}

func TestRefusal_ReadsTheWaitTheAPIAskedFor(t *testing.T) {
	for name, tc := range map[string]struct {
		header http.Header
		want   time.Duration
	}{
		"none":         {http.Header{}, 0},
		"seconds":      {http.Header{"Retry-After": {"2"}}, 2 * time.Second},
		"milliseconds": {http.Header{"Retry-After-Ms": {"1500"}, "Retry-After": {"2"}}, 1500 * time.Millisecond},
		"past date":    {http.Header{"Retry-After": {"Wed, 21 Oct 2015 07:28:00 GMT"}}, 0},
		"nonsense":     {http.Header{"Retry-After": {"soon"}}, 0},
	} {
		var r *RefusedForNow
		if !errors.As(Refusal(errors.New("429"), 429, tc.header), &r) {
			t.Fatalf("%s: not refused for now", name)
		}
		if r.After != tc.want {
			t.Errorf("%s: after = %v, want %v", name, r.After, tc.want)
		}
	}
}

func TestProvider_AskPacesAndRetriesTheModel(t *testing.T) {
	m := &script{}
	p := Provider{Name: "test", Tiers: map[Tier]TierModel{Small: {ID: "ask"}},
		New: func(string, string, Options) Model { return m }}
	s, ok := p.Ask("key", Small, Options{}).(*steady)
	if !ok || s.model != m || s.pace != paceOf("test", "ask") {
		t.Fatalf("Ask = %#v, want the model behind the shared pace", s)
	}
}
