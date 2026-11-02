package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/jakechorley/ilford-drop-in/pkg/core/llm"
)

// testPrompt is any prompt: the fake API does not read it.
var testPrompt = llm.NewPrompt("test", "Reply in JSON.", []byte(`{"type":"object"}`))

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

func TestModel_LeavesThinkingAsTheModelDefaultsUnlessShown(t *testing.T) {
	server, last := fakeAPI(t)
	model := newModel("claude-haiku-5-5", llm.Options{Effort: "low"}, option.WithBaseURL(server.URL), option.WithAPIKey("test"))

	got, err := model.Reply(context.Background(), testPrompt, []llm.Turn{{Role: llm.User, Text: "Sundays"}})
	if err != nil {
		t.Fatalf("Reply: %v", err)
	}
	if _, asked := (*last)["thinking"]; asked {
		t.Errorf("the request sets thinking: %v", (*last)["thinking"])
	}
	if got.Text != `{"status":"ok","pattern":"p"}` || got.Usage != (llm.Usage{InputTokens: 100, OutputTokens: 40}) {
		t.Errorf("response: %+v", got)
	}
}

func TestModel_ShowingThinkingAsksForItsSummaryAndReturnsIt(t *testing.T) {
	server, last := fakeAPI(t)
	model := newModel("claude-haiku-5-5", llm.Options{Effort: "low", ShowThinking: true}, option.WithBaseURL(server.URL), option.WithAPIKey("test"))

	got, err := model.Reply(context.Background(), testPrompt, []llm.Turn{{Role: llm.User, Text: "Sundays"}})
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
