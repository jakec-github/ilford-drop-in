package mistral

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jakechorley/ilford-drop-in/pkg/core/llm"
)

// reply is one answer of a scripted API: a status, and its Retry-After.
type reply struct {
	status     int
	retryAfter string
}

// scriptedAPI answers each call with the next of its replies, and a
// successful answer once they run out. It is a model that waits for nothing
// and records the waits it was asked for, and how many calls it made.
func scriptedAPI(t *testing.T, replies ...reply) (*model, *[]time.Duration, *int) {
	t.Helper()
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if len(replies) == 0 {
			io.WriteString(w, answer(`"{}"`, "stop"))
			return
		}
		next := replies[0]
		replies = replies[1:]
		if next.retryAfter != "" {
			w.Header().Set("Retry-After", next.retryAfter)
		}
		w.WriteHeader(next.status)
		io.WriteString(w, `{"object":"error","message":"Rate limit exceeded","type":"rate_limited"}`)
	}))
	t.Cleanup(server.Close)

	var waits []time.Duration
	m := Provider.New("test-key", "mistral-small-2603", llm.Options{}).(*model)
	m.url = server.URL
	m.pace = &pace{}
	m.sleep = func(_ context.Context, d time.Duration) error {
		if d > 0 {
			waits = append(waits, d)
		}
		return nil
	}
	return m, &waits, &calls
}

func askOnce(m *model, ctx context.Context) (llm.Response, error) {
	return m.Reply(ctx, testPrompt, []llm.Turn{{Role: llm.User, Text: "x"}})
}

func TestModel_AsksAgainWhenTurnedAwayForNow(t *testing.T) {
	m, waits, calls := scriptedAPI(t, reply{429, "3"}, reply{503, ""})
	resp, err := askOnce(m, context.Background())
	if err != nil || resp.Text != "{}" {
		t.Fatalf("got %q, %v; want the third call's reply", resp.Text, err)
	}
	if *calls != 3 {
		t.Errorf("calls = %d, want 3", *calls)
	}
	if w := *waits; len(w) != 2 || w[0] != 3*time.Second || w[1] <= 0 || w[1] > 2*firstBackoff {
		t.Errorf("waits = %v, want the 3s asked for, then a backoff of up to %v", w, 2*firstBackoff)
	}
}

func TestModel_GivesUpAfterThreeRetries(t *testing.T) {
	m, _, calls := scriptedAPI(t, reply{429, ""}, reply{429, ""}, reply{429, ""}, reply{429, ""})
	_, err := askOnce(m, context.Background())
	var b *busy
	if !errors.As(err, &b) || !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "Rate limit exceeded") {
		t.Fatalf("err = %v, want the last 429, with Mistral's message", err)
	}
	if *calls != 1+retries {
		t.Errorf("calls = %d, want %d", *calls, 1+retries)
	}
}

func TestModel_ABadRequestIsNotAskedAgain(t *testing.T) {
	m, _, calls := scriptedAPI(t, reply{400, ""}, reply{401, ""})
	if _, err := askOnce(m, context.Background()); err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("err = %v, want the 400", err)
	}
	if *calls != 1 {
		t.Errorf("calls = %d, want 1", *calls)
	}
}

func TestModel_DoesNotWaitPastTheDeadline(t *testing.T) {
	m, waits, calls := scriptedAPI(t, reply{429, "60"})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := askOnce(m, ctx); err == nil {
		t.Fatal("no error; want the 429, returned at once")
	}
	if *calls != 1 || len(*waits) != 0 {
		t.Errorf("calls = %d, waits = %v; want one call and no wait", *calls, *waits)
	}
}

func TestPace_SpacesCallsToOneModelAndNotToAnother(t *testing.T) {
	p := &pace{gap: gap}
	if d := p.slot(); d != 0 {
		t.Errorf("first slot waits %v, want none", d)
	}
	if d := p.slot(); d < gap-50*time.Millisecond || d > gap {
		t.Errorf("second slot waits %v, want about %v", d, gap)
	}
	if d := p.slot(); d < 2*gap-50*time.Millisecond || d > 2*gap {
		t.Errorf("third slot waits %v, want about %v", d, 2*gap)
	}

	small := Provider.New("key", "mistral-small-2603", llm.Options{}).(*model)
	again := Provider.New("other-key", "mistral-small-2603", llm.Options{}).(*model)
	medium := Provider.New("key", "mistral-medium-2604", llm.Options{}).(*model)
	if small.pace != again.pace {
		t.Error("one model has two paces")
	}
	if small.pace == medium.pace {
		t.Error("two models share a pace")
	}
}

func TestRetryAfter_ReadsSecondsAndDates(t *testing.T) {
	for v, want := range map[string]time.Duration{
		"":                              0,
		"2":                             2 * time.Second,
		"0.5":                           500 * time.Millisecond,
		"Wed, 21 Oct 2015 07:28:00 GMT": 0,
		"soon":                          0,
	} {
		if got := retryAfter(http.Header{"Retry-After": {v}}); got != want {
			t.Errorf("%q: %v, want %v", v, got, want)
		}
	}
}
