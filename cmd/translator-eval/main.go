// Command translator-eval measures the plain-English translators against the
// cases in evals/cases and writes a report to evals/reports. Run it from the
// repository's root:
//
//	ANTHROPIC_API_KEY=... go run ./cmd/translator-eval
//	go run ./cmd/translator-eval -model stub -yes
//
// matrix runs the same eval on several models, each at its own effort, saves
// each as an ordinary report, and compares them in a page of HTML; compare
// builds that comparison again from saved reports alone, without asking any
// model:
//
//	go run ./cmd/translator-eval matrix -models anthropic-small:low,mistral-small:high
//	go run ./cmd/translator-eval compare evals/reports/a.json evals/reports/b.json
//
// A model is named by <provider>-<tier>, such as anthropic-medium (ADR 0016),
// and each provider's key is read from its own environment variable.
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
	"slices"
	"strings"
	"time"

	"github.com/jakechorley/ilford-drop-in/internal/translatoreval"
	"github.com/jakechorley/ilford-drop-in/pkg/core/llm"
	"github.com/jakechorley/ilford-drop-in/pkg/core/llm/providers"
	"github.com/jakechorley/ilford-drop-in/pkg/core/patterntranslator"
)

func main() {
	var err error
	args := os.Args[1:]
	switch {
	case len(args) > 0 && args[0] == "matrix":
		err = runMatrix(args[1:])
	case len(args) > 0 && args[0] == "compare":
		err = runCompare(args[1:])
	default:
		err = runOne(args)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "translator-eval:", err)
		os.Exit(1)
	}
}

// common is the flags every run shares.
type common struct {
	repeats, parallel *int
	which, judge      *string
	yes               *bool
	casesDir, out     *string
}

func commonFlags(fs *flag.FlagSet) common {
	return common{
		repeats:  fs.Int("repeats", 5, "how many times each case is translated"),
		which:    fs.String("cases", "all", "all, patterns, rules, a group's name, or one case's name"),
		judge:    fs.String("judge", "anthropic-large", "the <provider>-<tier> that judges the issues raised, or none"),
		parallel: fs.Int("parallel", 4, "how many translations run at once"),
		yes:      fs.Bool("yes", false, "run without asking first"),
		casesDir: fs.String("in", "evals/cases", "the directory of case files"),
		out:      fs.String("out", "evals/reports", "the directory reports are written to"),
	}
}

