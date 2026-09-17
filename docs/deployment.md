# Deployment runbook

Architecture and rationale: `docs/adr/0002-deployment-architecture.md`. In
short: one droplet running Docker Compose (Caddy + app), Postgres on Neon,
images built by CI on every merge to main and deployed over SSH.

## Continuous deployment (no action needed)

Every merge to main builds `ghcr.io/jakec-github/ilford-drop-in`, tagged with
the git SHA and `latest`, and — if the `DEPLOY_ENABLED` repository variable is
`"true"` — deploys it to the droplet. **Rollback**: re-run the workflow on a
previous commit (Actions → the old run → "Re-run all jobs").

**Config is not part of this.** `drop_in_config.prod.yaml` and the two
credential files live on the droplet by hand and are never shipped by a deploy —
deliberately, since they hold secrets the repo must not. Roll them out with
[`scripts/deploy-config.sh`](#config-rollout), on their own schedule.

A consequence worth knowing: a deploy can ship a binary that rejects the config
already on the box. That is what happened when `roles` became required — the
deploy was green, the app crash-looped, and no check anywhere failed. Whenever a
change makes a config key required, removed or renamed, run the config rollout
alongside the merge.

## One-time setup

### 1. Droplet and DNS

- Create the droplet (Ubuntu LTS, London). Note its IP.
- Point the domain's A record (host `@`) at the IP **before** first boot of
  the stack — Caddy's certificate issuance needs the name to resolve.
- Write the domain into `/opt/dropin/site.env` on the box. It is not in the
  repo — see [The site's domain](#the-sites-domain). The site itself may sit
  under a path below that host — see [Base path](#base-path).

### 2. Provision the box

```sh
scp scripts/provision.sh root@<ip>:
ssh root@<ip> ./provision.sh
```

Idempotent: installs Docker, enables ufw (22/80/443), creates a 2G swap file,
`/opt/dropin/config`, and a `/opt/dropin/site.env` skeleton with an empty
`SITE_DOMAIN`. Fill that in — the stack will not start until you do.

### 3. Config files

Four untracked files live on the droplet. Three are the server's, in
**`/opt/dropin/config/`** — the directory `deploy/compose.yaml` mounts into the
container as `/app`. The fourth is Caddy's, a level up.

| File | What it is | How it gets there |
| --- | --- | --- |
| `config/drop_in_config.prod.yaml` | the app's config | [`scripts/deploy-config.sh`](#config-rollout) |
| `config/oauthClientWeb.prod.json` | Google web client | `scp`, about once a year |
| `config/serviceAccount.prod.json` | Google service account | `scp`, about once a year |
| `site.env` | `SITE_DOMAIN=<domain>` for Caddy | written by hand, once |

The three in `config/` are the one thing the repo cannot regenerate; keep copies
with other personal secrets.

The two credentials change about once a year, so they go up by hand:

```sh
scp oauthClientWeb.prod.json serviceAccount.prod.json root@<ip>:/opt/dropin/config/
```

Check that `oauthClientWeb.prod.json` lists the production redirect URI
(`https://<domain>/auth/callback`) and that it is also registered on the Google
web client. It stays at the root of the domain even when the site itself sits
under a path — see [Base path](#base-path).

`drop_in_config.prod.yaml` changes often, so it gets a script — see
[Config rollout](#config-rollout) below. Run it once here too; it puts the file
in place and brings the app up.

### 4. GitHub Actions secrets and variables

Repository **secrets**:

| Name | Value |
| --- | --- |
| `DEPLOY_HOST` | droplet IP |
| `DEPLOY_SSH_KEY` | private key of a dedicated CI deploy key pair (`ssh-keygen -t ed25519`); add the public half to the droplet's `/root/.ssh/authorized_keys` |
| `DEPLOY_KNOWN_HOSTS` | output of `ssh-keyscan <ip>` |

Repository **variable**:

| Name | Value |
| --- | --- |
| `DEPLOY_ENABLED` | unset until go-live; `true` to enable the deploy job |

### 5. First deploy

Set `DEPLOY_ENABLED=true`, then run the workflow (Actions → Build and deploy →
"Run workflow", or merge anything to main). The deploy job copies
`deploy/compose.yaml` and `deploy/Caddyfile` to `/opt/dropin` and starts the
stack; Caddy obtains its certificate on first boot. `site.env` must already be
there with a domain in it, or the stack will not come up.

## The site's domain

The domain is a fact about where the box is reached, not about what the app
does, so it lives on the droplet exactly as the config files do (issue #202).
`deploy/Caddyfile` reads it from the environment:

```
{$SITE_DOMAIN} {
	reverse_proxy app:8080
}
```

and `deploy/compose.yaml` gives the caddy service `env_file: ./site.env`,
pointing at an untracked `/opt/dropin/site.env`:

```sh
ssh root@<ip> 'echo SITE_DOMAIN=example.org > /opt/dropin/site.env'
```

Hostname only — no scheme, no path, no port. Two details are deliberate and
worth not undoing:

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
   `drop_in_config.prod.yaml` with it — then run
   [`scripts/deploy-config.sh`](#config-rollout).
5. Update `SITE_DOMAIN` in your own `.deploy.env`, so the config rollout polls
   the right host.

Reverting is the same five steps with the old name.

### When it is missing

An absent or empty `SITE_DOMAIN` stops the stack rather than serving on a
wildcard address with no certificate:

- **No `site.env` at all** — `docker compose up` refuses to run, so the deploy
  job itself goes red: `env file /opt/dropin/site.env not found`.
- **Empty `SITE_DOMAIN`** — the caddy container exits, saying so by name:
  `SITE_DOMAIN is empty or unset: set it in /opt/dropin/site.env`. That check is
  in `deploy/compose.yaml` rather than left to Caddy, which rejects the empty
  site address as "unrecognized global option: reverse_proxy" — loud, but about
  the wrong thing.

## Base path

`server.basePath` in the environment's config puts the site's own pages under a
single path segment — `/<path>/` rather than `/`. It is optional and absent in
dev, where the site is at the root.

The value belongs to the deployment and is deliberately not in this repo: it is
one key in `drop_in_config.<env>.yaml`, and the app derives everything else from
it. A leading slash, one or more URL-safe segments, no trailing slash; anything
else fails at startup. Depth is not restricted — `/rota` and `/ilford/rota` work
the same way, because nothing ever splits the path up.

What moves is what leaves the app and cannot be corrected afterwards:

- **The site's own pages** — the rota, the Organiser screens, the availability
  form. They are bookmarked, and the availability form's URL is emailed.
- **`/calendars/{filename}`**, the calendar feeds. Once a volunteer subscribes,
  the URL lives in their calendar app and fails silently if it moves.

`/` redirects to the base path, temporarily — the root may one day belong to
something else.

What stays at the root of the domain:

- **`/api`.** Its only caller is the page's own JavaScript, which ships with the
  server, so it can be pointed anywhere later at no cost.
- **`/auth`.** Better off at the root, not merely cheaper: the callback URI is
  registered by hand in the Google console, so one shared `/auth/callback` is a
  one-time step however many sites a server ends up serving. **Nothing about
  the Google web client changes when you set a base path.**
- **`/health`**, which the deploy workflow, `scripts/deploy-config.sh` and
  `scripts/dev-stack.sh` all poll.

Caddy is not involved either way: it keeps proxying everything to `app:8080` and
knows nothing about the path. So rolling a base path out is one step — set the
key and run `scripts/deploy-config.sh`.

Changing it afterwards breaks every link already handed out, and two kinds of
link are out of reach once sent: a subscribed calendar feed lives in a
volunteer's calendar app and fails silently, and an availability link has been
emailed. That is why the path was set before go-live rather than after
(issue #201).

## Config rollout

Editing `drop_in_config.prod.yaml` and reaching the running server is one
command, from the repo root:

```sh
scripts/deploy-config.sh <ip>          # or set DEPLOY_HOST, or put it in .deploy.env
```

It validates the local file, shows what it configures, asks, copies it to
`/opt/dropin/config/`, recreates the app container, and waits for
`https://$SITE_DOMAIN/health` to answer — exiting non-zero with the app's logs if
it does not. There is no `docker compose` step to run afterwards.

`.deploy.env` names the box it is all pointed at. Both lines are needed — the
domain is no longer anywhere in the checkout for the script to read:

```sh
DEPLOY_HOST=<ip>
SITE_DOMAIN=<domain>
```

Either can be given in the environment instead, and `HEALTH_URL` overrides the
poll URL outright. With no domain to work one out from, the script says so and
stops before shipping anything.

Three things it is doing on purpose:

- **Validation happens before the file leaves the machine.** Under the hood it
  is `go run ./cmd/cli -e prod validate-config <path>`, which reads the file and
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

## Operations

- **Logs**: `ssh root@<ip> 'cd /opt/dropin && docker compose logs -f app'`
- **Status**: `docker compose ps` in `/opt/dropin`
- **Restart**: `docker compose restart app` — note this does *not* pick up a
  changed config; use the config rollout above for that.
- The box holds no unregenerable state: rebuilding it is droplet + provision +
  scp + deploy, per the ADR.

### One-time: moving config into `config/`

Droplets provisioned before the mount changed hold the three files directly in
`/opt/dropin`. Move them down a level once, then deploy normally:

```sh
ssh root@<ip> 'mkdir -p /opt/dropin/config && mv /opt/dropin/*.prod.yaml /opt/dropin/*.prod.json /opt/dropin/config/'
```

`scripts/deploy-config.sh` refuses to recreate the container while the
credentials are missing from `config/`, so a half-done move cannot take the site
down.
