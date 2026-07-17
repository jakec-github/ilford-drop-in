## Style

- Use British English

## Agent skills

- Issue tracker: GitHub issues on this repo — conventions in `docs/agents/issue-tracker.md`
- Triage label vocabulary: `docs/agents/triage-labels.md`
- Agent tracker actions run as the `jakec-agent` machine account via `GH_TOKEN`
  from `.claude/settings.local.json` (untracked; never commit it)
- Domain glossary: `CONTEXT.md`; decision records: `docs/adr/`

## PR workflow

- Never commit directly to main. Start each ticket on a branch named
  `issue-<n>-<slug>`, cut from up-to-date main. Ensure that main is up-to-date
  with origin/main.
- When the ticket's acceptance criteria pass, push the branch and open a PR
  with `gh pr create` — titled after the ticket, with `Closes #<n>` in the body.
- Request review from `jakec-github`. Never merge a PR; merging is the
  reviewer's decision.
- After review is requested switch back to main
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
