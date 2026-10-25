#!/bin/bash
#
# Boot a pull request's code on the dev stack to look at it, then put the
# checkout back the way it was.
#
# Usage: scripts/review-pr.sh <pr-number>
#
# Run from the primary checkout, clean and on main. It checks out the PR's head
# detached — the PR's branch is usually checked out in an agent's worktree
# already, and git will not check out one branch in two places — then starts the
# PR's own scripts/dev-stack.sh and follows its log. Ctrl-C stops the stack and
# returns to main.
#
# The stack runs against a throwaway database, ilford_dropin_pr<n>, dropped on
# exit: a migration main has not got would otherwise leave the shared
# ilford_dropin_dev unbootable from main.
#

set -euo pipefail

# Checking out the PR rewrites this very file, and bash reads a script as it
# runs rather than all at once. So the work is done by a copy outside the
# checkout, which nothing will touch.
if [[ -z "${REVIEW_PR_ROOT:-}" ]]; then
    REVIEW_PR_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
    copy="$(mktemp -t review-pr)"
    cp "$0" "$copy"
    REVIEW_PR_ROOT="$REVIEW_PR_ROOT" exec bash "$copy" "$@"
fi
REPO_ROOT="$REVIEW_PR_ROOT"
rm -f "$0"

CONFIG_FILE="env/dev/drop_in_config.yaml"

PR="${1:-}"
if [[ ! "$PR" =~ ^[0-9]+$ ]]; then
    echo "Usage: scripts/review-pr.sh <pr-number>" >&2
    exit 1
fi
DB_NAME="ilford_dropin_pr${PR}"

cd "$REPO_ROOT"

# Both refusals protect the way back: uncommitted work would be carried onto
# the PR (or block the checkout), and anywhere but main is not where exit goes.
if [[ "$(git symbolic-ref --quiet --short HEAD || true)" != "main" ]]; then
    echo "Not on main. Switch to main first — exit returns the checkout there." >&2
    exit 1
fi
if [[ -n "$(git status --porcelain)" ]]; then
    echo "The checkout has uncommitted changes. Commit or stash them first." >&2
    git status --short >&2
    exit 1
fi

echo "Fetching PR #${PR}..."
if ! git fetch --quiet origin "pull/${PR}/head"; then
    echo "Could not fetch PR #${PR} from origin." >&2
    exit 1
fi

LOG_PID=""

cleanup() {
    trap - EXIT INT TERM
    if [[ -n "$LOG_PID" ]]; then { kill "$LOG_PID"; wait "$LOG_PID"; } 2>/dev/null || true; fi
    echo ""
    echo "Cleaning up..."
    scripts/dev-stack.sh stop || true
    git checkout --quiet -- "$CONFIG_FILE" 2>/dev/null || true
    git checkout --quiet main
    scripts/test-db.sh drop "$DB_NAME" > /dev/null 2>&1 || true
    echo "Back on main; dropped ${DB_NAME}."
}
trap cleanup EXIT
trap 'exit 130' INT TERM

git checkout --quiet --detach FETCH_HEAD
echo "Checked out PR #${PR} at $(git rev-parse --short HEAD) (detached)"

# The PR's config may differ from main's, so the database is swapped in the
# file it brought rather than a copy of main's.
if [[ ! -f "$CONFIG_FILE" ]]; then
    echo "PR #${PR} has no ${CONFIG_FILE} — it predates the dev stack. Rebase it on main." >&2
    exit 1
fi
sed -i '' -E "s#(^databaseURL:.*/)[A-Za-z0-9_]+(\?)#\1${DB_NAME}\2#" "$CONFIG_FILE"
if ! grep -q "/${DB_NAME}?" "$CONFIG_FILE"; then
    echo "Could not point ${CONFIG_FILE} at ${DB_NAME}; refusing to touch the shared database." >&2
    exit 1
fi

scripts/dev-stack.sh start
echo ""
echo "Reviewing PR #${PR} against ${DB_NAME}. Ctrl-C to stop and return to main."
echo ""
# In the background and waited on, not in the foreground: bash runs a trap only
# once the foreground child exits, so a TERM would otherwise wait on tail.
tail -f logs/dev-stack.log &
LOG_PID=$!
wait "$LOG_PID"
