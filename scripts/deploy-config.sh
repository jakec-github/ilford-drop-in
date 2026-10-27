#!/bin/bash
#
# Ship one environment's env/<env>/drop_in_config.yaml to the droplet.
#
# Usage: scripts/deploy-config.sh <prod|preprod> [host] [-y]
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
#   - it recreates that environment's container rather than restarting it,
#     because a restarted container keeps whatever it already had mounted;
#   - it waits for the app to answer afterwards and exits non-zero if it does
#     not, printing the logs — rather than exiting 0 over a dead server.
#
# The last two are deploy/rollout.sh on the droplet, the script the deploy
# workflow runs too, so a config rollout and a code deploy start and check an
# environment the same way. It reads the domain to poll from the droplet's own
# site.env, so nothing here needs to know it.
#
# The host comes from the argument, $DEPLOY_HOST, or a DEPLOY_HOST line in the
# untracked .deploy.env at the repo root.
#

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# Where the compose file expects each environment's directory, which it mounts
# into that environment's container. Keep in step with deploy/compose.yaml.
REMOTE_DIR="/opt/dropin"

# The two credentials that live beside the config in that directory. They are
# not shipped here — they change once a year, not once a week — but the
# directory is mounted whole, so a recreate with either of them missing starts a
# server that cannot reach Google.
REMOTE_CREDENTIALS=(
    "oauthClientWeb.json"
    "serviceAccount.json"
)

usage() {
    cat >&2 <<EOF
Usage: $0 <prod|preprod> [host] [-y]

  env     the environment to ship: env/<env>/drop_in_config.yaml
  host    droplet address. Falls back to \$DEPLOY_HOST, then to a DEPLOY_HOST
          line in $REPO_ROOT/.deploy.env (untracked).
  -y      skip the confirmation prompt.
EOF
    exit 1
}

ENV=""
HOST=""
ASSUME_YES=false
for arg in "$@"; do
    case "$arg" in
        -y|--yes) ASSUME_YES=true ;;
        -h|--help) usage ;;
        -*) echo "Unknown option: $arg" >&2; usage ;;
        *)
            if [[ -z "$ENV" ]]; then
                ENV="$arg"
            elif [[ -z "$HOST" ]]; then
                HOST="$arg"
            else
                usage
            fi
            ;;
    esac
done

case "$ENV" in
    prod|preprod) ;;
    "") echo "Name the environment to ship: prod or preprod" >&2; usage ;;
    *) echo "Unknown environment '$ENV': want prod or preprod" >&2; usage ;;
esac

CONFIG_FILE="$REPO_ROOT/env/${ENV}/drop_in_config.yaml"
REMOTE_CONFIG_DIR="$REMOTE_DIR/env/${ENV}"

# .deploy.env carries the droplet's address, which the repo deliberately does
# not. An explicit environment wins over the file.
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

if [[ -z "$HOST" ]]; then
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
if ! (cd "$REPO_ROOT" && go run ./cmd/server -env "$ENV" -validate-config "$CONFIG_FILE"); then
    echo "" >&2
    echo "Not shipping: the server would reject this file." >&2
    exit 1
fi

# 2. Confirm. The counts above are what an operator checks against what they
#    meant to change — a dropped pin validates perfectly well.
echo ""
if [[ "$ASSUME_YES" != true ]]; then
    if [[ ! -t 0 ]]; then
        echo "Nothing on stdin to confirm with — rerun with -y to ship unattended." >&2
        exit 1
    fi
    read -r -p "Ship this to ${HOST} and recreate ${ENV}? [y/N] " reply
    if [[ ! "$reply" =~ ^[Yy]$ ]]; then
        echo "Aborted."
        exit 1
    fi
fi

# 3. Check the droplet can take it. rollout.sh arrives with the first deploy
#    that carries it, and the credentials must already be in the environment's
#    directory: the container mounts it whole, so recreating it without them
#    would start a server that cannot reach Google.
echo ""
echo "Checking ${REMOTE_CONFIG_DIR} on ${HOST}..."
if ! remote "test -f ${REMOTE_DIR}/rollout.sh"; then
    echo "" >&2
    echo "Not shipping: ${REMOTE_DIR}/rollout.sh is not on the droplet yet. The deploy" >&2
    echo "workflow copies it there, so run that once (Actions → Build and deploy →" >&2
    echo "Run workflow) and try again." >&2
    exit 1
fi
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
    echo "  scp env/${ENV}/oauthClientWeb.json env/${ENV}/serviceAccount.json ${SSH_TARGET}:${REMOTE_CONFIG_DIR}/" >&2
    exit 1
fi

# 4. Ship it.
echo "Copying to ${SSH_TARGET}:${REMOTE_CONFIG_DIR}/..."
scp "$CONFIG_FILE" "${SSH_TARGET}:${REMOTE_CONFIG_DIR}/"

# 5. Recreate and prove it came back. Without the health check this would exit
#    0 over a server crash-looping on the file it just shipped, which is the
#    failure this whole script exists to make impossible to miss. While the
#    environment is switched off (scripts/preprod.sh down), rollout.sh says so
#    and starts nothing; the config waits on the box for `up`.
echo ""
if ! remote "bash ${REMOTE_DIR}/rollout.sh ${ENV}"; then
    echo "" >&2
    echo "The config on the droplet is the one just shipped. Fix it locally and" >&2
    echo "run this script again, or roll back with the previous file." >&2
    exit 1
fi
