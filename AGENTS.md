# PullPreview Action — Current Behavior (Ruby)

This repository currently ships a **Docker-based GitHub Action** implemented in Ruby. The action entrypoint is `bin/pullpreview`, which exposes several subcommands and orchestrates AWS Lightsail instances to deploy app code using Docker Compose.

## Action entrypoint
- **Action file:** `action.yml`
- **Runs:** Docker container built from `Dockerfile`
- **Entrypoint:** `bin/pullpreview`
- **Main command:** `github-sync` (used by the action)
- **Outputs:** `url`, `host`, `username` (written to `GITHUB_OUTPUT`)

## Commands
### `pullpreview up path/to/app`
Creates or reuses a Lightsail instance, copies application code, and starts Docker Compose.

Key behavior:
- Tars the app directory (excluding `.git`) into `/tmp/app.tar.gz`. If `app_path` is a `http(s)` URL, it is cloned into `/tmp/app` first.
- Launches or restores a Lightsail instance (name is required) and waits for SSH readiness.
- Sets up SSH access for listed GitHub users by fetching their public keys.
- Uploads an update script and a pre-script to the instance.
- Uploads the app tarball and runs the update script remotely to deploy.
- Prints connection instructions and URL. If running in GitHub Actions, writes outputs to `GITHUB_OUTPUT`.

### `pullpreview down --name <instance>`
- Destroys a Lightsail instance by name.

### `pullpreview github-sync path/to/app`
- Reads GitHub Action environment (`GITHUB_EVENT_NAME`, `GITHUB_EVENT_PATH`, `GITHUB_REPOSITORY`, `GITHUB_REF`, etc.).
- Determines action based on event type and PR label state (`pullpreview` by default).
- Handles:
  - PR labeled/opened/reopened → deploy
  - PR synchronize/push with label → redeploy
  - PR unlabeled/closed → destroy and remove label
  - Push to always-on branches → deploy
  - Schedule → cleanup dangling deployments and environments
- Posts GitHub commit statuses and deployment statuses.
- Runs license check against `https://app.pullpreview.com/licenses/check`.
- Performs cleanup of dangling labels and orphaned GitHub environments.

### `pullpreview list org/repo`
- Lists Lightsail instances tagged for the given repo/org.

### `pullpreview console`
- Opens a Ruby REPL (debug only).

## Core logic
### Instance provisioning
- Provider: Lightsail (`PULLPREVIEW_PROVIDER=lightsail`).
- Uses AWS SDK to create or restore instance from latest snapshot.
- Sets firewall rules for ports (`--ports` plus `22` always open).
- Installs Docker and Docker Compose via user-data boot script.
- Marks readiness by creating `/etc/pullpreview/ready`.

### Update script
- `data/update_script.sh.erb` rendered with instance and compose settings.
- Sets env file `/etc/pullpreview/env` with `PULLPREVIEW_PUBLIC_DNS`, `PULLPREVIEW_PUBLIC_IP`, `PULLPREVIEW_URL`, `PULLPREVIEW_FIRST_RUN`, and `COMPOSE_FILE`.
- Replaces `/app` with new tarball content.
- Runs an optional pre-script and `docker-compose pull` + `docker-compose up -d`.
- Logs last 1000 lines of Docker Compose output.

### DNS naming
- Constructs public DNS based on instance IP, `dns` suffix (default `my.preview.run`), subdomain, and `PULLPREVIEW_MAX_DOMAIN_LENGTH` (default 62).
- Reserves 8 chars for user subdomain if default length is used.

### Inputs (as used by action)
- `app_path`, `admins`, `always_on`, `dns`, `ports`, `cidrs`, `default_port`, `compose_files`, `compose_options`, `instance_type`, `deployment_variant`, `registries`, `pre_script`, `ttl`, `github_token`, `license`, `provider`, `max_domain_length`.

## External dependencies
- AWS Lightsail API
- GitHub API (statuses, deployments, environments, labels)
- License server at `https://app.pullpreview.com`

## Go Migration Note
- During migration, execute Go commands through `mise` (for consistent toolchain/version), e.g. `mise exec -- go test ./...`.

## Go Migration Snapshot (Current Branch)
- Go CLI now exists at `cmd/pullpreview` with subcommands: `up`, `down`, `list`, `github-sync`.
- Action runtime now builds and runs the Go binary via `Dockerfile` (Ruby runtime replaced for action execution path).
- Local deploy command works with `mise`:
  - `AWS_PROFILE=runs-on-dev AWS_REGION=us-east-1 mise exec -- go run ./cmd/pullpreview up examples/example-app`
- Test deploys can be cleaned up with:
  - `AWS_PROFILE=runs-on-dev AWS_REGION=us-east-1 mise exec -- go run ./cmd/pullpreview down --name <instance-name>`
- Added unit tests and fixture-driven GitHub sync tests under:
  - `cmd/pullpreview/*_test.go`
  - `internal/**/**_test.go`
