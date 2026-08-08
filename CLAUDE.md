Guidance for AI agents and maintainers working in this repo. Outside
contributors: see [`CONTRIBUTING.md`](CONTRIBUTING.md) and
[`docs/local-setup.md`](docs/local-setup.md) — the "Maintainer & agent
operations" section below is specific to this repo's owner and automation and
does not apply to you.

## Style

- Use British English

## Frontend structure

- `web/src/ui/` — domain-blind primitives; must not import `types.ts` or
  reference domain concepts (volunteers, shifts, rotas). Each colocates its
  CSS (`Button.tsx` + `Button.css`).
- `web/src/components/` — domain-aware components and views.
- Rule of two: extract into `ui/` only when a second consumer exists, in that
  consumer's PR — never speculatively.
- Routing: hand-rolled pathname switch until routes outgrow it (third route or
  first parameterised route); then wouter, not react-router.
- State: view-local by default; context only for app-global concerns (auth).
  Server data behind per-resource hooks — views never call `fetch` directly.
  No state library; if server caching ever earns a dependency, TanStack Query.
- Design: volunteer-facing pages (the rota, the availability form) are mobile
  first — a volunteer reads them on a phone. Admin tools are **desktop first**:
  they must stay usable on a phone, but where the two conflict, design for the
  desk. An admin tool comparing several things at once (the responses grid) is
  allowed to be wider than a phone and scroll sideways; do not fold it into one
  column to avoid that. `admin-page--wide` on a tab widens the shell for one.
  A grid whose width is data — the responses grid is as wide as the rota is
  long — needs more than a wider fixed shell: `.round-bleed` lets that one
  panel out of the column as far as the viewport allows, so it only scrolls
  sideways when the screen really is too narrow.

# Maintainer & agent operations

> Repo-owner and automation specifics. Not needed to develop against or
> contribute to this project — outside contributors can ignore this whole
> section.

## Agent skills

- Issue tracker: GitHub issues on this repo — conventions in `docs/agents/issue-tracker.md`
- Triage label vocabulary: `docs/agents/triage-labels.md`
- Agent tracker actions run as the `jakec-agent` machine account via `GH_TOKEN`
  from `.claude/settings.local.json` (untracked; never commit it)
- Domain glossary: `CONTEXT.md`; decision records: `docs/adr/`

## Running the app

- To see the app rather than just its tests: `scripts/dev-stack.sh start`. It
  boots the whole stack on <http://localhost:8080> with no Google credentials,
  waits for `/health`, and returns — the server stays up detached. `.mcp.json`
  configures `playwright-mcp` to drive it. Full workflow, and what is and is not
  reachable against an empty database, in `docs/agents/dev-stack.md`.
- Work from accessibility-tree snapshots. Screenshots are evidence for a human
  reviewer, never your own verdict that something works.

## Worktrees

- Working in a git worktree rather than the primary checkout: run
  `scripts/worktree-init.sh` once inside it — credentials, config, ports,
  `bun install` and the worktree's own database. Full workflow in
  `docs/agents/worktrees.md`.

## PR workflow

- Never commit directly to main. Start each ticket on a branch named
  `issue-<n>-<slug>`, cut from up-to-date main. Ensure that main is up-to-date
  with origin/main.
- Unless asked otherwise complete work in a worktree.
- When the ticket's acceptance criteria pass, run `scripts/check.sh` — build,
  vet, tests (with the database up, so they cannot silently skip), the
  pyallocator suite, frontend typecheck and lint, in one exit code. Then push the branch and open a PR with
  `gh pr create` — titled after the ticket, with `Closes #<n>` in the body.
- PRs that include visual changes to the front end should have screenshots
  in the PR comments.
- Request review from `jakec-github`. Never merge a PR; merging is the
  reviewer's decision.
- The agent token cannot request reviewers via `gh pr edit --add-reviewer`
  (GraphQL needs `read:org`). Use the REST endpoint instead:
  `gh api repos/{owner}/{repo}/pulls/<n>/requested_reviewers -f 'reviewers[]=jakec-github'`
- Then block on CI: `gh pr checks <n> --watch --fail-fast`. It is a backstop,
  not the inner loop — one deliberate wait at the end, catching what differs
  from your machine (uncommitted files, locally-skipped tests, accumulated
  state). Fix anything it catches before handing over.
- Once the checks are green switch back to `main` (unless on a worktree)
- To address review feedback: read the PR conversation (`gh pr view <n>
--comments`), the inline review threads (`gh api
repos/{owner}/{repo}/pulls/<n>/comments`), and any failing checks
  (`gh pr checks <n>`). Push fixes to the same branch, reply to each comment
  as the fix lands (or push back with reasoning), and re-request review when
  done. Never resolve a thread without responding.

## Simple PR workflow

- Only use this flow if requested by the user
- Do not create a new branch
- Do not commit changes
- Do not push a PR
