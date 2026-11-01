package patterntranslator

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Stub is the dev-mode Translator: deterministic and free, so the natural
// editor can be driven on the dev stack, and screenshotted, without a key.
//
// Text containing "issue" is answered with two issues, text containing "fail"
// cannot be translated just now, and anything else is every Sunday at 19:30
// for three hours, from the first Sunday on or after today.
type Stub struct{}

func (Stub) Translate(_ context.Context, req Request) (Result, error) {
	if utf8.RuneCountInString(req.Text) > MaxText {
		return Result{}, ErrTooLong
	}
	text := strings.ToLower(req.Text)
	switch {
	case strings.Contains(text, "fail"):
		return Result{}, fmt.Errorf("%w: the dev-mode translator was asked to fail", ErrUnavailable)
	case strings.Contains(text, "issue"):
		return Result{Issues: []Issue{
			{Kind: Missing, Message: "Add how long the Sunday shifts run", Examples: []string{}},
			{Kind: Ambiguous, Message: "\"Evenings\" — at what time?", Examples: []string{"7pm", "7:30pm"}},
		}}, nil
	}

	loc, err := time.LoadLocation(req.Zone)
	if err != nil {
		return Result{}, fmt.Errorf("%w: the organisation's time zone %q cannot be loaded", ErrUnavailable, req.Zone)
	}
	today := req.Now.In(loc)
	sunday := today.AddDate(0, 0, (7-int(today.Weekday()))%7)
	return Result{Pattern: fmt.Sprintf(
		"BEGIN:VEVENT\nDTSTART:%sT193000\nDURATION:PT3H\nRRULE:FREQ=WEEKLY;BYDAY=SU\nEND:VEVENT",
		sunday.Format("20060102"))}, nil
}
