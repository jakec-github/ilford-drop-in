#!/bin/bash
#
# PostgreSQL test database management
# This is for the TEST environment only - not for production.
# Requires: Docker
#

set -e

# Pin this stack to OrbStack, leaving the global default context (e.g. Docker
# Desktop) untouched for everything else. DOCKER_CONTEXT overrides the configured
# default; the CLI is otherwise identical.
export DOCKER_CONTEXT=orbstack



CONTAINER_NAME="ilford-pg-test"
VOLUME_NAME="ilford-pg-test-data"
DB_NAME="ilford_dropin_test"
DB_USER="postgres"
DB_PASSWORD="postgres"
DB_PORT="5432"

usage() {
    echo "Usage: $0 {start|stop|status|reset|logs|psql|clone|drop|list}"
    echo ""
    echo "Commands:"
    echo "  start          Start the PostgreSQL container (creates if needed)"
    echo "  stop           Stop the PostgreSQL container"
    echo "  status         Show container status"
    echo "  reset          Stop container, delete volume, and start fresh"
    echo "  logs           Show container logs"
    echo "  psql [db]      Connect with psql (default: ${DB_NAME})"
    echo "  clone <db>     Copy ${DB_NAME} into a new database <db>"
    echo "  drop <db>      Drop database <db> (refuses to drop ${DB_NAME})"
    echo "  list           List the databases on this server"
    echo ""
    echo "Connection string:"
    echo "  postgres://${DB_USER}:${DB_PASSWORD}@localhost:${DB_PORT}/${DB_NAME}?sslmode=disable"
    exit 1
}

start() {
    if docker ps -q -f name="^${CONTAINER_NAME}$" | grep -q .; then
        echo "Container ${CONTAINER_NAME} is already running"
        return
    fi

    if docker ps -aq -f name="^${CONTAINER_NAME}$" | grep -q .; then
        echo "Starting existing container ${CONTAINER_NAME}..."
        docker start "${CONTAINER_NAME}"
    else
        echo "Creating and starting container ${CONTAINER_NAME}..."
        docker run -d \
            --name "${CONTAINER_NAME}" \
            -p "${DB_PORT}:5432" \
            -e POSTGRES_DB="${DB_NAME}" \
            -e POSTGRES_USER="${DB_USER}" \
            -e POSTGRES_PASSWORD="${DB_PASSWORD}" \
            -v "${VOLUME_NAME}:/var/lib/postgresql/data" \
            postgres:16
    fi

    echo "Waiting for PostgreSQL to be ready..."
    for i in {1..30}; do
        if docker exec "${CONTAINER_NAME}" pg_isready -U "${DB_USER}" > /dev/null 2>&1; then
            echo "PostgreSQL is ready"
            echo ""
            echo "Connection string:"
            echo "  postgres://${DB_USER}:${DB_PASSWORD}@localhost:${DB_PORT}/${DB_NAME}?sslmode=disable"
            return
        fi
        sleep 1
    done
    echo "Timed out waiting for PostgreSQL"
    exit 1
}

stop() {
    if docker ps -q -f name="^${CONTAINER_NAME}$" | grep -q .; then
        echo "Stopping container ${CONTAINER_NAME}..."
        docker stop "${CONTAINER_NAME}"
        echo "Container stopped (data preserved in volume ${VOLUME_NAME})"
    else
        echo "Container ${CONTAINER_NAME} is not running"
    fi
}

status() {
    if docker ps -q -f name="^${CONTAINER_NAME}$" | grep -q .; then
        echo "Container ${CONTAINER_NAME} is running"
        docker ps -f name="^${CONTAINER_NAME}$" --format "table {{.Status}}\t{{.Ports}}"
    elif docker ps -aq -f name="^${CONTAINER_NAME}$" | grep -q .; then
        echo "Container ${CONTAINER_NAME} exists but is stopped"
    else
        echo "Container ${CONTAINER_NAME} does not exist"
    fi

    if docker volume ls -q -f name="^${VOLUME_NAME}$" | grep -q .; then
        echo "Volume ${VOLUME_NAME} exists"
    else
        echo "Volume ${VOLUME_NAME} does not exist"
    fi
}

reset() {
    echo "This will delete all data in the database. Are you sure? (y/N)"
    read -r confirm
    if [[ ! "$confirm" =~ ^[Yy]$ ]]; then
        echo "Aborted"
        exit 0
    fi

    echo "Stopping and removing container..."
    docker rm -f "${CONTAINER_NAME}" 2>/dev/null || true

    echo "Removing volume..."
    docker volume rm "${VOLUME_NAME}" 2>/dev/null || true

    echo "Starting fresh..."
    start
}

