# Deployment runbook

Architecture and rationale: `docs/adr/0002-deployment-architecture.md`. In
short: one droplet running Docker Compose — Caddy, and an app container each
for prod and [preprod](#preprod) — Postgres on Neon, images built by CI on
every merge to main and deployed over SSH: to preprod by every merge, to prod
only by hand.

## Merges deploy preprod

Every merge to main builds `ghcr.io/jakec-github/ilford-drop-in`, tagged with
the git SHA and `latest`, smoke-tests it, and deploys it to
[preprod](#preprod). Nothing else: a merge never changes the prod container.
While preprod is [switched off](#switching-it-off) the deploy skips it with a
notice on the run.

Every rollout is `deploy/rollout.sh <env> <sha>` on the droplet, which:

- waits for any other rollout on the box to finish (a lock, held for at most
  ten minutes);
- records the tag in that environment's own `env/<env>/image.env`;
- recreates that environment's container and no other — and fails if the
  other environment's container changed;
- waits for `https://<its domain>/health` and fails, printing the app's last
  50 log lines, if it does not answer within two minutes;
- removes the images no container uses.

The merge's deploy also copies `deploy/compose.yaml`, `deploy/Caddyfile` and
`rollout.sh` to the droplet. They are main's for both environments; a
production deploy promotes an image and leaves them as they are.

## Deploy to production

Prod is released by hand: **Actions → Deploy to production → Run workflow**.

- **Leave the commit empty** to promote what preprod last ran — the build that
  was actually checked. It is the same image, not a rebuild, so prod runs the
  bytes preprod ran.
- **Give an older commit to roll back.** Full or short SHA; it must be a merge
  to main, since only those have an image. That is the whole of rollback.

Before anything on the droplet changes, the workflow reads which commit each
environment runs, resolves the one asked for, and checks that
`ghcr.io/jakec-github/ilford-drop-in:<sha>` exists. A branch name, an unknown
commit or a commit with no image fails there and touches nothing. A commit
other than preprod's is deployed with a notice saying so. The run's summary
shows prod's commit before and after, and preprod's.

Then it runs `rollout.sh prod <sha>`, so it waits for prod's `/health` as a
merge's deploy waits for preprod's, and fails the same way.

It runs in the GitHub Environment `production`. That is where required
reviewers can be turned on, and where prod's deploy secrets can move if prod
and preprod ever stop sharing a box.

A rollback past a migration runs the older build against the newer schema:
migrations are never reversed, and the older build ignores the ones it does
not know. That is harmless for a migration that only adds; check the
migrations between the two commits before rolling back across one that drops
or renames.

### On preprod first, then prod

A step that used to be "run X after the merge" is now **"run X on preprod, then
on prod before promoting"**. After a merge, preprod is running the new build
and prod the old one, so anything the new build needs — a config change, a
Role created on Organiser → Settings, a one-off command — is done on preprod
straight away, and on prod just before promoting: not at the merge, while prod
still runs the build that does not need it.

**Config is not part of either deploy.** Each environment's `drop_in_config.yaml` and
its two credential files live on the droplet by hand and are never shipped by a
deploy —
deliberately, since they hold secrets the repo must not. Roll them out with
[`scripts/deploy-config.sh`](#config-rollout), on their own schedule.

A consequence worth knowing: a deploy can ship a binary that rejects the config
already on the box. That is what happened when `roles` became required — the
deploy was green, the app crash-looped, and no check anywhere failed. Whenever a
change makes a config key required, removed or renamed, roll the config out to
preprod after the merge, and to prod just before promoting.

## The image

One image carries everything the server runs: the Go binary (with the frontend
embedded) and the CP-SAT solver it drafts rotas with, which is Python. The
runtime base is `gcr.io/distroless/python3-debian12:nonroot` — no shell, no
package manager, not root.

- The solver lives in a venv at `/opt/pyallocator`, and
  `ILFORD_CPSAT_PYTHON=/opt/pyallocator/bin/python` is how the server finds it.
- The venv is built in a `debian:12` stage with Debian's own `python3.11`: the
  same interpreter, at the same path, as the distroless runtime's. A venv only
  runs under the interpreter that made it, so changing either base means
  changing both — see the `Dockerfile`.
- `scripts/image-smoke.sh <image>` runs the solver inside a built image on a
  small input, and checks the image runs as nonroot with no shell. CI runs it on
  every PR, and the deploy runs it before pushing, so an image that cannot draft
  a rota fails the build rather than the first draft in prod. Run it locally
  after touching the `Dockerfile`:

  ```sh
  docker build -t ilford-drop-in:local . && scripts/image-smoke.sh ilford-drop-in:local
  ```

ortools makes the image a little under 500 MB, against the droplet's 8.7 GB
disk. Each deploy removes the images no container uses (see
[Operations](#operations)), so only one or two are ever on the box.

## One-time setup

### 1. Droplet and DNS

- Create the droplet (Ubuntu LTS, London). Note its IP.
- Point the domain's A record (host `@`) at the IP **before** first boot of
  the stack — Caddy's certificate issuance needs the name to resolve.
- Write the domain into `/opt/dropin/site.env` on the box. It is not in the
  repo — see [The site's domain](#the-sites-domain). Each drop-in sits under
  `/org/<id>/` on that host — see [Organisations](#organisations).

### 2. Provision the box

```sh
scp scripts/provision.sh root@<ip>:
ssh root@<ip> ./provision.sh
```

Idempotent: installs Docker, enables ufw (22/80/443), creates a 2G swap file,
`/opt/dropin/env/prod` and `/opt/dropin/env/preprod`, and a
`/opt/dropin/site.env` skeleton with empty `SITE_DOMAIN` and `PREPROD_DOMAIN`.
Fill in `SITE_DOMAIN` — the stack will not start until you do. Preprod has its
own setup, [below](#preprod).

### 3. Config files

Each environment has three untracked files on the droplet, in
**`/opt/dropin/env/<env>/`** — the directory `deploy/compose.yaml` mounts into
that environment's container as `/app/env/<env>`, which is where the server
looks for them. A fourth, Caddy's, is in `/opt/dropin`. Locally each
environment's three sit at the same paths under the repo root, `env/<env>/`.
Prod's are below; preprod's are the same three, in `env/preprod/`.

| File | What it is | How it gets there |
| --- | --- | --- |
| `env/prod/drop_in_config.yaml` | the app's config | [`scripts/deploy-config.sh`](#config-rollout) |
| `env/prod/oauthClientWeb.json` | Google web client | `scp`, about once a year |
| `env/prod/serviceAccount.json` | Google service account | `scp`, about once a year |
| `site.env` | `SITE_DOMAIN=<domain>` and `PREPROD_DOMAIN=<domain>` for Caddy | written by hand, once |

The three in `env/prod/` are the one thing the repo cannot regenerate; keep
copies with other personal secrets.

The two credentials change about once a year, so they go up by hand:

```sh
scp env/prod/oauthClientWeb.json env/prod/serviceAccount.json root@<ip>:/opt/dropin/env/prod/
```

Check that `oauthClientWeb.json` lists the production redirect URI
(`https://<domain>/auth/callback`) and that it is also registered on the Google
web client. It stays at the root of the domain, shared by every Organisation — see
[Organisations](#organisations).

The service account reads the volunteer sheet and writes the rota sheet, so
share both with its `client_email`: the volunteer sheet as a **Viewer**, the rota
sheet as an **Editor**. Without the second, the app runs as normal and the rota
sheet is left alone — every publish fails with a warning in the logs, which is
the only place it shows (issue #191).

`drop_in_config.yaml` changes often, so it gets a script — see
[Config rollout](#config-rollout) below. Run it once after the first deploy;
it puts the file in place and brings the app up.

### 4. GitHub Actions secrets and variables

Repository **secrets**:

| Name | Value |
| --- | --- |
| `DEPLOY_HOST` | droplet IP |
| `DEPLOY_SSH_KEY` | private key of a dedicated CI deploy key pair (`ssh-keygen -t ed25519`); add the public half to the droplet's `/root/.ssh/authorized_keys` |
| `DEPLOY_KNOWN_HOSTS` | output of `ssh-keyscan <ip>` |

Both workflows read them. The production one runs in the GitHub Environment
`production`, which GitHub creates on its first run; an environment secret of
the same name would override the repository one for prod alone.

### 5. First deploy

Run Actions → Build and deploy → "Run workflow", or merge anything to main. It
copies `deploy/compose.yaml`, `deploy/Caddyfile` and `deploy/rollout.sh` to
`/opt/dropin`, starts Caddy and deploys preprod; Caddy obtains its certificate
on first boot. Then run Actions → Deploy to production with the same commit to
start prod. `site.env` must already be there with a domain in it, or the stack
will not come up. Until preprod is set up, switch it off first
(`scripts/preprod.sh down`), or every merge's deploy fails.

## The site's domain

The domain is a fact about where the box is reached, not about what the app
does, so it lives on the droplet exactly as the config files do (issue #202).
`deploy/Caddyfile` reads it from the environment:

```
{$SITE_DOMAIN} {
	reverse_proxy app:8080
}

{$PREPROD_DOMAIN} http://preprod.invalid {
	reverse_proxy app-preprod:8080
}
```

and `deploy/compose.yaml` gives the caddy service `env_file: ./site.env`,
pointing at an untracked `/opt/dropin/site.env`:

```sh
ssh root@<ip> 'echo SITE_DOMAIN=example.org > /opt/dropin/site.env'
```

Hostname only — no scheme, no path, no port. `PREPROD_DOMAIN` is preprod's,
the same way; the `http://preprod.invalid` beside it is only there so an empty
one leaves a site nobody visits rather than a Caddyfile that will not load,
which would take prod down with it. Two details are deliberate and worth not
undoing:

- **`site.env`, not `.env`.** The deploy workflow writes `/opt/dropin/.env`
  wholesale on every deploy, so anything else put there survives until the next
  merge and no longer.
- **`env_file`, not compose interpolation.** Caddy resolves `{$SITE_DOMAIN}`
  itself, so the value has to reach its *process* environment; substituting it
  into the compose file would not get it there.

### Changing it

A domain move is a droplet-side change and nothing else — no commit, no merge,
no deploy:

1. Point the new name's A record at the droplet and wait for it to resolve.
2. Rewrite `/opt/dropin/site.env`.
3. `ssh root@<ip> 'cd /opt/dropin && docker compose up -d --force-recreate caddy'`.
   Caddy obtains a certificate for the new name as it starts.
4. Update the Google web client's authorised redirect URI to
   `https://<new domain>/auth/callback`, and `server.redirectURI` in
   `env/prod/drop_in_config.yaml` with it — then run
   [`scripts/deploy-config.sh prod`](#config-rollout).

Reverting is the same four steps with the old name. Preprod's domain moves the
same way, with `PREPROD_DOMAIN` and `env/preprod/drop_in_config.yaml`.

**Step 3 is what makes the edit take.** `SITE_DOMAIN` is read into Caddy's
process environment once, when the container starts, so only a *new* container
ever sees a new value. Neither of the two things that look like they would work
does: `docker compose restart caddy` restarts the process with the environment
it already has, and `caddy reload` re-adapts the Caddyfile from inside a process
whose variable is still the old one.

A bare `docker compose up -d` would in fact do it — compose resolves `env_file`
into the service's configuration and recreates the container when that changes,
which is *not* true of the app's bind-mounted config file, whose contents
compose cannot see (the issue #100 lesson, recorded in `deploy/compose.yaml`).
`--force-recreate` is given above anyway: it is unconditional, so it does not
ask the operator to remember which kinds of change compose notices. Expect a few
seconds of downtime while the new container starts and certifies the new name.

A changed `Caddyfile` needs the same step: a deploy copies the new file over
but never restarts Caddy, so the running one keeps the routes it started with.

### When it is missing

An absent or empty `SITE_DOMAIN` stops the stack rather than serving on a
wildcard address with no certificate:

- **No `site.env` at all** — every rollout refuses to run, so the deploy goes
  red: `/opt/dropin/site.env not found`.
- **Empty `PREPROD_DOMAIN`** — prod is unaffected, and a merge's deploy fails
  before touching anything, naming the variable. Switch preprod off until
  it has one.
- **Empty `SITE_DOMAIN`** — the caddy container exits, saying so by name:
  `SITE_DOMAIN is empty or unset: set it in /opt/dropin/site.env`. That check is
  in `deploy/compose.yaml` rather than left to Caddy, which rejects the empty
  site address as "unrecognized global option: reverse_proxy" — loud, but about
  the wrong thing.

## Organisations

One server runs any number of drop-ins — Organisations — each listed under
`organisations:` in the environment's config with its own `id`, `displayName`,
allowlists and sheets ([ADR 0011](adr/0011-one-server-many-organisations.md)).
The `id` is the Organisation's address: everything it has lives under
`/org/<id>/`.

```yaml
organisations:
  - id: <org-id>                    # lowercase letters, digits and hyphens
    displayName: <Name shown on the rota and in calendars>
    organiserEmails: [<organiser@example.com>]
    rotaEditorEmails: []
    volunteerSheetID: <sheet-id>
    serviceVolunteersTab: <tab-name>
    rotaSheetID: <sheet-id>
```

What lives under `/org/<id>/` is what leaves the app and cannot be corrected
afterwards, along with the API the pages call:

- **The Organisation's own pages** — the rota, the Organiser screens, the
  availability form. They are bookmarked, and the availability form's URL is
  emailed.
- **`/org/<id>/calendars/{filename}`**, the calendar feeds. Once a volunteer
  subscribes, the URL lives in their calendar app and fails silently if it
  moves.
- **`/org/<id>/api`**, which only the pages' own JavaScript calls.

What stays at the root of the domain:

- **`/`**, a splash page that names and links to no Organisation. An
  Organisation's address is the only way in. An unknown id gets the same
  "Page not found" as any other missing path, with a 404. Both are the React
  app, with its assets served at the root too.
- **`/auth`.** The callback URI is registered by hand in the Google console, so
  one shared `/auth/callback` is a one-time step however many Organisations a
  server runs. **Adding an Organisation changes nothing about the Google web
  client.** The session it sets is identity only: whether that person may do
  anything is checked against each Organisation's own allowlists.
- **`/health`**, which the deploy workflow, `scripts/deploy-config.sh` and
  `scripts/dev-stack.sh` all poll.

Caddy is not involved: it keeps proxying everything to `app:8080` and knows
nothing about the paths.

An `id` is permanent. Changing it breaks every link already handed out, and two
kinds are out of reach once sent: a subscribed calendar feed and an emailed
availability link.

### Adding an Organisation

Add an entry to `organisations:` and run `scripts/deploy-config.sh`. On start
the server inserts any id the database has not seen; it never deletes one, so
an entry dropped from config keeps its rows.

### Moving a single-site config across

A config from before Organisations — `server.basePath`, top-level
`volunteerSheetID`, `server.organiserEmails` and the like — is refused at
startup by name. To move one across, and keep its existing links working:

1. Write one `organisations:` entry whose `id` is the last segment of the old
   `server.basePath`. Old links keep working only where the old base path was
   already `/org/<id>`; any other moves. Move the old sheet ids, tab name and
   allowlists into it, and add a `displayName`.
2. Delete `server.basePath`, the moved keys, `gmailUserID` and `gmailSender`
   (the last two are no longer read, and only warn).
3. Validate with `go run ./cmd/server -env <env> -validate-config <path>`, then run
   `scripts/deploy-config.sh <env>` once the new image is out. Between the
   deploy and the config the server refuses to start, so expect downtime.

Migration 030 gives every existing row a placeholder Organisation. On the first
start with a config naming exactly one id the database has not seen, the server
renames the placeholder to that id, carrying every row with it, and logs that
it did. With two or more new ids it refuses to start rather than guess whose
the rota was — add the others in a later rollout.

## Config rollout

Editing `env/<env>/drop_in_config.yaml` and reaching the running server is one
command, from the repo root:

```sh
scripts/deploy-config.sh prod          # or preprod
```

It validates the local file, shows what it configures, asks, copies it to
`/opt/dropin/env/<env>/`, and runs `rollout.sh <env>` on the droplet — the same
recreate and `/health` wait a deploy does, against that environment's container
and host only, exiting non-zero with the app's logs if it does not answer. There
is no `docker compose` step to run afterwards. While preprod is switched off its
config is shipped but nothing is started; `scripts/preprod.sh up` picks it up.

`.deploy.env` names the box it is all pointed at:

```sh
DEPLOY_HOST=<ip>
```

It can be given in the environment instead, or as the argument after the
environment. Nothing here needs the domains any more: `rollout.sh` reads them
from the droplet's own `site.env`, so a `SITE_DOMAIN` line left in
`.deploy.env` is ignored.

Three things it is doing on purpose:

- **Validation happens before the file leaves the machine.** Under the hood it
  is `go run ./cmd/server -env <env> -validate-config <path>`, which reads the file and
  nothing else: no database, no Google. Run it on its own any time.
- **The summary is the point.** "Valid" is not the same as "right" — a config
  can parse and validate with a whole section missing. The counts it prints are
  what to check against the change you meant to make.
- **It recreates rather than restarts.** A restarted container keeps the mounts
  it already resolved, and a bare `docker compose up -d` is a no-op when the
  compose file has not changed, so neither reliably picks up a new config.

A key the running build does not know is warned about by name and ignored,
rather than failing the load. That is deliberate, and was learned the hard way:
rejecting them meant a config carrying one key from either side of a rename
could not start the server at all, which is an outage over a file the operator
had not touched. The warning appears in the app's logs and in this command's
output — it is worth reading, because the other kind of unknown key is one that
used to configure something and now configures nothing.

## Domain settings

The Roles the drop-in offers are **rows in the database**, not config (ADR
0006). Nothing seeds them: the migration that created the table left it empty on
purpose, so a database is in this state until somebody fills it, and the server
warns at startup when it is. With no Roles nobody on the roster holds one and
allocation refuses to run.

They are created on **Organiser → Settings**, which is reachable as soon as an Organiser
can log in — no SQL and no deploy. This is the pair the config used to carry:

| Name | Most per shift | Priority | Colour |
| --- | --- | --- | --- |
| Team lead | 1 | 1 | violet |
| Service volunteer | no limit | 2 | teal |

**Do it as soon as the deploy carrying the migration is up.** Everything that
resolves a Role by name is unhappy until then, allocation loudest.

The names must match the roster sheet's `Roles` column exactly. A Role is
permanent — there is no delete and no retire — so getting the name right the
first time saves a rename, which the roster has to be edited to match; the
screen says as much at the point of rename.

## Preprod

A second copy of the app on the same droplet, at its own host, deployed by
every merge to main (ADR 0002, issue #244). Prod only ever runs an image
preprod has run first, [by hand](#deploy-to-production). It is there to try a change on
before it reaches anyone, so it shares nothing with prod but the box and the
image:

- **Its own host**, `PREPROD_DOMAIN` in `site.env` — conventionally
  `preprod.<domain>`. Not a path under prod's: `/`, `/auth` and `/health`
  sit at the root of a host (see [Organisations](#organisations)). No basic auth;
  Organiser screens need a Google login, as in prod.
- **Its own database**, a separate Neon project — not a branch of prod's.
- **Its own synthetic roster and rota sheets.** Never a copy of prod's
  volunteers: the roster's emails are plus-addresses the owner controls, as in
  `test_data/volunteers.csv`, so the real availability emails preprod sends
  reach nobody else.
- **Its own config**, `env/preprod/drop_in_config.yaml`, with real OIDC —
  `devMode` is refused outside dev. It ships exactly as prod's does:
  `scripts/deploy-config.sh preprod`.

### Setting it up

The owner-side half (DNS, Neon, Google, the sheets) is issue #246. On the
droplet it comes to:

1. Point `preprod.<domain>`'s A record at the droplet.
2. Set `PREPROD_DOMAIN=preprod.<domain>` in `/opt/dropin/site.env`, then
   `ssh root@<ip> 'cd /opt/dropin && docker compose up -d --force-recreate caddy'`
   so Caddy reads it and certifies the name (a few seconds of prod downtime).
3. Register `https://preprod.<domain>/auth/callback` on preprod's Google web
   client, and give preprod's service account the same access to its sheets as
   prod's has to prod's (see [Config files](#3-config-files)).
4. `scp env/preprod/oauthClientWeb.json env/preprod/serviceAccount.json root@<ip>:/opt/dropin/env/preprod/`
5. `scripts/preprod.sh up`, if it was switched off, then
   `scripts/deploy-config.sh preprod`. If preprod has never been deployed,
   re-run the Build and deploy workflow first: `rollout.sh` needs a tag to
   start.

### Seeding synthetic data

A fresh preprod database is empty, as prod's was:

1. Create the Roles on **Organiser → Settings**, named as in the synthetic
   roster's `Roles` column (see [Domain settings](#domain-settings)).
2. Define a rota and open its availability round from the Organiser screens.
3. Answer the round with made-up availability, logged in as an Organiser and
   passing that session's cookie (real OIDC, so the script cannot log in by
   itself):

   ```sh
   scripts/fake-availability.py --api-url https://preprod.<domain> --session <cookie>
   ```

### Switching it off

Two apps on a 512 MiB box is tight. When it runs short, preprod gives way:

```sh
scripts/preprod.sh down     # stop and remove app-preprod
scripts/preprod.sh up       # bring back the image it last ran, and wait for /health
```

`down` leaves `/opt/dropin/env/preprod/disabled` on the box. While it is there,
every merge's deploy skips it with a notice on the run, and
`scripts/deploy-config.sh preprod` ships the file without starting anything.
Prod is untouched either way. Caddy keeps its preprod site, which answers 502
while preprod is down. `up` pulls the image back if a prune has removed it;
the image is public, so that needs no login.

## Operations

- **Logs**: `ssh root@<ip> 'cd /opt/dropin && docker compose logs -f app'` —
  `app-preprod` for preprod
- **Status**: `docker compose ps` in `/opt/dropin`
- **What each runs**: `env/prod/image.env` and `env/preprod/image.env` hold the
  tag each environment last deployed. `/opt/dropin/.env` is rebuilt from them on
  every rollout, so a bare `docker compose up -d` leaves both as they are; edit
  neither by hand.
- **Restart**: `docker compose restart app` — note this does *not* pick up a
  changed config; use the config rollout above for that.
- The box holds no unregenerable state: rebuilding it is droplet + provision +
  scp + deploy, per the ADR.
- **Disk**: `ssh root@<ip> 'df -h /'`. Each deploy removes the images no
  container is using, and provisioning caps the journal at 100M, so the disk
  should hold steady.

### Recovering a full disk

If a deploy fails with `no space left on device`, or the box's CPU is pinned
(rsyslog spins retrying writes it cannot make), clear it by hand:

```sh
ssh root@<ip> '
  docker image prune -af
  journalctl --vacuum-size=100M
  systemctl restart rsyslog   # only if it has been spinning on a full disk
  df -h /
'
```

The first line removes every image no running container uses — the running app
keeps its own — and the second trims the journal. Then re-run the failed deploy.
On a droplet provisioned before the journal cap existed, rerun
`scripts/provision.sh` (it is idempotent) so the journal stays capped.

### One-time: moving config into `env/prod/`

Before issue #243 the server's three files sat in `/opt/dropin/config/`, with
the environment as a filename suffix. The build that reads `env/prod/` does not
look there, so the files must be in place **before** it deploys, or the app will
not start. Copy rather than move, so the running build keeps its files until the
new one replaces it:

```sh
ssh root@<ip> '
  mkdir -p /opt/dropin/env/prod
  cp /opt/dropin/config/drop_in_config.prod.yaml /opt/dropin/env/prod/drop_in_config.yaml
  cp /opt/dropin/config/oauthClientWeb.prod.json /opt/dropin/env/prod/oauthClientWeb.json
  cp /opt/dropin/config/serviceAccount.prod.json /opt/dropin/env/prod/serviceAccount.json
'
```

Once the new build is answering on `/health`, remove the old directory with
`ssh root@<ip> 'rm -r /opt/dropin/config'`. `scripts/deploy-config.sh` refuses
to recreate the container while the credentials are missing from `env/prod/`,
so a half-done move cannot take the site down.
