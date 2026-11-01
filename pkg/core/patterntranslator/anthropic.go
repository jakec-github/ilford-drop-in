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
	client anthropic.Client
	model  string
	effort anthropic.OutputConfigEffort
}

// NewAnthropic is a Model that asks model, at effort, with apiKey. No provider
// abstraction sits over it: the Model interface is there for tests and the dev
// stack (ADR 0015).
func NewAnthropic(apiKey, model, effort string) *Anthropic {
	return &Anthropic{
		client: anthropic.NewClient(option.WithAPIKey(apiKey)),
		model:  model,
		effort: anthropic.OutputConfigEffort(effort),
	}
}

func (a *Anthropic) Reply(ctx context.Context, prompt *Prompt, turns []Turn) (string, error) {
	messages := make([]anthropic.MessageParam, 0, len(turns))
	for _, t := range turns {
		block := anthropic.NewTextBlock(t.Text)
		if t.Role == Assistant {
			messages = append(messages, anthropic.NewAssistantMessage(block))
		} else {
			messages = append(messages, anthropic.NewUserMessage(block))
		}
	}

	resp, err := a.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(a.model),
		MaxTokens: maxTokens,
		System:    []anthropic.TextBlockParam{{Text: prompt.System}},
		Messages:  messages,
		OutputConfig: anthropic.OutputConfigParam{
			Effort: a.effort,
			Format: anthropic.JSONOutputFormatParam{Schema: prompt.Schema},
		},
	})
	if err != nil {
		return "", err
	}

	// Neither stop leaves a reply that is sure to match the schema.
	switch resp.StopReason {
	case anthropic.StopReasonRefusal, anthropic.StopReasonMaxTokens:
		return "", fmt.Errorf("%w: stopped for %s", ErrUnusableReply, resp.StopReason)
	}

	var text strings.Builder
	for _, block := range resp.Content {
		if b, ok := block.AsAny().(anthropic.TextBlock); ok {
			text.WriteString(b.Text)
		}
	}
	return text.String(), nil
}
