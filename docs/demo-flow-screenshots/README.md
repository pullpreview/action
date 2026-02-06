# PullPreview Demo Flow Capture

This folder contains screenshots captured against PR [#114](https://github.com/pullpreview/action/pull/114) on branch `codex/go-context`.

1. `01-pr-open-with-label.png` — PR opened with `pullpreview` label applied.
2. `02-comment-deploying.png` — PullPreview PR comment while deployment is in progress.
3. `03-action-running.png` — GitHub Actions workflow run in progress.
4. `04-view-deployment-button.png` — PR UI showing the **View deployment** button after success.
5. `05-unlabelled-pr.png` — PR after removing the `pullpreview` label.
6. `06-comment-destroyed.png` — PullPreview PR comment after environment destruction.

## Quality note

Some earlier captures missed proper PR timeline positioning for comment screenshots.
For future captures, always scroll the PR page before taking comment screenshots so the PullPreview comment card is fully visible in viewport.

Use the skill runbook:

- `skills/pullpreview-demo-flow/SKILL.md`

The demo PR title must always be:

- `Auto-deploy app with PullPreview`
