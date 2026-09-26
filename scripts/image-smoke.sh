#!/bin/bash
#
# Checks a built server image can draft a rota: runs the CP-SAT solver inside
# it the way the server does, on a small input, and expects a rota back.
#
# Usage: scripts/image-smoke.sh <image>
#
# The server finds its solver through $ILFORD_CPSAT_PYTHON
# (allocator.ResolvePythonInterpreter), so this reads that from the image rather
# than naming a path of its own: an image that forgets to set it fails here too.
# A venv built under a different Python from the runtime's, or a missing
# package, fails the same way — in CI rather than on the first draft in prod
# (#227).
#
# It also holds the image to its baseline: it runs as nonroot and has no shell.
#

set -euo pipefail

if [[ $# -ne 1 ]]; then
    echo "Usage: $0 <image>" >&2
    exit 2
fi
IMAGE="$1"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
FIXTURE="$REPO_ROOT/pyallocator/tests/testdata/cli_input.json"

fail() {
    echo "FAIL: $*" >&2
    exit 1
}

user="$(docker image inspect -f '{{.Config.User}}' "$IMAGE")"
case "$user" in
    nonroot | 65532 | 65532:65532) echo "ok: runs as $user" ;;
    *) fail "image runs as '${user:-root}', want nonroot" ;;
esac

if docker run --rm --entrypoint /bin/sh "$IMAGE" -c true >/dev/null 2>&1; then
    fail "image has a shell at /bin/sh; the runtime should be distroless"
fi
echo "ok: no shell"

python="$(docker image inspect -f '{{range .Config.Env}}{{println .}}{{end}}' "$IMAGE" |
    sed -n 's/^ILFORD_CPSAT_PYTHON=//p')"
[[ -n "$python" ]] || fail "image does not set ILFORD_CPSAT_PYTHON"

if ! output="$(docker run --rm -i --entrypoint "$python" "$IMAGE" -m pyallocator <"$FIXTURE")"; then
    fail "$python -m pyallocator exited non-zero"
fi
if ! jq -e '.success == true and .shifts[0].assignments[0].volunteer_id == "v1"' <<<"$output" >/dev/null; then
    fail "solver did not return the expected rota: $output"
fi
echo "ok: $python -m pyallocator solved the fixture"
