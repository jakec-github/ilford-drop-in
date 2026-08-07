# Ilford Drop-in

Scheduling tools for a weekly charity drop-in in Ilford, London, which provides
food and support to vulnerable community members. The system collects volunteer
availability, allocates volunteers to shifts, and publishes the resulting rota.

It has two parts:

- A **Go CLI** (`cmd/cli`) for the admin workflow — defining rotas, requesting
  availability, allocating, and publishing.
- A **Go web server + React frontend** (`cmd/server`, `web/`) that serves the
  public rota page and admin tools.

The volunteer roster lives in Google Sheets; scheduling data lives in Postgres;
allocation is solved by a Python CP-SAT service (`pyallocator/`).

## Quick start

```bash
scripts/test-db.sh start     # local Postgres in Docker
go build -o cli ./cmd/cli
./cli -e test defineRota 12   # create a rota with 12 weekly shifts
scripts/dev.sh test           # run server + frontend → http://localhost:5173
```

This needs a Google Cloud project and a few config files first — the full,
step-by-step walkthrough is in **[`docs/local-setup.md`](docs/local-setup.md)**.

To just look at the app, skip all of that:

```bash
scripts/dev-stack.sh start    # no credentials → http://localhost:8080
```

The `dev` environment reads its roster from a CSV and logs you in without
Google. The database starts empty, so the rota page does too — see
[`docs/agents/dev-stack.md`](docs/agents/dev-stack.md).

> Production and the availability journey aren't expected to work for outside
> contributors yet; local setup covers running the app and viewing the rota.

## Documentation

| Doc | What it covers |
| --- | --- |
| [`docs/local-setup.md`](docs/local-setup.md) | Full local setup: Google Cloud, config, database, running the app. |
| [`docs/agents/dev-stack.md`](docs/agents/dev-stack.md) | Running the app with no credentials, and driving it headlessly. |
| [`CONTRIBUTING.md`](CONTRIBUTING.md) | How to contribute: project layout, standards, tests, PRs. |
| [`CONTEXT.md`](CONTEXT.md) | Domain glossary (Shift, Rotation, Allocation, …). |
| [`docs/adr/`](docs/adr/) | Architecture decision records. |
| [`docs/deployment.md`](docs/deployment.md) | Deployment runbook (maintainers). |
| [`web/README.md`](web/README.md) | Frontend details. |
| [`pyallocator/README.md`](pyallocator/README.md) | The allocation service. |

## CLI commands

All commands take `-e`/`--env` to pick the config environment
(`drop_in_config.<env>.yaml`):

```bash
./cli -e test <command> [args]
./cli --help
```

| Command | Description |
| --- | --- |
| `listVolunteers` | List volunteers from the volunteer sheet. |
| `defineRota <n>` | Create a rotation with `n` weekly shifts (DB only). |
| `publishRota` | Publish the latest rota to the rota sheet. |
| `changeRota ...` | Apply post-allocation changes (covers/alterations). |
| `viewHistoricalResponses ...` | Inspect past availability responses. |

Allocating is not among them. It happens in the app, where it re-solves and
commits only the rota the admin was shown; a command that solved and committed
in one step could not honour that (ADR 0008). Availability is likewise collected
and chased through the web app: mint a round and send the links from the
availability tab. The commands that remain read the volunteer sheet; see
[`docs/local-setup.md`](docs/local-setup.md) for what they need.

## Requirements

Go ≥ 1.25, Docker, and [Bun](https://bun.sh/) for the frontend. See
[`docs/local-setup.md`](docs/local-setup.md) for the rest.

## Licence

See [`LICENSE`](LICENSE).
