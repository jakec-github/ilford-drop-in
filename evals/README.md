# Translator evals

The plain-English translators (ADR 0015) are measured against the cases in
`cases/`. Each run asks the real model and writes a report to `reports/`.

Evals cost money, so they are **run by hand only**. Neither `scripts/check.sh`
nor CI calls the API. What they do run is free: the case files are loaded, and
each case is checked against a fake model that gives the case's own expected
answer, so a broken case is caught before a paid run.

## Running

From the repository's root, with a key for the evals' Anthropic workspace:

```bash
ANTHROPIC_API_KEY=… go run ./cmd/translator-eval
```

Before the first call it prints how many calls the run will make and roughly
what they cost, then waits for a yes.

| Flag | Default | What it does |
|---|---|---|
| `-model` | `claude-sonnet-5-5` | the model being tested; `stub` runs the dev-mode stub, free and without a key |
| `-effort` | `low` | the model's effort |
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
- **Wording is scored, and never fails a run.** Each message of issues the
  model raised scores from 0 to 1. A message naming the format's terms
  (RRULE, DTSTART, …) scores 0. A message over the prompts' 15-word limit
  loses in proportion: 18 words scores 0.8, and 30 scores 0. A run's score is
  the mean over its messages, and the report gives the mean beside each pass
  rate.

  Each issue's `about` note says what it should be about. The code doesn't
  check it, since that takes a judge.
- Each case fixes `today`, so a case means the same thing whenever it is run.

## Reports

Each run writes `reports/<date>-<model>-<effort>.md`, and `.json` beside it
for `-compare`.

Each report starts with:
- the model and effort;
- a hash of each prompt;
- pass rates and wording scores, overall and for each group.

For each case it gives:
- the pass rate and wording score;
- the calls each run took: first, second or third, gave up, or unavailable;
- the cost;
- typical and slow (p95) latency.

Below that, every failed run is listed with what went wrong. A run that gave
up shows each call's raw reply, since a reply the loop cannot read, such as
issues with none in them, never reaches the parser and leaves no message of
its own. Every message that lost wording points is listed after that.

Commit only the reports worth keeping, such as the run that chose a model or
the runs before and after a prompt change. They hold only our own cases and
the model's replies.
