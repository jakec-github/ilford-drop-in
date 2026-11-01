package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jakechorley/ilford-drop-in/internal/config"
	"github.com/jakechorley/ilford-drop-in/pkg/core/patterntranslator"
	"github.com/jakechorley/ilford-drop-in/pkg/db"
)

// fakeTranslator answers every translation with result and err, and keeps the
// requests it was handed.
type fakeTranslator struct {
	result       patterntranslator.Result
	ruleResult   patterntranslator.RuleResult
	err          error
	requests     []patterntranslator.Request
	ruleRequests []patterntranslator.RuleRequest
	deadline     bool
}

func (f *fakeTranslator) Translate(ctx context.Context, req patterntranslator.Request) (patterntranslator.Result, error) {
	f.requests = append(f.requests, req)
	_, f.deadline = ctx.Deadline()
	return f.result, f.err
}

func (f *fakeTranslator) TranslateRule(ctx context.Context, req patterntranslator.RuleRequest) (patterntranslator.RuleResult, error) {
	f.ruleRequests = append(f.ruleRequests, req)
	_, f.deadline = ctx.Deadline()
	return f.ruleResult, f.err
}

// naturalOrg is the test Organisation, opted in to the natural editor.
var naturalOrg = func() *config.Organisation {
	org := *apiTestCfg
	org.NaturalEditor = true
	return &org
}()

func newTranslatingHandler(store *mockStore, org *config.Organisation, translator patterntranslator.Translator) http.Handler {
	h := NewHandler(store, testVolunteers(), org, newTestAuthenticator(), nil, nil, nil, nil, zap.NewNop())
	if translator != nil {
		h.SetTranslator(translator)
	}
	return h.Routes()
}

func translateBody(text string) string {
	return `{"text":` + jsonString(text) + `}`
}

func TestTranslateShiftPatternAnswersWithThePatternAndItsEntries(t *testing.T) {
	store := &mockStore{rotaDefaults: &db.RotaDefaults{ShiftTimezone: "America/New_York"}}
	translator := &fakeTranslator{result: patterntranslator.Result{Pattern: weeklySunday}}

	rec := doRequest(t, newTranslatingHandler(store, naturalOrg, translator), http.MethodPost,
		"/api/rota-defaults/shift-pattern/translate", translateBody("Sundays 7:30 for 2 hours"), organiserCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body translateShiftPatternResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "ok", body.Status)
	assert.Equal(t, weeklySunday, body.Pattern)
	require.Len(t, body.Entries, 1)
	assert.Equal(t, "Every Sunday at 19:30 for 2 hours, from 4 Jan 2026", body.Entries[0].Description)
	assert.Empty(t, body.Issues)

	require.Len(t, translator.requests, 1)
	assert.Equal(t, "Sundays 7:30 for 2 hours", translator.requests[0].Text)
	assert.Equal(t, "America/New_York", translator.requests[0].Zone, "read in the Organisation's zone")
	assert.True(t, translator.deadline, "a translation has a ceiling")
	assert.Empty(t, store.savedPatterns, "translating never saves")
}

func TestTranslateShiftPatternAnswersWithIssues(t *testing.T) {
	translator := &fakeTranslator{result: patterntranslator.Result{Issues: []patterntranslator.Issue{
		{Kind: patterntranslator.Ambiguous, Message: "\"Evenings\" — at what time?", Examples: []string{"7pm"}},
	}}}

	rec := doRequest(t, newTranslatingHandler(&mockStore{}, naturalOrg, translator), http.MethodPost,
		"/api/rota-defaults/shift-pattern/translate", translateBody("Sunday evenings"), organiserCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body translateShiftPatternResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "issues", body.Status)
	assert.Empty(t, body.Pattern)
	assert.Equal(t, []translateIssueResponse{
		{Kind: "ambiguous", Message: "\"Evenings\" — at what time?", Examples: []string{"7pm"}},
	}, body.Issues)
}

