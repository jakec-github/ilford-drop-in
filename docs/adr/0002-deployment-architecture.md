# Deployment: one VPS, one CI-built image, managed Postgres

Status: accepted

Production is a single DigitalOcean droplet (London) running Docker Compose
with two services: Caddy, acting purely as a TLS-terminating reverse proxy,
and one app container in which the Go server serves both the API and the
embedded frontend build (`web/dist/`). Postgres is not on the box: it lives
on Neon (managed serverless, London). GitHub Actions builds the image on
every merge to main, tags it with the git SHA, pushes it to GHCR, and
deploys over SSH, and the droplet never builds anything or holds a git
clone.

Amended in issue #244: main is no longer prod. Every merge deploys the image
to **preprod** first: a second app container on the same droplet, served by
the same Caddy at `preprod.<domain>`, with its own Neon database, its own
synthetic roster and rota sheets, and its own config in `env/preprod/`. Prod
still deploys on every merge for now; issue #245 turns it into a manual
promote of an image preprod has already run.

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
- **A preprod beside prod, on the same box** (issue #244). Merging an agent's
  PR should not be the same as releasing it, and nothing resembling prod
  existed to check a change on first. A second droplet would double the bill
  for a box that sits idle; a second container costs memory, so preprod can be
  switched off and on (`scripts/preprod.sh`) without touching prod. It must be
  a subdomain rather than a path, because `/api`, `/auth` and `/health` sit at
  the root of a host. Its data is synthetic — never a copy or branch of prod's —
  and its roster's emails are addresses the owner controls, so a real email
  from preprod reaches nobody else.
- **Auto-deploy on merge, but gated until go-live.** The deploy step sits
  behind a repository variable, originally off because the server could not
  boot headless (it acquired a Sheets token interactively at startup) and
  `POST /alterations` was unauthenticated. The OIDC admin-sync work has
  since landed (2026-07-18): the server fetches the roster with its own
  service account (non-fatal on failure) and alterations are admin-gated,
  so the gate now waits only on the remaining go-live prerequisites below.
- **Secrets are three plain files scp'd to the box** (`drop_in_config.prod.yaml`,
  `oauthClientWeb.prod.json`, `serviceAccount.prod.json`), mounted read-only
  into the container. The installed-app OAuth client
  (`oauthClient.prod.json`) is used only by the CLI from a laptop and never
  goes on the box. Rejected for now: SOPS-encrypted config in the repo, and
  secrets held in GitHub Actions — both add machinery for three small files
  that change a few times a year. Consequence: these files are the one
  thing the repo cannot regenerate; keep copies with other personal
  secrets.
- **Prod logs go to stdout only**, rotated by Docker's json-file driver.
  The existing per-boot log file under `./logs` is dev-only behaviour; in
  a container it is ephemeral, unrotated state with a second place to look.
- **Provisioning is an idempotent script in the repo, not IaC.** One
  permanent droplet does not justify Terraform state; rebuilding the box
  is: create droplet, run the script, scp the three config files.

## Consequences

- Go-live prerequisites: the pipeline itself built (Dockerfile, Compose,
  Caddyfile, workflow, provisioning script), a domain registered and
  pointed at the droplet (the same hostname goes into the Caddyfile and
  the Google web-client redirect URI), the three config files on the box,
  deploy gate flipped. (OIDC admin-sync, formerly a prerequisite, merged
  2026-07-18.)
- Rollback is rerunning the deploy workflow on a previous SHA (or only its
  prod leg).
- Two app containers share a 512 MiB box. Preprod is the one that gives way:
  `scripts/preprod.sh down` stops it, and deploys skip it until `up`.
- Everything assumes exactly one app instance per environment: the in-memory volunteer
  cache and run-migrations-at-startup are both only safe with a single
  replica. Scaling out would reopen both.
