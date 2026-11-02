# Translator evals

The plain-English translators (ADR 0015) are measured against the cases in
`cases/`. Each run asks the real model and writes a report to `reports/`.

Evals cost money, so they are **run by hand only**. Neither `scripts/check.sh`
nor CI calls the API. What they do run is free: the case files are loaded, and
each case is checked against a fake model that gives the case's own expected
answer, so a broken case is caught before a paid run.

## Running

From the repository's root, with a key for each provider the run asks, from
the evals' own workspace with it:

```bash
ANTHROPIC_API_KEY=… go run ./cmd/translator-eval
```

A model is named `<provider>-<tier>` (ADR 0016): `anthropic-small`,
`anthropic-medium`, `anthropic-large`, `mistral-small`, `mistral-medium` or
`mistral-large`. Each provider's key comes from its own environment variable,
`ANTHROPIC_API_KEY` or `MISTRAL_API_KEY`, and a run whose model and judge are
from different providers needs both:

```bash
ANTHROPIC_API_KEY=… MISTRAL_API_KEY=… go run ./cmd/translator-eval -model mistral-medium
```

Before the first call it prints how many calls the run will make and roughly
what they cost, the judge's apart, then waits for a yes.

| Flag | Default | What it does |
|---|---|---|
| `-model` | `anthropic-medium` | the `<provider>-<tier>` being tested, the tier the server translates with by default; `stub` runs the dev-mode stub, free and without a key |
| `-effort` | `low` | the model's effort; a provider with nothing like it ignores it |
| `-judge` | `anthropic-large` | the `<provider>-<tier>` that judges the issues raised (see below); `none` judges nothing |
| `-repeats` | `5` | runs per case |
| `-cases` | `all` | `patterns`, `rules`, a group's name, or one case's name |
| `-compare` | | an earlier report's `.json`, to say what changed |
| `-parallel` | `4` | translations run at once |
| `-yes` | | skips the confirm step |

To check one case quickly: `-cases first-saturday -repeats 1`. To run one
group from both files: `-cases "prompt injection"`.

## Cases

`cases/patterns.yaml` holds descriptions of a whole Shift pattern.
`cases/rules.yaml` holds descriptions of what an Override matches, each read
against a saved pattern. Each file's cases sit in groups by what they test,
and the report gives a pass rate for each group. A file's `zone`, `today`
and, for rules, `pattern` apply to every case in it unless the case gives its
own. Each case expects either a translation or issues:

```yaml
groups:
  - name: days and times
    cases:
      - name: first-saturday
        text: First Saturday of the month, 10am for 3 hours
        expect:
          pattern: |
            BEGIN:VEVENT
            DTSTART:20261107T100000
            DURATION:PT3H
            RRULE:FREQ=MONTHLY;BYDAY=1SA
            END:VEVENT

  - name: missing or unclear
    cases:
      - name: weekday-mornings-without-a-length
        text: Sunday evenings at 7:30 for 3 hours, and weekday mornings at 9
        expect:
          issues:
            - {kind: missing, about: how long the weekday morning shifts run}
```

An expected issue whose kind could fairly be either of two, where the
difference would not matter to an Organiser, gives both:
`{kind: ambiguous, or: missing, about: …}`.

A case tests one thing. Say everything else in its text plainly, so a reply
that gets that one thing right has nothing else to raise.

- **A translation is graded by its Shifts, not its text.** Both patterns are
  expanded over a year from the case's `today` and must give the same Shift
  starts and lengths. A rule must pick out the same Shifts of its pattern as
  the expected rule. The expected answer is one way of writing them, not the
  only one.
- **Issues are graded in code** on the reply's status and its set of kinds.
- **Then a judge pairs them.** Whether "Add how long the shifts run" is the
  case's "missing: the duration" takes judgement, so a second model, the
  judge, is given the description, the issues the case wants and the ones the
  model raised, each wanted one with its `about` note. It pairs each one
  raised with the one wanted that it is about, or marks it **made up**, and marks each one wanted that nothing was
  about as **missed**. It does not judge the wording.
  - Only replies of issues where issues were wanted are judged, one call
    each. The stub is never judged.
  - Its verdicts never fail a run: the report counts them, and lists every
    one with the judge's reason, so a wrong verdict can be seen.
  - Its prompt and schema are `internal/translatoreval/judge.md` and
    `judge.schema.json`. A judge that is the model being tested is warned
    about, since it may be blind to its own mistakes.
