#!/bin/bash
#
# Manual-preallocation test helper: create, list and delete manual
# preallocations against a running web server (see scripts/dev.sh).
# This is for local/test poking only — not a production tool.
#
# Usage:
#   scripts/preallocs.sh list [from] [to]
#   scripts/preallocs.sh create <date> volunteer <volunteerId> [teamlead]
#   scripts/preallocs.sh create <date> custom "<value>"
#   scripts/preallocs.sh delete <id>
#
# Dates are YYYY-MM-DD. The target server defaults to http://localhost:8080;
# override with API_URL, e.g. API_URL=http://localhost:9090 scripts/preallocs.sh list
#
# Examples:
#   scripts/preallocs.sh create 2026-08-02 volunteer vol-123
#   scripts/preallocs.sh create 2026-08-02 volunteer vol-123 teamlead
#   scripts/preallocs.sh create 2026-08-02 custom "Guest chef"
#   scripts/preallocs.sh list 2026-08-01 2026-08-31
#   scripts/preallocs.sh delete 7f3c...-id
#

set -euo pipefail

API_URL="${API_URL:-http://localhost:8080}"

# Pretty-print JSON when jq is present; otherwise pass through untouched.
pretty() {
    if command -v jq >/dev/null 2>&1; then
        jq .
    else
        cat
    fi
}

usage() {
    sed -n '2,25p' "$0"
    exit 1
}

cmd="${1:-}"
shift || true

case "$cmd" in
    list)
        from="${1:-}"
        to="${2:-}"
        query=""
        [[ -n "$from" ]] && query="from=${from}"
        [[ -n "$to" ]] && query="${query:+${query}&}to=${to}"
        url="${API_URL}/preallocations${query:+?${query}}"
        curl -sS "$url" | pretty
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

        curl -sS -X POST "${API_URL}/preallocations" \
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
        code=$(curl -sS -o /dev/null -w '%{http_code}' -X DELETE "${API_URL}/preallocations/${id}")
        echo "HTTP ${code}"
        ;;

    *)
        usage
        ;;
esac
