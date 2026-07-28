#!/bin/bash
#
# Prepare a git worktree for development.
#
# A fresh worktree has none of the untracked-but-required files (credentials,
# config), no node_modules, no database of its own, and would collide with the
# primary checkout on the server and dev-server ports. This script fixes all of
# that in one go, so an agent can start work in a new worktree without a human.
#
# Usage:
#   scripts/worktree-init.sh              # set the worktree up
#   scripts/worktree-init.sh --reset-db   # drop and re-clone this worktree's database
#
# Run it from inside the worktree. Safe to re-run: existing links, the config
# and the database are left alone unless --reset-db is passed.
#
# See docs/agents/worktrees.md for the workflow this belongs to.
#

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# The primary checkout is the directory holding the real .git directory; a
# worktree's .git is a file pointing into it.
PRIMARY_ROOT="$(cd "$(git -C "$REPO_ROOT" rev-parse --path-format=absolute --git-common-dir)/.." && pwd)"

ENV_FILE="$REPO_ROOT/.worktree.env"
CONFIG_FILE="drop_in_config.test.yaml"

# Symlinked from the primary checkout: shared, secret, and identical everywhere.
LINKED_FILES=(
    "serviceAccount.test.json"
    "oauthClientWeb.test.json"
    "oauthClient.test.json"
    ".claude/settings.local.json"
)

# Ports are allocated in pairs: offset N gives the Go server 8080+N and the
# frontend dev server 5173+N. Offset 0 is the primary checkout's.
BASE_API_PORT=8080
BASE_WEB_PORT=5173
MAX_OFFSET=5

if [[ "$REPO_ROOT" == "$PRIMARY_ROOT" ]]; then
    echo "This is the primary checkout, not a worktree — nothing to set up." >&2
    echo "Create a worktree first:  git worktree add ../ilford-<name> -b <branch>" >&2
    exit 1
fi

WORKTREE_NAME="$(basename "$REPO_ROOT")"
# Database and env-var names come from the directory name, so fold it to a
# plain identifier.
WORKTREE_SLUG="$(echo "$WORKTREE_NAME" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9' '_' | sed 's/^_*//; s/_*$//')"
DB_NAME="ilford_wt_${WORKTREE_SLUG}"

# --- port allocation ---------------------------------------------------------

# Take the lowest offset no other worktree has claimed. Claims are recorded in
# each worktree's .worktree.env, so this is stable across re-runs: a worktree
# that already has an env file keeps the offset written in it.
allocate_offset() {
    if [[ -f "$ENV_FILE" ]]; then
        local existing
        existing="$(grep -E '^WORKTREE_PORT_OFFSET=' "$ENV_FILE" | cut -d= -f2 || true)"
        if [[ -n "$existing" ]]; then
            echo "$existing"
            return
        fi
    fi

    local taken=()
    local path
    while read -r path; do
        [[ "$path" == "$REPO_ROOT" ]] && continue
        [[ -f "$path/.worktree.env" ]] || continue
        local offset
        offset="$(grep -E '^WORKTREE_PORT_OFFSET=' "$path/.worktree.env" | cut -d= -f2 || true)"
        [[ -n "$offset" ]] && taken+=("$offset")
    done < <(git -C "$REPO_ROOT" worktree list --porcelain | awk '/^worktree /{print $2}')

    local candidate
    for candidate in $(seq 1 "$MAX_OFFSET"); do
        local claimed=false
        local t
        for t in "${taken[@]:-}"; do
            [[ "$t" == "$candidate" ]] && claimed=true
        done
        if [[ "$claimed" == false ]]; then
            echo "$candidate"
            return
        fi
    done

    echo "No free port offset left (1-${MAX_OFFSET} all claimed by other worktrees)." >&2
    echo "Remove a stale worktree with 'git worktree remove', or raise MAX_OFFSET here" >&2
    echo "and register the matching redirect URI with the web OAuth client." >&2
    exit 1
}

OFFSET="$(allocate_offset)"
API_PORT=$((BASE_API_PORT + OFFSET))
WEB_PORT=$((BASE_WEB_PORT + OFFSET))
REDIRECT_URI="http://localhost:${WEB_PORT}/auth/callback"

# --- --reset-db --------------------------------------------------------------

if [[ "${1:-}" == "--reset-db" ]]; then
    echo "Resetting ${DB_NAME} from the template..."
    "$REPO_ROOT/scripts/test-db.sh" drop "$DB_NAME"
    "$REPO_ROOT/scripts/test-db.sh" clone "$DB_NAME"
    exit 0
