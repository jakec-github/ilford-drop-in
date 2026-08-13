# Draft Rota Allocations live in their own table and are confirmed by output hash

Status: accepted. Amended 2026-08-07: drafts re-solve when they are read, not on
a timer (#142) — see "Re-solves happen when a draft is read". Amended 2026-08-09:
reading a draft waits for a solve already running rather than reporting one
(#179) — same section. Amended 2026-08-13: a draft is shown on the Allocation
tab only, not in the rota view — see the last consequence.

An unallocated Rotation continuously carries a **Draft Rota Allocation**: a
speculative rota the solver produces from whatever availability, Shapes and
pins exist so far, so an admin can see the rota taking shape rather than
discovering it at the moment of allocation. Drafts are stored in a
`draft_allocation` table of their own, and allocating re-solves and commits
only if the fresh result hashes identically to the draft the admin was shown.

## Decisions and their reasons

- **A separate table, not `allocation` rows with a null `allocated_datetime`.**
  Five call sites read `allocation` today, and **two are public**: `GET
  /api/shifts` carries no `requireAdmin`, and `/calendars/{filename}` pushes to
  calendar apps volunteers have already subscribed. A null-stamp draft gives all
  five a new obligation to join to `rotation` and check, and one forgotten join
  publishes a draft rota to volunteers' phones — for the entire availability
  window, since drafts re-solve throughout it. A separate table makes the leak
  unrepresentable instead of merely forbidden, and leaves `allocation` meaning
  what `CONTEXT.md` says it means.

- **Confirmation compares the output, not the input.** The solver is
  deterministic — `random_seed = 0`, `num_search_workers = 1` — so allocating
  re-solves, hashes the result, and commits only on a match; otherwise the new
  solve becomes the draft and the admin confirms that one. Hashing the
  *assembled input* was considered first and is worse: it is sensitive to map
  ordering and numeric formatting, and it trips on input changes that cannot
  change the output, throwing confirmations that mean nothing. `solution.py`
  already emits assignments in canonical volunteer order, so the output hashes
  stably with no extra work.

- **Commit is copy-then-stamp in one transaction**, reusing the existing
  `FOR UPDATE` guard in `InsertAllocationsAndSetAllocated` (#8). Allocation
  cannot half-happen and cannot race.

  *Amended 2026-08-07 (#144), where the confirmation was built.* Three details
  the design left open, settled by the implementation:

  - **The hash is derived, never stored.** A draft's fingerprint is computed
    from its Seats — Shift, Role, and the volunteer or custom entry in it —
    every time it is read, by the same function the fresh solve hashes its
    answer with. A stored column could disagree with the rows it claims to
    fingerprint; a derived one cannot. Seat ids and row order are deliberately
    outside it: every solve mints fresh ids, and the store returns the rows in
    whatever order it likes.
  - **The confirmation is the hash the client states**, not the one the stored
    draft carries. The two differ exactly when another Admin's read re-solved
    the draft while this one had the rota open — and comparing against the store
    would then commit a rota nobody had looked at, which is the outcome the
    whole mechanism exists to prevent.
  - **The commit consumes the draft**, deleting it in the same transaction.
    The speculative rota has become the rota, and a draft outliving it would be
    a second answer for a Rotation nobody can draft again.

- **Re-solves happen when a draft is read, not per-write.** The Rotation records
  when an allocator input last moved; a draft keeps the stamp it read when its
  solve began, and the two disagreeing is what "dirty" means. Reading the draft
  re-solves it when it is dirty, and reports it as it stands when it is not.

  *Amended 2026-08-07 (#142). The superseded design* was a hard-coded six-hourly
  tick, deliberately ungated on the flag. It was dropped for the simpler thing
  once a solve turned out to be quick enough to sit on a request: a timer solves
  a rota nobody is looking at, and is stale again by the time somebody is. A read
  is the moment the answer is actually wanted. What the tick was for — the
  roster, a Google Sheet with no change notification, where a new volunteer or a
  newly held Role moves no stamp here — is served by the manual re-solve control
  instead, which solves whether or not anything is known to have moved.

  Dirtiness is derived from two stamps rather than stored as a flag a solve
  clears. A flag would be cleared by the solve that ran, taking with it any
  change that landed during it — thirty seconds is long enough for one — whereas
  a change that moves the Rotation's stamp past the one the draft captured simply
  leaves the draft dirty, costing a re-solve rather than losing the change.

  One solve runs at a time per process, and everything that solves — a read, the
  re-solve control, allocating — queues for that one slot.

  *Amended 2026-08-09 (#179). The superseded design* did not queue: a reader
  arriving while a solve ran was given the draft as it stands, told a solve was
  in flight, and left to ask again. That made a draft read two things — an
  answer, or a state to act on — and left every screen holding a retry policy of
  its own, with a re-solve refused outright as a third case. Reading now waits
  for the slot and then **re-reads the dirtiness inside it**, solving only if it
  is still dirty: the solve queued ahead may have started before the edit that
  sent this reader here, so waiting for it is not on its own enough to make its
  answer the right one. A reader whose edit that solve already covered returns at
  once with its answer; one whose edit it predates gets its own solve. The cost
  is that a slow solve and a hung server look alike to a client, which is
  accepted — and a solve therefore runs under a ceiling of its own (60s), since a
  wedged subprocess now holds up every reader rather than only its own caller.

  The slot is per-process, which is a constraint on the deployment: it holds for
  one app container, and `deploy/compose.yaml` runs exactly one. A second
  instance would mean a second slot and so two concurrent solves racing to store
  their answers — wasted CPU and a flapping draft, but not a wrong allocation,
  because the hash guard still means only a rota that was shown can be committed.
  Growing past one container means making the slot a Postgres advisory lock keyed
  on the rota.

- **Drafts are read-only.** No drag-and-drop before allocation: a hand
  placement would be destroyed by the next solve. The durable way to say "put
  her there" is a Preallocation, which the rota view already supports.

## Consequences

- **There is no way to say "not her" before allocation.** Preallocations only
  add. A negative pin was rejected as a concept existing only to fight the
  allocator; the intended answer is to fix the *input* by letting admins edit a
  volunteer's availability — a separate feature with its own value — and, until
  then, to allocate and correct with an Alteration.

- A solve sits on the allocate path, capped at 30 seconds, so that action needs
  an honest spinner rather than an optimistic UI.

- A draft lost to a restart costs a re-solve, not correctness, which is why the
  job is in-process like `sendjobs` rather than a queue.

- Drafts render as dashed-border chips on the Shift rows of the rota in flight,
  beside the pins those rows already carry.

  *Amended 2026-08-13. The superseded design* drew them in the rota view as
  well, on the unallocated Shifts an admin already sees there. That made one
  screen say two kinds of thing at once — what has been decided, and what the
  solver currently guesses — and it put a read that can trigger a thirty-second
  solve behind opening the rota. The draft is drawn on the Allocation tab alone
  now. The rota view keeps the unallocated Shifts and everything an admin
  decides about them — pins, closures, Shape and hours — and carries a line
  pointing at the tab where the draft is.
