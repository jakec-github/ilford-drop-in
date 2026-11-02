package patterntranslator

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/option"
)

// fakeAPI answers every Messages request with a thinking block and a text
// block, and keeps the last request's body.
func fakeAPI(t *testing.T) (*httptest.Server, *map[string]any) {
	t.Helper()
	var last map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &last); err != nil {
			t.Errorf("request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{
			"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-haiku-5-5",
			"content": [
				{"type": "thinking", "thinking": "Sundays at 19:30, three hours.", "signature": "sig"},
				{"type": "text", "text": "{\"status\":\"ok\",\"pattern\":\"p\"}"}
			],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 100, "output_tokens": 40}
		}`)
	}))
	t.Cleanup(server.Close)
	return server, &last
}

func TestAnthropic_LeavesThinkingAsTheModelDefaultsUnlessShown(t *testing.T) {
	server, last := fakeAPI(t)
	model := newAnthropic("claude-haiku-5-5", "low", option.WithBaseURL(server.URL), option.WithAPIKey("test"))

	got, err := model.Reply(context.Background(), patternPrompt, []Turn{{Role: User, Text: "Sundays"}})
	if err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if _, asked := (*last)["thinking"]; asked {
		t.Errorf("the request sets thinking: %v", (*last)["thinking"])
	}
	if got.Text != `{"status":"ok","pattern":"p"}` || got.Usage != (Usage{InputTokens: 100, OutputTokens: 40}) {
		t.Errorf("response: %+v", got)
	}
}

func TestAnthropic_ShowingThinkingAsksForItsSummaryAndReturnsIt(t *testing.T) {
	server, last := fakeAPI(t)
	model := newAnthropic("claude-haiku-5-5", "low", option.WithBaseURL(server.URL), option.WithAPIKey("test"))
	model.ShowThinking = true

	got, err := model.Reply(context.Background(), patternPrompt, []Turn{{Role: User, Text: "Sundays"}})
	if err != nil {
		t.Fatalf("Reply: %v", err)
	}
	thinking, _ := (*last)["thinking"].(map[string]any)
	if thinking["type"] != "adaptive" || thinking["display"] != "summarized" {
		t.Errorf("thinking asked for: %v, want adaptive and summarized", (*last)["thinking"])
	}
	if got.Thinking != "Sundays at 19:30, three hours." || got.Text != `{"status":"ok","pattern":"p"}` {
		t.Errorf("response: %+v", got)
	}
}
