# Roles are jobs volunteers hold

Status: accepted. Amended 2026-08-02: Tracks removed before implementation —
see "Tracks, considered and dropped". Amended 2026-08-04: a Preallocation
grants the Role it names for that one Shift (#109) — see "Eligibility is exact
match on a held Role". Amended 2026-08-05: the roster holds Roles in one
multi-select column, not one column per Role (#121) — see "The roster holds
Roles in one `Roles` column".

Two hardcoded roles (`RoleTeamLead`, `RoleVolunteer`) are being replaced by
configured **Roles**. A Role is a job on a Shift; a volunteer **holds** the
Roles they will do; only a holder may be allocated to one. A person fills at
most one Role on a Shift, however many they hold. A Shift owns its **Shape**:
which Roles it needs and how many Seats of each.

The word "role" previously spanned two things — what a volunteer *is* (the
roster's Role column) and what someone *does on a shift*
(`allocation.role`). This ADR collapses them back into one concept rather than
splitting them into two, which is the counter-intuitive part and the reason the
ADR exists.

## Decisions and their reasons

- **Eligibility is exact match on a held Role.** To fill Team lead you hold
  Team lead. We rejected a `filledFrom` mapping on the Role definition (a Role
  naming which *pools* may fill it, so Assistant TL could accept team leads and
  ordinary volunteers) because it recreates the very split it was meant to
  solve: role names would mean "pool" in one place and "job" in another, and
  every eligibility question needs two lookups. The cost is real and accepted —
  a broadly-open Role must be named per volunteer, and a deputy Role means
  saying who will actually deputise. That is arguably the truer statement:
  not every team lead wants the deputy job.

  *Amended 2026-08-04 (#109):* a **Preallocation is the one exception**. It
  grants the Role it names, for the Shift it names and no other. A pin is a
  decision already taken off-system — somebody has been asked to do that job
  that week — and refusing it because the roster has not caught up fails the
  whole allocation over a missing tick, at the moment there is least time to
  fix it. The alternative, adding the Role to the volunteer, was rejected: it
  is not per-Shift, so a pin for one week would silently change who the solver
  may pick them as for every other week in the rota. Availability is granted
  the same way and for the same reason. What a pin still cannot do is invent a
  Seat: a Role the Shift's Shape has none of remains an error, because that is
  a statement about the Shift rather than about the person.

- **No "open to all" shortcut.** Tempting, and always slightly wrong: it is not
  true that anyone on the roster may deputise, because the roster includes
  people who only ever collect food. Breadth costs an entry per person.

- **One Role per person per Shift.** A person fills exactly one Seat, whichever
  Roles they hold. This is what stops the same person being both Team lead and
  Assistant TL, and it is what the system already does: today a volunteer is on
  a shift once, as lead or as ordinary. No exclusion structure has to be
  configured or maintained for it to hold.

- **Tracks, considered and dropped.** This ADR originally grouped Roles into
  **Tracks** — line-ups within which a person fills at most one Role, with
  independent Tracks letting Emma lead the serving line-up *and* collect the
  food on the same Shift. Tracks also carried the male-cover requirement.
  Dropped before implementation: the only thing they bought was that one
  person-two-jobs case, which the drop-in does not actually need, and they cost
  a config concept, a validation rule, a per-Track solver constraint and a
  per-Track rewrite of male cover. Symmetric per-Role conflict lists and a
  boolean primary-or-additional Role were rejected earlier for the same reason
  Tracks now are: structure maintained by hand to express an exclusion rule
  that "one Seat per person" already gives for free. If somebody must hold two
  jobs on one Shift, the answer is a Role that names the combination.

- **Counts are targets; ceilings are limits.** A Shape's count is what the
  allocator fills up to in Role priority order, leaving Seats empty when people
  are scarce — a Shift with no available team lead must still allocate, as it
  does today. A Role's `max` is a separate, role-level ceiling governing what an
  admin may add afterwards. Role-level rather than per-Shape so that a per-date
  Shape override cannot forget to cap team leads.

- **Role priority decides what goes empty.** Without it the solver is
  indifferent between filling a Team lead Seat and a Service volunteer Seat,
  and will spend a scarce qualified person on an ordinary Seat while the
  distinguished Seat stands empty — a strictly worse rota with an identical
  score. Admins order Roles; the objective weights behind that order are ours.

- **Structure is enforced, standing is advisory.** A second team lead is
  refused; an unlisted person placed in the lead Seat warns and proceeds.
  Blocking the second does not stop it happening, it stops it being *recorded* —
  the rota starts lying while the truth lives in a note.

- **The Shift owns its Shape from mint, frozen at allocation.** The same rule
  as manual preallocations (ADR 0003): freely editable while the Rotation is
  unallocated, fixed once the allocator has run. Shift size is currently
  recomputed from config on every read, so editing config silently rewrites what
  a *past* shift asked for; enumerated Seats make that visible and wrong.

- **The roster holds Roles in one `Roles` column.** A multi-select dropdown
  listing the configured Role names, so a value cannot be mistyped and what a
  volunteer does is one cell to read rather than a row to scan across. The cell
  is **comma-joined**, which is what such a dropdown writes: a list by
  convention rather than by structure, so the parser trims each value and
  `config.Validate` refuses a Role name containing a comma. Config remains
  authoritative for which Roles exist — a value config does not name warns and
  is skipped, keeping the rest of the cell.

  *Amended 2026-08-05 (#121). The superseded shape (S1, #89)* was one
  `<name> - Role` tick-box column per Role, discovered by header suffix. Its
  premise was that a Sheets dropdown could hold only one value, so a single cell
  could not say "Team lead *and* Service volunteer". That was simply wrong —
  multi-select dropdowns exist — and with the premise gone the design was pure
  cost: a header column per Role, a manual sheet edit before anyone could hold a
  new one, and Roles spread across a row where the columns were narrow enough to
  truncate. Retiring it also retires the warning for a configured Role no column
  supplies: there is no column to be missing, and "nobody holds this Role" is
  not a thing the header can show any more.

- **Male cover stays a Shift-level rule, not a general attribute system.** A
  `requiresMale` flag in config; every open Shift must have a male allocated or
  leave some Seat open, so one can be added by hand. That is exactly today's
  rule with its two hardcoded escapes (no lead allocated / an ordinary seat
  free) generalised over Roles. Generalising volunteer attributes into
  configurable cover requirements is a larger surface for no present gain.

## Consequences

- **Headcount becomes distinct people**, so the "does this Role count toward
  shift size" flag never needs inventing. `seat_cost` and the rule that team
  leads do not count both disappear.

- **Every Seat costs a person.** A Shift wanting a Food collector and six
  servers needs seven people, because the collector cannot also take a serving
  Seat. That is the accepted price of dropping Tracks; if a week is short, the
  Shape is what gets adjusted.

- **The solver assigns Roles.** `x[(volunteer, shift)]` becomes
  `x[(volunteer, shift, role)]`, with an attendance variable ORed over Roles
  carrying the grouping, availability, fairness and frequency constraints.
  `at_most_one_team_lead` disappears into per-Role ceilings — including its
  per-group counting hack.

- **`defaultShiftSize` and `RotaOverride.shiftSize` retire** in favour of a
  default Shape and Shape overrides. Size was only ever a one-Role Shape.

- **Migration is not one Role per volunteer.** Today a team lead is only a team
  lead on the roster, yet the allocator routinely places non-designated leads
  in ordinary seats. Every team lead must therefore hold *both* Team lead and
  Service volunteer, or they vanish from ordinary Seats.

- `Service volunteer` keeps its name, so historical `allocation.role` values
  need no migration.
