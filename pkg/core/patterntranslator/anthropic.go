package patterntranslator

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// maxTokens bounds one reply. A ten-entry pattern is well under it, and a
// rule is one line.
const maxTokens = 2000

// Anthropic is a Model on the Claude API.
type Anthropic struct {
	// ShowThinking asks for a summary of the model's thinking with each reply,
	// for the eval to record. The server leaves it off: nothing reads it there,
	// and the thinking the model does is the same either way.
	ShowThinking bool

	client anthropic.Client
	model  string
	effort anthropic.OutputConfigEffort
}

// NewAnthropic is a Model that asks model, at effort, with apiKey. No provider
// abstraction sits over it: the Model interface is there for tests and the dev
// stack (ADR 0015).
func NewAnthropic(apiKey, model, effort string) *Anthropic {
	return newAnthropic(model, effort, option.WithAPIKey(apiKey))
}

func newAnthropic(model, effort string, opts ...option.RequestOption) *Anthropic {
	return &Anthropic{
		client: anthropic.NewClient(opts...),
		model:  model,
		effort: anthropic.OutputConfigEffort(effort),
	}
}

func (a *Anthropic) Reply(ctx context.Context, prompt *Prompt, turns []Turn) (Response, error) {
	messages := make([]anthropic.MessageParam, 0, len(turns))
	for _, t := range turns {
		block := anthropic.NewTextBlock(t.Text)
		if t.Role == Assistant {
			messages = append(messages, anthropic.NewAssistantMessage(block))
		} else {
			messages = append(messages, anthropic.NewUserMessage(block))
		}
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(a.model),
		MaxTokens: maxTokens,
		System:    []anthropic.TextBlockParam{{Text: prompt.System}},
		Messages:  messages,
		OutputConfig: anthropic.OutputConfigParam{
			Effort: a.effort,
			Format: anthropic.JSONOutputFormatParam{Schema: prompt.Schema},
		},
	}
	if a.ShowThinking {
		// Adaptive is the default; display is what changes. Without it the
		// thinking comes back empty.
		params.Thinking = anthropic.ThinkingConfigParamUnion{OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{
			Display: anthropic.ThinkingConfigAdaptiveDisplaySummarized,
		}}
	}
	resp, err := a.client.Messages.New(ctx, params)
	if err != nil {
		return Response{}, err
	}

	var text, thinking strings.Builder
	for _, block := range resp.Content {
		switch b := block.AsAny().(type) {
		case anthropic.TextBlock:
			text.WriteString(b.Text)
		case anthropic.ThinkingBlock:
			thinking.WriteString(b.Thinking)
		}
	}
	out := Response{Text: text.String(), Thinking: thinking.String(), Usage: Usage{
		InputTokens:      resp.Usage.InputTokens,
		OutputTokens:     resp.Usage.OutputTokens,
		CacheReadTokens:  resp.Usage.CacheReadInputTokens,
		CacheWriteTokens: resp.Usage.CacheCreationInputTokens,
	}}

	// Neither stop leaves a reply that is sure to match the schema.
	switch resp.StopReason {
	case anthropic.StopReasonRefusal, anthropic.StopReasonMaxTokens:
		return out, fmt.Errorf("%w: stopped for %s", ErrUnusableReply, resp.StopReason)
	}
	return out, nil
}
