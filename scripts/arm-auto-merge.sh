#!/bin/bash
#
# Arms GitHub auto-merge on a PR, as a merge commit: the PR lands by itself the
# moment the reviewer approves it and the main ruleset is satisfied. Nobody
# merges by hand, and the approval is the merge decision.
#
# Usage: scripts/arm-auto-merge.sh <pr-number>
#
# Why not `gh pr merge --auto --merge`: the agent token (jakec-agent) is a
# classic PAT with only `public_repo`, and gh's own queries around a merge ask
# for fields that need `read:org`, which fails the whole call. The mutation
# itself needs nothing more than `public_repo` on a public repo, so this looks
# the PR's node id up over REST and sends that one mutation, asking for no
# field beyond the PR's own.
#
# Run it once CI is green, not before: the ruleset requires an approval but no
# status checks, so an armed PR approved while red would still merge.
#

set -euo pipefail

if [[ $# -ne 1 || ! "$1" =~ ^[0-9]+$ ]]; then
    echo "Usage: $0 <pr-number>" >&2
    exit 2
fi
PR="$1"

REPO="$(gh repo view --json nameWithOwner -q .nameWithOwner 2>/dev/null ||
    git remote get-url origin | sed -E 's#(git@github.com:|https://github.com/)##; s#\.git$##')"

NODE_ID="$(gh api "repos/$REPO/pulls/$PR" --jq .node_id)"

gh api graphql \
    -f query='mutation($id: ID!) {
      enablePullRequestAutoMerge(input: {pullRequestId: $id, mergeMethod: MERGE}) {
        pullRequest { number autoMergeRequest { mergeMethod enabledAt } }
      }
    }' \
    -f id="$NODE_ID" \
    --jq '.data.enablePullRequestAutoMerge.pullRequest | "PR #\(.number): auto-merge armed (\(.autoMergeRequest.mergeMethod))"'
