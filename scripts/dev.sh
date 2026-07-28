#!/bin/bash
#
# Local development stack: runs the Go web server and the frontend dev server
# together against the chosen environment's data.
#
# Usage: scripts/dev.sh {test|prod}
#
# The frontend dev server proxies API requests to the web server on
# localhost:8080 (override with API_PORT if the server config uses a
# different port) and listens on 5173 (override with WEB_PORT).
#

set -e

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV="${1:-}"

# In a git worktree, ports and the shared CP-SAT venv come from the file
# scripts/worktree-init.sh wrote (see docs/agents/worktrees.md). Absent in the
# primary checkout, where the defaults are right.
if [[ -f "$REPO_ROOT/.worktree.env" ]]; then
    set -a
    # shellcheck disable=SC1091
    source "$REPO_ROOT/.worktree.env"
    set +a
fi

if [[ "$ENV" != "test" && "$ENV" != "prod" ]]; then
    echo "Usage: $0 {test|prod}"
    exit 1
fi

if [[ "$ENV" == "test" ]]; then
    "$REPO_ROOT/scripts/test-db.sh" start
fi

echo "Building server..."
SERVER_BIN="$(mktemp -d)/server"
(cd "$REPO_ROOT" && go build -o "$SERVER_BIN" ./cmd/server)

cleanup() {
    # shellcheck disable=SC2046
    kill $(jobs -p) 2>/dev/null || true
    wait 2>/dev/null || true
}
trap cleanup EXIT

(cd "$REPO_ROOT" && exec "$SERVER_BIN" -env "$ENV") &

(cd "$REPO_ROOT/web" && exec bun dev)