func TestTranslateShiftPatternWhenTheTranslatorFails(t *testing.T) {
	for name, tc := range map[string]struct {
		err    error
		status int
	}{
		"unavailable": {fmt.Errorf("%w: 529", patterntranslator.ErrUnavailable), http.StatusServiceUnavailable},
		"too long":    {patterntranslator.ErrTooLong, http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			translator := &fakeTranslator{err: tc.err}
			rec := doRequest(t, newTranslatingHandler(&mockStore{}, naturalOrg, translator), http.MethodPost,
				"/api/rota-defaults/shift-pattern/translate", translateBody("Sundays"), organiserCookie())
			assert.Equal(t, tc.status, rec.Code, rec.Body.String())
		})
	}
}

func TestTranslateShiftPatternRefusesAnOverlongDescriptionWithoutAsking(t *testing.T) {
	translator := &fakeTranslator{}
	rec := doRequest(t, newTranslatingHandler(&mockStore{}, naturalOrg, translator), http.MethodPost,
		"/api/rota-defaults/shift-pattern/translate", translateBody(strings.Repeat("a", patterntranslator.MaxText+1)), organiserCookie())
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Empty(t, translator.requests)
}

func TestTranslateShiftPatternIsOnlyWhereTheNaturalEditorIs(t *testing.T) {
	for name, tc := range map[string]struct {
		org        *config.Organisation
		translator patterntranslator.Translator
	}{
		"no translator": {naturalOrg, nil},
		"not opted in":  {apiTestCfg, &fakeTranslator{}},
	} {
		t.Run(name, func(t *testing.T) {
			rec := doRequest(t, newTranslatingHandler(&mockStore{}, tc.org, tc.translator), http.MethodPost,
				"/api/rota-defaults/shift-pattern/translate", translateBody("Sundays"), organiserCookie())
			assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		})
	}
}

func TestTranslateShiftPatternIsOrganiserOnly(t *testing.T) {
	rec := doRequest(t, newTranslatingHandler(&mockStore{}, naturalOrg, &fakeTranslator{}), http.MethodPost,
		"/api/rota-defaults/shift-pattern/translate", translateBody("Sundays"))
	assert.Equal(t, http.StatusUnauthorized, rec.Code, rec.Body.String())
}

func TestGetRotaDefaultsSaysWhetherTheNaturalEditorIsAvailable(t *testing.T) {
	for name, tc := range map[string]struct {
		org          *config.Organisation
		translator   patterntranslator.Translator
		pattern      string
		want         naturalEditorResponse
		wantOverride naturalEditorResponse
	}{
		"available": {naturalOrg, &fakeTranslator{}, weeklySunday,
			naturalEditorResponse{Available: true}, naturalEditorResponse{Available: true}},
		"no pattern yet": {naturalOrg, &fakeTranslator{}, "",
			naturalEditorResponse{Available: true}, naturalEditorResponse{Reason: noPatternReason}},
		"no translator": {naturalOrg, nil, weeklySunday,
			naturalEditorResponse{Reason: noTranslatorReason}, naturalEditorResponse{Reason: noTranslatorReason}},
		"not opted in": {apiTestCfg, &fakeTranslator{}, weeklySunday,
			naturalEditorResponse{Reason: notOptedInReason}, naturalEditorResponse{Reason: notOptedInReason}},
	} {
		t.Run(name, func(t *testing.T) {
			store := &mockStore{rotaDefaults: &db.RotaDefaults{ShiftPattern: tc.pattern}}
			rec := doRequest(t, newTranslatingHandler(store, tc.org, tc.translator), http.MethodGet,
				"/api/rota-defaults", "", organiserCookie())
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

			var body rotaDefaultsResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, tc.want, body.NaturalEditor)
			assert.Equal(t, tc.wantOverride, body.OverrideNaturalEditor)
		})
	}
}

// twoSundays has two Shifts on a Sunday, at 10:00 and 19:30.
const twoSundays = "BEGIN:VEVENT\nDTSTART:20260104T100000\nDURATION:PT2H\nRRULE:FREQ=WEEKLY;BYDAY=SU\nEND:VEVENT\n\n" + weeklySunday

