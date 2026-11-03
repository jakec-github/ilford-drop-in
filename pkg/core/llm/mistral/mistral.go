// Package mistral is the Provider of Mistral's models, on its chat completions
// API. Mistral has no official Go SDK, so this is a small client of its own
// over net/http rather than an unofficial dependency.
package mistral

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jakechorley/ilford-drop-in/pkg/core/llm"
)

// Provider answers each tier with a Mistral model, pinned to a version so a
// report's model id says what ran: Small 4, Medium 3.5 and Large 3. The prices
// are the list prices on 2026-11-02. Mistral charges nothing to write its
// cache, and a tenth of the input price to read it.
//
// Large 3 costs less than Medium 3.5 and does not reason; Large 4 is newer but
// in preview. Which is the large tier is for the evals to settle.
var Provider = llm.Provider{
	Name:   "mistral",
	KeyEnv: "MISTRAL_API_KEY",
	Tiers: map[llm.Tier]llm.TierModel{
		llm.Small:  {ID: "mistral-small-2603", Price: llm.Price{Input: 0.15, Output: 0.60, CacheRead: 0.015}},
		llm.Medium: {ID: "mistral-medium-2604", Price: llm.Price{Input: 1.50, Output: 7.50, CacheRead: 0.15}},
		llm.Large:  {ID: "mistral-large-2512", Price: llm.Price{Input: 0.50, Output: 1.50, CacheRead: 0.05}},
	},
	New: func(apiKey, id string, o llm.Options) llm.Model {
		return &model{url: endpoint, apiKey: apiKey, id: id, options: o, client: http.DefaultClient,
			pace: paceOf(id), sleep: sleep}
	},
}

const endpoint = "https://api.mistral.ai/v1/chat/completions"

// maxTokens bounds one reply, thinking included. At high effort Small 4 can
// think past 2000 tokens on one translation and stop before it answers, so
// the bound is twice Anthropic's; the 30s translation limit still holds.
const maxTokens = 4000

