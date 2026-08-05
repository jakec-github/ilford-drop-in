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
#   scripts/worktree-init.sh                # set the worktree up
#   scripts/worktree-init.sh --reset-db     # drop and re-clone this worktree's database
#   scripts/worktree-init.sh --remove       # remove this worktree and drop its database
#   scripts/worktree-init.sh --orphans      # list databases left by removed worktrees
#
# Run it from inside the worktree (--orphans works from the primary checkout
# too). Safe to re-run: existing links, the config and the database are left
# alone unless --reset-db is passed.
#
# See docs/agents/worktrees.md for the workflow this belongs to.
#

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
# The primary checkout is the directory holding the real .git directory; a
# worktree's .git is a file pointing into it. --git-common-dir can answer
# relative to the working directory, hence the fix-up.
COMMON_GIT_DIR="$(git -C "$REPO_ROOT" rev-parse --git-common-dir)"
[[ "$COMMON_GIT_DIR" == /* ]] || COMMON_GIT_DIR="$REPO_ROOT/$COMMON_GIT_DIR"
PRIMARY_ROOT="$(cd "$COMMON_GIT_DIR/.." && pwd)"

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

# The database name comes from the worktree's directory name, folded to a plain
# identifier and with a leading "ilford" dropped: .worktrees/issue-61 becomes
# ilford_wt_issue_61, and so does an older ../ilford-issue-61 alongside the
# checkout, rather than ilford_wt_ilford_issue_61.
db_name_for() {
    local slug
    slug="$(basename "$1" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9' '_' | sed 's/^_*//; s/_*$//; s/^ilford_//')"
    echo "ilford_wt_${slug}"
}

# --- --orphans ---------------------------------------------------------------
#
# Removing a worktree does not remove the database it cloned, so those outlive
# their worktree unless dropped. Read-only: it prints what to run rather than
# dropping anything, since a database is cheap to keep and expensive to lose.

orphan_dbs() {
    local live=()
    local path
    while read -r path; do
        live+=("$(db_name_for "$path")")
    done < <(git -C "$REPO_ROOT" worktree list --porcelain | awk '/^worktree /{print $2}')

    local found=false
    local db
    while read -r db; do
        [[ "$db" == ilford_wt_* ]] || continue
        local matched=false
        local l
        for l in "${live[@]:-}"; do
            [[ "$l" == "$db" ]] && matched=true
        done
        if [[ "$matched" == false ]]; then
            [[ "$found" == false ]] && echo "Databases with no matching worktree:"
            found=true
            echo "  ${db}    → scripts/test-db.sh drop ${db}"
        fi
    done < <("$PRIMARY_ROOT/scripts/test-db.sh" list)

    [[ "$found" == false ]] && echo "No orphaned worktree databases."
    return 0
}

if [[ "${1:-}" == "--orphans" ]]; then
    orphan_dbs
    exit 0
fi

if [[ "$REPO_ROOT" == "$PRIMARY_ROOT" ]]; then
    echo "This is the primary checkout, not a worktree — nothing to set up." >&2
    echo "Create a worktree first:  git worktree add .worktrees/<name> -b <branch>" >&2
    echo "(--orphans works from here, and lists databases left behind by removed worktrees.)" >&2
    exit 1
fi

WORKTREE_NAME="$(basename "$REPO_ROOT")"
DB_NAME="$(db_name_for "$REPO_ROOT")"

# --- --remove ----------------------------------------------------------------
#
# Teardown is the mirror of setup: the directory and everything git put in it,
# then the database, which lives outside the directory and is the one thing
# that would otherwise accumulate. The port offset needs nothing — it is read
# from the live worktree list, so it frees itself the moment the tree goes.

if [[ "${1:-}" == "--remove" ]]; then
    # Built in full rather than as an optional flag: bash 3.2, which is what
    # macOS ships, treats an empty array under `set -u` as unbound.
    if [[ "${2:-}" == "--force" ]]; then
        remove_cmd=(git worktree remove --force "$REPO_ROOT")
    elif [[ -n "${2:-}" ]]; then
        echo "Usage: $0 --remove [--force]" >&2
        exit 1
    else
        remove_cmd=(git worktree remove "$REPO_ROOT")
    fi

    echo "Removing worktree '${WORKTREE_NAME}' and dropping ${DB_NAME}..."
    # Run from the primary checkout: this script's own directory is about to go.
    cd "$PRIMARY_ROOT"
    if ! "${remove_cmd[@]}"; then
        echo "" >&2
        echo "Worktree not removed, so its database has been left alone." >&2
        echo "Uncommitted work is the usual cause — check it, then re-run with --force." >&2
        exit 1
    fi
    "$PRIMARY_ROOT/scripts/test-db.sh" drop "$DB_NAME"
    echo "Removed. Port offset freed."
    exit 0
fi

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
    echo "Usage: $0 [--reset-db | --remove [--force] | --orphans]" >&2
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

# Google only accepts redirect URIs registered with the web OAuth client, and the
# server refuses to start on an unregistered one. Naming this worktree's URI is
# therefore conditional on it being registered: without it everything but admin
# login still works, which beats a server that will not boot.
CONFIGURED_REDIRECT_URI="$REDIRECT_URI"
REDIRECT_URI_REGISTERED=true
if [[ -e "$REPO_ROOT/oauthClientWeb.test.json" ]] &&
    ! grep -qF "\"${REDIRECT_URI}\"" "$REPO_ROOT/oauthClientWeb.test.json"; then
    CONFIGURED_REDIRECT_URI=""
    REDIRECT_URI_REGISTERED=false
fi

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
        -v redirect="${CONFIGURED_REDIRECT_URI}" '
        /^databaseURL:/ { printf "databaseURL: \"%s\"\n", dburl; next }
        /^[[:space:]]+port:/ && !seen {
            match($0, /^[[:space:]]+/)
            indent = substr($0, 1, RLENGTH)
            printf "%sport: %s\n", indent, port
            if (redirect != "") printf "%sredirectURI: \"%s\"\n", indent, redirect
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
if [[ "$REDIRECT_URI_REGISTERED" == false ]]; then
    echo ""
    echo "  Admin login will NOT work in this worktree: ${REDIRECT_URI} is not"
    echo "  registered with the web OAuth client. Everything else runs. To fix it once"
    echo "  for all worktrees, add http://localhost:$((BASE_WEB_PORT + 1))/auth/callback ..."
    echo "  http://localhost:$((BASE_WEB_PORT + MAX_OFFSET))/auth/callback in the Google Cloud console"
    echo "  (Credentials → the web OAuth client → Authorised redirect URIs), then re-run"
    echo "  this script after deleting ${CONFIG_FILE}."
fi