- **Wording is scored, and never fails a run.** Each message of issues the
  model raised scores from 0 to 1. A message naming the format's terms
  (RRULE, DTSTART, …) scores 0. A message over the prompts' 15-word limit
  loses in proportion: 18 words scores 0.8, and 30 scores 0. A run's score is
  the mean over its messages, and the report gives the mean beside each pass
  rate.
- Each case fixes `today`, so a case means the same thing whenever it is run.

### Repair cases

When the parser refuses a reply, the translator sends its messages back and
asks again, up to three calls. A good model is rarely refused, so first
attempts say little about that loop. A **repair case** says more: it plants
the refused reply the model is taken to have written first.

```yaml
      - name: repair-no-duration
        text: Sundays 7:30 to 10:30pm
        planted: |
          BEGIN:VEVENT
          DTSTART:20261101T193000
          RRULE:FREQ=WEEKLY;BYDAY=SU
          END:VEVENT
        expect:
          pattern: |
            …
```

- The first call returns `planted` without asking the model, and costs
  nothing. The translator's own loop then runs as it does in the app: the
  parser refuses the reply, its real messages go back, and the model's
  replies after that are graded against `expect`.
- A planted reply the parser accepts is a broken case, and stops the run
  before any call.
- Passing the parser is not enough. A repair that drops the hard part of
  the description parses too, and is graded as the wrong answer it is.
- The report keeps repairs out of the pass rate, in a section of their own:
  fixed, and of those how many on the model's first call; parsed but changed
  (an answer, but the wrong one); not fixed (every call refused).

Both files keep theirs in a group called "repairing a refused reply", so
`-cases "repairing a refused reply"` runs just those.

## Reports

Each run writes `reports/<date>-<provider>-<tier>-<effort>.md`, and `.json` beside it
for `-compare`.

Every call after a run's first means the model's reply did not stand, so a
report leads with how often the first reply was right, not only how often the
run was right in the end. A pass on a later call still passes: the product
worked, but it took longer and cost more.

Each report starts with:
- the `<provider>-<tier>` and the model it was, and the effort, so a report
  still says what ran after a tier moves to a newer model;
- a hash of each prompt;
- pass rates on the first call and in the end, wording scores and, with a
  judge, how many replies of issues it judged right (nothing made up, nothing
  missed), overall and for each group;
- every call made again, by why: the parser refused the reply before it, or
  the reply could not be used at all (issues with none in them, an empty
  answer, JSON that does not read, a refusal, a reply cut off);
- how many issues the judge found made up and missed, and its cost, apart from
  the cost of the model being tested.

For each case it gives:
- the pass rates on the first call and in the end, the wording score and, with
  a judge, the replies it judged right;
- the calls each run took: first, second or third, gave up, or unavailable;
- the cost;
- typical and slow (p95) latency.

Below that, every failed run is listed with what went wrong, then under
Second goes every run that passed on a later call. Both show a run's calls
when it took more than one, or gave up: each call's raw reply, the summary of
the model's thinking behind it, and why the model was asked again. A reply
that says nothing, such as issues with none in them, goes back with what it
left out. A reply the loop cannot read at all is asked again the same way and
leaves no message, so only its reply says what happened. The eval asks for the thinking; the
server does not. The `.json` keeps it for every call, not only the ones shown.
Under Judged issues, every judged run lists each issue raised with the judge's
verdict and reason, then each one missed. Every message that lost wording
points is listed after that.

`-compare` gives both rates before and after. An older report without the
reasons for its calls still compares: its first-call rate comes from how many
calls each run took.

Commit only the reports worth keeping, such as the run that chose a model or
the runs before and after a prompt change. They hold only our own cases and
the model's replies and thinking.
