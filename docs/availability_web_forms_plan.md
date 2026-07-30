# Plan: availability requests served by the web server

## Context

Availability is collected through **Google Forms**. `RequestAvailability` creates one
form per volunteer (`forms.body`), emails the link with the CLI operator's Gmail
token (`gmail.send`), and every later read — `ViewResponses`, `AllocateRota`,
`SendAvailabilityReminders`, `ViewHistoricalResponses` — fetches the answers back
over the Forms API (`forms.responses.readonly`). **No response data is stored
locally at all.** The database holds only `form_id` and `form_url`.

That shape has three problems. The CLI needs five OAuth scopes and an interactive
consent to do routine work. Availability — a core domain fact — lives outside the
system, so every read is a network call to a third party that can rate-limit or
fail. And `availability_request` is the last table addressing shifts by date
rather than `shift_id`, which is why it blocks #23.

**Decision (2026-07-16, refined by grilling 2026-07-30)**: the web server serves
the availability form itself, and responses are stored in Postgres. Google Forms
is removed entirely. Google is *not* removed from the notification path — email
still sends through Gmail, but as the signed-in admin holding a short-lived
token, not as an ambient stored credential.

## Decisions made

| Question | Decision |
|---|---|
| How volunteers reach their form | Per-volunteer tokenised link, `/availability/{token}`. The link is the identity; volunteers never log in |
| Token storage | Raw, so an admin can re-display it to resend. Stops working once `rotation.allocated_datetime` is set |
| Request grain | Per volunteer, not per group — the individual grain is what makes a reminder addressable |
| Who sends the email | The signed-in admin, via Gmail, as themselves |
| Gmail credential | Short-lived access token obtained by **incremental consent at send time**; never persisted, no refresh token |
| Minting vs sending | **Separate operations.** Minting writes the rows and tokens; sending is a distinct, repeatable action over rows that already exist |
| Deadline | A per-send string, email-only. Not stored, not shown on the site, not enforced |
| Real cutoff | `rotation.allocated_datetime` — allocation, not the deadline |
| Response storage | Append-only **generations**: one `availability_response` per submission, its positives in `shift_availability` |
| Response encoding | **Positive-only** — an absent row is a no. Each submission writes a complete generation, never a delta |
| Response key | `availability_request_id`, so a response cannot exist for a volunteer who was never asked |
| Shift reference | `shift_id`, finishing ADR 0001 and unblocking #23 |
| Answer vocabulary | `CHECK (answer IN ('YES', 'PREFERRED'))` — `PREFERRED` is unused for now but needs no migration later |
| Idempotency key | None. A duplicate submission writes another generation; latest-wins makes the outcome identical |
| Form landing state | All open shifts pre-selected (opt-out), matching today's behaviour |
| Group rule | Available iff at least one member responded **and every responding member said YES** |
| Rollout | Expand/contract. The new request table runs *alongside* the legacy one, so the Forms path stays operational until the final slice |
| Historical data | Backfilled once from Forms before teardown |
| CLI | Loses `requestAvailability`, `viewResponses`, `sendAvailabilityReminders`. Scopes shrink from five to Sheets + email |

## Prerequisite

**#75 — `POST /rotations`.** An availability round is minted against a rota, and
there is currently no way to create a rota over HTTP: `define_rota` is CLI-only
and `cmd/cli/main.go:138` builds the Google clients unconditionally, so it cannot
run credential-free. Without it none of this is demonstrable in the dev stack.

This follows from a working rule: **agents seed dev state through the real HTTP
endpoints**, never by writing fixtures straight to Postgres. Fixtures that bypass
the code rot as the schema moves. The corollary is that everything an agent needs
must be reachable over HTTP — minting a round, listing the tokens, submitting a
response — which makes the admin-facing token list a product requirement rather
than a debug hatch.

## Schema

### Migration 011 — expand

```sql
-- The tokenised replacement for availability_request. Runs alongside the legacy
-- table so the Forms path keeps working untouched until the final slice; renamed
-- to availability_request by the contract migration.
CREATE TABLE availability_request_v2 (
    id UUID PRIMARY KEY,
    rota_id UUID NOT NULL REFERENCES rotation(id),
    volunteer_id TEXT NOT NULL,
    -- The link is the identity: /availability/{token}. Stored raw so an admin
    -- can re-display it to resend; expires when the rota is allocated.
    token TEXT NOT NULL UNIQUE,
    -- NULL means minted but not yet sent — the round/notify split depends on
    -- telling the two apart.
    sent_at TIMESTAMPTZ,
    CONSTRAINT availability_request_v2_rota_volunteer_key UNIQUE (rota_id, volunteer_id)
);

CREATE INDEX idx_availability_request_v2_rota ON availability_request_v2(rota_id);

-- One row per submission. A volunteer may resubmit freely; each submission is a
-- complete generation and the latest one before the cutoff wins.
CREATE TABLE availability_response (
    id UUID PRIMARY KEY,
    availability_request_id UUID NOT NULL REFERENCES availability_request_v2(id),
    submitted_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_availability_response_request
    ON availability_response(availability_request_id, submitted_at DESC);

-- The positives in one generation. An absent row is a no, so every submission
-- must write a complete set — never a delta.
CREATE TABLE shift_availability (
    id UUID PRIMARY KEY,
    response_id UUID NOT NULL REFERENCES availability_response(id),
    shift_id UUID NOT NULL REFERENCES shift(id),
    answer TEXT NOT NULL CHECK (answer IN ('YES', 'PREFERRED')),
    CONSTRAINT shift_availability_response_shift_key UNIQUE (response_id, shift_id)
);
```

