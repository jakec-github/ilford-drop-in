# Deployment runbook

Architecture and rationale: `docs/adr/0002-deployment-architecture.md`. In
short: one droplet running Docker Compose (Caddy + app), Postgres on Neon,
images built by CI on every merge to main and deployed over SSH.

## Continuous deployment (no action needed)

Every merge to main builds `ghcr.io/jakec-github/ilford-drop-in`, tagged with
the git SHA and `latest`, and — if the `DEPLOY_ENABLED` repository variable is
`"true"` — deploys it to the droplet. **Rollback**: re-run the workflow on a
previous commit (Actions → the old run → "Re-run all jobs").

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
and `/opt/dropin`.

### 3. Config files

scp the three files the server needs to `/opt/dropin/`:

```sh
scp drop_in_config.prod.yaml oauthClientWeb.prod.json serviceAccount.prod.json root@<ip>:/opt/dropin/
```

These are the one thing the repo cannot regenerate — keep copies with other
personal secrets. Check that `oauthClientWeb.prod.json` lists the production
redirect URI (`https://<domain>/auth/callback`) and that it is also registered
on the Google web client.

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

## Operations

- **Logs**: `ssh root@<ip> 'cd /opt/dropin && docker compose logs -f app'`
- **Status**: `docker compose ps` in `/opt/dropin`
- **Restart**: `docker compose restart app`
- The box holds no unregenerable state: rebuilding it is droplet + provision +
  scp + deploy, per the ADR.