logs() {
    docker logs -f "${CONTAINER_NAME}"
}

psql_connect() {
    local target="${DB_NAME}"
    # A leading non-flag argument names the database; anything else is passed
    # through to psql (e.g. -c "SELECT ...").
    if [[ -n "${1:-}" && "${1:0:1}" != "-" ]]; then
        target="$1"
        shift
    fi
    # -t only when there is a terminal, so 'psql -c "…"' works from a script too.
    local docker_flags=(-i)
    [[ -t 0 ]] && docker_flags=(-i -t)
    docker exec "${docker_flags[@]}" "${CONTAINER_NAME}" psql -U "${DB_USER}" -d "${target}" "$@"
}

# sql runs a statement against the maintenance database, which is never the one
# being created or dropped.
sql() {
    docker exec "${CONTAINER_NAME}" psql -U "${DB_USER}" -d postgres -qtAc "$1"
}

require_running() {
    if ! docker ps -q -f name="^${CONTAINER_NAME}$" | grep -q .; then
        echo "Container ${CONTAINER_NAME} is not running — run '$0 start' first" >&2
        exit 1
    fi
}

# Database names are interpolated into SQL, so only accept plain identifiers.
require_db_name() {
    if [[ -z "$1" ]]; then
        echo "Usage: $0 $2 <database>" >&2
        exit 1
    fi
    if [[ ! "$1" =~ ^[a-z_][a-z0-9_]*$ ]]; then
        echo "Invalid database name '$1' — use lowercase letters, digits and underscores" >&2
        exit 1
    fi
}

db_exists() {
    [[ -n "$(sql "SELECT 1 FROM pg_database WHERE datname = '$1'")" ]]
}

# clone copies the seeded template database into a new one. Postgres does this
# natively (CREATE DATABASE ... TEMPLATE), so no dump file is involved — but it
# requires the template to have no open connections.
clone() {
    local target="$1"
    require_db_name "$target" clone
    require_running

    if db_exists "$target"; then
        echo "Database ${target} already exists — nothing to do"
        return
    fi

    if ! db_exists "${DB_NAME}"; then
        echo "Template database ${DB_NAME} does not exist on this server" >&2
        exit 1
    fi

    local sessions
    sessions="$(sql "SELECT count(*) FROM pg_stat_activity WHERE datname = '${DB_NAME}'")"
    if [[ "$sessions" != "0" ]]; then
        echo "Cannot clone ${DB_NAME}: ${sessions} open connection(s) to it." >&2
        echo "Postgres needs the template idle. Stop anything connected to it (a running" >&2
        echo "server, an open psql, a test run) and try again. To see who:" >&2
        echo "  $0 psql -c \"SELECT pid, application_name, client_addr FROM pg_stat_activity WHERE datname = '${DB_NAME}'\"" >&2
        exit 1
    fi

    echo "Cloning ${DB_NAME} into ${target}..."
    sql "CREATE DATABASE ${target} TEMPLATE ${DB_NAME}" > /dev/null
    echo "Created ${target}"
    echo "  postgres://${DB_USER}:${DB_PASSWORD}@localhost:${DB_PORT}/${target}?sslmode=disable"
}

drop_db() {
    local target="$1"
    require_db_name "$target" drop
    require_running

    if [[ "$target" == "${DB_NAME}" ]]; then
        echo "Refusing to drop ${DB_NAME} — it is the seeded template. Use '$0 reset' if you really mean it." >&2
        exit 1
    fi

    if ! db_exists "$target"; then
        echo "Database ${target} does not exist — nothing to do"
        return
    fi

    echo "Dropping ${target}..."
    sql "DROP DATABASE ${target} WITH (FORCE)" > /dev/null
    echo "Dropped ${target}"
}

list() {
    require_running
    sql "SELECT datname FROM pg_database WHERE datistemplate = false ORDER BY datname"
}

case "${1:-}" in
    start)  start ;;
    stop)   stop ;;
    status) status ;;
    reset)  reset ;;
    logs)   logs ;;
    psql)   shift; psql_connect "$@" ;;
    clone)  clone "${2:-}" ;;
    drop)   drop_db "${2:-}" ;;
    list)   list ;;
    *)      usage ;;
esac
