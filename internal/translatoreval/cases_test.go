package translatoreval

import (
	"strings"
	"testing"
	"time"

	"github.com/jakechorley/ilford-drop-in/pkg/core/patterntranslator"
)

const sundays = `BEGIN:VEVENT
DTSTART:20261101T193000
DURATION:PT3H
RRULE:FREQ=WEEKLY;BYDAY=SU
END:VEVENT`

const twoSundays = `BEGIN:VEVENT
DTSTART:20261101T100000
DURATION:PT2H
RRULE:FREQ=WEEKLY;BYDAY=SU
END:VEVENT
BEGIN:VEVENT
DTSTART:20261101T193000
DURATION:PT3H
RRULE:FREQ=WEEKLY;BYDAY=SU
END:VEVENT`

func TestReadCases_TheFileGivesDefaultsACaseCanOverride(t *testing.T) {
	cases, err := ReadCases(Patterns, []byte(`
zone: Europe/London
today: 2026-11-01
groups:
  - name: g
    cases:
      - name: sundays
        text: Sundays 7:30 to 10:30pm
        expect:
          pattern: |
`+indent(sundays, 12)+`
  - name: h
    cases:
      - name: no-length
        text: Sunday evenings
        today: 2026-12-25
        zone: America/New_York
        expect:
          issues:
            - kind: missing
              about: how long the shifts run
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 2 {
		t.Fatalf("got %d cases, want 2", len(cases))
	}
	if c := cases[0]; c.Kind != Patterns || c.Group != "g" || c.Zone != "Europe/London" || !c.Today.Equal(date(2026, 11, 1)) || !c.Translates() {
		t.Errorf("first case: %+v", c)
	}
	c := cases[1]
	if c.Group != "h" || c.Zone != "America/New_York" || !c.Today.Equal(date(2026, 12, 25)) || c.Translates() {
		t.Errorf("second case: %+v", c)
	}
	want := []ExpectedIssue{{Kind: patterntranslator.Missing, About: "how long the shifts run"}}
	if len(c.Expect.Issues) != 1 || c.Expect.Issues[0] != want[0] {
		t.Errorf("issues: %+v, want %+v", c.Expect.Issues, want)
	}
}

func TestReadCases_ARuleCaseIsReadAgainstThePatternItNames(t *testing.T) {
	cases, err := ReadCases(Rules, []byte(`
zone: Europe/London
today: 2026-11-01
pattern: |
`+indent(twoSundays, 2)+`
groups:
  - name: g
    cases:
      - name: evening
        text: The Sunday evening shift
        expect:
          rule: |
            DTSTART;VALUE=DATE:20261101
            RRULE:FREQ=WEEKLY;BYDAY=SU;BYHOUR=19;BYMINUTE=30
`))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"Every Sunday at 10:00 for 2 hours, from 1 Nov 2026",
		"Every Sunday at 19:30 for 3 hours, from 1 Nov 2026",
	}
	if got := cases[0].Descriptions(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("descriptions: %q, want %q", got, want)
	}
}

func TestReadCases_ABrokenCaseStopsTheRun(t *testing.T) {
	for _, tc := range []struct {
		name, kind, file, says string
	}{
		{"an expected pattern the parser refuses", Patterns, `
zone: Europe/London
today: 2026-11-01
groups:
  - name: g
    cases:
      - name: broken
        text: Sundays
        expect:
          pattern: "BEGIN:VEVENT\nDTSTART:20261101T193000\nRRULE:FREQ=WEEKLY\nEND:VEVENT"
`, `broken: the expected pattern`},
		{"an expected rule the parser refuses", Rules, `
zone: Europe/London
today: 2026-11-01
pattern: "` + strings.ReplaceAll(sundays, "\n", `\n`) + `"
groups:
  - name: g
    cases:
      - name: broken
        text: Sundays
        expect:
          rule: "DTSTART;VALUE=DATE:20261101\nRRULE:FREQ=WEEKLY;BYHOUR=19"
`, `broken: the expected rule`},
		{"a rule case with no pattern", Rules, `
zone: Europe/London
today: 2026-11-01
groups:
  - name: g
    cases:
      - name: unmatched
        text: Sundays
        expect:
          rule: "DTSTART;VALUE=DATE:20261101\nRRULE:FREQ=WEEKLY;BYDAY=SU"
`, `unmatched: no pattern`},
		{"both a translation and issues", Patterns, `
zone: Europe/London
today: 2026-11-01
groups:
  - name: g
    cases:
      - name: both
        text: Sundays
        expect:
          pattern: "` + strings.ReplaceAll(sundays, "\n", `\n`) + `"
          issues: [{kind: missing, about: length}]
`, `both: expect`},
		{"a rule in a pattern case", Patterns, `
zone: Europe/London
today: 2026-11-01
groups:
  - name: g
    cases:
      - name: wrong
        text: Sundays
        expect:
          rule: "DTSTART;VALUE=DATE:20261101\nRRULE:FREQ=WEEKLY;BYDAY=SU"
`, `wrong: expect`},
		{"an issue of no kind there is", Patterns, `
zone: Europe/London
today: 2026-11-01
groups:
  - name: g
    cases:
      - name: odd
        text: Sundays
        expect:
          issues: [{kind: vague, about: length}]
`, `odd: issue kind "vague"`},
		{"an issue about nothing", Patterns, `
zone: Europe/London
today: 2026-11-01
groups:
  - name: g
    cases:
      - name: blank
        text: Sundays
        expect:
          issues: [{kind: missing}]
`, `blank: every expected issue says what it is about`},
		{"two cases of one name", Patterns, `
zone: Europe/London
today: 2026-11-01
groups:
  - name: g
    cases:
      - {name: twice, text: a, expect: {issues: [{kind: missing, about: x}]}}
      - {name: twice, text: b, expect: {issues: [{kind: missing, about: x}]}}
`, `twice: two cases`},
		{"no today", Patterns, `
zone: Europe/London
groups:
  - name: g
    cases:
      - {name: when, text: a, expect: {issues: [{kind: missing, about: x}]}}
`, `when: today`},
		{"a zone there is not", Patterns, `
zone: Europe/Ilford
today: 2026-11-01
groups:
  - name: g
    cases:
      - {name: where, text: a, expect: {issues: [{kind: missing, about: x}]}}
`, `where: zone`},
		{"a description too long to translate", Patterns, `
zone: Europe/London
today: 2026-11-01
groups:
  - name: g
    cases:
      - {name: long, text: ` + strings.Repeat("a", patterntranslator.MaxText+1) + `, expect: {issues: [{kind: missing, about: x}]}}
`, `long: the text`},
		{"a key the file does not have", Patterns, `
zone: Europe/London
today: 2026-11-01
groups:
  - name: g
    cases:
      - {name: typo, text: a, expcet: {issues: [{kind: missing, about: x}]}}
`, `expcet`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ReadCases(tc.kind, []byte(tc.file))
			if err == nil || !strings.Contains(err.Error(), tc.says) {
				t.Errorf("err = %v, want one saying %q", err, tc.says)
			}
		})
	}
}

func TestReadCases_EveryGroupHasANameOfItsOwn(t *testing.T) {
	for name, groups := range map[string]string{
		"none":  "  - cases: [{name: a, text: a, expect: {issues: [{kind: missing, about: x}]}}]\n",
		"twice": "  - {name: g, cases: [{name: a, text: a, expect: {issues: [{kind: missing, about: x}]}}]}\n  - {name: g, cases: []}\n",
	} {
		_, err := ReadCases(Patterns, []byte("zone: Europe/London\ntoday: 2026-11-01\ngroups:\n"+groups))
		if err == nil || !strings.Contains(err.Error(), "every group needs a name of its own") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func indent(s string, n int) string {
	pad := strings.Repeat(" ", n)
	return pad + strings.ReplaceAll(s, "\n", "\n"+pad)
}

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestSelect_EveryCaseEitherKindAGroupOrOneByName(t *testing.T) {
	cases := []Case{{Name: "a", Kind: Patterns, Group: "g"}, {Name: "b", Kind: Rules, Group: "g"}, {Name: "c", Kind: Patterns}}
	for which, want := range map[string]string{"": "abc", "all": "abc", Patterns: "ac", Rules: "b", "b": "b", "g": "ab"} {
		got, err := Select(cases, which)
		var names string
		for _, c := range got {
			names += c.Name
		}
		if err != nil || names != want {
			t.Errorf("Select(%q) = %q, %v; want %q", which, names, err, want)
		}
	}
	if _, err := Select(cases, "d"); err == nil {
		t.Error("an unknown case was selected")
	}
}
