# Screenshots on a PR

A PR that changes what the front end looks like needs pictures in its comments,
because a reviewer cannot see a rendered page in a diff. This page is the whole
mechanism: where the images live, how they get there, and what must be left
behind afterwards — which is nothing.

The rule underneath all of it: **an image never enters the working tree of a
branch you are going to push, and never enters `main` at all.** No PNG has ever
been committed to `main` in this repo's history, and that is worth keeping. A
code review should be code.

## Where the images live

On `pr-screenshots`, an orphan branch of this repo — no common ancestor with
`main`, no code on it, nothing but images filed by PR number:

```
pr-194/before-main.png
pr-194/allocation-after.png
pr-196/pin-dialog-repeat.png
```

GitHub renders an image in a comment from a URL, and the drag-and-drop uploader
that a human uses is not reachable from a token. A public repo serves any file
on any branch over `raw.githubusercontent.com`, so committing the image
somewhere harmless and linking to it is the way an agent gets a picture into a
comment. `pr-screenshots` is that harmless somewhere.

One branch, not one per PR. `pr-screenshots-83` and `pr-screenshots-151` exist
as local strays from before this was written down; do not add to them.

## Capture into `logs/playwright/`

`.mcp.json` already points `playwright-mcp` at `--output-dir logs/playwright`,
and `**/logs/` is gitignored, so a screenshot taken with no path lands somewhere
git will never see. Keep it that way: pass `browser_take_screenshot` a bare
filename (`135-rota-times.png`), never a path that climbs out of the output
directory, and never an absolute path into the checkout.

Everything raster is gitignored as a backstop (see `.gitignore`), so a shot that
does escape is at least invisible to `git status` rather than riding along in
the next `git add`. That is a net, not a licence — an image in the repo root is
still litter, and litter in a worktree is litter in someone's `git clean`.

Name a file for what it shows, in lowercase kebab: `settings-times-not-set.png`,
`grid-mobile.png`, `before-tickbox.png`. Prefix with the issue number while you
are working if it helps you keep rounds apart; the PR directory is what
disambiguates in the end.

## Upload in one commit

Use a scratch worktree of the orphan branch, outside the repo, so the files are
staged somewhere that has no code in it and the whole set goes up as one commit:

```bash
n=194                                   # the PR number
shots=$(mktemp -d)
git fetch origin pr-screenshots
git worktree add "$shots" pr-screenshots
mkdir -p "$shots/pr-$n"
cp logs/playwright/before-main.png logs/playwright/allocation-after.png "$shots/pr-$n/"
git -C "$shots" add "pr-$n"
git -C "$shots" commit -m "Screenshots for PR #$n"
git -C "$shots" push origin pr-screenshots
git worktree remove "$shots"
```

`git worktree remove` is part of the procedure, not tidying — a worktree left
registered turns up in every later `git worktree list` and confuses the next
agent about which checkout it is in.

The alternative is `gh api` against the contents endpoint, one call per file.
It works and needs no worktree, but each call is its own commit: PR #194's
eleven screenshots went up as eleven commits saying the same thing. Prefer the
worktree; reach for the API only for a single afterthought image.

## Reference them by raw URL

```markdown
![Before](https://raw.githubusercontent.com/jakec-github/ilford-drop-in/pr-screenshots/pr-194/before-main.png)
```

Post them as a PR **comment**, not in the body, under a `## Screenshots`
heading. Caption each one with what the reviewer is meant to notice — the
picture is evidence for a claim, so make the claim in prose and let the image
support it. Pair a before with an after whenever the change is to something that
already existed.

Raw URLs are only public because this repo is. If it ever goes private the links
go dead and this whole mechanism needs rethinking.

## Leave the checkout clean

Before you hand over, from the checkout or worktree you worked in:

```bash
git status --short
```

Untracked images here mean the capture escaped `logs/playwright/`. Delete them —
they are either already on `pr-screenshots` or they were never wanted. Do not
add them to a feature branch to "keep" them, and do not leave them for the
maintainer to find on `main` weeks later.

Screenshots are also not your own evidence that something works: read the
accessibility tree for that (see [dev-stack.md](dev-stack.md)). A picture is for
the human reviewer.
