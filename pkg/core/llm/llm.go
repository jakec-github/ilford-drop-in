// Package llm asks a language model for a reply in a JSON shape: a Prompt
// fixes the system prompt and the shape, and a Model gives its next reply to
// the exchange so far. What the reply means, and whether to trust it, is for
// the caller.
package llm

import (
	"context"
	"encoding/json"
	"errors"
)

// ErrUnusableReply is a reply a Model returns that cannot be read as an
// answer — a refusal, or one cut short at its token limit. It counts as a
// failed call, like a reply that is not JSON.
var ErrUnusableReply = errors.New("the model's reply cannot be used")

// Prompt is what a Model is asked under: a system prompt, the same on every
// call, and the reply shape the API's structured outputs enforce, so a reply
// always parses. Everything that varies goes in the user turn.
type Prompt struct {
	Name   string
	System string
	Schema map[string]any
}

// NewPrompt is the Prompt called name, with system as its system prompt and
// schemaJSON as its reply shape. It panics on a schema that is not JSON: the
// prompts are embedded, so that is a broken build, not a bad input.
func NewPrompt(name, system string, schemaJSON []byte) *Prompt {
	var schema map[string]any
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		panic("llm: " + name + " schema: " + err.Error())
	}
	return &Prompt{Name: name, System: system, Schema: schema}
}

// Role is who says a Turn.
type Role string

const (
	User      Role = "user"
	Assistant Role = "assistant"
)

// Turn is one message of the exchange with the model.
type Turn struct {
	Role Role
	Text string
}

// Model is a language model: given a Prompt and the exchange so far, its next
// reply. An error other than ErrUnusableReply means it could not be asked; with
// ErrUnusableReply the Response still says what the call used and thought.
type Model interface {
	Reply(ctx context.Context, prompt *Prompt, turns []Turn) (Response, error)
}

// Response is one call's reply: the JSON text the model wrote, the summary of
// its thinking where it was asked for one, and what the call used.
type Response struct {
	Text     string
	Thinking string
	Usage    Usage
}

// Usage is the tokens one call used, as the API counts them, for whatever
// measures the calls.
type Usage struct {
	InputTokens      int64 `json:"inputTokens"`
	OutputTokens     int64 `json:"outputTokens"`
	CacheReadTokens  int64 `json:"cacheReadTokens"`
	CacheWriteTokens int64 `json:"cacheWriteTokens"`
}
