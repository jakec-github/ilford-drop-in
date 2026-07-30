# Availability responses are positive-only generations

Status: accepted

Availability used to live in Google Forms. The database stored a `form_id` and
every read — allocation, the responses report, reminders, the historical matrix —
fetched the answers back over the Forms API and re-derived them from a heuristic
(`parseFormResponse` treats a single-answer response as "available for all"). We
are moving availability into Postgres as two tables: an `availability_response`
per submission, and the shifts that submission said yes to in
`shift_availability`. Requests carry a token instead of a form; the link is the
volunteer's identity.

## Decisions and their reasons

- **A submission is a generation, and generations are append-only.** A volunteer
  may resubmit as often as they like — the current email promises exactly that —
  and each submission writes a new `availability_response` with its own
  `submitted_at`. Reads take the latest generation, bounded by the rota's
  `allocated_datetime` when set. This preserves `ViewHistoricalResponses`'
  point-in-time question ("was their answer in before we allocated?") as a plain
  timestamp filter, with no snapshotting mechanism and no separate audit table.
  Mutating rows in place would have forced one or the other.

- **Positives are recorded; an absent row is a no.** The Forms model was the
  reverse — availability was the default and unavailability the exception — which
  meant a volunteer could be allocated to a shift they never affirmatively
  accepted. Recording positives makes that structurally impossible. The cost is
  N rows for a volunteer available for everything; at ~29 volunteers × ~6 shifts
  that is irrelevant.

- **Therefore every submission writes a *complete* generation, never a delta.**
  The encoding cannot distinguish "said no to this shift" from "was never asked
  about it" — both are an absent row. Nothing currently adds shifts to a defined
  rota, so this cannot bite today, but a partial write would silently record
  unavailability. This is an invariant of the write path, not a preference.

- **The UI default is independent of the encoding.** The volunteer's form lands
  with every open shift pre-selected, matching today's opt-out behaviour. A
  mis-tap then records full availability — benign, and the existing failure mode.
  Starting blank would let a mis-tap record *zero* availability, silently
  dropping a volunteer from the round in a way indistinguishable from a genuine
  "I can't do any of these". Positive-only storage and opt-out presentation are
  not in tension: the default fills the affirmatives in rather than assuming
  them.

- **`shift_availability` references `shift_id`.** `availability_request` was the
  last table addressing shifts by date; `allocation`, `alteration` and
  `manual_preallocation` were all re-keyed under ADR 0001 and migration 008
  dropped the legacy columns. This finishes that work, and is why #31 blocks #23.
  It also deletes real code: `fetchAvailabilityResponses` currently maps
  unavailable-date *strings* back to shift indices by reformatting every shift
  date and string-comparing.

- **Responses key on `availability_request_id`, not `rota_id` + `volunteer_id`.**
  The token *is* a request, so the write path already holds the id; carrying the
  other two would dereference the request only to copy columns back out. The
  schema then makes a response for a volunteer who was never asked
  unrepresentable. Reads already fetch requests by rota first, so they hold the
  ids before they need responses.

- **`answer` is `CHECK`-constrained text, admitting `'YES'` and `'PREFERRED'`
  from the start.** `PREFERRED` has no consumer yet, but including it in the
  constraint means expressing a preference later needs a UI and a solver
  weighting, not a migration. Follows `alteration.direction` (migration 003).

- **No idempotency key.** A duplicated submission writes a second generation with
  a later timestamp; latest-wins makes the end state identical. The hazard
  idempotency keys exist to prevent — a retry producing a *different* result —
  cannot arise.

- **The group rule lives in one place.** A group is available for a shift iff at
  least one member responded and every responding member said yes: a NO takes the
  group out, a YES opts in only members who have not replied. This is the
  intersection over responders, the exact dual of today's union-of-unavailability,
  so behaviour is unchanged. It is currently implemented three times over
  (`viewResponses.go`, `allocationHelpers.go`, `allocator/init.go`); it should
  survive this change once.

- **Requests stay per volunteer, though allocation is per group.** The group is
  the atomic unit the solver places, so a group token would be the more honest
  model. The individual grain is kept because it is what makes a reminder
  addressable — `sendAvailabilityReminders.go:126` already skips a volunteer
  whose group has answered while still knowing *who* has not. Group keys also
  come from a Google Sheet and change between rounds; a per-volunteer token
  survives a couple splitting mid-round, a per-group token does not.

- **Tokens are stored raw and expire at allocation.** Hashing is the textbook
  choice for bearer tokens and is rejected here deliberately: minting and sending
  are separate operations, so the server must be able to reproduce a link after
  minting — for resends, out-of-band distribution when Gmail fails, and
  credential-free agent testing. Hashing would make a link displayable exactly
  once. The blast radius is one volunteer's availability for one rota, and anyone
  who can read this table can also read the session secret that mints admin
  sessions outright. `rotation.allocated_datetime` bounds how long a leaked dump
  stays useful.

- **The replacement request table is created alongside the old one, not migrated
  in place.** Expand/contract across several slices, so the Forms path is
  untouched by construction rather than merely expected to keep working, and the
  allocator switches exactly once. The interim table name is confined to
  `pkg/db`; it claims the real name in the contract migration, once the legacy
  table's index and constraint names have been freed by dropping it.

## Consequences

Availability becomes a first-class domain fact rather than a projection of a
third-party service. `forms.body` and `forms.responses.readonly` lose their last
callers, and the CLI's scope list drops from five to Sheets + email.

Google is not eliminated: email still sends through Gmail, as the signed-in admin
holding a short-lived token obtained by incremental consent. That is a different
credential shape from the ambient stored token it replaces, not the same problem
relocated.

Historical answers exist only in Forms and are backfilled once, before the client
is deleted. The backfill bakes `parseFormResponse`'s "available for all"
inference into permanent rows rather than re-deriving it per read — accepted,
because the alternative is a report that is blind for most of a year.
