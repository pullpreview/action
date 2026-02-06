---
name: pullpreview-demo-flow
description: Repeatable PullPreview demo workflow that creates a PR, applies label, captures lifecycle screenshots, and verifies deploy/destroy comment transitions.
allowed-tools: Bash(gh:*), Bash(agent-browser:*), Bash(jq:*), Bash(git:*)
---

# PullPreview Demo Flow (Screenshots)

Use this skill to run a full, repeatable PullPreview demo with screenshots.

## Non-negotiable rules

1. Demo PR title must always be exactly: `Auto-deploy app with PullPreview`.
2. Before taking PR comment screenshots, scroll the PR page so the comment card is visible in viewport.
3. Keep the screenshot filename set stable:
   - `01-pr-open-with-label.png`
   - `02-comment-deploying.png`
   - `03-action-running.png`
   - `04-view-deployment-button.png`
   - `05-unlabelled-pr.png`
   - `06-comment-destroyed.png`

## Prerequisites

- `gh` authenticated with access to `pullpreview/action`.
- `agent-browser` installed.
- Repo label `pullpreview` exists.
- Workflow on base branch supports comment and deployment lifecycle.

## Create demo PR

Run helper script:

```bash
./skills/pullpreview-demo-flow/scripts/create_demo_pr.sh
```

Script output includes:

- `PR_URL=...`
- `PR_NUMBER=...`
- `BRANCH=...`

The script always creates a PR with title `Auto-deploy app with PullPreview`.

## Capture screenshots

Use:

```bash
export REPO="pullpreview/action"
export PR_NUMBER="<from-script>"
export BRANCH="<from-script>"
export SCREEN_DIR="docs/demo-flow-screenshots"
mkdir -p "${SCREEN_DIR}"
```

### 1) PR opened with label

```bash
agent-browser --session pp-demo open "https://github.com/${REPO}/pull/${PR_NUMBER}"
agent-browser --session pp-demo wait --load networkidle
agent-browser --session pp-demo screenshot "${SCREEN_DIR}/01-pr-open-with-label.png"
```

### 2) Deploying PR comment (must scroll first)

```bash
agent-browser --session pp-demo open "https://github.com/${REPO}/pull/${PR_NUMBER}"
agent-browser --session pp-demo eval "window.scrollTo(0, document.body.scrollHeight)"
agent-browser --session pp-demo wait --text "Deploying action with"
agent-browser --session pp-demo screenshot "${SCREEN_DIR}/02-comment-deploying.png"
```

### 3) Workflow run in progress

```bash
RUN_URL="$(gh run list --repo "${REPO}" --branch "${BRANCH}" --workflow pullpreview --limit 1 --json url | jq -r '.[0].url')"
agent-browser --session pp-demo open "${RUN_URL}"
agent-browser --session pp-demo wait --text "in progress"
agent-browser --session pp-demo screenshot "${SCREEN_DIR}/03-action-running.png"
```

### 4) Successful deploy with "View deployment"

Wait for completion:

```bash
RUN_ID="$(echo "${RUN_URL}" | awk -F/ '{print $NF}')"
gh run watch "${RUN_ID}" --repo "${REPO}"
```

Then capture:

```bash
agent-browser --session pp-demo open "https://github.com/${REPO}/pull/${PR_NUMBER}"
agent-browser --session pp-demo wait --text "View deployment"
agent-browser --session pp-demo screenshot "${SCREEN_DIR}/04-view-deployment-button.png"
```

### 5) Remove label and capture unlabeled PR

```bash
gh pr edit "${PR_NUMBER}" --repo "${REPO}" --remove-label pullpreview
agent-browser --session pp-demo open "https://github.com/${REPO}/pull/${PR_NUMBER}"
agent-browser --session pp-demo wait --load networkidle
agent-browser --session pp-demo screenshot "${SCREEN_DIR}/05-unlabelled-pr.png"
```

### 6) Destroyed PR comment (must scroll first)

```bash
agent-browser --session pp-demo open "https://github.com/${REPO}/pull/${PR_NUMBER}"
agent-browser --session pp-demo eval "window.scrollTo(0, document.body.scrollHeight)"
agent-browser --session pp-demo wait --text "Preview destroyed"
agent-browser --session pp-demo screenshot "${SCREEN_DIR}/06-comment-destroyed.png"
```

## Verification checklist

- PR title is `Auto-deploy app with PullPreview`.
- Deploy comment shows pending then success.
- "View deployment" is visible after success.
- Destroy comment shows after label removal.
- Comment screenshots show the comment body in viewport (not off-screen).
