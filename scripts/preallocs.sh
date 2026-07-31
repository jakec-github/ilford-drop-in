#!/bin/bash
#
# Preallocation test helper: create, list and delete preallocations against a
# running web server (see scripts/dev.sh). This is for local/test poking only —
# not a production tool.
#
# Usage:
#   scripts/preallocs.sh login
#   scripts/preallocs.sh list [from] [to]
#   scripts/preallocs.sh create <date> volunteer <volunteerId> [teamlead]
#   scripts/preallocs.sh create <date> custom "<value>"
#   scripts/preallocs.sh delete <id>
#
# Every endpoint here is admin-only, so run `login` first: against the
# credential-free dev stack it mints a session with no Google round trip (see
# docs/agents/dev-stack.md). The cookie lands in COOKIE_JAR (default
# ./cookies.txt) and every later command sends it.
#
# `list` returns both kinds of pin: the manual ones stored against a shift, and
# the ones the server's rota overrides resolve to. Only manual pins carry an
# "id", which is also the only thing `delete` can be pointed at — a config pin
# is changed by editing the config.
#
# Dates are YYYY-MM-DD. The target server defaults to http://localhost:8080;
# override with API_URL, e.g. API_URL=http://localhost:9090 scripts/preallocs.sh list
#
# Examples:
#   scripts/preallocs.sh login
#   scripts/preallocs.sh create 2026-08-02 volunteer vol-123
#   scripts/preallocs.sh create 2026-08-02 volunteer vol-123 teamlead
#   scripts/preallocs.sh create 2026-08-02 custom "Guest chef"
#   scripts/preallocs.sh list 2026-08-01 2026-08-31
#   scripts/preallocs.sh delete 7f3c...-id
#

set -euo pipefail

API_URL="${API_URL:-http://localhost:8080}"
COOKIE_JAR="${COOKIE_JAR:-cookies.txt}"

# curl with the admin session attached. Sends nothing when there is no jar yet,
# so the failure is the server's 401 — which names the problem — rather than
# curl refusing to start on a file that is not there.
api() {
    if [[ -f "$COOKIE_JAR" ]]; then
        curl -sS -b "$COOKIE_JAR" "$@"
    else
        curl -sS "$@"
    fi
}

# Pretty-print JSON when jq is present; otherwise pass through untouched. A body
# that is not JSON is printed as it came — a rejection says "Unauthorized", and
# a jq parse error on top of it only hides the answer.
pretty() {
    local body
    body=$(cat)
    if command -v jq >/dev/null 2>&1 && printf '%s' "$body" | jq . 2>/dev/null; then
        return
    fi
    printf '%s\n' "$body"
}

usage() {
    sed -n '2,33p' "$0"
    exit 1
}

cmd="${1:-}"
shift || true

case "$cmd" in
    login)
        # Follows the redirect back to the app so the jar ends up holding the
        # session cookie the redirect was issued with.
        code=$(curl -sS -L -c "$COOKIE_JAR" -o /dev/null -w '%{http_code}' "${API_URL}/auth/login")
        echo "HTTP ${code} — session in ${COOKIE_JAR}"
        ;;

    list)
        from="${1:-}"
        to="${2:-}"
        query=""
        [[ -n "$from" ]] && query="from=${from}"
        [[ -n "$to" ]] && query="${query:+${query}&}to=${to}"
        url="${API_URL}/api/preallocations${query:+?${query}}"
        api "$url" | pretty
        ;;

    create)
        date="${1:-}"
        kind="${2:-}"
        value="${3:-}"
        flag="${4:-}"
        if [[ -z "$date" || -z "$kind" || -z "$value" ]]; then
            echo "create needs: <date> volunteer|custom <value> [teamlead]" >&2
            exit 1
        fi

        team_lead=false
        [[ "$flag" == "teamlead" ]] && team_lead=true

        case "$kind" in
            volunteer)
                body=$(jq -nc --arg d "$date" --arg v "$value" --argjson tl "$team_lead" \
                    '{date:$d, volunteerId:$v, teamLead:$tl}')
                ;;
            custom)
                if [[ "$team_lead" == true ]]; then
                    echo "teamlead is only valid for a volunteer pin" >&2
                    exit 1
                fi
                body=$(jq -nc --arg d "$date" --arg c "$value" '{date:$d, custom:$c}')
                ;;
            *)
                echo "unknown kind '$kind' (want: volunteer|custom)" >&2
                exit 1
                ;;
        esac

        api -X POST "${API_URL}/api/preallocations" \
            -H 'Content-Type: application/json' \
            -d "$body" | pretty
        ;;

    delete)
        id="${1:-}"
        if [[ -z "$id" ]]; then
            echo "delete needs: <id>" >&2
            exit 1
        fi
        # DELETE returns 204 No Content on success; surface the status code so an
        # empty body is not mistaken for a silent failure.
        code=$(api -o /dev/null -w '%{http_code}' -X DELETE "${API_URL}/api/preallocations/${id}")
        echo "HTTP ${code}"
        ;;

    *)
        usage
        ;;
esac
