#!/bin/bash
#
# Ship drop_in_config.prod.yaml to the droplet.
#
# Usage: scripts/deploy-config.sh [host] [-y]
#
# Config rollout is manual and deliberately separate from deployment: the deploy
# workflow ships code, this ships the file that code reads, and the two change
# on their own schedules. Nothing here is triggered by a push.
#
# What it adds over a bare `scp` is the three things whose absence turned one
# bad config into a crash-looping site (issue #100):
#
#   - it validates the file before anything leaves this machine, so a config the
#     server would reject never reaches the server;
#   - it recreates the app container rather than restarting it, because a
#     restarted container keeps whatever it already had mounted;
#   - it waits for the app to answer afterwards and exits non-zero if it does
#     not, printing the logs — rather than exiting 0 over a dead server.
#
# The host comes from the argument, $DEPLOY_HOST, or a DEPLOY_HOST line in the
# untracked .deploy.env at the repo root.
#

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

ENV="prod"
CONFIG_FILE="$REPO_ROOT/drop_in_config.${ENV}.yaml"

# Where the compose file expects to find the config directory it mounts into the
# container. Keep in step with deploy/compose.yaml.
REMOTE_DIR="/opt/dropin"
REMOTE_CONFIG_DIR="$REMOTE_DIR/config"

# The two credentials that live beside the config in that directory. They are
# not shipped here — they change once a year, not once a week — but the
# directory is mounted whole, so a recreate with either of them missing starts a
# server that cannot reach Google.
REMOTE_CREDENTIALS=(
    "oauthClientWeb.${ENV}.json"
    "serviceAccount.${ENV}.json"
)

# How long to give the app to answer after the recreate. It pulls nothing and
# runs migrations against a database that already exists, so this is generous.
READY_TIMEOUT_SECONDS="${DEPLOY_CONFIG_TIMEOUT:-90}"

usage() {
    cat >&2 <<EOF
Usage: $0 [host] [-y]

  host    droplet address. Falls back to \$DEPLOY_HOST, then to a DEPLOY_HOST
          line in $REPO_ROOT/.deploy.env (untracked).
  -y      skip the confirmation prompt.

Environment:
  DEPLOY_HOST             droplet address
  HEALTH_URL              what to poll afterwards (default: the host in deploy/Caddyfile)
  DEPLOY_CONFIG_TIMEOUT   seconds to wait for the app to answer (default: 90)
EOF
    exit 1
}

HOST=""
ASSUME_YES=false
for arg in "$@"; do
    case "$arg" in
        -y|--yes) ASSUME_YES=true ;;
        -h|--help) usage ;;
        -*) echo "Unknown option: $arg" >&2; usage ;;
        *) [[ -n "$HOST" ]] && usage; HOST="$arg" ;;
    esac
done

if [[ -z "$HOST" ]]; then
    HOST="${DEPLOY_HOST:-}"
fi
if [[ -z "$HOST" && -f "$REPO_ROOT/.deploy.env" ]]; then
    set -a
    # shellcheck disable=SC1091
    source "$REPO_ROOT/.deploy.env"
    set +a
    HOST="${DEPLOY_HOST:-}"
fi
if [[ -z "$HOST" ]]; then
    echo "No host given: pass one, set DEPLOY_HOST, or put DEPLOY_HOST=<ip> in .deploy.env" >&2
    exit 1
fi

SSH_TARGET="root@${HOST}"

remote() {
    ssh "$SSH_TARGET" "$@"
}

compose() {
    remote "cd ${REMOTE_DIR} && docker compose $*"
}

# The domain is Caddy's, and Caddy's config is in the repo, so the health URL is
# derived from it rather than being a second place to update when it changes.
health_url() {
    if [[ -n "${HEALTH_URL:-}" ]]; then
        echo "$HEALTH_URL"
        return
    fi
    local domain
    domain="$(awk 'NF && $1 !~ /^#/ { sub(/\{.*/, "", $0); print $1; exit }' "$REPO_ROOT/deploy/Caddyfile")"
    if [[ -z "$domain" ]]; then
        echo "Could not read a domain from deploy/Caddyfile — set HEALTH_URL" >&2
        exit 1
    fi
    echo "https://${domain}/health"
}

