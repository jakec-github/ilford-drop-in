# Roles are jobs volunteers hold, grouped into Tracks

Status: accepted

Two hardcoded roles (`RoleTeamLead`, `RoleVolunteer`) are being replaced by
configured **Roles**. A Role is a job on a Shift; a volunteer **holds** the
Roles they will do; only a holder may be allocated to one. Roles belong to
**Tracks** — line-ups within which a person fills at most one Role, and which
carry the male-cover requirement. A Shift owns its **Shape**: which Roles it
needs and how many Seats of each.

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
  a broadly-open Role must be ticked per volunteer, and a deputy Role means
  saying who will actually deputise. That is arguably the truer statement:
  not every team lead wants the deputy job.

- **No "open to all" shortcut.** Tempting, and always slightly wrong: it is not
  true that anyone on the roster may deputise, because the roster includes
  people who only ever collect food. Breadth costs ticks.

- **Tracks, not per-Role exclusion lists.** A person holds at most one Role per
  Track. This is what permits the same person to lead *and* collect while never
  being both Team lead and Assistant TL. Symmetric per-Role conflict lists were
  rejected as hand-maintained and silently breakable; a boolean
  primary-or-additional was rejected as permanently two-tiered. Tracks also own
  the male-cover requirement, which makes them a real concept rather than
  bookkeeping.

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

- **The roster holds Roles in `Role - <name>` tick-box columns.** Discovered by
  prefix, so the sheet's many unread columns stay invisible and a data-validated
  tick cannot be mistyped. Config remains authoritative for which Roles exist: a
  `Role - ` column config does not name warns and does nothing, and a configured
  Role no column supplies warns too.

- **`requiresMale` stays a named flag on a Track, not a general attribute
  system.** Generalising volunteer attributes into configurable cover
  requirements is a larger surface for no present gain.

## Consequences

- **Headcount becomes distinct people**, so the "does this Role count toward
  shift size" flag never needs inventing. `seat_cost` and the rule that team
  leads do not count both disappear.

- **The solver assigns Roles.** `x[(volunteer, shift)]` becomes
  `x[(volunteer, shift, role)]`, with an attendance variable ORed over Roles
  carrying the grouping, availability, fairness and frequency constraints.
  `at_most_one_team_lead` disappears into per-Role ceilings — including its
  per-group counting hack.

- **`defaultShiftSize` and `RotaOverride.shiftSize` retire** in favour of a
  default Shape and Shape overrides. Size was only ever a one-Role Shape.

- **Migration is not one tick per volunteer.** Today a team lead is only a team
  lead on the roster, yet the allocator routinely places non-designated leads
  in ordinary seats. Every team lead must therefore hold *both* Team lead and
  Service volunteer, or they vanish from ordinary Seats.

- `Service volunteer` keeps its name, so historical `allocation.role` values
  need no migration.
