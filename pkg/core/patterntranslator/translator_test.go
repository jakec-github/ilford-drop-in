package patterntranslator

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const zone = "Europe/London"

// now is a Sunday evening in London, and already Monday on Kiritimati (UTC+14).
var now = time.Date(2026, 11, 1, 20, 0, 0, 0, time.UTC)

const sundays = "BEGIN:VEVENT\nDTSTART:20261101T193000\nDURATION:PT3H\nRRULE:FREQ=WEEKLY;BYDAY=SU\nEND:VEVENT"

// tuesdayStartingSunday starts on a Sunday its rule, every Tuesday, never
// generates: the parser refuses it.
const tuesdayStartingSunday = "BEGIN:VEVENT\nDTSTART:20261101T190000\nDURATION:PT4H\nRRULE:FREQ=WEEKLY;BYDAY=TU\nEND:VEVENT"

// scriptedModel answers each call with the next reply in its script, and
// keeps every conversation it was handed.
type scriptedModel struct {
	replies []scripted
	calls   [][]Turn
	prompts []*Prompt
}

type scripted struct {
	text string
	err  error
}

func (m *scriptedModel) Reply(_ context.Context, prompt *Prompt, turns []Turn) (Response, error) {
	m.prompts = append(m.prompts, prompt)
	m.calls = append(m.calls, append([]Turn(nil), turns...))
	if len(m.calls) > len(m.replies) {
		return Response{}, errors.New("scriptedModel: called more times than scripted")
	}
	r := m.replies[len(m.calls)-1]
	return Response{Text: r.text}, r.err
}

func ok(pattern string) scripted {
	return scripted{text: `{"status":"ok","pattern":` + quote(pattern) + `}`}
}

func quote(s string) string {
	return `"` + strings.ReplaceAll(s, "\n", `\n`) + `"`
}

func translate(t *testing.T, model *scriptedModel, text string) (Result, error) {
	t.Helper()
	return New(model).Translate(context.Background(), Request{Text: text, Zone: zone, Now: now})
}

func TestTranslate_AFirstReplyThatReadsIsThePattern(t *testing.T) {
	model := &scriptedModel{replies: []scripted{ok(sundays)}}

	got, err := translate(t, model, "Sundays 7:30 to 10:30pm")
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if got.Pattern != sundays || len(got.Issues) != 0 {
		t.Fatalf("got %+v, want the pattern and no issues", got)
	}
	if len(model.calls) != 1 {
		t.Fatalf("made %d calls, want 1", len(model.calls))
	}
	if model.prompts[0] != patternPrompt {
		t.Errorf("asked with the %q prompt, want the pattern's", model.prompts[0].Name)
	}
}

func TestTranslate_TheModelIsToldTodayInTheOrganisationsZoneAndTheDescription(t *testing.T) {
	model := &scriptedModel{replies: []scripted{ok(sundays)}}

	req := Request{Text: "Sundays 7:30 to 10:30pm", Zone: "Pacific/Kiritimati", Now: now}
	if _, err := New(model).Translate(context.Background(), req); err != nil {
		t.Fatalf("Translate: %v", err)
	}

	first := model.calls[0]
	if len(first) != 1 || first[0].Role != User {
		t.Fatalf("first call was %+v, want one user turn", first)
	}
	for _, want := range []string{
		"Today: 2026-11-02 (Monday)",
		"Organisation time zone: Pacific/Kiritimati",
		"<description>\nSundays 7:30 to 10:30pm\n</description>",
	} {
		if !strings.Contains(first[0].Text, want) {
			t.Errorf("user turn %q lacks %q", first[0].Text, want)
		}
	}
}

// A model counts dates badly: "the first Saturday" from today came out a month
// late. The calendar lets it read a date off rather than work it out.
func TestTranslate_TheModelIsToldEachWeekdaysDatesToTheEndOfNextMonth(t *testing.T) {
	model := &scriptedModel{replies: []scripted{ok(sundays)}}

	req := Request{Text: "First Saturday of the month", Zone: "Pacific/Kiritimati", Now: now}
	if _, err := New(model).Translate(context.Background(), req); err != nil {
		t.Fatalf("Translate: %v", err)
	}

	want := "Dates from today to the end of next month:\n" +
		"- Monday: 2, 9, 16, 23, 30 November 2026; 7, 14, 21, 28 December 2026\n" +
		"- Tuesday: 3, 10, 17, 24 November 2026; 1, 8, 15, 22, 29 December 2026\n" +
		"- Wednesday: 4, 11, 18, 25 November 2026; 2, 9, 16, 23, 30 December 2026\n" +
		"- Thursday: 5, 12, 19, 26 November 2026; 3, 10, 17, 24, 31 December 2026\n" +
		"- Friday: 6, 13, 20, 27 November 2026; 4, 11, 18, 25 December 2026\n" +
		"- Saturday: 7, 14, 21, 28 November 2026; 5, 12, 19, 26 December 2026\n" +
		"- Sunday: 8, 15, 22, 29 November 2026; 6, 13, 20, 27 December 2026\n" +
		"</context>"
	if first := model.calls[0][0].Text; !strings.Contains(first, want) {
		t.Errorf("user turn %q lacks the calendar %q", first, want)
	}
}

