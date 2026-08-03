# Booting the app without credentials

An agent needs to *see* the app, not just its tests. This page is how: one
command brings the whole thing up with no Google account, no OAuth client and no
service account, and `playwright-mcp` drives it from there.

Everything here is the `dev` environment. It is credential-free by design and
cannot be turned on anywhere else — see [Why this is safe](#why-this-is-safe).

## Start it

```bash
scripts/dev-stack.sh start
```

That returns once the app is serving — it does not stay in the foreground. It
starts Postgres, builds the frontend, builds the server, launches it detached,
and polls `GET /health` until it answers 200. If that never happens it prints
the server log and **exits non-zero**, so a caller waiting on it is never left
hanging.

| Command | |
| --- | --- |
| `scripts/dev-stack.sh start` | Build and start, waiting for readiness |
| `scripts/dev-stack.sh status` | Running and healthy? (non-zero if not) |
| `scripts/dev-stack.sh logs` | Follow the server log |
| `scripts/dev-stack.sh stop` | Stop the server |

The app is on <http://localhost:8080> — one process on one port, the Go server
serving the frontend build embedded in its own binary, the same shape as
production. Not `scripts/dev.sh`, which is the two-process foreground stack with
a separate frontend dev server.

State lives in `logs/` (git-ignored): `dev-stack.log`, `dev-stack.pid` and the
built binary. The log is truncated on each start, so it only ever holds the
current run.

| Environment variable | |
| --- | --- |
| `API_PORT` | Port to listen on. Set automatically in a git worktree via `.worktree.env` (see [worktrees.md](worktrees.md)), so two checkouts can run stacks at once. |
| `DEV_STACK_TIMEOUT` | Seconds to wait for readiness. Default 60. |

## Log in

```bash
curl -i -c cookies.txt localhost:8080/auth/login
```

`GET /auth/login` mints a signed admin session for `devMode.adminEmail` and
redirects to `/`. No Google, no consent screen, no browser interaction — a
`curl` or a headless browser hitting that URL comes back logged in.

Only the *identity provider* is stubbed. The cookie is signed, expired and
re-checked exactly as in production, `requireAdmin` still refuses requests
without it, and the address is still re-checked against `server.adminEmails` on
every request. What you are looking at is the real gate, not an open door.
`GET /auth/callback` 404s, since there is no OAuth exchange to complete.

## Define a rota

The database starts empty, so nothing downstream of a shift is reachable until a
rota exists. Define one over the API — with the session from above:

```bash
curl -b cookies.txt -X POST localhost:8080/api/rotations -d '{"shiftCount": 6}'
```

That mints six weekly shifts and returns them with their ids;
`GET /api/shifts` then serves them. The Rota tab in the admin area does the same
thing through the UI.

Seed through the endpoints rather than by writing rows into Postgres by hand:
fixtures that bypass the code go stale as the schema moves, and a rota assembled
by hand can be a shape the app would never create.

Defining is **not idempotent** — each call takes the weeks after the last rota,
so calling it twice gives you two consecutive rotas. To start over, empty the
tables (`DELETE FROM shift; DELETE FROM rotation;`) rather than hunting for the
rota you meant.

## Collect availability

With a rota defined, mint a round — one link per active volunteer — and then
answer as one of them. Minting and the roster are admin-gated; the volunteer's
link is not, which is the point of it.

```bash
curl -b cookies.txt -X POST localhost:8080/api/availability-rounds -d '{}'
curl -b cookies.txt localhost:8080/api/availability-rounds    # who has answered
curl localhost:8080/api/availability/<token>
curl -H 'Content-Type: application/json' \
     -X POST localhost:8080/api/availability/<token> -d '{"shiftIds":["<id>"]}'
```

The token is the last path segment of the `link` on each roster entry. Minting
is idempotent: running it again after the roster changes adds links for the
newcomers and leaves everyone else's alone.

The token names two URLs, and they are not interchangeable. The `link` on a
roster entry is `/availability/<token>`, the page a volunteer opens; the payload
behind it is `/api/availability/<token>`, which answers 404 for an unknown token
and 410 once the rota has been allocated.

`shiftIds` is the volunteer's whole answer, never a delta — an absent shift is a
no. Submitting again appends a generation and the latest wins.

## Send the round

Minting writes the links; sending emails them. In production that is an OAuth
redirect out to Google for a short-lived `gmail.send` grant, and the mail goes
out as the signed-in admin. Dev mode stubs the grant and the mailbox, so the
whole flow works with no credentials — **the emails are written to the log
instead of being sent**.

```bash
curl -b cookies.txt -L 'localhost:8080/auth/gmail?mode=round&deadline=Friday+28+August'
curl -b cookies.txt -L 'localhost:8080/auth/gmail?mode=reminder&deadline=Friday+28+August'
curl -b cookies.txt -L 'localhost:8080/auth/gmail?mode=resend&volunteerId=<id>&deadline=Friday+28+August'
```

Each redirects to `/admin/availability?send=<job id>` and the emails go out
behind it — a send is a background job, not the body of the request, because
thirty of them at Gmail's three-second throttle takes about ninety seconds.
`GET /api/availability-sends/<job id>` reports how far it has got and, once it
has finished, who it reached and which addresses it failed on.

To read what would have been sent, including each volunteer's link:

```bash
scripts/dev-stack.sh logs | grep "pretending to send"
```

Only the *delivery* is stubbed. Who gets an email, what it says, and the
`sent_at` stamp that stops the next send asking them again are all the real
thing. The three modes differ in who they select: `round` takes everyone not yet
sent, `reminder` takes everyone who was sent and has not answered — skipping a
volunteer whose group partner answered for them — and `resend` takes one named
volunteer whether or not they have been sent already.

The deadline is quoted in the email and stored nowhere. Allocation is the real
cutoff, and a send is refused once the rota has been allocated: the links stopped
working then, so every email would carry a dead one.

## Drive it with a browser

`.mcp.json` in the repo root configures `playwright-mcp` — headless, isolated
profile, output to `logs/playwright/`. Navigate to
`http://localhost:8080/auth/login` first and the session carries through
everything after it.

It is pinned to `--browser chromium`, Playwright's own build, rather than the
default `chrome` channel. Two reasons. It does not assume a Google Chrome
install, so the config works on any checkout. And it does not drive the
machine's everyday browser: launching that starts Chrome's updater, which tries
to modify `/Applications/Google Chrome.app`, which macOS blocks under App
Management — reported against whichever app is at the top of the process tree,
so an agent's browsing surfaces as *"VS Code was prevented from modifying apps
on your Mac"*. Playwright's headless shell has no updater and lives in
`~/Library/Caches/ms-playwright/`, so nothing is ever asked for.

That cache is per-user and shared by every Playwright process on the machine.
The first run downloads into it (~100MB); after that it is free. To populate it
by hand:

```bash
npx -y playwright install chromium
```

Work from **accessibility-tree snapshots**, not screenshots: they are cheaper,
they are stable, and they say what a thing *is* rather than what it looks like.
A screenshot is evidence to hand a human reviewer — never the basis for your own
verdict that something works.

## What you can actually see

The database starts **empty**. Shifts you can seed yourself (see
[Define a rota](#define-a-rota)) and so can availability answers (see
[Collect availability](#collect-availability)); allocation itself you cannot,
since the web server does not expose it. So:

| Works | |
| --- | --- |
| Login and logout | The header shows the signed-in address and a Log out button |
| Header nav | Rota ↔ Admin |
| `/admin` tab routing | Redirects to `/admin/volunteers`; Config is still an unbuilt stub |
| The availability tab | Starts a round for the latest rota and lists every volunteer, their link and whether they have answered |
| The volunteer's form | `/availability/<token>`, public — no session, no header, mobile first |
| Admin sync | The Volunteers tab's Sync button re-reads the CSV and returns 204 |
| The volunteer list | The Volunteers tab lists the whole roster with its counts, from `test_data/volunteers.csv` |
| `GET /api/volunteers` | The full roster from `test_data/volunteers.csv`, behind `requireAdmin` |
| The rota tab | Defines a rota and lists the shifts it minted, behind `requireAdmin` |
| `POST /api/rotations` | Mints a rota's shifts with no Google credentials — the one way to get shifts into a dev database |
| The 404 route | Any unmatched path renders "Page not found" |

| Does not | |
| --- | --- |
| The rota page | Renders empty until you define a rota. After that an admin sees the minted shifts flagged as unallocated, and the public sees nothing — filling them needs an allocation run, which the web server does not expose |
| Deep-linking an admin tab | `http://localhost:8081/admin/volunteers` typed straight into the address bar renders blank: the build emits relative asset paths, so a nested route asks for `/admin/chunk-*.js` and the SPA fallback answers with `index.html`. Reach the tab by loading `/` and clicking through. |
| Sync copy | The Volunteers tab's sync caption says "the Google Sheet" — in dev mode it is the CSV. |
| Anything Sheets, Forms or Gmail | Never reached in dev mode |

To work against real data instead, point `databaseURL` at the maintainer's
populated `ilford_dropin_test` — but only when nothing needs to clone it, since
Postgres will not clone a database with a connection open on it.

## Why this is safe

`drop_in_config.dev.yaml` is the one config file committed to the repo. It holds
no secrets: fake sheet IDs that are never read, an `agent@example.com` admin
address, and a session secret that signs sessions for a server which already
mints them on request.

The `devMode` block is what turns the stubs on, and
`internal/config.checkDevMode` **fails the config load** if it appears under any
environment but `dev`. Gating on the name rather than on "not prod" means a new
environment is credential-backed unless someone deliberately calls it `dev`;
failing rather than ignoring means nobody is left believing a gate they set is
off when it is on. `pkg/api.NewStubAuthenticator` is unreachable without that
block, and refuses to build if `devMode.adminEmail` is missing from
`server.adminEmails`.

The server also logs a `DEV MODE` warning at startup and again on every stub
login, so a stack running with the stubs on says so in its own log.

## When it will not start

| Symptom | Fix |
| --- | --- |
| `Something is already serving http://localhost:8080/health` | Another stack has the port. Stop it, or set `API_PORT`. |
| `Server exited during startup` | The log tail is printed with it — usually the port or the database. |
| `Timed out after 60s` | The server is up but `/health` is not answering; the database is the usual cause. `scripts/test-db.sh status`. |
| `Missing drop_in_config.dev.yaml` | It is committed — `git checkout drop_in_config.dev.yaml`. |
