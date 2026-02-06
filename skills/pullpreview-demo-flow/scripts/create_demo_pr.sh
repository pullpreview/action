#!/usr/bin/env bash
set -euo pipefail

REPO="${REPO:-pullpreview/action}"
BASE_BRANCH="${BASE_BRANCH:-codex/go-context}"
LABEL="${LABEL:-pullpreview}"
PR_TITLE="Auto-deploy app with PullPreview"
STAMP="$(date -u +%Y%m%d-%H%M%S)"
BRANCH="codex/demo-flow-${STAMP}"
WORKDIR="${WORKDIR:-$(mktemp -d /tmp/pullpreview-demo-XXXXXX)}"
CLONE_DIR="${WORKDIR}/repo"

echo "Using workdir: ${WORKDIR}"

git clone "https://github.com/${REPO}.git" "${CLONE_DIR}"
cd "${CLONE_DIR}"
git checkout "${BASE_BRANCH}"
git switch -c "${BRANCH}"

mkdir -p demo
cat > demo/go-flow-marker.txt <<EOF
Demo flow marker
Timestamp: $(date -u +%Y-%m-%dT%H:%M:%SZ)
Base branch: ${BASE_BRANCH}
EOF

git add demo/go-flow-marker.txt
git commit -m "docs: add marker for PullPreview demo flow"
git push -u origin "${BRANCH}"

PR_URL="$(gh pr create \
  --repo "${REPO}" \
  --base "${BASE_BRANCH}" \
  --head "${BRANCH}" \
  --title "${PR_TITLE}" \
  --body "Automated demo PR for PullPreview screenshot capture.")"

PR_NUMBER="$(echo "${PR_URL}" | sed -E 's#.*/pull/([0-9]+).*#\1#')"
gh pr edit "${PR_NUMBER}" --repo "${REPO}" --add-label "${LABEL}"

echo "PR_URL=${PR_URL}"
echo "PR_NUMBER=${PR_NUMBER}"
echo "BRANCH=${BRANCH}"
echo "REPO=${REPO}"
echo "WORKDIR=${WORKDIR}"
