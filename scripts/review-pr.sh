#!/bin/bash
#
# Boot a pull request's code on the test environment to look at it, then put
# the checkout back the way it was.
#
# Usage: scripts/review-pr.sh <pr-number>
#
# Run from the primary checkout, clean and on main. It checks out the PR's head
# detached — the PR's branch is usually checked out in an agent's worktree
# already, and git will not check out one branch in two places — then runs the
# PR's own `scripts/dev.sh test` in the foreground. Ctrl-C stops it and returns
# to main.
#
# The server runs against ilford_dropin_pr<n>, a copy of the seeded
# ilford_dropin_test dropped on exit: the PR's migrations must never reach the
# template every worktree's database is cloned from.
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

CONFIG_FILE="env/test/drop_in_config.yaml"

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
if [[ ! -f "$CONFIG_FILE" ]]; then
    echo "Missing ${CONFIG_FILE} — the test environment is not set up (docs/local-setup.md)." >&2
    exit 1
fi

echo "Fetching PR #${PR}..."
if ! git fetch --quiet origin "pull/${PR}/head"; then
    echo "Could not fetch PR #${PR} from origin." >&2
    exit 1
fi

# The config is untracked and holds real settings, so git cannot restore it:
# a copy is taken before it is touched, and put back on every way out.
BACKUP_DIR="$(mktemp -d -t review-pr-config)"
cp "$CONFIG_FILE" "$BACKUP_DIR/drop_in_config.yaml"

cleanup() {
    trap - EXIT INT TERM
    echo ""
    echo "Cleaning up..."
    cp "$BACKUP_DIR/drop_in_config.yaml" "$CONFIG_FILE" && rm -rf "$BACKUP_DIR"
    git checkout --quiet main
    scripts/test-db.sh drop "$DB_NAME" > /dev/null 2>&1 || true
    echo "Back on main; config restored; dropped ${DB_NAME}."
}
trap cleanup EXIT
# Ctrl-C reaches dev.sh too, which stops its own server and frontend; this
# script carries on into cleanup once it has.
trap 'exit 130' INT TERM

scripts/test-db.sh start > /dev/null
# A copy left by a run that died before cleanup would hold another commit's
# migrations, so it is replaced rather than reused.
scripts/test-db.sh drop "$DB_NAME" > /dev/null 2>&1 || true
scripts/test-db.sh clone "$DB_NAME" > /dev/null

sed -i '' -E "s#(^databaseURL:.*/)[A-Za-z0-9_]+(\?)#\1${DB_NAME}\2#" "$CONFIG_FILE"
if ! grep -q "/${DB_NAME}?" "$CONFIG_FILE"; then
    echo "Could not point ${CONFIG_FILE} at ${DB_NAME}; refusing to touch the template." >&2
    exit 1
fi

git checkout --quiet --detach FETCH_HEAD
echo "Checked out PR #${PR} at $(git rev-parse --short HEAD) (detached), against ${DB_NAME}"
echo "  app: http://localhost:${WEB_PORT:-5173}/   Ctrl-C to stop and return to main."
echo ""

scripts/dev.sh test
