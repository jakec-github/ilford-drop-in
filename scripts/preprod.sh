#!/bin/bash
#
# Switch preprod off and on again, to give the droplet its memory back.
#
# Usage: scripts/preprod.sh <down|up> [host]
#
#   down  stops and removes the app-preprod container, and leaves
#         /opt/dropin/env/preprod/disabled on the droplet. While that marker is
#         there, deploys and config rollouts skip preprod with a notice rather
#         than starting it again.
#   up    removes the marker and starts the image preprod last ran, then waits
#         for it to answer on its own host (deploy/rollout.sh).
#
# Prod is untouched either way. Caddy keeps its preprod site, which answers 502
# while preprod is down.
#
# The host comes from the argument, $DEPLOY_HOST, or a DEPLOY_HOST line in the
# untracked .deploy.env at the repo root.
#

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
REMOTE_DIR="/opt/dropin"
MARKER="${REMOTE_DIR}/env/preprod/disabled"

usage() {
    echo "Usage: $0 <down|up> [host]" >&2
    exit 1
}

[[ $# -ge 1 && $# -le 2 ]] || usage
ACTION="$1"
HOST="${2:-}"

ENV_DEPLOY_HOST="${DEPLOY_HOST:-}"
if [[ -f "$REPO_ROOT/.deploy.env" ]]; then
    set -a
    # shellcheck disable=SC1091
    source "$REPO_ROOT/.deploy.env"
    set +a
fi
if [[ -n "$ENV_DEPLOY_HOST" ]]; then
    DEPLOY_HOST="$ENV_DEPLOY_HOST"
fi
HOST="${HOST:-${DEPLOY_HOST:-}}"
if [[ -z "$HOST" ]]; then
    echo "No host given: pass one, set DEPLOY_HOST, or put DEPLOY_HOST=<ip> in .deploy.env" >&2
    exit 1
fi

case "$ACTION" in
    down)
        # The marker first, so a deploy that starts meanwhile already skips. The
        # container is only removed if compose knows the service: a droplet that
        # has not yet had a deploy carrying preprod has nothing to remove, and
        # setting the marker there is how a first deploy is told to skip it.
        ssh "root@${HOST}" "
            set -e
            mkdir -p ${REMOTE_DIR}/env/preprod
            touch ${MARKER}
            cd ${REMOTE_DIR}
            if docker compose config --services | grep -qx app-preprod; then
                docker compose rm --stop --force app-preprod
            fi
        "
        echo "preprod is down; deploys will skip it until scripts/preprod.sh up."
        ;;
    up)
        ssh "root@${HOST}" "rm -f ${MARKER} && bash ${REMOTE_DIR}/rollout.sh preprod"
        ;;
    *) usage ;;
esac