func TestTranslate_IssuesAreAnAnswerAndAreNotRetried(t *testing.T) {
	model := &scriptedModel{replies: []scripted{{text: `{"status":"issues","issues":[` +
		`{"kind":"missing","message":"Add how long the Sunday shifts run","examples":[]},` +
		`{"kind":"ambiguous","message":"\"Evenings\" — at what time?","examples":["7pm","7:30pm"]}]}`}}}

	got, err := translate(t, model, "Sunday evenings")
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	want := []Issue{
		{Kind: Missing, Message: "Add how long the Sunday shifts run", Examples: []string{}},
		{Kind: Ambiguous, Message: "\"Evenings\" — at what time?", Examples: []string{"7pm", "7:30pm"}},
	}
	if got.Pattern != "" || !sameIssues(got.Issues, want) {
		t.Fatalf("got %+v, want issues %+v", got, want)
	}
	if len(model.calls) != 1 {
		t.Fatalf("made %d calls, want 1", len(model.calls))
	}
}

func TestTranslate_AReplyTheParserRefusesIsSentBackWithItsProblems(t *testing.T) {
	model := &scriptedModel{replies: []scripted{ok(tuesdayStartingSunday), ok(sundays)}}

	got, err := translate(t, model, "Tuesdays 7 till 11")
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if got.Pattern != sundays {
		t.Fatalf("got %+v, want the second reply's pattern", got)
	}

	if len(model.calls) != 2 {
		t.Fatalf("made %d calls, want 2", len(model.calls))
	}
	second := model.calls[1]
	if len(second) != 3 || second[1].Role != Assistant || second[2].Role != User {
		t.Fatalf("second call was %+v, want the first turn, the reply and a user turn", second)
	}
	if !strings.Contains(second[1].Text, "20261101T190000") {
		t.Errorf("the reply sent back %q is not the model's own", second[1].Text)
	}
	if !strings.Contains(second[2].Text, "entry 1: DTSTART is Sun 1 Nov 2026 at 19:00, which its RRULE does not generate") {
		t.Errorf("the problems sent back %q do not name the entry and what the parser said", second[2].Text)
	}
}

func TestTranslate_GivesUpAfterThreeCallsWithTheParsersMessages(t *testing.T) {
	model := &scriptedModel{replies: []scripted{
		ok(tuesdayStartingSunday), ok(tuesdayStartingSunday), ok(tuesdayStartingSunday),
	}}

	got, err := translate(t, model, "Tuesdays 7 till 11")
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if len(model.calls) != 3 {
		t.Fatalf("made %d calls, want 3", len(model.calls))
	}
	if got.Pattern != "" || len(got.Issues) != 2 {
		t.Fatalf("got %+v, want the fallback issue and the parser's one problem", got)
	}
	if got.Issues[0].Message != GaveUp {
		t.Errorf("first issue is %+v, want %q", got.Issues[0], GaveUp)
	}
	if !strings.Contains(got.Issues[1].Message, "which its RRULE does not generate") {
		t.Errorf("second issue %+v is not the parser's message", got.Issues[1])
	}
}

func TestTranslate_WarningsDoNotStopAPattern(t *testing.T) {
	withUID := strings.Replace(sundays, "DURATION", "UID:abc@example.com\nDURATION", 1)
	model := &scriptedModel{replies: []scripted{ok(withUID)}}

	got, err := translate(t, model, "Sundays 7:30 to 10:30pm")
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if got.Pattern != withUID || len(model.calls) != 1 {
		t.Fatalf("got %+v after %d calls, want the pattern at once", got, len(model.calls))
	}
}

func TestTranslate_AnUnusableReplyIsAFailedCall(t *testing.T) {
	for name, bad := range map[string]scripted{
		"refusal or max_tokens": {err: ErrUnusableReply},
		"not JSON":              {text: "Sure! Here is your pattern"},
		"neither shape":         {text: `{"status":"maybe"}`},
	} {
		t.Run(name, func(t *testing.T) {
			model := &scriptedModel{replies: []scripted{bad, ok(sundays)}}

			got, err := translate(t, model, "Sundays 7:30 to 10:30pm")
			if err != nil {
				t.Fatalf("Translate: %v", err)
			}
			if got.Pattern != sundays || len(model.calls) != 2 {
				t.Fatalf("got %+v after %d calls, want the second call's pattern", got, len(model.calls))
			}
			// Nothing of an unusable reply is worth showing the model again.
			if len(model.calls[1]) != 1 {
				t.Errorf("second call was %+v, want the first turn alone", model.calls[1])
			}
		})
	}
}

