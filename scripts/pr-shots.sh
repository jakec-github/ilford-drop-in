#!/usr/bin/env bash
# Upload screenshots for a PR to the pr-screenshots orphan branch, in one
# commit under pr-<n>/, and print the markdown that embeds them. The images
# never touch this checkout's working tree. Mechanism and rules in
# docs/agents/pr-screenshots.md.
#
#   scripts/pr-shots.sh 283 logs/playwright/settings.png logs/playwright/grid.png
#   scripts/agent-gh.sh pr comment 283 --body "$(scripts/pr-shots.sh 283 ...)"
set -euo pipefail

if [[ $# -lt 2 ]]; then
    echo "Usage: $0 <pr-number> <image>..." >&2
    exit 1
fi
pr="$1"; shift
for f in "$@"; do
    [[ -f "$f" ]] || { echo "pr-shots: no such file: $f" >&2; exit 1; }
done

repo="$(gh repo view --json nameWithOwner -q .nameWithOwner 2>/dev/null \
    || git remote get-url origin | sed -E 's#.*github.com[:/](.*)\.git$#\1#; s#.*github.com[:/]##')"
scratch="$(mktemp -d)"
trap 'git worktree remove --force "$scratch/tree" >/dev/null 2>&1 || true; rm -rf "$scratch"' EXIT

git fetch -q origin pr-screenshots
git worktree add -q --detach "$scratch/tree" origin/pr-screenshots
mkdir -p "$scratch/tree/pr-$pr"
cp "$@" "$scratch/tree/pr-$pr/"
git -C "$scratch/tree" add "pr-$pr"
git -C "$scratch/tree" commit -q -m "Screenshots for #$pr"
git -C "$scratch/tree" push -q origin HEAD:pr-screenshots

for f in "$@"; do
    name="$(basename "$f")"
    echo "![${name%.*}](https://raw.githubusercontent.com/$repo/pr-screenshots/pr-$pr/$name)"
done
