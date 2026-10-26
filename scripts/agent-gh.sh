#!/usr/bin/env bash
# gh as the jakec-agent machine account: reads GH_TOKEN from the primary
# checkout's .claude/settings.local.json (untracked), so it works from any
# worktree and the token never appears on a command line.
#
#   scripts/agent-gh.sh pr view 283
#   scripts/agent-gh.sh issue create --title ... --body-file ...
set -euo pipefail

# A worktree's .git is a file; its common dir is the primary checkout's .git.
common="$(git rev-parse --path-format=absolute --git-common-dir)"
settings="$(dirname "$common")/.claude/settings.local.json"

if [[ ! -f "$settings" ]]; then
    echo "agent-gh: no $settings — nothing to read the agent token from" >&2
    exit 1
fi

token="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["env"]["GH_TOKEN"])' "$settings")"
GH_TOKEN="$token" exec gh "$@"
