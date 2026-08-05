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
- The hostname lives in `deploy/Caddyfile`; change it there if the domain
  changes, and update the Google web client's redirect URI to match.

### 2. Provision the box

```sh
scp scripts/provision.sh root@<ip>:
ssh root@<ip> ./provision.sh
```

Idempotent: installs Docker, enables ufw (22/80/443), creates a 2G swap file
and `/opt/dropin/config`.

### 3. Config files

The server reads three files, and they all live in **`/opt/dropin/config/`** —
the directory `deploy/compose.yaml` mounts into the container as `/app`. They
are the one thing the repo cannot regenerate; keep copies with other personal
secrets.

The two credentials change about once a year, so they go up by hand:

```sh
scp oauthClientWeb.prod.json serviceAccount.prod.json root@<ip>:/opt/dropin/config/
```

Check that `oauthClientWeb.prod.json` lists the production redirect URI
(`https://<domain>/auth/callback`) and that it is also registered on the Google
web client.

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
stack; Caddy obtains its certificate on first boot.

## Config rollout

Editing `drop_in_config.prod.yaml` and reaching the running server is one
command, from the repo root:

```sh
scripts/deploy-config.sh <ip>          # or set DEPLOY_HOST, or put it in .deploy.env
```

It validates the local file, shows what it configures, asks, copies it to
`/opt/dropin/config/`, recreates the app container, and waits for
`https://<domain>/health` to answer — exiting non-zero with the app's logs if it
does not. There is no `docker compose` step to run afterwards.

Three things it is doing on purpose:

- **Validation happens before the file leaves the machine.** Under the hood it
  is `go run ./cmd/cli -e prod validate-config <path>`, which reads the file and
  nothing else: no database, no Google. Run it on its own any time.
- **The summary is the point.** "Valid" is not the same as "right" — a config
  can parse and validate with every preallocation missing. The preallocation
  count is what to check against the change you meant to make.
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

They are created on **Admin → Settings**, which is reachable as soon as an admin
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
