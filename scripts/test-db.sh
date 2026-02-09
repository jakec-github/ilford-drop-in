#!/bin/bash
#
# PostgreSQL test database management
# This is for the TEST environment only - not for production.
# Requires: Docker
#

set -e

CONTAINER_NAME="ilford-pg-test"
VOLUME_NAME="ilford-pg-test-data"
DB_NAME="ilford_dropin_test"
DB_USER="postgres"
DB_PASSWORD="postgres"
DB_PORT="5432"

usage() {
    echo "Usage: $0 {start|stop|status|reset|logs|psql}"
    echo ""
    echo "Commands:"
    echo "  start   Start the PostgreSQL container (creates if needed)"
    echo "  stop    Stop the PostgreSQL container"
    echo "  status  Show container status"
    echo "  reset   Stop container, delete volume, and start fresh"
    echo "  logs    Show container logs"
    echo "  psql    Connect to the database with psql"
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
    docker exec -it "${CONTAINER_NAME}" psql -U "${DB_USER}" -d "${DB_NAME}"
}

case "${1:-}" in
    start)  start ;;
    stop)   stop ;;
    status) status ;;
    reset)  reset ;;
    logs)   logs ;;
    psql)   psql_connect ;;
    *)      usage ;;
esac
