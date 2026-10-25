<p align="center">
  <img src="docs/images/logo.svg" alt="" width="96" height="96" />
</p>

<h1 align="center">Rota Wise</h1>

Volunteer rotas for drop-ins and other services run by volunteers. Rota Wise
collects volunteer availability, allocates volunteers to shifts, and publishes
the resulting rota. One server runs several drop-ins, each under its own
address; the first was a weekly charity drop-in in Ilford, London, which
provides food and support to vulnerable community members.

It is a **Go web server + React frontend** (`cmd/server`, `web/`) that serves
the public rota page and every Organiser tool — defining a rota, asking volunteers
for their availability, allocating it and changing it afterwards. A small
**Go CLI** (`cmd/cli`) is left over from before the app; see
[CLI commands](#cli-commands).

The volunteer roster lives in Google Sheets; scheduling data lives in Postgres;
allocation is solved by a Python CP-SAT service (`pyallocator/`). The server
republishes the rota to a Google Sheet after every change.

## Quick start

```bash
scripts/test-db.sh start      # local Postgres in Docker
scripts/dev.sh test           # run server + frontend → http://localhost:5173
```

This needs a Google Cloud project and a few config files first — the full,
step-by-step walkthrough is in **[`docs/local-setup.md`](docs/local-setup.md)**.

To just look at the app, skip all of that:

```bash
scripts/dev-stack.sh start    # no credentials → http://localhost:8080/org/dev-org/
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

The CLI is on its way out ([#220](https://github.com/jakec-github/ilford-drop-in/issues/220)).
The whole life of a rota is in the app now — defining it, preparing its shifts,
asking volunteers, allocating and changing it afterwards all happen on Organiser →
Allocation and the rota page. Allocating in particular could never be a command:
it re-solves and commits only the rota the Organiser was shown, and a command that
solved and committed in one step could not honour that (ADR 0008).

The roster is on Organiser → Volunteers, and the rota sheet republishes itself
after every change. To republish by hand, press **Sync** on that tab: it re-reads
the roster and publishes again.

What is left takes `-e`/`--env` to pick the environment, whose files are read
from `env/<env>/` (`env/test/drop_in_config.yaml` and so on):

```bash
go run ./cmd/cli -e test <command> [args]
go run ./cmd/cli --help
```

| Command | Description |
| --- | --- |
| `validate-config <path>` | Check a config file and print what it configures, without connecting to anything. `scripts/deploy-config.sh` runs it before shipping a config. |
| `viewHistoricalResponses <count>` | How each volunteer answered the availability requests for the last `count` allocated rotas. |

`viewHistoricalResponses` signs in to Google on every run; see
[`docs/local-setup.md`](docs/local-setup.md) for what that needs.

## Requirements

Go ≥ 1.25, Docker, and [Bun](https://bun.sh/) for the frontend. See
[`docs/local-setup.md`](docs/local-setup.md) for the rest.

## Licence

See [`LICENSE`](LICENSE).
