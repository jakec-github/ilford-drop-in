# Plan: OIDC admin login + admin-triggered volunteer sync

## Context

The web server currently fetches volunteer data from the volunteers Google Sheet using an
installed-app OAuth token persisted at `~/.ilford-drop-in/tokens/token-<env>.json`. That token is
not a permanent secret, and users of the public-facing site have no way to re-authenticate the
server when it expires. The in-memory caching strategy (`pkg/api/volunteercache.go`) also isn't
loved.

**Decision**: replace the installed-app token with a **service account** that has read access to
the volunteer sheet. The server holds the service account key (loaded from config, like the OAuth
client configs) and reads the sheet as itself: once at startup to populate the roster, and again on
an admin-triggered sync. Admins log in via OIDC with Google; a "Sync volunteers" button (shown to
admins only) triggers the refetch. The admin only needs to be authorised — no token is taken from
them, so a sync is a plain authenticated POST rather than an OAuth round-trip.

> **Revised 2026-07-17.** This originally used the *admin's* OAuth access token, obtained via
> incremental authorization the first time an admin clicked Sync and discarded after a one-shot
> fetch. That avoided a permanent stored credential, but meant the roster was empty after every
> restart until an admin synced, and every sync depended on the admin re-consenting to the Sheets
> scope. Switching to a service account trades a managed credential for a roster that is warm at
> startup and a sync that needs no per-user grant. OIDC login and admin gating are unchanged — they
> are still wanted for alterations auth; only the *credential that reads the sheet* changed.

## Decisions made

| Question | Decision |
|---|---|
| Which credential fetches the sheet | Server's own service account (read access shared to the sheet); loaded from `serviceAccount.<env>.json` |
| When the roster is populated | At startup, then on each admin-triggered sync |
| Volunteer storage | Keep the in-memory cache (not Postgres) for now |
| Admin identification | Email allowlist in per-env config |
| Sessions | Signed cookie (HMAC), stateless — no session store |
| Session model | Admin-only: a non-allowlisted account is rejected at the callback, no cookie set. A session existing means admin |
| Allowlist enforcement | Re-checked against config on every request; the cookie proves identity (email), not authority. Removing an admin takes effect on config reload, not cookie expiry |
| Session lifetime | 60 days |
| Config placement | `sessionSecret` + `adminEmails` under the `server:` block of `drop_in_config.<env>.yaml`; web OAuth client in `oauthClientWeb.<env>.json`; service account key in `serviceAccount.<env>.json` (all gitignored) |
| Dev topology | `/auth` proxied through the frontend dev server (`web/dev.ts`); registered dev redirect URI is `http://localhost:5173/auth/callback` |
| Post-login redirect | `/` |

## Current state (relevant code)

| File | What it does today |
|---|---|
| `pkg/clients/sheetsclient/volunteers.go` | `ListVolunteers` reads the sheet via `GetValues` |
| `pkg/clients/sheetsclient/client.go` | `NewClient` builds an authenticated client from the installed-app token flow |
| `pkg/utils/oauth.go` | Installed-app OAuth flow: token file persistence, refresh, local callback on port 3000 |
| `internal/config/oauth.go` | Loads `oauthClient.<env>.json` — expects the `installed` key |
| `pkg/api/volunteercache.go` | In-memory cache, 5-minute TTL, `RefreshVolunteers` refresh-on-miss (10s rate limit) |
| `pkg/api/calendars.go:32-39` | Refresh-on-miss when a calendar request hits an unknown volunteer ID |
| `pkg/api/alterations.go:45-46` | No auth: trusts a client-supplied `userEmail` field |
| `cmd/server/main.go` | Obtains the sheets token once at startup; server has no auth at all |

## Work items

### 1. Google Cloud console (no code)

- Create a second OAuth client of type **"Web application"** in the same project (coexists with
  the installed-app client).
- Register redirect URIs: the production callback and `http://localhost:<port>/auth/callback` for
  dev. Web-client redirect URIs must match **exactly, port included** — register each dev port used.
- If the consent screen is in Testing status, ensure all admin emails are listed as test users.
  (The consent screen is per-project, so the existing test-user list may already cover it.)

### 2. Config

- New `oauthClientWeb.<env>.json` — web clients use a `web` key, not `installed`, so a small
  parallel struct alongside `internal/config/oauth.go` rather than reuse.
- Session-signing secret (per env).
- Admin email allowlist (per env).

### 3. Auth endpoints (~200-300 lines on the existing `ServeMux`)

- `GET /auth/login` — generate random `state`, stash in short-lived cookie, redirect to Google
  with scopes `openid email profile`.
- `GET /auth/callback` — verify state, exchange code, **verify the ID token** (signature, issuer,
  audience, expiry — not just decode) using `github.com/coreos/go-oidc/v3` (one new dependency;
  pairs with the existing `golang.org/x/oauth2`). Extract email, check allowlist, set session
  cookie.
