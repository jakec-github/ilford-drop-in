package patterntranslator

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Stub is the dev-mode Translator: deterministic and free, so the natural
// editor can be driven on the dev stack, and screenshotted, without a key.
//
// Text containing "issue" is answered with two issues, and text containing
// "fail" cannot be translated just now. Anything else is, for a pattern, every
// Sunday at 19:30 for three hours, from the first Sunday on or after today;
// for an Override, every Shift on the first Sunday of the month, from today.
type Stub struct{}

// stubbed is the answer every description of the two kinds shares, or nil
// when there is a translation to make.
func stubbed(text string, issues []Issue) ([]Issue, error) {
	if utf8.RuneCountInString(text) > MaxText {
		return nil, ErrTooLong
	}
	text = strings.ToLower(text)
	switch {
	case strings.Contains(text, "fail"):
		return nil, fmt.Errorf("%w: the dev-mode translator was asked to fail", ErrUnavailable)
	case strings.Contains(text, "issue"):
		return issues, nil
	}
	return nil, nil
}

func (Stub) Translate(_ context.Context, req Request) (Result, error) {
	issues, err := stubbed(req.Text, []Issue{
		{Kind: Missing, Message: "Add how long the Sunday shifts run", Examples: []string{}},
		{Kind: Ambiguous, Message: "\"Evenings\" — at what time?", Examples: []string{"7pm", "7:30pm"}},
	})
	if err != nil || issues != nil {
		return Result{Issues: issues}, err
	}

	today, err := todayIn(req.Zone, req.Now)
	if err != nil {
		return Result{}, err
	}
	sunday := today.AddDate(0, 0, (7-int(today.Weekday()))%7)
	return Result{Pattern: fmt.Sprintf(
		"BEGIN:VEVENT\nDTSTART:%sT193000\nDURATION:PT3H\nRRULE:FREQ=WEEKLY;BYDAY=SU\nEND:VEVENT",
		sunday.Format("20060102"))}, nil
}

func (Stub) TranslateRule(_ context.Context, req RuleRequest) (RuleResult, error) {
	issues, err := stubbed(req.Text, []Issue{
		{Kind: Missing, Message: "Say which Sundays", Examples: []string{"the first Sunday", "every Sunday"}},
		{Kind: Ambiguous, Message: "Which Sunday shift?", Examples: []string{"10:00", "19:30", "every shift"}},
	})
	if err != nil || issues != nil {
		return RuleResult{Issues: issues}, err
	}

	today, err := todayIn(req.Zone, req.Now)
	if err != nil {
		return RuleResult{}, err
	}
	return RuleResult{Rule: fmt.Sprintf(
		"DTSTART;VALUE=DATE:%s\nRRULE:FREQ=MONTHLY;BYDAY=1SU", today.Format("20060102"))}, nil
}
