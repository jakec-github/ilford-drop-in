package translatoreval

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jakechorley/ilford-drop-in/pkg/core/patterntranslator"
)

// The judge pairs the issues a model raised with the ones its case wants:
// which of each it is about, which were made up, and which were missed. That
// takes judgement the code checks cannot make — whether "Add how long the
// shifts run" is "the duration" — so a second model makes it.
var (
	//go:embed judge.md
	judgeSystem string
	//go:embed judge.schema.json
	judgeSchema []byte

	judgePrompt = newJudgePrompt()
)

func newJudgePrompt() *patterntranslator.Prompt {
	var schema map[string]any
	if err := json.Unmarshal(judgeSchema, &schema); err != nil {
		panic("translatoreval: judge.schema.json: " + err.Error())
	}
	return &patterntranslator.Prompt{Name: "judge", System: judgeSystem, Schema: schema}
}

// judgeTimeout is the ceiling on one judgement.
const judgeTimeout = time.Minute

// Judgement is the judge's pairing of the issues one run raised with the ones
// its case wants. It never fails a run: it is there to be read beside the
// kinds the code checks.
type Judgement struct {
	// Raised is a verdict on each issue raised, in order.
	Raised []Verdict `json:"raised,omitempty"`
	// Missed is each expected issue nothing raised was paired with.
	Missed []Miss                  `json:"missed,omitempty"`
	Usage  patterntranslator.Usage `json:"usage"`
	// Cost is the judge's, apart from the run's own.
	Cost float64 `json:"cost"`
	// Error is why there is no pairing: the judge could not be asked, or its
	// reply did not fit the issues.
	Error string `json:"error,omitempty"`
}

// Verdict is what one raised issue is about.
type Verdict struct {
	// Matches is the expected issue it is about, numbered from 1, or 0 for
	// one made up.
	Matches int    `json:"matches"`
	Reason  string `json:"reason"`
}

// Miss is an expected issue nothing raised was about.
type Miss struct {
	// Expected is its number, from 1.
	Expected int    `json:"expected"`
	Reason   string `json:"reason"`
}

// MadeUp is how many raised issues were about nothing wanted.
func (j Judgement) MadeUp() int {
	n := 0
	for _, v := range j.Raised {
		if v.Matches == 0 {
			n++
		}
	}
	return n
}

// judge asks the judge model to pair the issues raised with the case's.
func judge(ctx context.Context, model patterntranslator.Model, name string, c Case, raised []patterntranslator.Issue) *Judgement {
	ctx, cancel := context.WithTimeout(ctx, judgeTimeout)
	defer cancel()
	resp, err := model.Reply(ctx, judgePrompt, []patterntranslator.Turn{{Role: patterntranslator.User, Text: judgeTurn(c, raised)}})
	j := &Judgement{Usage: resp.Usage, Cost: cost(name, resp.Usage)}
	if err == nil {
		j.Raised, j.Missed, err = readJudgement(resp.Text, len(raised), len(c.Expect.Issues))
	}
	if err != nil {
		j.Error = err.Error()
	}
	return j
}

func judgeTurn(c Case, raised []patterntranslator.Issue) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<description>\n%s\n</description>\n\n", c.Text)
	if c.Kind == Rules {
		b.WriteString("<pattern>\n")
		for _, d := range c.Descriptions() {
			fmt.Fprintf(&b, "- %s\n", d)
		}
		b.WriteString("</pattern>\n\n")
	}
	b.WriteString("<expected>\n")
	for i, e := range c.Expect.Issues {
		kind := string(e.Kind)
		if e.Or != "" {
			kind += " or " + string(e.Or)
		}
		fmt.Fprintf(&b, "%d. %s: %s\n", i+1, kind, e.About)
	}
	b.WriteString("</expected>\n\n<raised>\n")
	for i, issue := range raised {
		fmt.Fprintf(&b, "%d. %s: %s\n", i+1, issue.Kind, issue.Message)
		if len(issue.Examples) > 0 {
			fmt.Fprintf(&b, "   Examples: %s\n", strings.Join(issue.Examples, "; "))
		}
	}
	b.WriteString("</raised>")
	return b.String()
}

// readJudgement is the judge's reply, checked against the issues it judged:
// a verdict on every raised issue, once each, pairing it with an expected one
// there is. What was missed is what nothing was paired with, whatever the
// judge listed; its reasons are kept for those.
func readJudgement(text string, raised, expected int) ([]Verdict, []Miss, error) {
	var reply struct {
		Raised []struct {
			Issue   int    `json:"issue"`
			Matches int    `json:"matches"`
			Reason  string `json:"reason"`
		} `json:"raised"`
		Missed []Miss `json:"missed"`
	}
	if err := json.Unmarshal([]byte(text), &reply); err != nil {
		return nil, nil, fmt.Errorf("the judge's reply does not read: %v", err)
	}
	verdicts := make([]Verdict, raised)
	seen := make([]bool, raised)
	paired := make([]bool, expected+1)
	for _, v := range reply.Raised {
		switch {
		case v.Issue < 1 || v.Issue > raised:
			return nil, nil, fmt.Errorf("the judge judged issue %d of %d", v.Issue, raised)
		case seen[v.Issue-1]:
			return nil, nil, fmt.Errorf("the judge judged issue %d twice", v.Issue)
		case v.Matches < 0 || v.Matches > expected:
			return nil, nil, fmt.Errorf("the judge paired issue %d with expected issue %d of %d", v.Issue, v.Matches, expected)
		}
		seen[v.Issue-1] = true
		verdicts[v.Issue-1] = Verdict{Matches: v.Matches, Reason: v.Reason}
		paired[v.Matches] = true
	}
	for i, ok := range seen {
		if !ok {
			return nil, nil, fmt.Errorf("the judge left out issue %d", i+1)
		}
	}
	reasons := map[int]string{}
	for _, m := range reply.Missed {
		reasons[m.Expected] = m.Reason
	}
	var missed []Miss
	for e := 1; e <= expected; e++ {
		if !paired[e] {
			missed = append(missed, Miss{Expected: e, Reason: reasons[e]})
		}
	}
	return verdicts, missed, nil
}

// judgeHash is a short hash of the judge's prompt and schema, as the
// translators' prompts are hashed.
func judgeHash() string {
	sum := sha256.Sum256(append([]byte(judgeSystem), judgeSchema...))
	return hex.EncodeToString(sum[:])[:12]
}
