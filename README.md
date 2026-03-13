# <img width="25" height="25" alt="pullpreview" src="https://github.com/user-attachments/assets/3aeb0f94-cac5-44b2-9f8e-abdb12be9cfe" /> PullPreview

A GitHub Action for running live preview environments for pull requests in your own cloud account. Add a label to a PR, let PullPreview provision or restore an instance, deploy your app with your existing setup, and report the preview URL back into GitHub.

[![pullpreview](https://github.com/pullpreview/action/actions/workflows/pullpreview.yml/badge.svg)](https://github.com/pullpreview/action/actions/workflows/pullpreview.yml)
<a href="https://news.ycombinator.com/item?id=23221471"><img src="https://img.shields.io/badge/Hacker%20News-83-%23FF6600" alt="Hacker News"></a>

## Preview flow

### Step 1 - Add the `pullpreview` label

Adding the label triggers the deployment. A PR comment appears immediately with the status set to pending.

<img src="img/01-label-added.png">

### Step 2 - Instance is provisioned

PullPreview creates a preview instance and waits for SSH access.

<img src="img/02-deploying.png">

### Step 3 - Preview environment is live

The PR comment is updated with a live preview URL.

<img src="img/03-deploy-successful.png">

### Step 4 - Remove the label to destroy the preview

When the label is removed, the preview environment is automatically destroyed.

<img src="img/04-preview-destroyed.png">

## High-level features

- **Fits existing deployment setups**: Deploy with Docker Compose or Helm instead of adapting your app to a proprietary preview platform.
- **Runs in your own cloud account**: PullPreview supports Lightsail and Hetzner while keeping your code and infrastructure under your control.
- **Reviewer-friendly lifecycle**: Preview creation, redeploys, status comments, and cleanup all stay tied to the pull request workflow in GitHub.
- **Practical to operate**: HTTPS, SSH access, persistent state across redeploys, and troubleshooting workflows are built into the deployment model.

## How to get started

1. Follow the [Getting Started guide](https://github.com/pullpreview/action/wiki/Getting-Started).
2. Choose between [Deployment Targets](https://github.com/pullpreview/action/wiki/Deployment-Targets) for `compose` and `helm`.
3. Pick a provider in [Providers](https://github.com/pullpreview/action/wiki/Providers).
4. Start from one of the [Workflow Examples](https://github.com/pullpreview/action/wiki/Workflow-Examples).

## Documentation

Full documentation lives in the [wiki](https://github.com/pullpreview/action/wiki).

### Getting started

- [Getting Started](https://github.com/pullpreview/action/wiki/Getting-Started)
- [Migrating from v5 to v6](https://github.com/pullpreview/action/wiki/Migrating-from-v5-to-v6)
- [Workflow Examples](https://github.com/pullpreview/action/wiki/Workflow-Examples)

### Choose and configure

- [Deployment Targets](https://github.com/pullpreview/action/wiki/Deployment-Targets)
- [Providers](https://github.com/pullpreview/action/wiki/Providers)
- [Inputs](https://github.com/pullpreview/action/wiki/Inputs)
- [Outputs](https://github.com/pullpreview/action/wiki/Outputs)
- [SSL HTTPS Configuration](https://github.com/pullpreview/action/wiki/SSL-HTTPS-Configuration)
- [Using a custom domain](https://github.com/pullpreview/action/wiki/Using-a-custom-domain)

### Operate and debug

- [Lifecycle](https://github.com/pullpreview/action/wiki/Lifecycle)
- [PR Comments and Job Summary](https://github.com/pullpreview/action/wiki/PR-Comments-and-Job-Summary)
- [CLI](https://github.com/pullpreview/action/wiki/CLI)
- [Troubleshooting](https://github.com/pullpreview/action/wiki/Troubleshooting)
- [FAQ](https://github.com/pullpreview/action/wiki/FAQ)

PullPreview is open source. For licensing details for commercial repositories, see [pullpreview.com](https://pullpreview.com) and the [FAQ](https://github.com/pullpreview/action/wiki/FAQ).
