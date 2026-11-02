package patterntranslator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// twoSundays is a saved pattern's entries as the rule translator is told them:
// two Shifts on a Sunday, so a rule for one of them has to name its time.
var twoSundays = []string{
	"Every Sunday at 10:00 for 2 hours, from 1 Nov 2026",
	"Every Sunday at 19:30 for 3 hours, from 1 Nov 2026",
}

const (
	sundayEvenings = "DTSTART;VALUE=DATE:20261101\nRRULE:FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=30"
	// hourWithoutMinute names an hour but no minute: the rule parser refuses it.
	hourWithoutMinute = "DTSTART;VALUE=DATE:20261101\nRRULE:FREQ=WEEKLY;BYDAY=SU;BYHOUR=19"
)

func okRule(rule string) scripted {
	return scripted{text: `{"status":"ok","rule":` + quote(rule) + `}`}
}

func translateRule(t *testing.T, model *scriptedModel, text string) (RuleResult, error) {
	t.Helper()
	return New(model).TranslateRule(context.Background(), RuleRequest{
		Text: text, Pattern: twoSundays, Zone: zone, Now: now,
	})
}

func TestTranslateRule_AFirstReplyThatReadsIsTheRule(t *testing.T) {
	model := &scriptedModel{replies: []scripted{okRule(sundayEvenings)}}

	got, err := translateRule(t, model, "the Sunday evening shift")
	if err != nil {
		t.Fatalf("TranslateRule: %v", err)
	}
	if got.Rule != sundayEvenings || len(got.Issues) != 0 {
		t.Fatalf("got %+v, want the rule and no issues", got)
	}
	if model.prompts[0] != rulePrompt {
		t.Errorf("asked with the %q prompt, want the rule's", model.prompts[0].Name)
	}
}

func TestTranslateRule_TheModelIsToldTheSavedPatternAndTheDescription(t *testing.T) {
	model := &scriptedModel{replies: []scripted{okRule(sundayEvenings)}}

	if _, err := translateRule(t, model, "the Sunday evening shift"); err != nil {
		t.Fatalf("TranslateRule: %v", err)
	}
	first := model.calls[0][0].Text
	for _, want := range []string{
		"Today: 2026-11-01 (Sunday)",
		"<pattern>\n- " + twoSundays[0] + "\n- " + twoSundays[1] + "\n</pattern>",
		"<description>\nthe Sunday evening shift\n</description>",
	} {
		if !strings.Contains(first, want) {
			t.Errorf("user turn %q lacks %q", first, want)
		}
	}
}

// Near the year's end the calendar runs into the next year, and a weekday with
// no dates left this month starts in the next.
func TestTranslateRule_TheCalendarRunsIntoTheNextYear(t *testing.T) {
	model := &scriptedModel{replies: []scripted{okRule(sundayEvenings)}}

	req := RuleRequest{Text: "the Sunday evening shift", Pattern: twoSundays, Zone: zone,
		Now: time.Date(2026, 12, 30, 12, 0, 0, 0, time.UTC)}
	if _, err := New(model).TranslateRule(context.Background(), req); err != nil {
		t.Fatalf("TranslateRule: %v", err)
	}
	first := model.calls[0][0].Text
	for _, want := range []string{
		"- Monday: 4, 11, 18, 25 January 2027\n",
		"- Wednesday: 30 December 2026; 6, 13, 20, 27 January 2027\n",
		"- Thursday: 31 December 2026; 7, 14, 21, 28 January 2027\n",
	} {
		if !strings.Contains(first, want) {
			t.Errorf("user turn %q lacks %q", first, want)
		}
	}
}

func TestTranslateRule_IssuesAreAnAnswerAndAreNotRetried(t *testing.T) {
	model := &scriptedModel{replies: []scripted{{text: `{"status":"issues","issues":[` +
		`{"kind":"ambiguous","message":"Which Sunday shift?","examples":["10:00","19:30"]}]}`}}}

	got, err := translateRule(t, model, "Sundays")
	if err != nil {
		t.Fatalf("TranslateRule: %v", err)
	}
	want := []Issue{{Kind: Ambiguous, Message: "Which Sunday shift?", Examples: []string{"10:00", "19:30"}}}
	if got.Rule != "" || !sameIssues(got.Issues, want) || len(model.calls) != 1 {
		t.Fatalf("got %+v after %d calls, want issues %+v at once", got, len(model.calls), want)
	}
}

func TestTranslateRule_ARuleTheParserRefusesIsSentBackWithItsProblems(t *testing.T) {
	model := &scriptedModel{replies: []scripted{okRule(hourWithoutMinute), okRule(sundayEvenings)}}

	got, err := translateRule(t, model, "Sundays at 7:30pm")
	if err != nil {
		t.Fatalf("TranslateRule: %v", err)
	}
	if got.Rule != sundayEvenings || len(model.calls) != 2 {
		t.Fatalf("got %+v after %d calls, want the second reply's rule", got, len(model.calls))
	}
	retry := model.calls[1][2].Text
	if !strings.Contains(retry, "The app cannot use that rule") ||
		!strings.Contains(retry, "give BYHOUR and BYMINUTE together") {
		t.Errorf("the problems sent back %q do not say what the rule parser said", retry)
	}
}

func TestTranslateRule_GivesUpAfterThreeCallsWithTheParsersMessages(t *testing.T) {
	model := &scriptedModel{replies: []scripted{
		okRule(hourWithoutMinute), okRule(hourWithoutMinute), okRule(hourWithoutMinute),
	}}

	got, err := translateRule(t, model, "Sundays at 7:30pm")
	if err != nil {
		t.Fatalf("TranslateRule: %v", err)
	}
	if len(got.Issues) != 2 || got.Issues[0].Message != RuleGaveUp ||
		!strings.Contains(got.Issues[1].Message, "give BYHOUR and BYMINUTE together") {
		t.Fatalf("got %+v, want the fallback issue and the parser's problem", got)
	}
}

func TestTranslateRule_AnUnusableReplyIsAFailedCall(t *testing.T) {
	model := &scriptedModel{replies: []scripted{
		{text: `{"status":"ok","pattern":"BEGIN:VEVENT"}`}, okRule(sundayEvenings),
	}}

	got, err := translateRule(t, model, "the Sunday evening shift")
	if err != nil {
		t.Fatalf("TranslateRule: %v", err)
	}
	if got.Rule != sundayEvenings || len(model.calls) != 2 {
		t.Fatalf("got %+v after %d calls, want an ok without a rule passed over", got, len(model.calls))
	}
}

func TestTranslateRule_TheModelFailingIsUnavailable(t *testing.T) {
	model := &scriptedModel{replies: []scripted{{err: errors.New("529 overloaded")}}}

	if _, err := translateRule(t, model, "Sundays"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("got %v, want ErrUnavailable", err)
	}
}

func TestTranslateRule_AnOverlongDescriptionIsRefusedWithoutAsking(t *testing.T) {
	model := &scriptedModel{}

	if _, err := translateRule(t, model, strings.Repeat("a", MaxText+1)); !errors.Is(err, ErrTooLong) {
		t.Fatalf("got %v, want ErrTooLong", err)
	}
	if len(model.calls) != 0 {
		t.Fatalf("made %d calls, want none", len(model.calls))
	}
}
