// Command translator-eval measures the plain-English translators against the
// cases in evals/cases and writes a report to evals/reports. Run it from the
// repository's root:
//
//	ANTHROPIC_API_KEY=... go run ./cmd/translator-eval
//	go run ./cmd/translator-eval -model stub -yes
//
// A second model, the judge, pairs the issues each reply raises with the ones
// its case wants. The stub is never judged.
//
// It asks the real model and costs money, so it is run by hand, never by
// scripts/check.sh or CI. Before the first call it says how many calls it will
// make and roughly what they cost, and waits for a yes.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/jakechorley/ilford-drop-in/internal/translatoreval"
	"github.com/jakechorley/ilford-drop-in/pkg/core/llm"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "translator-eval:", err)
		os.Exit(1)
	}
}

func run() error {
	model := flag.String("model", "claude-sonnet-5-5", "the model to evaluate, or stub for the dev-mode stub, which is free")
	effort := flag.String("effort", "low", "the model's effort")
	repeats := flag.Int("repeats", 5, "how many times each case is translated")
	which := flag.String("cases", "all", "all, patterns, rules, a group's name, or one case's name")
	judge := flag.String("judge", "claude-opus-5-5", "the model that judges the issues raised, or none")
	compare := flag.String("compare", "", "an earlier report's .json, to say what changed")
	parallel := flag.Int("parallel", 4, "how many translations run at once")
	yes := flag.Bool("yes", false, "run without asking first")
	casesDir := flag.String("in", "evals/cases", "the directory of case files")
	out := flag.String("out", "evals/reports", "the directory reports are written to")
	flag.Parse()

	var all []translatoreval.Case
	for _, kind := range []string{translatoreval.Patterns, translatoreval.Rules} {
		data, err := os.ReadFile(filepath.Join(*casesDir, kind+".yaml"))
		if err != nil {
			return err
		}
		cases, err := translatoreval.ReadCases(kind, data)
		if err != nil {
			return err
		}
		all = append(all, cases...)
	}
	cases, err := translatoreval.Select(all, *which)
	if err != nil {
		return err
	}

	var earlier *translatoreval.Report
	if *compare != "" {
		data, err := os.ReadFile(*compare)
		if err != nil {
			return err
		}
		if earlier, err = translatoreval.ReadReport(data); err != nil {
			return fmt.Errorf("%s: %w", *compare, err)
		}
	}

	subject := translatoreval.Subject{Model: *model, Effort: *effort}
	if *model == "stub" {
		subject.Effort = ""
	} else {
		// From the environment only, so a key in a config file is never used
		// without someone choosing to export it.
		key := os.Getenv("ANTHROPIC_API_KEY")
		if key == "" {
			return fmt.Errorf("set ANTHROPIC_API_KEY to ask %s, or use -model stub", *model)
		}
		base := llm.NewAnthropic(key, *model, *effort)
		// The thinking is there to read beside a reply that went wrong.
		base.ShowThinking = true
		subject.Base = base

		if *judge != "none" {
			if *judge == *model {
				fmt.Printf("Warning: %s is judging its own issues, and may be blind to what it gets wrong.\n", *judge)
			}
			subject.JudgeModel = *judge
			subject.Judge = llm.NewAnthropic(key, *judge, judgeEffort)
		}
	}

	e := translatoreval.EstimateRun(*model, subject.JudgeModel, cases, *repeats)
	fmt.Printf("%d cases × %d repeats on %s: %d calls, up to %d if the parser refuses answers",
		len(cases), *repeats, *model, e.Calls, e.MaxCalls)
	switch {
	case subject.Base == nil:
		fmt.Println(", free.")
	case e.Priced:
		fmt.Printf(", about $%.2f.\n", e.Cost)
	default:
		fmt.Println(". The model has no price, so the cost is unknown.")
	}
	if subject.Judge != nil {
		fmt.Printf("%s judges each reply of issues: about %d calls", subject.JudgeModel, e.JudgeCalls)
		if e.JudgePriced {
			fmt.Printf(", about $%.2f.\n", e.JudgeCost)
		} else {
			fmt.Println(". It has no price, so the cost is unknown.")
		}
	}
	if !*yes && subject.Base != nil {
		fmt.Print("Run it? [y/N] ")
		answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
			fmt.Println("Not run.")
			return nil
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	report := translatoreval.Run(ctx, subject, cases, translatoreval.Options{
		Repeats:  *repeats,
		Parallel: *parallel,
		Progress: func(done, total int) { fmt.Fprintf(os.Stderr, "\r%d/%d", done, total) },
	})
	fmt.Fprintln(os.Stderr)

	if err := os.MkdirAll(*out, 0o755); err != nil {
		return err
	}
	base := filepath.Join(*out, translatoreval.FileName(report))
	for n := 2; exists(base+".json") || exists(base+".md"); n++ {
		base = filepath.Join(*out, fmt.Sprintf("%s-%d", translatoreval.FileName(report), n))
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(base+".json", append(data, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(base+".md", []byte(translatoreval.Render(report, earlier)), 0o644); err != nil {
		return err
	}

	s := translatoreval.Summarise(report.Cases)
	repairs := ""
	if r := s.Repairs.Rate(); r.Runs > 0 {
		repairs = fmt.Sprintf(", fixed %s, %s on the first model call", r, r.FirstCall())
	}
	judged := ""
	if j := s.Judged; j.Replies > 0 {
		judged = fmt.Sprintf("; the judge found %d made up, %d missed", j.MadeUp, j.Missed)
	}
	fmt.Printf("Passed %s on the first call, %s in the end%s%s. Report: %s.md\n", s.All.FirstCall(), s.All, repairs, judged, base)
	return nil
}

// judgeEffort is the judge's effort. Pairing a few issues is not hard, and a
// reply shares the translators' token limit, thinking included.
const judgeEffort = "medium"

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
