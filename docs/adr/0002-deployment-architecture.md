# Deployment: one VPS, one CI-built image, managed Postgres

Status: accepted

Production is a single DigitalOcean droplet (London) running Docker Compose
with two services: Caddy, acting purely as a TLS-terminating reverse proxy,
and one app container in which the Go server serves both the API and the
embedded frontend build (`web/dist/`). Postgres is not on the box: it lives
on Neon (managed serverless, London). GitHub Actions builds the image on
every merge to main, tags it with the git SHA, pushes it to GHCR, and
deploys over SSH with `docker compose pull && up -d` — so main *is* prod,
and the droplet never builds anything or holds a git clone.

## Decisions and their reasons

- **VPS + Compose over a PaaS or managed cloud.** Fixed ~£5/month, no
  vendor or free-tier churn for a system expected to run for years. The
  "principled" properties a PaaS would provide are recovered by building
  images in CI — the box only ever pulls and runs.
- **One image, frontend embedded in the Go binary.** Nothing in the repo
  served `web/dist/` in production before this decision (the API server
  was API-only; only the dev proxy joined them). Embedding via `embed.FS`
  with an SPA fallback means frontend and API deploy atomically and can
  never version-skew, and the Caddy config stays a dumb proxy with no
  route list to maintain. Rejected: a second Caddy image with `dist/`
  baked in (two artifacts to version together).
- **Managed Postgres instead of a container on the box.** Availability
  responses, allocations and alterations are the only unregenerable data
  in the system; outsourcing their durability makes the droplet fully
  disposable and removes backup operations entirely. Accepted trade: a
  second vendor and free-tier risk. Side benefit: the CLI rota workflow
  keeps running from a laptop, connecting straight to the prod database —
  nothing operational runs on the box.
- **Auto-deploy on merge, but gated until go-live.** The deploy step sits
  behind a repository variable, off until the OIDC admin-sync work lands,
  because until then the server cannot boot headless (it acquires a Sheets
  token interactively at startup) and `POST /alterations` is
  unauthenticated. Do not flip the gate before that work merges.
- **Secrets are two plain files scp'd to the box** (`drop_in_config.prod.yaml`,
  `oauthClient.prod.json`), mounted read-only into the container. Rejected
  for now: SOPS-encrypted config in the repo, and secrets held in GitHub
  Actions — both add machinery for two small files that change a few times
  a year. Consequence: these files are the one thing the repo cannot
  regenerate; keep copies with other personal secrets.
- **Prod logs go to stdout only**, rotated by Docker's json-file driver.
  The existing per-boot log file under `./logs` is dev-only behaviour; in
  a container it is ephemeral, unrotated state with a second place to look.
- **Provisioning is an idempotent script in the repo, not IaC.** One
  permanent droplet does not justify Terraform state; rebuilding the box
  is: create droplet, run the script, scp the two config files.

## Consequences

- Go-live prerequisites: OIDC admin-sync merged, a domain registered and
  pointed at the droplet (the same hostname goes into the Caddyfile and
  the Google web-client redirect URI), config files on the box, deploy
  gate flipped.
- Rollback is rerunning the deploy workflow on a previous SHA.
- Everything assumes exactly one app instance: the in-memory volunteer
  cache and run-migrations-at-startup are both only safe with a single
  replica. Scaling out would reopen both.
