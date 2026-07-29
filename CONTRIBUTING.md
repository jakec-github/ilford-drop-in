# Contributing

Thanks for helping out with the Ilford Drop-in rota tool. This guide covers how
to get set up and how changes make it into the project.

## Getting set up

Follow [`docs/local-setup.md`](docs/local-setup.md) end to end. It takes you
from a fresh clone to a running rota page, including the Google Cloud project
the app currently needs.

If you only want to work on the **frontend or the tests**, you can get a long
way without any Google setup: the test suite needs no credentials, and the
frontend can run against a locally-populated database.

## Project layout

| Path | What lives here |
| --- | --- |
| `cmd/` | CLI (`cmd/cli`) and web server (`cmd/server`) entry points. |
| `pkg/core/` | Domain logic: services, models, the allocator contract. |
| `pkg/db/` | Postgres access and SQL migrations. |
| `pkg/clients/` | Google Sheets/Forms/Gmail clients. |
| `pkg/api/` | HTTP handlers, auth, volunteer cache. |
| `internal/config/` | Config and credential loading. |
| `web/` | React + TypeScript frontend (see `web/README.md`). |
| `pyallocator/` | Python CP-SAT allocator (see `pyallocator/README.md`). |
| `docs/` | Setup, deployment, architecture decision records (`docs/adr/`). |

Domain vocabulary is defined in [`CONTEXT.md`](CONTEXT.md) — please use those
terms (Shift, Rotation, Allocation, Alteration, …) in code and PRs.

## Coding standards

- **British English** in code, comments, and docs.
- Frontend structure and state conventions are documented in
  [`CLAUDE.md`](CLAUDE.md) (the "Frontend structure" section) — most notably the
  `ui/` (domain-blind) vs `components/` (domain-aware) split and the "rule of
  two" for extracting shared components.
- Match the style of the surrounding code: structured logging with zap,
  table-driven tests with testify, interfaces at I/O boundaries for testability.

## Tests

```bash
scripts/check.sh                 # everything, one exit code
```

That is Go build, vet and tests, then the frontend typecheck (`tsc`, via the
build) and lint. It takes seconds, and the same script runs in CI on every pull
request. The individual commands, if you want to run one on its own:

```bash
go test ./...                    # Go
scripts/test-db.sh start         # enable Postgres integration tests
cd web && bun run build          # frontend: typecheck (tsc) and build
cd web && bun run lint           # frontend lint
```

Please add tests for new behaviour. Integration tests that touch the database
use the throwaway-DB harness in `pkg/db/dbtest`. They **skip silently** when the
test Postgres isn't running, so a bare `go test ./...` on a checkout with no
database passes without having exercised the schema. `check.sh` closes that hole:
it starts the database first, and sets `DBTEST_REQUIRED` so an unreachable server
fails the run instead of skipping it.

The frontend has no test suite yet; `bun run build` typechecks it, which is the
closest equivalent for now.

## Pull requests

- Branch off an up-to-date `main`; don't commit to `main` directly.
- Keep PRs focused and reference any related issue.
- Make sure `scripts/check.sh` passes before opening the PR; CI runs it too.
- A maintainer reviews and merges — please don't merge your own PR.

Issues and feature requests are tracked in this repository's GitHub issues.

> **Maintainers:** the agent/automation workflow and reviewer conventions live
> in [`CLAUDE.md`](CLAUDE.md) under "Maintainer & agent operations", and
> deployment in [`docs/deployment.md`](docs/deployment.md).