fail_with_logs() {
    echo "" >&2
    echo "$1" >&2
    echo "" >&2
    echo "Last 50 lines of the app log:" >&2
    compose "logs --tail=50 --no-color app" >&2 || true
    echo "" >&2
    echo "The config on the droplet is the one just shipped. Fix it locally and" >&2
    echo "run this script again, or roll back with the previous file." >&2
    exit 1
}

# 1. Validate locally. This is the whole point of the script existing, so it
#    happens before anything touches the network — including the ssh to the
#    droplet, which would otherwise prompt for a key on a run destined to abort.
if [[ ! -f "$CONFIG_FILE" ]]; then
    echo "Missing ${CONFIG_FILE}" >&2
    echo "It is git-ignored and unregenerable — restore it from your own copy." >&2
    exit 1
fi

echo "Validating ${CONFIG_FILE}..."
echo ""
if ! (cd "$REPO_ROOT" && go run ./cmd/cli -e "$ENV" validate-config "$CONFIG_FILE"); then
    echo "" >&2
    echo "Not shipping: the server would reject this file." >&2
    exit 1
fi

# 2. Confirm. The counts above are what an operator checks against what they
#    meant to change — a dropped preallocation validates perfectly well.
echo ""
if [[ "$ASSUME_YES" != true ]]; then
    if [[ ! -t 0 ]]; then
        echo "Nothing on stdin to confirm with — rerun with -y to ship unattended." >&2
        exit 1
    fi
    read -r -p "Ship this to ${HOST} and recreate the app? [y/N] " reply
    if [[ ! "$reply" =~ ^[Yy]$ ]]; then
        echo "Aborted."
        exit 1
    fi
fi

# 3. Check the credentials are already in the config directory. The container
#    mounts the directory whole, so recreating it without them would take the
#    site down — and the likeliest reason for them to be missing is a droplet
#    still holding its config files a level up, from before the mount changed
#    (see docs/deployment.md).
echo ""
echo "Checking ${REMOTE_CONFIG_DIR} on ${HOST}..."
remote "mkdir -p ${REMOTE_CONFIG_DIR}"
missing=()
for credential in "${REMOTE_CREDENTIALS[@]}"; do
    if ! remote "test -f ${REMOTE_CONFIG_DIR}/${credential}"; then
        missing+=("$credential")
    fi
done
if ((${#missing[@]})); then
    echo "" >&2
    echo "Not shipping: ${REMOTE_CONFIG_DIR} is missing ${missing[*]}" >&2
    echo "The app mounts that directory whole, so recreating it now would start a" >&2
    echo "server with no Google credentials. Put them there first:" >&2
    echo "" >&2
    echo "  ssh ${SSH_TARGET} 'mv ${REMOTE_DIR}/*.${ENV}.json ${REMOTE_CONFIG_DIR}/'" >&2
    echo "" >&2
    echo "or scp them from your own copies." >&2
    exit 1
fi

# 4. Ship it.
echo "Copying to ${SSH_TARGET}:${REMOTE_CONFIG_DIR}/..."
scp "$CONFIG_FILE" "${SSH_TARGET}:${REMOTE_CONFIG_DIR}/"

# 5. Recreate — not restart, and not a bare `up -d`. A restarted container keeps
#    the mounts it already resolved, and `up -d` alone is a no-op when the
#    compose file has not changed, which is exactly the case here: the config
#    changed and compose knows nothing about it.
echo ""
echo "Recreating the app container..."
compose "up -d --force-recreate app"

# 6. Prove it came back. Without this the script would exit 0 over a server that
#    is crash-looping on the file it just shipped, which is the failure this
#    whole script exists to make impossible to miss.
url="$(health_url)"
echo ""
echo "Waiting for ${url}..."
deadline=$((SECONDS + READY_TIMEOUT_SECONDS))
while ((SECONDS < deadline)); do
    if curl -fsS -o /dev/null --max-time 5 "$url"; then
        echo ""
        echo "Config shipped and the app is answering."
        exit 0
    fi
    sleep 2
done

fail_with_logs "Timed out after ${READY_TIMEOUT_SECONDS}s: ${url} never answered."
