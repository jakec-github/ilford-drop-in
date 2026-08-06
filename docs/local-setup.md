# Local setup

This guide gets a new contributor from a fresh clone to a running rota page,
using the system exactly as it works today. It is deliberately faithful to the
current design: the app reads its volunteer roster from Google Sheets, and the
CLI authenticates with Google on startup, so **you will need your own Google
Cloud project** for the path this guide describes.

> **Just want to look at the app?** There is now a Google-free `dev`
> environment: `scripts/dev-stack.sh start` boots the whole stack on
> <http://localhost:8080> with no credentials at all, reading the roster from
> `test_data/volunteers.csv` and logging you in as an admin without Google. It
> needs only Docker, Go and Bun. The database starts empty, so the rota page
> renders empty — everything else works. See
> [`docs/agents/dev-stack.md`](agents/dev-stack.md). The rest of this guide is
> the real, Sheets-backed path, and is what you need to develop against actual
> data or to use the CLI.

> **Scope.** Production and the sheet-backed commands (`allocateRota`,
> `publishRota`, `listVolunteers`) are **not** expected to work for outside
> contributors — they need real Sheets wired up and, for prod, infrastructure
> access. This guide covers everything needed to run the app, view the rota, and
> develop against it locally.

## 1. Prerequisites

| Tool | Version | Notes |
| --- | --- | --- |
| [Go](https://go.dev/dl/) | ≥ 1.25 | Builds the CLI and server. |
| [Docker](https://www.docker.com/) | any recent | Runs the local Postgres. |
| [Bun](https://bun.sh/) | latest | Frontend runtime/bundler: `curl -fsSL https://bun.sh/install \| bash`. |

A Google account you can use for testing (a throwaway or your own) is also
required for the Google Cloud steps below.

## 2. Google Cloud project

The app talks to Google through **three separate credentials**, each used by a
different part of the system. Set up all three for the `test` environment.

1. **Create a project** in the [Google Cloud Console](https://console.cloud.google.com/)
   and enable these APIs (APIs & Services → Library):
   - Google Sheets API
   - Gmail API

2. **Desktop OAuth client** — used by the **CLI**.
   - APIs & Services → Credentials → Create credentials → OAuth client ID →
     **Desktop app**.
   - Download the JSON and save it in the repo root as `oauthClient.test.json`.
   - On first CLI run this opens a browser to authorise two scopes: Sheets, to
     read the volunteer roster, and email, to record who ran the command. The
     resulting token is cached at
     `~/.ilford-drop-in/tokens/token-test.json` — delete that file to force
     re-authorisation.
   - The Google account you authorise **must have access to the sheets** in
     step 3.

3. **Web OAuth client** — used by the **server** for admin login.
   - Create credentials → OAuth client ID → **Web application**.
   - Add `http://localhost:5173/auth/callback` as an authorised redirect URI.
     That is the **frontend** dev server, not the Go server on 8080: locally you
     browse the app on 5173 and it proxies `/auth/*` through to the server, so
     the URI Google sends the browser back to is the one on 5173. In production
     the server serves the frontend itself, so there the callback is on the
     app's own domain (see [`docs/deployment.md`](deployment.md)).
   - Download the JSON and save it as `oauthClientWeb.test.json`.

4. **Service account** — used by the **server** to read the volunteer roster.
   - IAM & Admin → Service Accounts → create one → add a JSON key → download.
   - Save it as `serviceAccount.test.json`.
   - Copy the service account's `client_email` (looks like
     `name@project.iam.gserviceaccount.com`) — you'll share the volunteer sheet
     with it in the next step.

All three files are git-ignored, so they never get committed. See the
[credentials reference](#credentials-reference) at the bottom for which part of
the code loads each one.

## 3. Google Sheets

Create two spreadsheets in the same Google account:

- **Volunteer sheet** — the roster the app reads.
- **Rota sheet** — where a published rota is written (only needed for the
  publish flow, which is out of scope here, but the ID is a required config
  field).

Note each spreadsheet's ID from its URL
(`https://docs.google.com/spreadsheets/d/`**`<THIS-PART>`**`/edit`).

**Grant access:**

- Share the **volunteer sheet** with the **service account** `client_email`
  (Viewer is enough) so the server can sync the roster.
- Make sure the **Google account you authorise in the CLI** can also read both
  sheets.

### Volunteer sheet format

Put the roster on a tab (e.g. `volunteers`) whose name you'll set as
`serviceVolunteersTab`. The **first row must be a header** containing exactly
these column names (order doesn't matter; extra columns are ignored):

```
Unique ID | First name | Last name | Roles | Status | Sex/Gender | Email | Group key
```

Rows with an empty `First name` are skipped.

`Roles` holds the jobs that volunteer does — the roles named in your config
([§4](#4-configuration-file)) — as a comma-separated list, so someone who both
leads and serves reads `Team lead, Service volunteer`. Anything the config does
not name is warned about in the logs and ignored, so a typo costs one person one
role rather than failing the whole sync.

Role names can hold commas and quotation marks: Sheets writes the cell by the
CSV rules, quoting any value that needs it and doubling the quotation marks
inside one, and that is how the cell is read back. Nothing needs escaping by
hand — configure the name you want and pick it from the dropdown.

| Roles configured | The cell reads |
| --- | --- |
| `Team lead`, `Service volunteer` | `Team lead, Service volunteer` |
| `Kitchen, hot food` | `"Kitchen, hot food"` |
| `The "spare pair"` | `"The ""spare pair"""` |

Set the column up as a **multi-select dropdown** so the values can't be
mistyped: select the column, **Insert → Dropdown**, add one option per
configured role, and turn on multiple selections in the rule's advanced options.
Editing a cell then gives you chips to pick from, and Sheets stores what you
pick as the comma-separated string above.

**Seed data:** `test_data/volunteers.csv` is a ready-made sample roster with the
correct headers — paste it into the sheet. The sample emails use Gmail
plus-addressing (`youremail+sarah.johnson@gmail.com`) so every notification
lands in one inbox during testing; replace `youremail` with your own Gmail
username if you want mail to route to you.

## 4. Configuration file

Create `drop_in_config.test.yaml` in the repo root (also git-ignored). Every
field below without an "optional" note is required:

```yaml
# Google Sheet IDs (from the sheet URLs)
volunteerSheetID: 'your-volunteer-sheet-id'
rotaSheetID: 'your-rota-sheet-id'
serviceVolunteersTab: 'volunteers'          # the tab name in the volunteer sheet

# Local Postgres (matches scripts/test-db.sh)
databaseURL: 'postgres://postgres:postgres@localhost:5432/ilford_dropin_test?sslmode=disable'

# Gmail (only used by the availability/reminder flows)
gmailUserID: 'me'
gmailSender: 'your-email@gmail.com'          # optional

# Allocation
defaultShiftSize: 4                           # volunteers per shift (excluding team lead)

# What the drop-in decides about itself is not configured here. The Roles
# volunteers hold, the Rota Defaults (the default shift start, end and
# timezone) and the Allocation Settings (which optional allocator rules apply,
# and the share of a rota one volunteer may work) are rows in the database
# (ADR 0006), set on the Settings screen rather than in this file. A `roles:`,
# `shiftStartTime:`, `shiftEndTime:`, `shiftTimezone:`, `maxAllocationFrequency:`,
# `requiresMale:` or `preallocations:` key left over from an older config is
# ignored with a warning — see "Creating the Roles" below.

# HTTP server (required to run the web server)
server:
  port: 8080
  sessionSecret: 'change-me-min-16-chars'     # signs admin session cookies; ≥16 chars
  adminEmails:                                 # Google accounts allowed to log in as admin
    - 'your-email@gmail.com'

# Optional: overrides for specific recurring shifts. An override says how big a
# shift is and nothing else — who is pinned to one is a Standing Preallocation,
# set on Admin → Settings, and it seeds ordinary pins when a rota is defined.
rotaOverrides:
  - rrule: 'FREQ=MONTHLY;BYDAY=3SU'            # third Sunday monthly
    shiftSize: 5
```

The `test` suffix in the filename matches the `-e test` / `-env test` flag you
pass at runtime. Config files are also searched for in your home directory if
not found in the repo root.

### Creating the Roles

Roles live in the database, and no migration seeds them: a fresh database has
none, which means nobody on the roster holds a Role and allocation refuses to
run. The server says so at startup.

The credential-free dev stack (`scripts/dev-stack.sh start`) seeds its own, so
this only applies to a database you point a `test` or `prod` config at. Create
them on **Admin → Settings**, which needs nothing but an admin login. The pair
the app shipped with is:

| Name | Most per shift | Priority | Colour |
| --- | --- | --- | --- |
| Team lead | 1 | 1 | violet |
| Service volunteer | no limit | 2 | teal |

The names have to match the values in the roster sheet's `Roles` column
exactly, and should be the options that column's dropdown offers. The ceiling is
how many of that Role a shift may ever hold; leaving it blank is no ceiling, and
it is the uncapped Role's seats that `defaultShiftSize` buys. Priority orders
the filling of seats. Colour is one of twelve palette tokens rather than a
colour value, because the app owns what each looks like in light and dark mode:

```
violet  teal    blue  indigo  cyan   green
amber   orange  rose  pink    brown  slate
```

Give every Role its own token: two Roles sharing one is legal and unreadable.

A Role is permanent — the screen offers no delete and no retire, so that nothing
referencing a Role can dangle. Renaming is allowed, and is the one edit the app
cannot finish on its own: the roster sheet holds Roles by name, so it needs the
same edit or nobody holds that Role any more.

A Role is permanent — there is no delete and no retire, so nothing that
references one can dangle. Renaming one is allowed, but the roster names Roles
by string, so the sheet needs the same edit; the server warns on every sync
about a Role nobody holds and a roster value it does not know, which is what a
half-finished rename looks like.

### Setting the shift times

When the drop-in runs is set on the same screen, under **Rota Defaults**: a
start time, an end time and the timezone they are read in. Like the Roles, no
migration seeds them.

Until they are set, defining a rota and allocating one both refuse and name what
is missing. A shift's date is the date it starts, so a shift cannot be minted
without knowing when it runs. Nothing else is gated: everything that only reads
still reads.

A shift has to end the evening it starts — a session running past midnight is
refused rather than stored as one ending before it began.

These are the times each *new* shift is minted with, not a live setting the
shifts follow. A shift keeps the times it was minted with when they change
later, and an admin who wants one evening to run differently edits that shift on
the rota, under **Edit rota** → the date.

### Choosing the allocation rules

**Admin → Settings → Allocation rules** is which of the optional allocator
rules apply: a cap on how often one person works (a switch and a share of the
rota), male cover, no back-to-back shifts, and at most one shift a month. Like
the Roles and the shift times, nothing seeds them, and a rule nobody has
switched on is off — so a fresh database allocates with none of them.

The rules that make a rota a rota — availability, seat capacity, grouping,
closed shifts, preallocations — are not listed and cannot be switched off.

`maxAllocationFrequency` and `requiresMale` used to be config keys. They are
these settings now (ADR 0006): both were an admin's decision rather than an
operator's, and male cover was two halves of one idea in two places. A config
still carrying them is warned about and otherwise ignored.

A key the app does not recognise — a typo, or an option since renamed — is
warned about by name and then ignored. It does not fail the load: the same file
gets read by whatever build is running, so a key that only some versions know
must not stop the app starting. To check a file without starting anything:

```bash
go run ./cmd/cli -e test validate-config drop_in_config.test.yaml
```

That reads the file and nothing else — no database, no Google — and prints what
the config configures, which is worth a glance: a config can be perfectly valid
and still not say what you meant.

## 5. Local database

The `test` environment uses a Postgres container managed by a helper script:

```bash
scripts/test-db.sh start     # create/start the container
scripts/test-db.sh status    # check it
scripts/test-db.sh psql      # open a psql shell
scripts/test-db.sh reset     # wipe and recreate (destroys data)
```

It listens on `localhost:5432` with database `ilford_dropin_test`
(user/password `postgres`/`postgres`), matching the `databaseURL` above.
Migrations run automatically the first time the CLI or server connects — there
is no separate migrate step.

## 6. Build the CLI and populate the database

Build:

```bash
go build -o cli ./cmd/cli
```

The rota page reads shifts from the database, which starts empty. Create a
rotation and its weekly shifts:

```bash
./cli -e test defineRota 12
```

`defineRota` writes straight to Postgres, but note the CLI authenticates with
Google on **every** command — so this first run opens a browser for the OAuth
flow (step 2's desktop client). After that the token is cached and subsequent
commands are quiet.

Other read-only commands to explore:

```bash
./cli -e test listVolunteers    # prints the roster from the volunteer sheet
./cli --help                    # all commands
```

## 7. Run the app

Run the full stack (Postgres + Go server + frontend dev server) with:

```bash
scripts/dev.sh test
```

- Frontend: <http://localhost:5173>
- API/server: <http://localhost:8080>

The rota page is public — no login needed to view it. Admin actions (creating
alterations and preallocations) require logging in with a Google account listed
in `adminEmails`, via the web OAuth client from step 2.

To run the frontend alone (assuming the server is already up), see
[`web/README.md`](../web/README.md).

For the credential-free alternative — one process on 8080, serving the built
frontend itself, started in the background — see
[`docs/agents/dev-stack.md`](agents/dev-stack.md).

## 8. Run the tests

```bash
scripts/check.sh                     # build, vet, tests, solver tests, frontend typecheck + lint
go test ./...                        # everything
go test ./pkg/core/services/...      # one package
go test -cover ./...                 # with coverage
```

`scripts/check.sh` is the one to run before pushing — it is what CI runs on
every pull request.

Tests need **no Google credentials**. Integration tests that need Postgres
mint throwaway databases on the `test-db.sh` server and **self-skip** if it
isn't running, so a bare `go test ./...` passes on a checkout with no database.
Start the DB (`scripts/test-db.sh start`) to exercise them, or run
`scripts/check.sh`, which starts it for you and fails rather than skips if it
cannot be reached.

The Python allocator has its own tests — see
[`pyallocator/README.md`](../pyallocator/README.md). `check.sh` runs them too,
building `pyallocator/.venv` first if it is not there yet.

## Troubleshooting

| Symptom | Fix |
| --- | --- |
| `failed to load config` | File must be named `drop_in_config.test.yaml` in the repo root (or home dir); check every required field is present. |
| `failed to load service account` / `web OAuth client config` | The server needs `serviceAccount.test.json` and `oauthClientWeb.test.json` in the repo root. |
| `missing required field in header` | The volunteer sheet header row must contain the exact column names in [§3](#volunteer-sheet-format). |
| `volunteer sheet names a Role no configured Role matches` (warning) | A value in a `Roles` cell isn't one of the roles in your config. The volunteer loads without it. |
| Nobody gets allocated to a role | Check the `Roles` column actually names it — a role nobody holds has no one to fill its seats. |
| Empty rota page | Run `./cli -e test defineRota 12` to create shifts. |
| Empty roster / `failed to fetch volunteers` | Share the volunteer sheet with the service account email; check `volunteerSheetID` and `serviceVolunteersTab`. |
| OAuth loops or missing scopes | Delete `~/.ilford-drop-in/tokens/token-test.json` and re-run to re-authorise. |
| `redirect_uri_mismatch` on admin login | The web OAuth client needs `http://localhost:5173/auth/callback` registered ([§2](#2-google-cloud-project)) — the frontend's port, not the server's. |
| Postgres unreachable | `scripts/test-db.sh start` (Docker must be running). |

## Credentials reference

| File (git-ignored) | Loaded by | Purpose |
| --- | --- | --- |
| `oauthClient.test.json` | CLI (`internal/config/oauth.go`) | Desktop OAuth; Sheets/Forms/Gmail access for CLI commands. Token cached in `~/.ilford-drop-in/tokens/`. |
| `oauthClientWeb.test.json` | Server (`internal/config/oauthweb.go`) | Web OAuth; admin login (OIDC). |
| `serviceAccount.test.json` | Server (`internal/config/serviceaccount.go`) | Reads the volunteer sheet to sync the roster at startup and on admin sync. |
| `drop_in_config.test.yaml` | Both (`internal/config/config.go`) | Sheet IDs, DB URL, shift/allocation settings, server config. |

`drop_in_config.dev.yaml` is the one exception: it **is** committed, holds no
secrets, and needs none of the three credential files above — it turns on the
stubs described in [`docs/agents/dev-stack.md`](agents/dev-stack.md).
`internal/config.checkDevMode` refuses to load its `devMode` block under any
environment but `dev`, so it cannot affect `test` or `prod`.
