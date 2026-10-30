# Shifts come from a pattern, and Overrides change them

Status: proposed, 2026-10-27. Supersedes the one-Shift-per-date rule in
[ADR 0007](0007-shift-start-as-local-time.md), and the shift times and Standing
Preallocations in [ADR 0006](0006-domain-settings-in-the-app.md); the rest of
both stands. Partly superseded by
[ADR 0014](0014-pattern-entries-carry-their-own-start-and-length.md): Start
from beside the rule, one default length, and one line making one Shift. The
work is split across #291–#297 under #23. The prototype is
[`docs/design/issue-23/shift-patterns.html`](../design/issue-23/shift-patterns.html).

## In plain words

An Organisation no longer has one start time and a weekly rota. It has a
**Shift pattern**: one or more RRULEs saying when Shifts start — every Sunday at
19:30, every weekday at 10:00 and 19:30, the first Saturday of the month. Defining
a rota asks for **how many Shifts**, and the app takes the next that many the
pattern generates, carrying on from exactly where the previous rota stopped.

What used to be a Standing Preallocation becomes an **Override**: another RRULE
that picks out some of the generated Shifts and changes them — moves their start,
changes their length or Shape, pins people to them. Overrides are a list; a later
one wins.

Preallocations are renamed **Pins**, the word Organisers already use.

## Decisions and their reasons

- **The pattern is an RRULE set, kept in Rota Defaults.** One RRULE per line. RRULE
  is the one notation that already says "the last Sunday of the month" or "every
  other Tuesday", and Organisers who need something unusual can say it without
  the app growing a screen for each case. It is not friendly; a plain-English
  description of each line and a calendar of where Shifts fall make it readable,
  and the friendliness is a later layer over the same stored rule. A set rather
  than one rule because BYHOUR and BYMINUTE combine as a cross product: one rule
  cannot say 10:00 and 19:30 without also saying 10:30 and 19:00.

- **Every pattern line states its start time** with BYHOUR and BYMINUTE. FREQ is
  DAILY or coarser. COUNT and UNTIL are allowed, but not both on a line.
  BYSETPOS, BYWEEKNO, BYYEARDAY and BYSECOND are refused as things nobody here
  needs and few people can read.

- **Start from is a date beside the rule, not DTSTART inside it.** It is
  DTSTART at 00:00, defaults to the day it is set, and is always stored and shown.
  It is where "every other week" counts from and it fills in what a rule leaves
  out — the weekday of a bare `FREQ=WEEKLY`, the day of a bare `FREQ=MONTHLY` —
  so hiding it would leave a rule meaning something nobody can see. DTSTART,
  RDATE, EXDATE and EXRULE in the text are refused: one place says where a rule
  starts.

- **A time zone and one default length sit beside the pattern.** Generation is
  in local wall-clock time, as ADR 0007 decided for Shifts themselves: 19:30 is
  19:30 either side of a clock change. A Shift that runs past midnight belongs to
  the day it starts.

- **Defining a rota asks for a number of Shifts, not dates.** Asking for dates
  makes the Organiser do the pattern's arithmetic, and invites the off-by-one of
  "twelve weeks" landing eleven or thirteen Shifts. The first rota starts from
  Start from. Each later one carries on from the exact generated start the
  previous one stopped after, which is stored on the Rotation. A date would not
  do: a rota ending after the morning Shift of a two-Shift day must be followed by
  that day's evening Shift, neither skipping it nor minting the morning twice.
  An optional "not before" date leaves a gap, for a break between rotas.

- **Shifts may not overlap**, across rotas as well as within one. Several may
  start on one date. This replaces ADR 0007's one Shift per date, which was
  standing in for "these two cannot both happen" and stops being true the moment
  a day has two sessions.

- **There are no one-off Shifts.** Every Shift comes from the pattern. A Shift
  the pattern does not describe is either a change to one it does — an Override,
  or an edit to the Shift itself — or a sign the pattern is wrong.

- **Overrides replace Standing Preallocations.** Each is one RRULE with its own
  Start from, and may set any of a start time (moving the Shift within its day),
  a length, a Shape (replacing the whole Shape) and Pins. They are an ordered
  list and a later one wins, property by property; a later Pin for the same
  person replaces their earlier one. They apply when a rota is defined, as
  Standing Preallocations did: changing an Override later leaves defined rotas
  alone (Rota Defaults seed, they never reach back).

- **An Override matches an exact time or a whole date.** One stating BYHOUR and
  BYMINUTE matches the Shift generated at that time; one without matches every
  Shift starting on that date. The choice is stored on the Override, set from the
  rule when it is saved, so what an Override matches is a fact on its row rather
  than something every reader re-derives from text.

- **Overrides match the generated start, never a moved one.** Matching against
  a start some earlier Override moved would make the list's order decide what
  matches as well as what wins, and an Organiser would have to know the moved
  time to aim at it.

- **No Override closes a Shift.** Closed stays a fact set by hand on the Shift,
  for the holiday that comes up once.

- **ADR 0010 holds for Overrides.** An Override is a person's decision. Its Pins
  are ordinary Pins and its Shape an ordinary Shape: refused only for what
  cannot be represented, never for breaking an allocation rule.

- **One evaluator.** The calendar, the plain-English descriptions and minting
  all come from the server's RRULE evaluator. A second one in the browser would
  disagree with it at the edges — "monthly on the 31st" skipping short months is
  the kind of thing two implementations get differently — and the screen would
  promise Shifts the server does not mint.

## Considered and rejected

- **A weekly pattern with extra options** (every n weeks, several days, several
  times). Each irregular cadence would have needed its own option; RRULE already
  has them all.

- **Picking a start and end date** for each rota, or a number of weeks. Both make
  the Organiser translate the pattern into dates, and neither works once a week
  holds a varying number of Shifts.

- **Restarting each rota from a date.** It either skips or repeats a Shift
  whenever a rota ends part-way through a day.

- **One-off Shifts.** Rejected for simplicity; see above.

- **A "Closed" Override, and Shape deltas** ("+2 Service volunteers"), both from
  #182. Closed is rare enough to set by hand. A delta reads differently depending
  on what earlier Overrides did; a whole Shape says the same thing wherever it is.

- **Inferring an Override's granularity whenever it is read.** Same answer every
  time, but stated nowhere a person or a query could see it.

- **Migrating Standing Preallocations into Overrides.** There are few of them, and
  the Organiser re-enters them by hand.

## Consequences

- Shifts are referred to by id everywhere. A date no longer identifies one, so
  every request that names a Shift by date changes (#293).

- The one-Shift-per-date index becomes an exclusion constraint on each Shift's
  time range within an Organisation (#294). It keeps the index's other job:
  two rotas defined at once still cannot mint the same time.

- `shift_gap` counts Shifts, not days, so a gap of one also keeps somebody off
  the evening Shift after the morning one. That is the rule doing what it says.

- Rota Defaults lose the start and end time; the length is kept. After the
  change ships, each environment's pattern is entered by hand, and Define Rota
  refuses until it is.

- Standing Preallocations go when Overrides arrive (#296), and are re-entered by
  hand as Overrides.

- Preallocations are renamed Pins throughout code, API and screens (#292).
