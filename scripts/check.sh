#!/bin/bash
#
# Everything a change has to pass, in one command with one exit code: Go build,
# vet and tests, the pyallocator suite, then the frontend typecheck (tsc, via
# the build), its own test suite (bun:test) and lint.
#
# Usage: scripts/check.sh
#
# Meant to be fast enough to run before every push — seconds, not minutes.
# Nothing that needs a browser or a running app belongs here.
#
# It starts the test Postgres itself, because the database integration tests
# self-skip when the server is unreachable and a skipped run is indistinguishable
# from a passing one (see pkg/db/dbtest). DBTEST_REQUIRED turns that skip into a
# failure, so this script cannot report a green suite that exercised no schema.
#

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# Decided before the worktree env is sourced below, since that sets
# TEST_DATABASE_URL. CI brings its own Postgres as a service container, and
# test-db.sh pins DOCKER_CONTEXT=orbstack, which exists only on the maintainer's
# machine. A caller who set TEST_DATABASE_URL has likewise pointed us at a server
# of their own.
START_DB=true
if [[ -n "${CI:-}" || -n "${TEST_DATABASE_URL:-}" ]]; then
    START_DB=false
fi

# In a git worktree, TEST_DATABASE_URL points the tests at this worktree's own
# database (see docs/agents/worktrees.md). Absent in the primary checkout, where
# the dbtest default is right.
if [[ -f "$REPO_ROOT/.worktree.env" ]]; then
    set -a
    # shellcheck disable=SC1091
    source "$REPO_ROOT/.worktree.env"
    set +a
fi

# Echoing each command before running it means a failure part-way through names
# itself, without the caller having to count which step the output stopped at.
run() {
    echo ""
    echo "==> $*"
    "$@"
}

if [[ "$START_DB" == true ]]; then
    run "$REPO_ROOT/scripts/test-db.sh" start
fi

cd "$REPO_ROOT"

# go build covers the packages go test does not — the legacy CLI has no tests of
# its own, so vet and test alone would let it rot.
run go build ./...
run go vet ./...
run env DBTEST_REQUIRED=1 go test ./...

# The solver lives in Python, so go test alone leaves it unguarded. Its tests
# need ortools and pytest: worktrees share the primary checkout's venv through
# ILFORD_CPSAT_PYTHON (docs/agents/worktrees.md), CI and a fresh clone have
# neither and get one built here.
PYALLOCATOR_VENV="$REPO_ROOT/pyallocator/.venv"
PYALLOCATOR_PYTHON="${ILFORD_CPSAT_PYTHON:-$PYALLOCATOR_VENV/bin/python}"
if ! "$PYALLOCATOR_PYTHON" -c "import ortools, pytest" >/dev/null 2>&1; then
    run python3 -m venv "$PYALLOCATOR_VENV"
    run "$PYALLOCATOR_VENV/bin/pip" install --quiet --disable-pip-version-check \
        --editable "$REPO_ROOT/pyallocator[dev]"
    PYALLOCATOR_PYTHON="$PYALLOCATOR_VENV/bin/python"
fi

# That venv installs pyallocator editable, so in a worktree it resolves to the
# primary checkout's source. PYTHONPATH puts this checkout's source first, so a
# worktree tests the code it is about to push rather than main's.
# -rs prints why anything skipped, in the same spirit as DBTEST_REQUIRED: the
# e2e golden stands down when the CP-SAT build is not the one it was recorded
# against, and that should be visible rather than a bare 's'.
run env PYTHONPATH="$REPO_ROOT/pyallocator/src" \
    "$PYALLOCATOR_PYTHON" -m pytest -rs "$REPO_ROOT/pyallocator/tests"

# bun run build runs tsc -b first (web/build.ts), so this is the typecheck.
(cd "$REPO_ROOT/web" && run bun run build)
(cd "$REPO_ROOT/web" && run bun run test)
(cd "$REPO_ROOT/web" && run bun run lint)

echo ""
echo "All checks passed"