const sundayEveningRule = "DTSTART;VALUE=DATE:20260104\nRRULE:FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=30"

func TestTranslateOverrideRuleAnswersWithTheRuleAndWhatItSays(t *testing.T) {
	store := &mockStore{rotaDefaults: &db.RotaDefaults{ShiftPattern: twoSundays, ShiftTimezone: "America/New_York"}}
	translator := &fakeTranslator{ruleResult: patterntranslator.RuleResult{Rule: sundayEveningRule}}

	rec := doRequest(t, newTranslatingHandler(store, naturalOrg, translator), http.MethodPost,
		"/api/rota-defaults/overrides/rule/translate", translateBody("the Sunday evening shift"), organiserCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body translateOverrideRuleResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, translateOverrideRuleResponse{
		Status:      "ok",
		RRule:       sundayEveningRule,
		Granularity: "time",
		Description: "Every Sunday at 19:30, from 4 Jan 2026",
	}, body)

	require.Len(t, translator.ruleRequests, 1)
	req := translator.ruleRequests[0]
	assert.Equal(t, "the Sunday evening shift", req.Text)
	assert.Equal(t, "America/New_York", req.Zone)
	assert.Equal(t, []string{
		"Every Sunday at 10:00 for 2 hours, from 4 Jan 2026",
		"Every Sunday at 19:30 for 2 hours, from 4 Jan 2026",
	}, req.Pattern, "told what the saved pattern's entries say")
	assert.True(t, translator.deadline, "a translation has a ceiling")
	assert.Empty(t, store.overrides, "translating never saves")
}

func TestTranslateOverrideRuleAnswersWithIssues(t *testing.T) {
	store := &mockStore{rotaDefaults: &db.RotaDefaults{ShiftPattern: twoSundays}}
	translator := &fakeTranslator{ruleResult: patterntranslator.RuleResult{Issues: []patterntranslator.Issue{
		{Kind: patterntranslator.Ambiguous, Message: "Which Sunday shift?", Examples: []string{"10:00", "19:30"}},
	}}}

	rec := doRequest(t, newTranslatingHandler(store, naturalOrg, translator), http.MethodPost,
		"/api/rota-defaults/overrides/rule/translate", translateBody("Sundays"), organiserCookie())
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body translateOverrideRuleResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "issues", body.Status)
	assert.Equal(t, []translateIssueResponse{
		{Kind: "ambiguous", Message: "Which Sunday shift?", Examples: []string{"10:00", "19:30"}},
	}, body.Issues)
}

func TestTranslateOverrideRuleWhenItCannotBeAsked(t *testing.T) {
	withPattern := func() *mockStore { return &mockStore{rotaDefaults: &db.RotaDefaults{ShiftPattern: twoSundays}} }
	for name, tc := range map[string]struct {
		store      *mockStore
		org        *config.Organisation
		translator *fakeTranslator
		text       string
		status     int
	}{
		"no pattern yet": {&mockStore{rotaDefaults: &db.RotaDefaults{}}, naturalOrg, &fakeTranslator{}, "Sundays", http.StatusForbidden},
		"not opted in":   {withPattern(), apiTestCfg, &fakeTranslator{}, "Sundays", http.StatusForbidden},
		"too long":       {withPattern(), naturalOrg, &fakeTranslator{}, strings.Repeat("a", patterntranslator.MaxText+1), http.StatusBadRequest},
		"unavailable": {withPattern(), naturalOrg, &fakeTranslator{err: fmt.Errorf("%w: 529", patterntranslator.ErrUnavailable)},
			"Sundays", http.StatusServiceUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			rec := doRequest(t, newTranslatingHandler(tc.store, tc.org, tc.translator), http.MethodPost,
				"/api/rota-defaults/overrides/rule/translate", translateBody(tc.text), organiserCookie())
			assert.Equal(t, tc.status, rec.Code, rec.Body.String())
			if tc.status != http.StatusServiceUnavailable {
				assert.Empty(t, tc.translator.ruleRequests, "refused without asking")
			}
		})
	}
}