// A reply that reads but says nothing, issues with none in them or an ok with
// no pattern, would come back the same if asked the same way, so the model is
// told what was missing.
func TestTranslate_AReplyThatSaysNothingIsSentBackSayingWhatWasMissing(t *testing.T) {
	for name, tc := range map[string]struct {
		reply scripted
		want  string
	}{
		"issues with none in them": {scripted{text: `{"status":"issues","issues":[]}`}, "Your reply listed no issues."},
		"ok without a pattern":     {scripted{text: `{"status":"ok","pattern":" "}`}, "Your reply gave no pattern."},
	} {
		t.Run(name, func(t *testing.T) {
			model := &scriptedModel{replies: []scripted{tc.reply, ok(sundays)}}

			got, err := translate(t, model, "Sundays 7:30 to 10:30pm")
			if err != nil {
				t.Fatalf("Translate: %v", err)
			}
			if got.Pattern != sundays || len(model.calls) != 2 {
				t.Fatalf("got %+v after %d calls, want the second call's pattern", got, len(model.calls))
			}
			turns := model.calls[1]
			if len(turns) != 3 || turns[1].Role != Assistant || turns[1].Text != tc.reply.text ||
				turns[2].Role != User || !strings.Contains(turns[2].Text, tc.want) {
				t.Errorf("second call was %+v, want the reply and a turn saying %q", turns, tc.want)
			}
		})
	}
}

func TestTranslate_ThreeRepliesThatSayNothingGiveUp(t *testing.T) {
	empty := scripted{text: `{"status":"issues","issues":[]}`}
	model := &scriptedModel{replies: []scripted{empty, empty, empty}}

	got, err := translate(t, model, "Sundays 7:30 to 10:30pm")
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if len(got.Issues) != 1 || got.Issues[0].Message != GaveUp || len(model.calls) != MaxCalls {
		t.Fatalf("got %+v after %d calls, want the fallback issue alone", got, len(model.calls))
	}
}

func TestTranslate_ThreeUnusableRepliesGiveUpWithNothingElseToSay(t *testing.T) {
	model := &scriptedModel{replies: []scripted{
		{err: ErrUnusableReply}, {err: ErrUnusableReply}, {err: ErrUnusableReply},
	}}

	got, err := translate(t, model, "Sundays 7:30 to 10:30pm")
	if err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if len(got.Issues) != 1 || got.Issues[0].Message != GaveUp {
		t.Fatalf("got %+v, want the fallback issue alone", got)
	}
}

func TestTranslate_TheModelFailingIsUnavailable(t *testing.T) {
	model := &scriptedModel{replies: []scripted{{err: errors.New("529 overloaded")}}}

	_, err := translate(t, model, "Sundays 7:30 to 10:30pm")
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("got %v, want ErrUnavailable", err)
	}
	if len(model.calls) != 1 {
		t.Fatalf("made %d calls, want 1: the SDK has already retried", len(model.calls))
	}
}

func TestTranslate_RunningOutOfTimeIsUnavailable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	model := &cancellingModel{cancel: cancel}

	_, err := New(model).Translate(ctx, Request{Text: "Tuesdays 7 till 11", Zone: zone, Now: now})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("got %v, want ErrUnavailable", err)
	}
	if model.calls != 1 {
		t.Fatalf("made %d calls, want 1: no call starts once the time is up", model.calls)
	}
}

// cancellingModel replies with a pattern the parser refuses, and the time for
// the whole translation runs out while it does.
type cancellingModel struct {
	cancel func()
	calls  int
}

func (m *cancellingModel) Reply(context.Context, *Prompt, []Turn) (Response, error) {
	m.calls++
	m.cancel()
	return Response{Text: `{"status":"ok","pattern":` + quote(tuesdayStartingSunday) + `}`}, nil
}

func TestTranslate_AnOverlongDescriptionIsRefusedWithoutAsking(t *testing.T) {
	model := &scriptedModel{}

	_, err := translate(t, model, strings.Repeat("é", MaxText+1))
	if !errors.Is(err, ErrTooLong) {
		t.Fatalf("got %v, want ErrTooLong", err)
	}
	if len(model.calls) != 0 {
		t.Fatalf("made %d calls, want none", len(model.calls))
	}

	// MaxText counts characters, not bytes.
	model = &scriptedModel{replies: []scripted{ok(sundays)}}
	if _, err := translate(t, model, strings.Repeat("é", MaxText)); err != nil {
		t.Fatalf("a description of exactly MaxText characters: %v", err)
	}
}

func sameIssues(a, b []Issue) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Kind != b[i].Kind || a[i].Message != b[i].Message || strings.Join(a[i].Examples, "|") != strings.Join(b[i].Examples, "|") {
			return false
		}
	}
	return true
}
