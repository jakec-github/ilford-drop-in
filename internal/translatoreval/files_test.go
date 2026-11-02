package translatoreval

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jakechorley/ilford-drop-in/pkg/core/patterntranslator"
)

// readCaseFiles is the cases the command runs, read as it reads them.
func readCaseFiles(t *testing.T) []Case {
	t.Helper()
	var all []Case
	names := map[string]bool{}
	for _, kind := range []string{Patterns, Rules} {
		data, err := os.ReadFile(filepath.Join("..", "..", "evals", "cases", kind+".yaml"))
		if err != nil {
			t.Fatal(err)
		}
		cases, err := ReadCases(kind, data)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cases {
			if names[c.Name] {
				t.Errorf("%s: two cases have this name, so -cases cannot pick one", c.Name)
			}
			names[c.Name] = true
		}
		all = append(all, cases...)
	}
	return all
}

// TestCaseFiles_EveryCasePassesWithItsOwnAnswer runs each case against a model
// that replies with the case's own expected answer. A case that fails then is
// one no model could pass — an expected issue no message could satisfy, say —
// so it is caught here, for free, rather than in a paid run.
func TestCaseFiles_EveryCasePassesWithItsOwnAnswer(t *testing.T) {
	for _, c := range readCaseFiles(t) {
		reply := map[string]any{"status": "ok"}
		switch {
		case c.Expect.Pattern != "":
			reply["pattern"] = c.Expect.Pattern
		case c.Expect.Rule != "":
			reply["rule"] = c.Expect.Rule
		default:
			var issues []patterntranslator.Issue
			for _, want := range c.Expect.Issues {
				issues = append(issues, patterntranslator.Issue{Kind: want.Kind, Message: "Say more", Examples: []string{}})
			}
			reply = map[string]any{"status": "issues", "issues": issues}
		}
		text, _ := json.Marshal(reply)
		model := &fakeModel{replies: map[string][]string{c.Text: {string(text)}}}

		r := Run(context.Background(), Subject{Model: "oracle", Base: model}, []Case{c}, Options{Repeats: 1}).Cases[0].Runs[0]
		if !r.Pass {
			t.Errorf("%s: %v", c.Name, r.Failures)
		}
	}
}