### Contract migration — final slice

```sql
DROP TABLE availability_request;   -- legacy Forms requests, backfilled by then
ALTER TABLE availability_request_v2 RENAME TO availability_request;
ALTER INDEX availability_request_v2_pkey RENAME TO availability_request_pkey;
ALTER TABLE availability_request RENAME CONSTRAINT
    availability_request_v2_rota_volunteer_key TO availability_request_rota_volunteer_key;
ALTER INDEX idx_availability_request_v2_rota RENAME TO idx_availability_request_rota;
```

### Why a separate table rather than altering the existing one

Total isolation. Across four slices the Forms path is not merely *expected* to
keep working, it is untouched by construction, and the allocator switches over
exactly once with the old path still there to fall back to. It also lets both
flows exist for the same rota during transition, which
`availability_request_rota_volunteer_key` would otherwise forbid.

The new table cannot take the good name immediately: renaming the legacy table
does not free its index and constraint names — those share a schema namespace —
so `CREATE TABLE availability_request (id UUID PRIMARY KEY …)` would collide with
the existing `availability_request_pkey`. Freeing them means renaming the old
`pkey`, unique constraint and index on the very table we are trying not to
disturb. The interim name is confined to `pkg/db`; the Go type stays
`AvailabilityRequest`. Foreign keys bind to the table OID, so the rename does not
disturb `availability_response`.

## Reading availability

Every consumer runs the same query: the latest generation for a request, bounded
by the rota's `allocated_datetime` when it is set.

```sql
SELECT sa.shift_id, sa.answer
FROM availability_response ar
JOIN shift_availability sa ON sa.response_id = ar.id
WHERE ar.availability_request_id = $1
  AND ar.submitted_at <= COALESCE($2, 'infinity')   -- rotation.allocated_datetime
  AND ar.id = (
      SELECT id FROM availability_response
      WHERE availability_request_id = $1
        AND submitted_at <= COALESCE($2, 'infinity')
      ORDER BY submitted_at DESC, id DESC
      LIMIT 1
  );
```

The `LIMIT` binds to the *generation*, not the shift rows — a subquery or lateral,
never a bare `LIMIT` on the join. `id DESC` breaks `submitted_at` ties
deterministically.

This lives in **one** place in `pkg/db` and is called by every consumer. It must
not be re-implemented per caller, which is how the current group logic ended up
duplicated across `viewResponses.go`, `allocationHelpers.go` and `init.go`.

## The group rule

A group is the atomic unit of allocation — `init.go:95-115` makes the group
respond if any member did, and all members are placed together. Availability is
therefore a group property, while requests are per-volunteer. The rule that
bridges them, where a *responder* is a member with any submitted response:

| Michael | Emma | Group on that date |
|---|---|---|
| YES | YES | Available |
| YES | no reply | Available — Emma is opted in |
| YES | NO | Unavailable — Emma's own answer governs her, and a NO takes the group out |
| NO | no reply | Unavailable |
| no reply | no reply | Group not in the round at all |

Which collapses to: **available iff at least one member responded and every
responding member said YES.** That is the intersection over responders — the
exact dual of today's union-of-unavailability, so the semantics are unchanged and
only the encoding flips.

Like the read query, this belongs in one place.

## The volunteer's form

`GET /availability/{token}` — public, unauthenticated, no session.

- Greets by name ("Availability for Michael Smith"). A wrong name is how someone
  notices they have a forwarded link.
- Every open shift **pre-selected**. The model is opt-out, matching today's form,
  where answering "available for all" submits immediately
  (`getResponses.go:117`). A mis-tap recording *full* availability is benign; a
  mis-tap recording *zero* availability would silently drop a volunteer from the
  round and look identical to a legitimate "I can't do any of these".
- Closed shifts listed but not selectable, so volunteers can see the drop-in is
  not running rather than wondering why a date is missing.
- Group membership stated — "This also covers Emma Williams" — because it
  genuinely does.
- Re-openable: the same link shows the current answer and allows a change, which
  is what the existing email promises. Generations make this free.
- Mobile first.

`POST /availability/{token}` writes one complete generation. It returns 410 (or
equivalent) once the rota is allocated.

Note the storage decision and the UI default are independent: rows remain
positive-only, so nobody is ever allocated to a shift without an affirmative row.
The pre-selection just fills those affirmatives in rather than starting blank.

