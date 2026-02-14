# <img width="25" height="25" alt="pullpreview" src="https://github.com/user-attachments/assets/3aeb0f94-cac5-44b2-9f8e-abdb12be9cfe" /> PullPreview

A GitHub Action that starts live environments for your pull requests.

[![pullpreview](https://github.com/pullpreview/action/actions/workflows/pullpreview.yml/badge.svg)](https://github.com/pullpreview/action/actions/workflows/pullpreview.yml)
<a href="https://news.ycombinator.com/item?id=23221471"><img src="https://img.shields.io/badge/Hacker%20News-83-%23FF6600" alt="Hacker News"></a>

## Spin environments in one click

Once installed in your repository, this action is triggered any time a change
is made to Pull Requests labelled with the `pullpreview` label.

When triggered, it will:

1. Check out the repository code
2. Provision a preview instance (Lightsail by default, or Hetzner with `provider: hetzner`), with docker and docker-compose set up
3. Continuously deploy the specified pull requests using your docker-compose file(s)
4. Report the preview instance URL in the GitHub UI

It is designed to be the **no-nonsense, cheap, and secure** alternative to
services that require access to your code and force your app to fit within
their specific deployment system and/or require a specific config file.

### Step 1 — Add the `pullpreview` label

Adding the label triggers the deployment. A PR comment appears immediately with the status set to pending.

<img src="img/01-label-added.png">

### Step 2 — Instance is provisioned

PullPreview creates (or restores) a preview instance and waits for SSH access.

<img src="img/02-deploying.png">

### Step 3 — Preview environment is live

The PR comment is updated with a live preview URL.

<img src="img/03-deploy-successful.png">

### Step 4 — Remove the label to destroy the preview

When the label is removed, the preview environment is automatically destroyed.

<img src="img/04-preview-destroyed.png">

## Useful for the entire team

- **Product Owners**: Interact with a new feature as it's built, give valuable feedback earlier, reduce wasted development time.
- **Developers**: Show your work in progress, find bugs early, deliver the right feature.
- **Ops**: Concentrate on high value tasks, not maintaining staging environments.
- **CTOs**: Don't let your code run on third-party servers: your code always stays private on either GitHub's or your servers.

## Features

Preview environments that:

- work with your **existing tooling**: If your app can be started with
  docker-compose, it can be deployed to preview environments with PullPreview.

- can be **started and destroyed easily**: You can manage preview environments
  by adding or removing the `pullpreview` label on your Pull Requests.

- are **cheap too run**: Preview environments are launched on AWS Lightsail
  instances, which are both very cheap (10USD per month, proratized to the
  duration of the PR), and all costs included (bandwith, storage, etc.)

- take the **privacy** of your code seriously: The workflow happens all within
  a GitHub Action, which means your code never leaves GitHub or your Lightsail
  instances.

- make the preview URL **easy to find** for your reviewers: Marker-based PR
  comments are updated with live preview state and URL.

- **persist state** across deploys: Any state stored in docker volumes (e.g.
  database data) will be persisted across deploys, making the life of reviewers
  easier.

- can **auto-enable HTTPS** with Let's Encrypt: Set `proxy_tls` to inject a
  Caddy reverse proxy that terminates TLS and forwards traffic to your service.
  This switches the preview URL to HTTPS on port `443`.

- are **easy to troubleshoot**: You can give specific GitHub users the
  authorization to SSH into the preview instance (with sudo privileges) to
  further troubleshoot any issue. The SSH keys that they use to push to GitHub
  will automatically be installed on the preview servers.

- are **integrated into the GitHub UI**: Logs for each deployment run are
  available within the Actions section, and direct links to the preview
  environments are available in PullPreview PR comments.

## Installation & Usage

- [Getting Started](https://github.com/pullpreview/action/wiki/Getting-Started)
- Action [Inputs](https://github.com/pullpreview/action/wiki/Inputs) / [Outputs](https://github.com/pullpreview/action/wiki/Outputs)
- Handling [Seed Data](https://github.com/pullpreview/action/wiki/Seed-Data)
- [Workflow Examples](https://github.com/pullpreview/action/wiki/Workflow-Examples)
- [FAQ](https://github.com/pullpreview/action/wiki/FAQ)

&rarr; Please see the [wiki](https://github.com/pullpreview/action/wiki) for the full documentation.

## Action Inputs (v6)

All supported `with:` inputs from `action.yml`:

| Input | Default | Description |
| --- | --- | --- |
| `app_path` | `.` | Path to your application containing Docker Compose files (relative to `${{ github.workspace }}`). |
| `dns` | `my.preview.run` | DNS suffix used for generated preview hostnames. Built-in alternatives: `rev1.click` through `rev9.click` (see note below). |
| `max_domain_length` | `62` | Maximum generated FQDN length (cannot exceed 62 for Let's Encrypt). |
| `label` | `pullpreview` | Label that triggers preview deployments. |
| `github_token` | `${{ github.token }}` | GitHub token used for labels/comments/collaborator/key API operations. |
| `admins` | `@collaborators/push` | Comma-separated GitHub users whose SSH keys are installed on preview instances. |
| `ports` | `80/tcp,443/tcp` | Firewall ports to expose publicly (SSH `22` is always open). |
| `cidrs` | `0.0.0.0/0` | Allowed source CIDR ranges for exposed ports. |
| `default_port` | `80` | Port used to build the preview URL output. |
| `compose_files` | `docker-compose.yml` | Comma-separated Compose files passed to deploy. |
| `compose_options` | `--build` | Additional options appended to `docker compose up`. |
| `license` | `""` | PullPreview license key. |
| `instance_type` | `small` | Provider-specific instance size (`small` for Lightsail, `cpx21` for Hetzner). |
| `region` | `` | Optional provider region/datacenter override (`AWS_REGION`/Hetzner location). If empty, provider defaults apply. |
| `image` | `ubuntu-24.04` | Instance image for Hetzner (provider-specific) and ignored for AWS. |
| `deployment_variant` | `""` | Optional short suffix to run multiple preview environments per PR (max 4 chars). |
| `provider` | `lightsail` | Cloud provider (`lightsail`, `hetzner`). |
| `registries` | `""` | Private registry credentials, e.g. `docker://user:password@ghcr.io`. |
| `proxy_tls` | `""` | Automatic HTTPS forwarding with Caddy + Let's Encrypt (`service:port`, e.g. `web:80`). |
| `pre_script` | `""` | Path to a local shell script (relative to `app_path`) executed inline over SSH before compose deploy (should be self-contained). |
| `ttl` | `infinite` | Maximum deployment lifetime (e.g. `10h`, `5d`, `infinite`). |

Notes:

- `proxy_tls` forces URL/output/comment links to HTTPS on port `443`, injects a Caddy proxy service, and suppresses firewall exposure for port `80`. **When using `proxy_tls`, it is strongly recommended to set `dns` to a [custom domain](https://github.com/pullpreview/action/wiki/Using-a-custom-domain) or one of the built-in `revN.click` alternatives** to avoid hitting shared Let's Encrypt rate limits on `my.preview.run`.
- `admins: "@collaborators/push"` uses GitHub API collaborators with push permission (first page, up to 100 users; warning is logged if more exist).
- SSH key fetches are cached between runs in the action cache.
- For Hetzner, configure credentials and defaults via action inputs and environment: `HCLOUD_TOKEN` (required), `HETZNER_CA_KEY` (required), optional `region` and `image` (`region` defaults to `nbg1`, `image` defaults to `ubuntu-24.04`). `instance_type` defaults to `cpx21` when provider is Hetzner.
- `HETZNER_CA_KEY` must be an SSH private key (RSA or Ed25519) for the instance-access CA. PullPreview signs a per-run ephemeral login key with this CA key and uses SSH certificates (`...-cert.pub`) instead of reusing a persistent private key across runs.
- Generate a CA key once for your repository secret:

```bash
ssh-keygen -t rsa -b 3072 -m PEM -N "" -f hetzner_ca_key
```

- **Let's Encrypt rate limits**: Let's Encrypt allows a maximum of [50 certificates per registered domain per week](https://letsencrypt.org/docs/rate-limits/#new-certificates-per-registered-domain). If you use `proxy_tls` and hit this limit on the default `my.preview.run` domain, switch to one of the built-in alternatives: `rev1.click`, `rev2.click`, ... `rev9.click`. Set `dns: rev1.click` in your workflow inputs. You can also use a [custom domain](https://github.com/pullpreview/action/wiki/Using-a-custom-domain).
- For local CLI runs, set `HCLOUD_TOKEN` and `HETZNER_CA_KEY` (for example via `.env`) when using `provider: hetzner` to avoid relying on action inputs.

## Example

Workflow file for pullpreview-driven deployments:

```yaml
# .github/workflows/pullpreview.yml
name: PullPreview
on:
  # the schedule is optional, but helps to make sure no dangling resources are left when GitHub Action does not behave properly
  schedule:
    - cron: "30 2 * * *"
  pull_request:
    types: [labeled, unlabeled, synchronize, closed, reopened]

jobs:
  deploy:
    if: github.event_name == 'schedule' || github.event.label.name == 'pullpreview' || contains(github.event.pull_request.labels.*.name, 'pullpreview')
    runs-on: ubuntu-latest
    timeout-minutes: 30
    steps:
      - uses: actions/checkout@v5
      - uses: pullpreview/action@v6
        with:
          # Those GitHub users will have SSH access to the servers
          admins: crohr,other-github-user
          # Use the cidrs option to restrict access to the live environments to specific IP ranges
          cidrs: "0.0.0.0/0"
          # PullPreview will use those 2 files when running docker-compose up
          compose_files: docker-compose.yml,docker-compose.pullpreview.yml
          # The preview URL will target this port
          default_port: 80
          # Use a 512MB RAM instance type instead of the default 2GB
          instance_type: nano
          # Ports to open on the server
          ports: 80,5432
          # Optional: automatic HTTPS forwarding via Caddy + Let's Encrypt
          proxy_tls: web:80
        env:
          AWS_ACCESS_KEY_ID: "${{ secrets.AWS_ACCESS_KEY_ID }}"
          AWS_SECRET_ACCESS_KEY: "${{ secrets.AWS_SECRET_ACCESS_KEY }}"
          AWS_REGION: "us-east-1"
```

## Hetzner example

```yaml
# .github/workflows/pullpreview-hetzner.yml
name: PullPreview
on:
  schedule:
    - cron: "30 */4 * * *"
  pull_request:
    types: [labeled, unlabeled, synchronize, closed, reopened, opened]

jobs:
  deploy_hetzner:
    runs-on: ubuntu-slim
    if: github.event_name == 'schedule' || github.event.label.name == 'pullpreview' || contains(github.event.pull_request.labels.*.name, 'pullpreview')
    timeout-minutes: 30
    steps:
      - uses: actions/checkout@v5
      - uses: pullpreview/action@v6
        with:
          admins: "@collaborators/push"
          app_path: ./examples/workflow-smoke
          provider: hetzner
          # optional Hetzner runtime options
          instance_type: cpx21
          image: ubuntu-24.04
          region: nbg1
          dns: preview.chunk.io
          max_domain_length: 30
          # Open HTTPS preview URL through Caddy + Let's Encrypt.
          proxy_tls: web:8080
          ttl: 1h
        env:
          HCLOUD_TOKEN: "${{ secrets.HCLOUD_TOKEN }}"
          HETZNER_CA_KEY: "${{ secrets.HETZNER_CA_KEY }}"

```

## CLI usage (installed binary)

Pull the released CLI binary from GitHub Releases, install it in your PATH, then use:

```bash
pullpreview up examples/workflow-smoke --name pullpreview-local-smoke
pullpreview list
pullpreview list my-org/my-repo
pullpreview down --name pullpreview-local-smoke
```

For installation details and local validation instructions (including Hetzner env setup), see [CLI wiki page](https://github.com/pullpreview/action/wiki/CLI).

## Is this free?

The code for this Action is completely open source, and licensed under the
Prosperity Public License (see LICENSE).

If you are a non-profit individual, then it is free to run (in that case, please tell me
so and/or pass the word around!).

In all other cases, you must buy a license. More details on [pullpreview.com](https://pullpreview.com).

Thanks for reading until the end!