func runOne(args []string) error {
	fs := flag.NewFlagSet("translator-eval", flag.ExitOnError)
	model := fs.String("model", "anthropic-"+string(patterntranslator.Tier), "the <provider>-<tier> to evaluate, or stub for the dev-mode stub, which is free")
	effort := fs.String("effort", "low", "the model's effort")
	compare := fs.String("compare", "", "an earlier report's .json, to say what changed")
	f := commonFlags(fs)
	fs.Parse(args)
	if err := stray(fs, args); err != nil {
		return err
	}

	cases, err := loadCases(*f.casesDir, *f.which)
	if err != nil {
		return err
	}
	var earlier *translatoreval.Report
	if *compare != "" {
		if earlier, err = readReport(*compare); err != nil {
			return err
		}
	}
	judge, err := newJudge(*f.judge, []string{*model})
	if err != nil {
		return err
	}
	subject, err := newSubject(*model, *effort, judge)
	if err != nil {
		return err
	}

	if !estimate([]translatoreval.Subject{subject}, cases, *f.repeats, *f.yes) {
		return nil
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	_, err = runAndSave(ctx, subject, cases, f, earlier)
	return err
}

func runMatrix(args []string) error {
	fs := flag.NewFlagSet("translator-eval matrix", flag.ExitOnError)
	models := fs.String("models", "", "the <provider>-<tier>:<effort> pairs to evaluate, comma separated, such as anthropic-small:low,mistral-small:high")
	f := commonFlags(fs)
	fs.Parse(args)
	if err := stray(fs, args); err != nil {
		return err
	}
	if *models == "" {
		return fmt.Errorf("matrix needs -models, such as -models anthropic-small:low,anthropic-medium:low")
	}

	cases, err := loadCases(*f.casesDir, *f.which)
	if err != nil {
		return err
	}
	var refs, efforts []string
	for _, pair := range strings.Split(*models, ",") {
		ref, effort, _ := strings.Cut(strings.TrimSpace(pair), ":")
		if effort == "" {
			effort = "low"
		}
		refs, efforts = append(refs, ref), append(efforts, effort)
	}
	// One judge for every pair, so their issues are judged alike.
	judge, err := newJudge(*f.judge, refs)
	if err != nil {
		return err
	}
	var subjects []translatoreval.Subject
	for i, ref := range refs {
		s, err := newSubject(ref, efforts[i], judge)
		if err != nil {
			return fmt.Errorf("%s:%s: %w", ref, efforts[i], err)
		}
		subjects = append(subjects, s)
	}

	if !estimate(subjects, cases, *f.repeats, *f.yes) {
		return nil
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	var reports []*translatoreval.Report
	for _, s := range subjects {
		if ctx.Err() != nil {
			break
		}
		r, err := runAndSave(ctx, s, cases, f, nil)
		if err != nil {
			return err
		}
		reports = append(reports, r)
	}
	return saveComparison(reports, *f.out)
}

// stray refuses what is left of args once fs has parsed its flags. The flag
// package stops at the first word that is not a flag, so a subcommand after a
// flag, as in -repeats 5 matrix, would otherwise quietly run one eval of the
// default model and drop everything after it.
func stray(fs *flag.FlagSet, args []string) error {
	if fs.NArg() == 0 {
		return nil
	}
	word := fs.Arg(0)
	if word != "matrix" && word != "compare" {
		return fmt.Errorf("unexpected %q: every option is a flag, such as -model %s", word, word)
	}
	rest := slices.Clone(args)
	rest = slices.Delete(rest, len(args)-fs.NArg(), len(args)-fs.NArg()+1)
	return fmt.Errorf("%s must come before any flag: translator-eval %s", word, strings.Join(append([]string{word}, rest...), " "))
}

func runCompare(args []string) error {
	fs := flag.NewFlagSet("translator-eval compare", flag.ExitOnError)
	out := fs.String("out", "evals/reports", "the directory the comparison is written to")
	fs.Parse(args)
	if fs.NArg() == 0 {
		return fmt.Errorf("compare needs the reports' .json files")
	}
	var reports []*translatoreval.Report
	for _, path := range fs.Args() {
		r, err := readReport(path)
		if err != nil {
			return err
		}
		reports = append(reports, r)
	}
	return saveComparison(reports, *out)
}

func loadCases(dir, which string) ([]translatoreval.Case, error) {
	var all []translatoreval.Case
	for _, kind := range []string{translatoreval.Patterns, translatoreval.Rules} {
		data, err := os.ReadFile(filepath.Join(dir, kind+".yaml"))
		if err != nil {
			return nil, err
		}
		cases, err := translatoreval.ReadCases(kind, data)
		if err != nil {
			return nil, err
		}
		all = append(all, cases...)
	}
	return translatoreval.Select(all, which)
}

func readReport(path string) (*translatoreval.Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	r, err := translatoreval.ReadReport(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return r, nil
}

// judge is the model that judges, or nil for none.
type judge struct {
	ref, id string
	model   llm.Model
	price   *llm.Price
}

// newJudge is the judge ref names for models, or none where ref is none or
// every model is the stub, which is never judged.
func newJudge(ref string, models []string) (*judge, error) {
	if ref == "none" || !slices.ContainsFunc(models, func(m string) bool { return m != "stub" }) {
		return nil, nil
	}
	m, id, price, err := ask(ref, llm.Options{Effort: judgeEffort})
	if err != nil {
		return nil, err
	}
	return &judge{ref: ref, id: id, model: m, price: price}, nil
}

// newSubject is model at effort, judged by j where there is one. The stub is
// never judged.
func newSubject(model, effort string, j *judge) (translatoreval.Subject, error) {
	subject := translatoreval.Subject{Model: model, Effort: effort}
	if model == "stub" {
		subject.Effort = ""
		return subject, nil
	}
	// The thinking is there to read beside a reply that went wrong.
	base, id, price, err := ask(model, llm.Options{Effort: effort, ShowThinking: true})
	if err != nil {
		return subject, err
	}
	subject.Base, subject.ModelID, subject.Price = base, id, price
	if j != nil {
		if j.ref == model {
			fmt.Printf("Warning: %s is judging its own issues, and may be blind to what it gets wrong.\n", model)
		}
		subject.Judge, subject.JudgeModel, subject.JudgeModelID, subject.JudgePrice = j.model, j.ref, j.id, j.price
	}
	return subject, nil
}

// estimate says what running cases on every subject will take, and is whether
// to go ahead: asked once for them all, unless yes or nothing costs anything.
func estimate(subjects []translatoreval.Subject, cases []translatoreval.Case, repeats int, yes bool) bool {
	var total, judging float64
	priced, judgePriced, paid := true, true, false
	for _, s := range subjects {
		e := translatoreval.EstimateRun(s, cases, repeats)
		fmt.Printf("%d cases × %d repeats on %s: %d calls, up to %d if the parser refuses answers",
			len(cases), repeats, name(s.Model, s.ModelID), e.Calls, e.MaxCalls)
		switch {
		case s.Base == nil:
			fmt.Println(", free.")
		case e.Priced:
			fmt.Printf(", about $%.2f.\n", e.Cost)
		default:
			fmt.Println(". The model has no price, so the cost is unknown.")
		}
		if s.Judge != nil {
			fmt.Printf("  %s judges each reply of issues: about %d calls", name(s.JudgeModel, s.JudgeModelID), e.JudgeCalls)
			if e.JudgePriced {
				fmt.Printf(", about $%.2f.\n", e.JudgeCost)
			} else {
				fmt.Println(". It has no price, so the cost is unknown.")
			}
		}
		paid = paid || s.Base != nil
		priced = priced && (s.Base == nil || e.Priced)
		judgePriced = judgePriced && (s.Judge == nil || e.JudgePriced)
		total += e.Cost
		judging += e.JudgeCost
	}
	if len(subjects) > 1 && paid {
		fmt.Printf("In all: about $%.2f", total+judging)
		if !priced || !judgePriced {
			fmt.Print(", and more for what has no price")
		}
		fmt.Println(".")
	}
	if yes || !paid {
		return true
	}
	fmt.Print("Run it? [y/N] ")
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
		fmt.Println("Not run.")
		return false
	}
	return true
}

// runAndSave runs cases on subject and writes its report, said against
// earlier where there is one.
func runAndSave(ctx context.Context, subject translatoreval.Subject, cases []translatoreval.Case, f common, earlier *translatoreval.Report) (*translatoreval.Report, error) {
	fmt.Fprintf(os.Stderr, "%s\n", name(subject.Model, subject.ModelID))
	report := translatoreval.Run(ctx, subject, cases, translatoreval.Options{
		Repeats:  *f.repeats,
		Parallel: *f.parallel,
		Progress: func(done, total int) { fmt.Fprintf(os.Stderr, "\r%d/%d", done, total) },
	})
	fmt.Fprintln(os.Stderr)

	base, err := free(*f.out, translatoreval.FileName(report), ".json", ".md")
	if err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(base+".json", append(data, '\n'), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(base+".md", []byte(translatoreval.Render(report, earlier)), 0o644); err != nil {
		return nil, err
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
	return report, nil
}

func saveComparison(reports []*translatoreval.Report, out string) error {
	if len(reports) == 0 {
		return nil
	}
	base, err := free(out, translatoreval.ComparisonFileName(time.Now()), ".html")
	if err != nil {
		return err
	}
	if err := os.WriteFile(base+".html", []byte(translatoreval.RenderComparison(translatoreval.Compare(reports))), 0o644); err != nil {
		return err
	}
	fmt.Printf("Comparison: %s.html\n", base)
	return nil
}

// free is the path in dir, without extension, that name with each of exts
// can be written to without overwriting a file: name, or name-2, -3 and so on.
func free(dir, name string, exts ...string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	base := filepath.Join(dir, name)
	taken := func(base string) bool {
		for _, ext := range exts {
			if exists(base + ext) {
				return true
			}
		}
		return false
	}
	for n := 2; taken(base); n++ {
		base = filepath.Join(dir, fmt.Sprintf("%s-%d", name, n))
	}
	return base, nil
}

// ask is the Model that ref, a <provider>-<tier>, names: the model id it
// resolves to, and its price. The provider's key comes from the environment
// only, so a key in a config file is never used without someone choosing to
// export it.
func ask(ref string, o llm.Options) (llm.Model, string, *llm.Price, error) {
	provider, tier, err := providers.Parse(ref)
	if err != nil {
		return nil, "", nil, err
	}
	key := os.Getenv(provider.KeyEnv)
	if key == "" {
		return nil, "", nil, fmt.Errorf("set %s to ask %s, or use -model stub", provider.KeyEnv, ref)
	}
	m := provider.Tiers[tier]
	return provider.Ask(key, tier, o), m.ID, &m.Price, nil
}

// name is a <provider>-<tier> with the model it is.
func name(ref, id string) string {
	if id == "" {
		return ref
	}
	return fmt.Sprintf("%s (%s)", ref, id)
}

// judgeEffort is the judge's effort. Pairing a few issues is not hard, and a
// reply shares the translators' token limit, thinking included.
const judgeEffort = "medium"

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