- `POST /auth/logout` — clear cookie.
- `GET /auth/me` — return the session's email so the frontend can show logged-in state.
- Session cookie: HMAC-signed over email + expiry (60 days). `HttpOnly`, `SameSite=Lax`, and
  `Secure` conditional on env (`Secure: env == "prod"` — envs in this repo are `test` and
  `prod`; `test` is local dev, and Safari rejects Secure cookies on plain-HTTP localhost).
- After a successful callback, redirect the browser to `/`.
- `requireAdmin` middleware wrapping admin routes. This same middleware later replaces the
  trusted `userEmail` field in `POST /alterations`.

### 4. Sync endpoint (service account)

- `POST /auth/sync`, gated by `requireAdmin`. No OAuth round-trip: the handler calls an injected
  `VolunteerSyncFunc`, which builds a read-only Sheets client from the service account key
  (`sheetsclient.NewClientFromServiceAccount`), runs `ListVolunteers`, and replaces the cached
  roster wholesale. Returns 204 on success, 502 on a failed fetch, 503 if no sync function is wired.
- The same `VolunteerSyncFunc` is called once at startup so the roster is warm before any admin
  syncs. A startup failure is logged and non-fatal — the server boots empty and an admin can retry.
- The service account must have read access to the sheet (shared to its `client_email`).

### 5. Cache surgery (`pkg/api/volunteercache.go`)

Once the server no longer holds its own Sheets credential, the cache's self-fetching paths must go
or they will error five minutes after every sync:

- Remove TTL-expiry refetch in `ListVolunteers` — serve whatever the last sync loaded.
- Remove `RefreshVolunteers` / the `VolunteerRefresher` interface and its 10-second rate limit;
  update `pkg/api/calendars.go` accordingly. A newly added volunteer's calendar 404s until an
  admin syncs — acceptable, since the admin who added them is the one who syncs.
- Add a sync entry point that replaces the cached slice wholesale.

### 6. Frontend (minimal; React/Bun app in `web/`)

- Add `/auth` to the proxied prefixes in `web/dev.ts` so the whole OAuth dance runs on the
  `localhost:5173` origin in dev.
- Login is a plain `<a href="/auth/login">` — the whole dance is redirects, no JS SDK, no
  client-side token handling. Keep auth in the cookie even if the frontend ends up an SPA.
- Show logged-in state via `GET /auth/me` (email + logout button in the header); show the Sync
  button to admins. The Sync button is a `fetch` `POST /auth/sync` (session cookie sent
  automatically) that reflects the outcome inline — no redirect.

## Known trade-offs / caveats

- **A managed credential to guard.** The service account key is a long-lived secret: keep it out of
  git (gitignored like the OAuth client files) and rotate it if leaked. This is the cost traded for
  a roster that survives restarts and a sync that needs no per-admin consent.
- **Startup depends on the sheet, softly.** Populating at startup means a Sheets outage at boot
  leaves the roster empty until an admin syncs — logged, non-fatal, and no worse than the old
  always-empty-on-restart behaviour.
- **Freshness becomes a human process.** An admin who edits the sheet and forgets to sync leaves
  the site stale indefinitely (today it self-heals within 5 minutes). Acceptable because the
  editor and the syncer are the same person.
- **HTTPS is a prerequisite for production login.** Google allows plain-HTTP redirect URIs only
  for localhost, and Secure cookies need TLS — TLS termination must exist before login works
  outside dev.
- **Local dev needs no domain.** Localhost redirect URIs are exempt from the HTTPS requirement and
  the consent screen's authorized-domains list. No tunnel, fake domain, or `/etc/hosts` tricks
  needed.

## Sequencing

Ticketed 2026-07-17 as three ordered issues:

1. **#48 — OIDC admin login**: items 2-3 plus the minimal frontend slice of item 6. Ships the
   `/auth` endpoints and the `requireAdmin` middleware, but gates nothing.
2. **#12 — gate admin endpoints**: wrap the write endpoints with `requireAdmin` and drop the
   trusted `userEmail` field from `POST /alterations`. Blocked by #48.
3. **#11 — admin-triggered volunteer sync**: items 4-5 and the Sync button.

Item 1 (Google console) is manual work by Jake, a prerequisite of #48. Nothing in #48 changes to
accommodate the later tickets.

## Verification

- **Local login round-trip**: run the server with the dev web client config, visit
  `/auth/login`, complete the Google flow, confirm `/auth/me` returns the admin email and that a
  non-allowlisted account is rejected at the callback.
- **Session integrity**: tamper with the cookie value and confirm `requireAdmin` rejects it;
  confirm logout clears it.
- **Startup populate**: boot the server with the service account key and confirm `/shifts` has
  volunteer data before any sync.
- **Sync**: edit a volunteer in the sheet, click Sync, confirm the POST returns 204 and `/shifts`
  reflects the change — no consent screen (the server reads as the service account).
- **No self-fetch remains**: request an unknown-volunteer calendar and confirm the server 404s
  without attempting a Sheets call (no token errors in logs); the roster only changes on sync.