fi

if [[ -n "${1:-}" ]]; then
    echo "Usage: $0 [--reset-db]" >&2
    exit 1
fi

echo "Setting up worktree '${WORKTREE_NAME}'"
echo "  primary checkout: ${PRIMARY_ROOT}"
echo "  port offset:      ${OFFSET} (server ${API_PORT}, frontend ${WEB_PORT})"
echo "  database:         ${DB_NAME}"
echo ""

# --- credentials -------------------------------------------------------------

for file in "${LINKED_FILES[@]}"; do
    target="$PRIMARY_ROOT/$file"
    link="$REPO_ROOT/$file"

    if [[ -e "$link" || -L "$link" ]]; then
        echo "  ${file}: already present"
        continue
    fi
    if [[ ! -e "$target" ]]; then
        echo "  ${file}: MISSING from the primary checkout — see docs/local-setup.md" >&2
        continue
    fi

    mkdir -p "$(dirname "$link")"
    ln -s "$target" "$link"
    echo "  ${file}: linked"
done

# --- config ------------------------------------------------------------------
#
# The config is copied rather than linked: databaseURL, server.port and
# server.redirectURI are all per-worktree.

if [[ -e "$REPO_ROOT/$CONFIG_FILE" ]]; then
    echo "  ${CONFIG_FILE}: already present (not overwritten)"
elif [[ ! -e "$PRIMARY_ROOT/$CONFIG_FILE" ]]; then
    echo "  ${CONFIG_FILE}: MISSING from the primary checkout — see docs/local-setup.md" >&2
else
    # Copy the primary's config, rewriting the three keys that are per-worktree:
    # the database it talks to, the port it listens on, and the redirect URI
    # matching this worktree's frontend port.
    awk -v dburl="postgres://postgres:postgres@localhost:5432/${DB_NAME}?sslmode=disable" \
        -v port="${API_PORT}" \
        -v redirect="${REDIRECT_URI}" '
        /^databaseURL:/ { printf "databaseURL: \"%s\"\n", dburl; next }
        /^[[:space:]]+port:/ && !seen {
            match($0, /^[[:space:]]+/)
            indent = substr($0, 1, RLENGTH)
            printf "%sport: %s\n", indent, port
            printf "%sredirectURI: \"%s\"\n", indent, redirect
            seen = 1
            next
        }
        /^[[:space:]]+redirectURI:/ { next }
        { print }
    ' "$PRIMARY_ROOT/$CONFIG_FILE" > "$REPO_ROOT/$CONFIG_FILE"
    echo "  ${CONFIG_FILE}: copied and rewritten for this worktree"
fi

# --- environment -------------------------------------------------------------
#
# Everything that is read from the environment rather than the config, written
# once so scripts and shells can source it.

cat > "$ENV_FILE" <<EOF
# Generated by scripts/worktree-init.sh — this worktree's slice of the shared
# machine. Sourced by scripts/dev.sh; source it yourself for go/bun commands:
#   set -a; source .worktree.env; set +a
WORKTREE_PORT_OFFSET=${OFFSET}
API_PORT=${API_PORT}
WEB_PORT=${WEB_PORT}
# Integration tests mint their own throwaway databases; this is only the admin
# connection they issue CREATE DATABASE on. Pointing it here rather than at the
# template keeps 'go test' from holding the template open.
TEST_DATABASE_URL=postgres://postgres:postgres@localhost:5432/${DB_NAME}?sslmode=disable
# The CP-SAT venv is shared with the primary checkout rather than rebuilt here;
# without this the allocator resolves a relative path and fails confusingly.
ILFORD_CPSAT_PYTHON=${PRIMARY_ROOT}/pyallocator/.venv/bin/python
EOF
echo "  .worktree.env: written"

# --- dependencies ------------------------------------------------------------

echo ""
echo "Installing frontend dependencies..."
(cd "$REPO_ROOT/web" && bun install)

# --- database ----------------------------------------------------------------

echo ""
"$REPO_ROOT/scripts/test-db.sh" start
"$REPO_ROOT/scripts/test-db.sh" clone "$DB_NAME"

echo ""
echo "Worktree ready."
echo "  scripts/dev.sh test   → server on ${API_PORT}, frontend on http://localhost:${WEB_PORT}"
echo "  Admin login needs ${REDIRECT_URI} registered with the web OAuth client"
echo "  (Google Cloud console → Credentials → the web client → Authorised redirect URIs)."
