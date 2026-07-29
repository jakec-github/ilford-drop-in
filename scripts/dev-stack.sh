#!/bin/bash
#
# Detachable development stack: brings the whole app up in the credential-free
# dev environment and returns, so an agent can start it and then drive it.
#
# Usage: scripts/dev-stack.sh {start|stop|status|logs}
#
# Unlike scripts/dev.sh this runs one process on one port — the Go server,
# serving the frontend build embedded in the binary at /, exactly as production
# does. No Google credentials are involved: drop_in_config.dev.yaml turns on the
# stubs (see docs/agents/dev-stack.md).
#
# start blocks only until GET /health returns 200, then leaves the server
# running in the background. It exits non-zero if that never happens, so a
# caller waiting on it is never left hanging.
#

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# In a git worktree, ports come from the file scripts/worktree-init.sh wrote
# (see docs/agents/worktrees.md), so two checkouts can run stacks at once.
# Absent in the primary checkout, where the config's own port is right.
if [[ -f "$REPO_ROOT/.worktree.env" ]]; then
    set -a
    # shellcheck disable=SC1091
    source "$REPO_ROOT/.worktree.env"
    set +a
fi

ENV="dev"
CONFIG_FILE="$REPO_ROOT/drop_in_config.${ENV}.yaml"

# logs/ is gitignored, so the pid file, the log and the built binary all live
# there rather than needing ignore entries of their own.
RUN_DIR="$REPO_ROOT/logs"
PID_FILE="$RUN_DIR/dev-stack.pid"
LOG_FILE="$RUN_DIR/dev-stack.log"
SERVER_BIN="$RUN_DIR/dev-stack-server"

# How long to wait for /health after the process starts. Generous: the first
# request also runs migrations against a database that may have just been made.
READY_TIMEOUT_SECONDS="${DEV_STACK_TIMEOUT:-60}"

usage() {
    echo "Usage: $0 {start|stop|status|logs}"
    echo ""
    echo "Commands:"
    echo "  start    Build and start the stack detached, waiting for /health"
    echo "  stop     Stop the running server"
    echo "  status   Report whether the server is running and healthy"
    echo "  logs     Follow the server log"
    echo ""
    echo "Environment:"
    echo "  API_PORT              Port to listen on (default: server.port from the config)"
    echo "  DEV_STACK_TIMEOUT     Seconds to wait for readiness (default: 60)"
    exit 1
}

# The config is the source of truth for the port and the database, and both are
# needed out here — to poll the right URL and to create the database before the
# server tries to connect. A worktree's API_PORT wins over the file.
config_port() {
    awk '/^server:/ { in_server = 1; next }
         /^[^[:space:]]/ { in_server = 0 }
         in_server && $1 == "port:" { print $2; exit }' "$CONFIG_FILE"
}

config_db_name() {
    awk -F/ '/^databaseURL:/ { sub(/\?.*/, "", $NF); gsub(/['\''"]/, "", $NF); print $NF; exit }' "$CONFIG_FILE"
}

require_config() {
    if [[ ! -f "$CONFIG_FILE" ]]; then
        echo "Missing ${CONFIG_FILE}" >&2
        echo "It is committed to the repo — restore it with 'git checkout drop_in_config.${ENV}.yaml'." >&2
        exit 1
    fi
}

server_pid() {
    [[ -f "$PID_FILE" ]] || return 1
    local pid
    pid="$(cat "$PID_FILE")"
    [[ -n "$pid" ]] || return 1
    # A pid file outliving its process is the normal case after a crash or a
    # reboot, so the file alone proves nothing.
    kill -0 "$pid" 2>/dev/null || return 1
    echo "$pid"
}

health_ok() {
    curl -fsS -o /dev/null --max-time 5 "http://localhost:${PORT}/health" 2>/dev/null
}

