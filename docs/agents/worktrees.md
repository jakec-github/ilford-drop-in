# Working in a git worktree

Agents work one ticket at a time in an isolated checkout, so several can run at
once without treading on each other. A git worktree gives each one its own
files; this page covers the parts git does not copy — credentials, config, node
modules, ports and the database.

Worktrees live on the maintainer's machine alongside the primary checkout.
Docker, Postgres, the Go module cache, the OAuth token cache and the CP-SAT
venv are all **shared**; everything below is what is **not**.

## Creating one

```bash
git worktree add ../ilford-<slug> -b issue-<n>-<slug> main
cd ../ilford-<slug>
scripts/worktree-init.sh
```

`scripts/worktree-init.sh` is idempotent — re-run it any time. It:

| Step | What it does |
| --- | --- |
| Credentials | Symlinks `serviceAccount.test.json`, `oauthClientWeb.test.json`, `oauthClient.test.json` and `.claude/settings.local.json` from the primary checkout. Without the last one the agent has no `GH_TOKEN` and cannot reach the issue tracker. |
| Config | Copies `drop_in_config.test.yaml` and rewrites `databaseURL`, `server.port` and `server.redirectURI` for this worktree. |
| Ports | Claims the lowest unused offset (1–5) and records it in `.worktree.env`. |
| Dependencies | Runs `bun install` in `web/`. |
| Database | Clones the seeded template into this worktree's own database. |

## Ports

Offset `N` gives the Go server `8080+N` and the frontend dev server `5173+N`.
The primary checkout keeps 8080/5173. `scripts/dev.sh` picks these up
automatically by sourcing `.worktree.env`; for anything else, source it
yourself:

```bash
set -a; source .worktree.env; set +a
```

**Admin login needs a one-time human step.** The OIDC callback goes to the
frontend port, and Google only accepts redirect URIs registered with the web
OAuth client. Register `http://localhost:5174/auth/callback` through
`http://localhost:5178/auth/callback` once (Google Cloud console → Credentials →
the web OAuth client → Authorised redirect URIs) and every worktree is covered.

**Pin the primary checkout's URI at the same time.** A config with no
`server.redirectURI` takes the first localhost URI the OAuth client has
registered — fine when 5173 is the only one, order-dependent once the worktree
URIs are there too. Add this to the primary checkout's
`drop_in_config.test.yaml` under `server:` when you register them, so its login
cannot drift onto a worktree's port:

```yaml
  redirectURI: "http://localhost:5173/auth/callback"
```

Until that is done, `worktree-init.sh` leaves `server.redirectURI` out of the
copied config and says so: the server still runs and everything but admin login
works. Once the URIs are registered, delete `drop_in_config.test.yaml` in the
worktree and re-run the script to pick the right one up. Where the key *is* set,
the server refuses to start if it names a URI the OAuth client does not have
registered, listing the ones it does — that error means the registration is
missing, not that the config is malformed.

## The database

`ilford_dropin_test` is a **golden template**: seeded through the Google-bound
availability journey, not reproducible without a human, and therefore not
something to develop against. Each worktree clones it
(`CREATE DATABASE … TEMPLATE …` — instant, no dump file) into
`ilford_wt_<slug>` and mutates its own copy freely.

```bash
scripts/worktree-init.sh --reset-db    # drop this worktree's DB and re-clone the template
scripts/test-db.sh list                # what exists on the server
scripts/test-db.sh psql ilford_wt_foo  # a psql shell on it
```

Postgres refuses to clone a template that has open connections, so nothing may
be connected to `ilford_dropin_test` while a worktree is being created or reset.
The script says so plainly, and tells you how to see what is connected. In
practice the culprit is a server still running against the primary checkout's
config, or a `go test` run in a checkout with no `.worktree.env`.

Integration tests are already safe to run concurrently: `dbtest.New` mints a
uniquely-named throwaway database per test and drops it afterwards. The
connection it does that over comes from `TEST_DATABASE_URL`, which
`.worktree.env` points at the worktree's own database so test runs never hold
the template open.

The template is worth backing up, since nothing can rebuild it:

```bash
docker exec ilford-pg-test pg_dump -U postgres ilford_dropin_test > ~/ilford-template-$(date +%F).sql
```

## Finishing up

```bash
cd ../ilford-drop-in                        # back to the primary checkout
git worktree remove ../ilford-<slug>
scripts/test-db.sh drop ilford_wt_<slug>    # frees the name and the port offset
```

Removing the worktree releases its port offset for the next one — the offsets
are read from the live worktree list, not a registry.

## Notes

- `scripts/worktree-init.sh` and `scripts/test-db.sh` assume macOS with
  OrbStack, matching the machine these worktrees run on.
- `go mod download` needs nothing: the module cache is shared.
- The CP-SAT venv is shared too — `.worktree.env` sets `ILFORD_CPSAT_PYTHON` to
  the primary checkout's interpreter, because the default path is resolved
  relative to the working directory and would otherwise fail confusingly.
