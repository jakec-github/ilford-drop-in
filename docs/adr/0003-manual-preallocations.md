# Manual preallocations: an add-only layer over config

Status: accepted

Preallocation — pinning a person to a shift so the allocator must place them —
already existed, but only as a property of a **Rota Override** (a config
recurrence rule). We are adding **manual preallocations**: a first-class,
per-shift, operator-editable set of pins, stored in their own table and set over
HTTP, so people can be pinned to specific shifts without a config edit (and, in
future, from a UI). Manual preallocations are **add-only** and **union** with
config preallocations at allocation time; config remains authoritative.

This sits on a two-phase rota lifecycle: **before allocation** a rota's
preallocations are freely editable; **allocation runs once**; **after
allocation** changes are Alterations (audited, append-only) and re-allocation
fails (#8). Manual preallocations only exist in, and only matter in, the first
phase.

## Decisions and their reasons

- **A separate `manual_preallocation` table keyed to `shift_id`**, mirroring the
  `allocation` row shape (`role`, nullable `volunteer_id`, nullable
  `custom_value`), one row per pinned assignee. A preallocation is *input* to the
  allocator; an allocation is its *output* — conflating them on one table with a
  status flag would blur that. Group atomicity is free: pinning one member of a
  couple/family forces the whole group via the existing solver constraint, so the
  table never models groups.

- **Add-only; manual never suppresses config.** A manual pin can only *force a
  person on*. It cannot remove or override a config preallocation. If config
  pins volunteer X and you don't want X guaranteed, that is what the
  **availability** mechanism is for — there is no "forbid" primitive here.

- **Config is authoritative for the single-valued team-lead slot.** A manual
  team-lead pin is **rejected at set-time** when config already pins a team lead
  for that shift's date (resolving the override rrule for the date). This is the
  only genuine cross-source conflict; volunteer/custom pins simply union.

- **Both sources union through the existing override path.** At allocation, each
  manual pin becomes a synthetic exact-date `ShiftOverride` appended to the
  config-derived overrides, so `InitShifts` unions them with no new merge logic
  and the two systems are literally the same mechanism downstream. Identical
  custom strings from both sources dedupe to one seat.

- **Stale pins fail loudly, pre-solve.** A pin validated as active at set-time can
  rot (the volunteer goes inactive/deleted before allocation runs). A pre-solve
  check fails the allocation naming the offending pin, rather than letting it
  reach the solver as an opaque `ProblemError`. This also shields config
  preallocations from the same crash.

## Considered and rejected

- **Allocation on top of allocation.** The idea that started this: re-running
  allocation against an already-allocated rota, folding its existing allocations
  (and Alterations) into preallocations, gated by a time cutoff on
  `allocated_datetime` so pre-allocation Alterations are ignored. Rejected as too
  messy — it fought the append-only Alteration model, made every effective-state
  projection time-aware, and needed replace/diff write semantics. A clean
  first-class preallocation store delivers the same "pin people ahead of time"
  value without any of it.

- **Manual overriding/suppressing config** (per-shift replacement, or removing a
  specific config pin). Rejected for v1: suppression needs a negative/forbid
  primitive, and forbidding is availability's job. Add-only is simple enough for
  users and cheap to build.

## Consequences

- **Partial allocation is not delivered here.** Manual preallocations let you
  *pin* ahead of time, but allocation still runs once and fully; you cannot
  allocate part of a rota now and top it up later (re-allocation fails, #8).
  Approximate it by pinning, then allocating when ready.

- **The solver is untouched.** The CP-SAT contract already models per-shift
  preallocations in all three flavours; this is entirely Go-side plumbing plus
  HTTP endpoints.

- A usable UI needs open (unallocated) shifts to be enumerable with an
  `allocated` flag — a separate change to repoint shift listing at the `shift`
  table (today it reconstructs shifts from allocation rows, so unallocated shifts
  are invisible).