start() {
    require_config

    if pid="$(server_pid)"; then
        echo "Stack already running (pid ${pid}) on port ${PORT}"
        if health_ok; then
            echo "  http://localhost:${PORT}/ — healthy"
            return
        fi
        echo "  but /health is not answering — run '$0 stop' and start again" >&2
        exit 1
    fi

    # Something already answering on the port is not this stack — the pid check
    # above ruled that out. Refuse rather than start a server that will fail to
    # bind: the readiness poll cannot tell the foreign server's 200 from ours,
    # so it would report a stack that is ready and is not the one just built.
    if health_ok; then
        echo "Something is already serving http://localhost:${PORT}/health, and it is not this stack." >&2
        echo "Stop it (scripts/dev.sh, another checkout) or set API_PORT to a free port." >&2
        exit 1
    fi

    mkdir -p "$RUN_DIR"

    echo "Starting database..."
    "$REPO_ROOT/scripts/test-db.sh" start > /dev/null
    "$REPO_ROOT/scripts/test-db.sh" create "$(config_db_name)"

    # The frontend must be built before the Go binary, not after: web/embed.go
    # embeds web/dist at compile time, so a stale or missing dist bakes in.
    if [[ ! -d "$REPO_ROOT/web/node_modules" ]]; then
        echo "Installing frontend dependencies..."
        (cd "$REPO_ROOT/web" && bun install)
    fi
    echo "Building frontend..."
    (cd "$REPO_ROOT/web" && bun run build)

    echo "Building server..."
    (cd "$REPO_ROOT" && go build -o "$SERVER_BIN" ./cmd/server)

    echo "Starting server on port ${PORT}..."
    # One run per log. Whatever went wrong is then the whole file rather than
    # the tail of an ever-growing one, and a failed start prints exactly it.
    : > "$LOG_FILE"

    # Started from the repo root: the config is found relative to the working
    # directory, and so is devMode.volunteersCSV.
    #
    # All three standard streams are detached from this shell's, stdin included.
    # A caller that waits for the pipeline's file descriptors to close — an
    # agent's shell tool, a CI step — otherwise blocks on the server for as long
    # as it runs, which is exactly the hang this script exists to avoid.
    #
    # `cd` on its own line, not `cd && nohup ... &`: with the && form the `&`
    # applies to the whole list, so bash backgrounds a subshell and $! is the
    # subshell's pid, not the server's. stop would then kill the wrapper and
    # leave the server holding the port.
    (
        cd "$REPO_ROOT"
        nohup "$SERVER_BIN" -env "$ENV" -port "$PORT" \
            < /dev/null >> "$LOG_FILE" 2>&1 &
        echo $! > "$PID_FILE"
    )

    wait_for_ready
}

# wait_for_ready polls until the server answers, the process dies, or the
# deadline passes. Before the listener is up a poller gets connection refused
# rather than a status code, so both are treated the same — not ready yet.
wait_for_ready() {
    local pid
    pid="$(cat "$PID_FILE")"
    local deadline=$((SECONDS + READY_TIMEOUT_SECONDS))

    while ((SECONDS < deadline)); do
        if ! kill -0 "$pid" 2>/dev/null; then
            echo "" >&2
            echo "Server exited during startup. Last lines of ${LOG_FILE}:" >&2
            tail -n 20 "$LOG_FILE" >&2
            rm -f "$PID_FILE"
            exit 1
        fi
        if health_ok; then
            echo ""
            echo "Stack ready (pid ${pid})"
            echo "  app:    http://localhost:${PORT}/"
            echo "  health: http://localhost:${PORT}/health"
            echo "  login:  http://localhost:${PORT}/auth/login  (mints an admin session, no Google)"
            echo "  logs:   $0 logs"
            return
        fi
        sleep 1
    done

    echo "" >&2
    echo "Timed out after ${READY_TIMEOUT_SECONDS}s waiting for http://localhost:${PORT}/health" >&2
    echo "Last lines of ${LOG_FILE}:" >&2
    tail -n 20 "$LOG_FILE" >&2
    # Leaving a half-started server behind would make the next start report
    # "already running" for something that never became healthy.
    kill "$pid" 2>/dev/null || true
    rm -f "$PID_FILE"
    exit 1
}

stop() {
    local pid
    if ! pid="$(server_pid)"; then
        echo "Stack is not running"
        rm -f "$PID_FILE"
        return
    fi

    echo "Stopping server (pid ${pid})..."
    kill "$pid" 2>/dev/null || true
    for _ in {1..10}; do
        if ! kill -0 "$pid" 2>/dev/null; then
            rm -f "$PID_FILE"
            echo "Stopped"
            return
        fi
        sleep 1
    done

    echo "Server did not shut down in 10s — killing it"
    kill -9 "$pid" 2>/dev/null || true
    rm -f "$PID_FILE"
}

status() {
    local pid
    if ! pid="$(server_pid)"; then
        echo "Stack is not running"
        exit 1
    fi
    echo "Stack is running (pid ${pid}) on port ${PORT}"
    if health_ok; then
        echo "  /health: ok"
    else
        echo "  /health: not answering"
        exit 1
    fi
}

follow_logs() {
    if [[ ! -f "$LOG_FILE" ]]; then
        echo "No log at ${LOG_FILE} — the stack has not been started" >&2
        exit 1
    fi
    tail -f "$LOG_FILE"
}

# Every command needs the port — to start on, or to ask /health about — so it is
# resolved once here rather than in each. usage() is reached without it.
if [[ -n "${1:-}" ]]; then
    require_config
    PORT="${API_PORT:-$(config_port)}"
    if [[ -z "$PORT" ]]; then
        echo "Could not determine a port: set API_PORT or add server.port to ${CONFIG_FILE}" >&2
        exit 1
    fi
fi

case "${1:-}" in
    start)  start ;;
    stop)   stop ;;
    status) status ;;
    logs)   follow_logs ;;
    *)      usage ;;
esac