## Notification

Minting a round requires no Google access at all. Sending is a separate action:

1. Admin triggers a send for a round, supplying the deadline string.
2. Server redirects to Google for **incremental consent** to `gmail.send`, with
   the pending action carried in the signed `state`.
3. Callback exchanges the code, holds the access token in memory, sends as the
   admin, and discards it. ~29 sends at the existing 3s throttle is ~90s, well
   inside the ~1h token life.
4. `sent_at` is stamped per request on success. Failures are reported per
   volunteer and remain resendable.

**Why incremental rather than at login.** The session cookie carries *identity,
not authority* (`pkg/api/auth.go:32`). Requesting `gmail.send` at login would
have the server hold a live Google credential for the whole session, and revoking
someone from `adminEmails` would not revoke a token they already hold. It would
also demand Gmail permission from an admin who only logs in to check a shift.

**Why the admin's own account.** Replies go to a human, deliverability is
Gmail's problem, there is no domain to verify, and every send lands in the
admin's Sent folder as a free audit trail. The consent screen for this project
already carries `gmail.send` (`pkg/utils/oauth.go:40`), and `gmail.send` is a
*sensitive* rather than *restricted* scope, so Testing publishing status needs no
app verification. Testing-mode refresh tokens expire after 7 days, which is
irrelevant because none is kept.

The consequences, accepted: sending requires an admin at a browser (as today,
with the CLI), and different admins send from different `From` addresses.

## Downstream consumers

| Consumer | Change |
|---|---|
| `AllocateRota` | Drops `FormsClientWithResponses`; reads availability from the store. `fetchAvailabilityResponses`' date-string-to-index matching (`allocationHelpers.go:57-67`) becomes a `shift_id` lookup |
| `ViewResponses` | Becomes the `/admin/availability` view. Its per-shift counts, shift size, delta and team-lead logic survive; the string date comparisons do not |
| `SendAvailabilityReminders` | Becomes a web action. "Has responded" is a DB read, not a Forms call |
| `ViewHistoricalResponses` | Stays CLI. Re-pointed at the DB; the cutoff is still `allocated_datetime`. Its `form_error` status ceases to exist |

`ViewHistoricalResponses`' five statuses map cleanly: `no_form` = no request row;
`no_response` = request but no generation before the cutoff; `no_availability` =
a generation with zero shift rows; `available` = a generation with rows.
"Available for nothing" is representable for the first time.

## Backfill

Existing availability answers live only in Google Forms. A one-off command walks
every legacy `availability_request`, reads its form's responses, and writes them
as generations stamped at their original `LastSubmittedTime`. It runs before the
Forms client is deleted, in the same slice.

The report it protects is longitudinal — who habitually does not reply in time —
so starting clean would leave it blind until roughly six rotas have passed, which
is most of a year.

One fidelity caveat, accepted: `parseFormResponse` *infers* "available for all"
from a single-answer response (`getResponses.go:117`). The backfill bakes that
heuristic into permanent rows rather than re-deriving it on each read.

## Teardown

- `pkg/clients/formsclient` deleted entirely.
- `gmailclient` moves to the server's admin-token path.
- CLI loses `requestAvailability`, `viewResponses`, `sendAvailabilityReminders`.
  It keeps `define`, `allocate`, `publish`, `change`, `listVolunteers`,
  `viewHistoricalResponses`.
- `requiredScopes()` (`pkg/utils/oauth.go:45`) drops from five scopes to **Sheets
  + email**: `forms.body` and `forms.responses.readonly` have no caller left, and
  `gmail.send` moves to the server's incremental grant. This is the
  credential-surface reduction the whole ticket is really about — it should be an
  explicit acceptance criterion, not a hoped-for side effect.

## Slices

Each is a complete vertical — endpoint and screen together.

| | Slice | Notes |
|---|---|---|
| **S1** | Core loop: migration 011, mint a round, admin roster with links, volunteer form, re-editable | Blocked by #75. Distribution is copy-the-link until S3 |
| **S2** | Availability analytics: per-shift counts vs shift size, delta, team-lead cover | Completes the admin view |
| **S3** | Sending: incremental `gmail.send`, send round, resend, reminders, deadline field | Independent of S2 |
| **S4** | Downstream reads: `AllocateRota` and `ViewHistoricalResponses` read the DB | Must precede S5 |
| **S5** | Backfill and teardown: one-off backfill, contract migration, delete `formsclient`, delete three CLI commands, shrink scopes | Backfill runs before teardown, same slice |

## Open questions

- Token entropy and format. `randomToken()` in `pkg/api/auth.go:367` already
  produces 32 URL-safe random bytes and is the obvious reuse.
- Whether the volunteer form needs a rate limit. It is the first unauthenticated
  write endpoint in the system; the token is unguessable, but nothing bounds
  submissions per token.
- What the admin sees when a volunteer's group partner has already answered —
  the roster needs to show "covered by Michael" rather than an alarming "no
  reply".
