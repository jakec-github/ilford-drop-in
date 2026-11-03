// Package anthropic is the Provider of Claude models, on the Claude API.
package anthropic

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/jakechorley/ilford-drop-in/pkg/core/llm"
)

// Provider answers each tier with a Claude model. The prices are the list
// prices on 2026-11-01. The translators do not cache prompts (ADR 0015), so the
// cache prices are there for when they do; Haiku's are taken as the usual
// tenth and five-quarters of its input price.
var Provider = llm.Provider{
	Name:   "anthropic",
	KeyEnv: "ANTHROPIC_API_KEY",
	Tiers: map[llm.Tier]llm.TierModel{
		llm.Small:  {ID: "claude-haiku-5-5", Price: llm.Price{Input: 0.10, Output: 0.50, CacheRead: 0.01, CacheWrite: 0.125}},
		llm.Medium: {ID: "claude-sonnet-5-5", Price: llm.Price{Input: 2, Output: 10, CacheRead: 0.20, CacheWrite: 2.50}},
		llm.Large:  {ID: "claude-opus-5-5", Price: llm.Price{Input: 4, Output: 20, CacheRead: 0.20, CacheWrite: 5}},
	},
	New: func(apiKey, id string, o llm.Options) llm.Model {
		return newModel(id, o, option.WithAPIKey(apiKey))
	},
}

// maxTokens bounds one reply. A ten-entry pattern is well under it, and a
// rule is one line.
const maxTokens = 2000

// model is one Claude model, asked at an effort.
type model struct {
	client       sdk.Client
	id           string
	effort       sdk.OutputConfigEffort
	showThinking bool
}

func newModel(id string, o llm.Options, opts ...option.RequestOption) *model {
	return &model{
		// No retries of the SDK's own: llm.Provider.Ask retries every provider
		// alike.
		client:       sdk.NewClient(append([]option.RequestOption{option.WithMaxRetries(0)}, opts...)...),
		id:           id,
		effort:       sdk.OutputConfigEffort(o.Effort),
		showThinking: o.ShowThinking,
	}
}

func (m *model) Reply(ctx context.Context, prompt *llm.Prompt, turns []llm.Turn) (llm.Response, error) {
	messages := make([]sdk.MessageParam, 0, len(turns))
	for _, t := range turns {
		block := sdk.NewTextBlock(t.Text)
		if t.Role == llm.Assistant {
			messages = append(messages, sdk.NewAssistantMessage(block))
		} else {
			messages = append(messages, sdk.NewUserMessage(block))
		}
	}

	params := sdk.MessageNewParams{
		Model:     sdk.Model(m.id),
		MaxTokens: maxTokens,
		System:    []sdk.TextBlockParam{{Text: prompt.System}},
		Messages:  messages,
		OutputConfig: sdk.OutputConfigParam{
			Effort: m.effort,
			Format: sdk.JSONOutputFormatParam{Schema: prompt.Schema},
		},
	}
	if m.showThinking {
		// Adaptive is the default; display is what changes. Without it the
		// thinking comes back empty.
		params.Thinking = sdk.ThinkingConfigParamUnion{OfAdaptive: &sdk.ThinkingConfigAdaptiveParam{
			Display: sdk.ThinkingConfigAdaptiveDisplaySummarized,
		}}
	}
	resp, err := m.client.Messages.New(ctx, params)
	var apiErr *sdk.Error
	if errors.As(err, &apiErr) {
		var header http.Header
		if apiErr.Response != nil {
			header = apiErr.Response.Header
		}
		return llm.Response{}, llm.Refusal(err, apiErr.StatusCode, header)
	}
	if err != nil {
		return llm.Response{}, err
	}

	var text, thinking strings.Builder
	for _, block := range resp.Content {
		switch b := block.AsAny().(type) {
		case sdk.TextBlock:
			text.WriteString(b.Text)
		case sdk.ThinkingBlock:
			thinking.WriteString(b.Thinking)
		}
	}
	out := llm.Response{Text: text.String(), Thinking: thinking.String(), Usage: llm.Usage{
		InputTokens:      resp.Usage.InputTokens,
		OutputTokens:     resp.Usage.OutputTokens,
		CacheReadTokens:  resp.Usage.CacheReadInputTokens,
		CacheWriteTokens: resp.Usage.CacheCreationInputTokens,
	}}

	// Neither stop leaves a reply that is sure to match the schema.
	switch resp.StopReason {
	case sdk.StopReasonRefusal, sdk.StopReasonMaxTokens:
		return out, fmt.Errorf("%w: stopped for %s", llm.ErrUnusableReply, resp.StopReason)
	}
	return out, nil
}
