# A Shift's start is local wall-clock time, and its date is derived

Status: accepted

A Shift carries `start_at` and `end_at` as `TIMESTAMP` **without** time zone,
holding local time in the drop-in's own zone, which is a settings property.
`shift.date` is deleted: a Shift's date is `start_at::date`. The rule that
there is at most one Shift per date becomes a unique index on that expression.

The obvious choice is `timestamptz`, and it is the wrong one here. With
`timestamptz` the date is `(start_at AT TIME ZONE 'Europe/London')::date`, an
expression PostgreSQL marks **STABLE rather than IMMUTABLE** because the tz
database can change — so it cannot be indexed, and enforcing one-Shift-per-date
needs an immutable wrapper function or a denormalised date column the
application maintains. `timestamp::date` is immutable, so the constraint is one
line the database enforces, with no application discipline behind it.

The modelling argument runs the same way. The drop-in happens in one place; a
Shift's start is a wall-clock fact about Ilford, not an instant on a global
timeline. Conversion to UTC belongs where it is actually needed — the calendar
feed, which already knows the timezone.

## Consequences

- Changing the timezone setting does not retroactively move historical Shifts.
  This is correct rather than a defect: past sessions happened at the wall-clock
  time they were held at.

- Times are **descriptive, not an allocator input** — the solver works in dates.
  So unlike a Shape or `Closed`, times stay editable after a Rotation is
  allocated. The freeze rule is not "everything freezes at allocation", it is
  "allocator inputs freeze at allocation".

- This closes #32 and the part of #23 that keys Shifts by date, and it is why
  that work has to land before Shifts are minted from a define screen.

- Editing a Shift's start onto another Shift's day is a conflict the database
  rejects; the API answers 409 naming the clash.

- Defining a Rotation refuses when the shift times are unset, which narrows ADR
  0006's "incomplete settings block allocation and **nothing else**". A Shift
  with no start is not a Shift with unknown hours — it is a Shift on no day at
  all. The reason is the one that made allocation the exception in the first
  place: defining a rota creates something people are told to turn up to, rather
  than rendering a page. Everything that only reads still reads.

- The migration making times mandatory (020) gives the whole of their day to any
  Shift nobody ever stated times for, rather than failing. Failing would take
  the site down at the one moment the fix is unreachable, since both the
  Settings screen and the per-Shift times are served by the server that would
  refuse to boot. Such a Shift is corrected on the rota screen.