// model is one Mistral model, asked at an effort, no more than once a gap, and
// asked again when Mistral turns a call away for now.
type model struct {
	url, apiKey, id string
	options         llm.Options
	client          *http.Client
	pace            *pace
	// sleep waits d, or until ctx ends; a test's waits nothing.
	sleep func(ctx context.Context, d time.Duration) error
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type request struct {
	Model           string    `json:"model"`
	Messages        []message `json:"messages"`
	MaxTokens       int       `json:"max_tokens"`
	ReasoningEffort string    `json:"reasoning_effort,omitempty"`
	ResponseFormat  struct {
		Type       string `json:"type"`
		JSONSchema struct {
			Name   string         `json:"name"`
			Schema map[string]any `json:"schema"`
			Strict bool           `json:"strict"`
		} `json:"json_schema"`
	} `json:"response_format"`
}

type response struct {
	Choices []struct {
		Message struct {
			// Content is a string, or with reasoning a list of chunks: text,
			// and thinking holding chunks of its own.
			Content json.RawMessage `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
}

type chunk struct {
	Type     string  `json:"type"`
	Text     string  `json:"text"`
	Thinking []chunk `json:"thinking"`
}

func (m *model) Reply(ctx context.Context, prompt *llm.Prompt, turns []llm.Turn) (llm.Response, error) {
	for attempt := 0; ; attempt++ {
		if err := m.sleep(ctx, m.pace.slot()); err != nil {
			return llm.Response{}, err
		}
		resp, err := m.ask(ctx, prompt, turns)
		var b *busy
		if !errors.As(err, &b) || attempt == retries {
			return resp, err
		}
		wait := b.after
		if wait == 0 {
			wait = backoff(attempt)
		}
		// A retry that cannot start in time is not worth waiting for.
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < wait {
			return resp, err
		}
		if err := m.sleep(ctx, wait); err != nil {
			return resp, fmt.Errorf("%w (and %w while waiting to ask again)", b, err)
		}
	}
}

// nonReasoning is the models that do not reason, and refuse to be told how
// hard to.
var nonReasoning = map[string]bool{"mistral-large-2512": true}

// reasoningEffort is effort as Mistral takes it for model id. Its reasoning
// models take only none and high, so low is no reasoning at all and anything
// more is high; nothing is sent where there is no effort or the model does not
// reason.
func reasoningEffort(id, effort string) string {
	switch {
	case effort == "" || nonReasoning[id]:
		return ""
	case effort == "low":
		return "none"
	default:
		return "high"
	}
}

// ask is one call to Mistral.
func (m *model) ask(ctx context.Context, prompt *llm.Prompt, turns []llm.Turn) (llm.Response, error) {
	req := request{Model: m.id, MaxTokens: maxTokens, ReasoningEffort: reasoningEffort(m.id, m.options.Effort)}
	req.Messages = append(req.Messages, message{Role: "system", Content: prompt.System})
	for _, t := range turns {
		req.Messages = append(req.Messages, message{Role: string(t.Role), Content: t.Text})
	}
	req.ResponseFormat.Type = "json_schema"
	req.ResponseFormat.JSONSchema.Name = prompt.Name
	req.ResponseFormat.JSONSchema.Schema = prompt.Schema
	req.ResponseFormat.JSONSchema.Strict = true

	body, err := json.Marshal(req)
	if err != nil {
		return llm.Response{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.url, bytes.NewReader(body))
	if err != nil {
		return llm.Response{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+m.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	httpResp, err := m.client.Do(httpReq)
	if err != nil {
		return llm.Response{}, err
	}
	defer httpResp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(httpResp.Body, 1<<20))
	if err != nil {
		return llm.Response{}, err
	}
	if httpResp.StatusCode != http.StatusOK {
		err := fmt.Errorf("mistral: %s: %s", httpResp.Status, strings.TrimSpace(string(data[:min(len(data), 500)])))
		return llm.Response{}, refusal(err, httpResp.StatusCode, httpResp.Header)
	}

	var resp response
	if err := json.Unmarshal(data, &resp); err != nil {
		return llm.Response{}, fmt.Errorf("mistral: reading the response: %w", err)
	}
	if len(resp.Choices) == 0 {
		return llm.Response{}, fmt.Errorf("mistral: the response has no choices")
	}
	choice := resp.Choices[0]
	text, thinking, err := readContent(choice.Message.Content)
	if err != nil {
		return llm.Response{}, err
	}
	if !m.options.ShowThinking {
		thinking = ""
	}
	// Mistral reports no cache counts here, so they stay zero.
	out := llm.Response{Text: text, Thinking: thinking, Usage: llm.Usage{
		InputTokens:  resp.Usage.PromptTokens,
		OutputTokens: resp.Usage.CompletionTokens,
	}}

	// None of these leaves a reply that is sure to match the schema. Mistral
	// has no refusal stop; a refusal is a reply the parser refuses.
	switch choice.FinishReason {
	case "length", "model_length", "error":
		return out, fmt.Errorf("%w: stopped for %s", llm.ErrUnusableReply, choice.FinishReason)
	}
	return out, nil
}

// readContent is a message's text and thinking: a string is all text, and a
// list of chunks is text chunks and thinking chunks.
func readContent(raw json.RawMessage) (text, thinking string, err error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", "", nil
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, "", nil
	}
	var chunks []chunk
	if err := json.Unmarshal(raw, &chunks); err != nil {
		return "", "", fmt.Errorf("mistral: reading the message: %w", err)
	}
	var t, th strings.Builder
	for _, c := range chunks {
		switch c.Type {
		case "text":
			t.WriteString(c.Text)
		case "thinking":
			for _, inner := range c.Thinking {
				if inner.Type == "text" {
					th.WriteString(inner.Text)
				}
			}
		}
	}
	return t.String(), th.String(), nil
}
