# Shift pattern entries carry their own start and length

Status: proposed, 2026-10-30. Supersedes three parts of
[ADR 0013](0013-shifts-come-from-a-pattern.md): Start from as a date beside the
rule, one default length beside the pattern, and one line making one Shift. The
rest of ADR 0013 stands. Issue #310.

## In plain words

A Shift pattern is now a list of **entries**, each a VEVENT that says when its
first Shift starts (DTSTART), how long each one runs (DURATION) and how it
repeats (RRULE):

```
BEGIN:VEVENT
DTSTART:20261101T193000
DURATION:PT3H
RRULE:FREQ=WEEKLY;BYDAY=SU
END:VEVENT
```

A Sunday evening session of three hours and a Tuesday one of four are two
entries, each with its own length, and "every other week" counts from each
entry's own first date. An event copied out of a calendar is the same shape, so
pasting one works.

An Override's rule carries the date it counts from the same way, as a
`DTSTART;VALUE=DATE:` line above its RRULE.

The time zone the pattern is read in becomes a setting of its own, at the top
of Config.

## Decisions and their reasons

- **One entry is one kind of Shift: DTSTART, DURATION and RRULE, each exactly
  once.** ADR 0013 put the start date and the length beside the rule and shared
  them across every line, so two sessions of different lengths could not both be
  described, and two fortnightly sessions a week apart could not both count from
  the right week. Each entry stating its own start and length removes both, and
  is the shape RFC 5545 already gives a recurring event.

- **DTSTART is a floating local time.** It is wall-clock in the Organisation's
  zone, as ADR 0007 decided for Shifts. A TZID naming that zone is accepted and
  dropped, since a calendar export writes one; any other TZID, and a trailing
  `Z`, are refused: either would mean a time somewhere else.

- **DTSTART must be a start its own RRULE generates.** RFC 5545 counts DTSTART
  as the first occurrence even when the rule would not produce it, which makes
  "every Sunday" from a Tuesday mint one Tuesday. Refusing it keeps the first
  Shift one the description admits to.

- **DURATION is positive and at most a day.** DTEND is refused with a reason
  rather than converted: a length and an end can disagree, and one way to say
  it is easier to read.

- **BYSETPOS is allowed, and BYHOUR and BYMINUTE are no longer required.** The
  time is DTSTART's. BYSETPOS is what says "the last weekday of the month"; it
  was refused in ADR 0013 only for being hard to read, and the plain-English
  description answers that.

- **EXDATE, RDATE and EXRULE are refused**, each with its reason, as is an entry
  without an RRULE. A one-off date belongs on the Shift (ADR 0013: no one-off
  Shifts), and a skipped one is a Closed Shift.

- **What a calendar adds is read past with a warning.** SUMMARY, UID, DTSTAMP, a
  VCALENDAR wrapper and the like are accepted and named as ignored, so a pasted
  event is accepted rather than refused for carrying metadata. Folded lines are
  unfolded first.

- **Two entries generating the same start are refused**, naming both. Shifts may
  not overlap (ADR 0013), and two entries at one time are certainly a mistake.

- **The first rota starts from the earliest entry's DTSTART.** There is no Start
  from beside the pattern to start from.

- **Each Shift takes its entry's length** unless an Override sets one.

- **An Override's DTSTART is a date and is required.** Matching granularity is
  unchanged: BYHOUR and BYMINUTE in its RRULE make it match a time, and without
  them it matches whole dates. BYSETPOS is allowed here too. A new Override is
  offered with today's date already written in.

- **The time zone is its own setting.** It was a field beside the pattern, but
  everything in Config is read in it, so it heads the page. The picker lists the
  zones the browser knows; the server refuses a zone it cannot load. Nobody
  having chosen one is not left for later: opening Config saves the browser's
  zone, which is almost always where the Organisation is.

- **Changing the zone does not re-read the stored pattern.** A pattern whose
  TZID no longer matches is refused when a rota is defined, with the reason,
  rather than silently reinterpreted.

## Considered and rejected

- **Keeping a shared Start from and length, with per-line overrides.** Two
  places saying the same thing, and every reader would have to work out which
  wins.

- **Accepting DTEND alongside DURATION.** See above.

- **Converting a foreign TZID or a UTC time.** It would turn 19:30 in one place
  into some other wall-clock time here, and the Organiser who pasted it meant
  the time they saw.

- **Carrying the old pattern over.** Its lines have no DTSTART or DURATION, and
  filling them from the old Start from and length would produce a pattern
  nobody wrote. There is one pattern per environment, re-entered by hand.

## Consequences

- Migration 038 clears each environment's Shift pattern and drops
  `pattern_start_from` and `shift_length_minutes`. Define Rota refuses until the
  pattern is entered again.

- Migration 038 folds each Override's `start_from` into its rule as a
  `DTSTART;VALUE=DATE:` line and drops the column. An Override matches the same
  Shifts after it as before.

- Each pattern entry comes back from the API with its description — "Every
  Sunday at 19:30 for 3 hours, from 1 Nov 2026" — and any problems and warnings,
  so the screen reads it without a second evaluator (ADR 0013: one evaluator).
