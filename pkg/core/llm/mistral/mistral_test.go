package mistral

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jakechorley/ilford-drop-in/pkg/core/llm"
)

var testPrompt = llm.NewPrompt("pattern", "Reply in JSON.", []byte(`{"type":"object"}`))

// fakeAPI answers every request with reply at status, and keeps the last
// request's body and headers.
func fakeAPI(t *testing.T, status int, reply string) (*model, *map[string]any, *http.Header) {
	t.Helper()
	var last map[string]any
	var header http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &last); err != nil {
			t.Errorf("request body: %v", err)
		}
		header = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	t.Cleanup(server.Close)
	m := Provider.New("test-key", "mistral-medium-2604", llm.Options{Effort: "low"}).(*model)
	m.url = server.URL
	m.pace, m.sleep = &pace{}, func(context.Context, time.Duration) error { return nil }
	return m, &last, &header
}

func answer(content, finish string) string {
	return `{"id":"c1","object":"chat.completion","model":"mistral-medium-2604","choices":[{"index":0,
		"message":{"role":"assistant","content":` + content + `},"finish_reason":"` + finish + `"}],
		"usage":{"prompt_tokens":100,"completion_tokens":40,"total_tokens":140}}`
}

func TestModel_AsksForTheSchemaWithTheSystemPromptAndTurns(t *testing.T) {
	m, last, header := fakeAPI(t, http.StatusOK, answer(`"{\"status\":\"ok\"}"`, "stop"))

	got, err := m.Reply(context.Background(), testPrompt, []llm.Turn{
		{Role: llm.User, Text: "Sundays"}, {Role: llm.Assistant, Text: "{}"}, {Role: llm.User, Text: "Again"},
	})
	if err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if got.Text != `{"status":"ok"}` || got.Usage != (llm.Usage{InputTokens: 100, OutputTokens: 40}) {
		t.Errorf("response: %+v", got)
	}

	if header.Get("Authorization") != "Bearer test-key" {
		t.Errorf("authorization: %q", header.Get("Authorization"))
	}
	body := *last
	if body["model"] != "mistral-medium-2604" || body["max_tokens"] != float64(maxTokens) || body["reasoning_effort"] != "low" {
		t.Errorf("request: %v", body)
	}
	format, _ := body["response_format"].(map[string]any)
	schema, _ := format["json_schema"].(map[string]any)
	if format["type"] != "json_schema" || schema["name"] != "pattern" || schema["strict"] != true ||
		schema["schema"].(map[string]any)["type"] != "object" {
		t.Errorf("response_format: %v", format)
	}
	messages, _ := body["messages"].([]any)
	var roles []string
	for _, msg := range messages {
		mm := msg.(map[string]any)
		roles = append(roles, mm["role"].(string)+":"+mm["content"].(string))
	}
	if strings.Join(roles, "|") != "system:Reply in JSON.|user:Sundays|assistant:{}|user:Again" {
		t.Errorf("messages: %v", roles)
	}
}

func TestModel_LeavesEffortOutWhenThereIsNone(t *testing.T) {
	m, last, _ := fakeAPI(t, http.StatusOK, answer(`"{}"`, "stop"))
	m.options.Effort = ""
	if _, err := m.Reply(context.Background(), testPrompt, []llm.Turn{{Role: llm.User, Text: "x"}}); err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if _, set := (*last)["reasoning_effort"]; set {
		t.Errorf("reasoning_effort sent: %v", (*last)["reasoning_effort"])
	}
}

// With reasoning the content is a list of chunks: the thinking is kept apart
// from the text, and only returned when it was asked for.
func TestModel_ReadsChunkedContentAndShowsThinkingOnlyWhenAsked(t *testing.T) {
	chunks := `[{"type":"thinking","thinking":[{"type":"text","text":"Sundays at 19:30."}]},{"type":"text","text":"{\"status\":"},{"type":"text","text":"\"ok\"}"}]`

	m, _, _ := fakeAPI(t, http.StatusOK, answer(chunks, "stop"))
	got, err := m.Reply(context.Background(), testPrompt, []llm.Turn{{Role: llm.User, Text: "x"}})
	if err != nil || got.Text != `{"status":"ok"}` || got.Thinking != "" {
		t.Errorf("hidden: %+v, %v", got, err)
	}

	m.options.ShowThinking = true
	got, err = m.Reply(context.Background(), testPrompt, []llm.Turn{{Role: llm.User, Text: "x"}})
	if err != nil || got.Text != `{"status":"ok"}` || got.Thinking != "Sundays at 19:30." {
		t.Errorf("shown: %+v, %v", got, err)
	}
}

func TestModel_AReplyCutShortIsUnusableButSaysWhatItUsed(t *testing.T) {
	for _, finish := range []string{"length", "model_length", "error"} {
		m, _, _ := fakeAPI(t, http.StatusOK, answer(`"{\"status\":"`, finish))
		got, err := m.Reply(context.Background(), testPrompt, []llm.Turn{{Role: llm.User, Text: "x"}})
		if !errors.Is(err, llm.ErrUnusableReply) || !strings.Contains(err.Error(), finish) {
			t.Errorf("%s: error %v, want ErrUnusableReply naming it", finish, err)
		}
		if got.Usage.OutputTokens != 40 {
			t.Errorf("%s: usage %+v", finish, got.Usage)
		}
	}
}

// An API error, such as a spending limit, means the model could not be asked:
// not an unusable reply, and the API's message is kept.
func TestModel_AnAPIErrorIsAFailedCallWithItsMessage(t *testing.T) {
	m, _, _ := fakeAPI(t, http.StatusTooManyRequests, `{"message":"Requests rate limit exceeded"}`)
	_, err := m.Reply(context.Background(), testPrompt, []llm.Turn{{Role: llm.User, Text: "x"}})
	if err == nil || errors.Is(err, llm.ErrUnusableReply) || !strings.Contains(err.Error(), "rate limit") || !strings.Contains(err.Error(), "429") {
		t.Errorf("error: %v", err)
	}
}
