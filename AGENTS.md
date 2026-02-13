# PullPreview Action — AGENTS

This repository ships a GitHub Action implemented in Go.

## Runtime
- Action definition: `action.yml`
- Action type: `composite`
- Runtime binary: prebuilt amd64 Linux artifact in `dist/`
- No Docker image build is required during action execution.

## Go Tooling
- Go commands should be run via `mise` for toolchain consistency.
- Examples:
  - `mise exec -- go test ./...`
  - `mise exec -- go run ./cmd/pullpreview up examples/example-app`
  - `make dist`
- Dist workflow:
  - Commit source changes first.
  - Run `make dist` afterwards.
  - `make dist` auto-commits the updated bundled binary with a standard commit message.
  - Before merging, `make rewrite` can rewrite the current branch and drop dist-only auto-commits (force-push required).

## CLI
Entrypoint source is `cmd/pullpreview/main.go`.

Supported commands:
- `pullpreview up path/to/app`
- `pullpreview down --name <instance>`
- `pullpreview list org/repo`
- `pullpreview github-sync path/to/app`

## Providers
- Default provider: `lightsail`.
- Supported providers: `lightsail`, `hetzner`.
- Provider discovery is via `internal/providers` registrations.
- New Hetzner provider is implemented in `internal/providers/hetzner`.
- `providers` package uses typed environment config parsing and factory registration.

## Deploy behavior (`up`)
- Launches/restores an instance via provider abstraction.
- Waits for SSH and runs provider-generated user-data.
- Uploads authorized SSH keys.
- Renders compose config, rewrites relative bind mounts under `app_path` to `/app/...`, and syncs only detected bind-mounted local paths via `rsync`.
- Deploys through Docker context on remote engine.
- Executes `pre_script` inline over SSH before `docker compose up`.
- Optional HTTPS via `proxy_tls` injects a Caddy sidecar and adjusts logging/port exposure.
- Emits heartbeat logs with:
  - preview URL
  - SSH command (`ssh user@ip`)
  - authorized users info
  - key upload status

## GitHub sync behavior (`github-sync`)
- Handles PR labeled/opened/reopened/synchronize/unlabeled/closed events.
- Handles push events for `always_on` branches.
- Handles scheduled cleanup of dangling labeled preview instances.
- Updates marker-based PR status comments.
- For `admins: "@collaborators/push"`:
  - loads collaborators from GitHub REST API with `affiliation=all` + `permission=push`
  - uses only the first page (up to 100 users)
  - emits warning if additional pages exist
  - fetches each admin's SSH public keys via GitHub API and forwards keys to the instance
  - uses local key cache directory (`PULLPREVIEW_SSH_KEYS_CACHE_DIR`) to avoid refetching keys across runs
- Always posts/updates marker-based PR status comments per environment/job with building/ready/error/destroyed state and preview URL.

## Action inputs/outputs
- Existing inputs are preserved.
- Provider-related inputs now include only:
  - `provider`
- Existing additional input:
  - `proxy_tls` (`service:port`, default empty)
- Hetzner uses environment variables for provider config (`HCLOUD_TOKEN` / `HETZNER_API_TOKEN`, `HETZNER_LOCATION`, `HETZNER_IMAGE`, `HETZNER_SERVER_TYPE`, `HETZNER_USERNAME`) so no dedicated action inputs are required.
- Outputs:
  - `url`
  - `host`
  - `username`

## Hetzner implementation notes
- File paths:
  - `internal/providers/hetzner/hetzner.go`
  - provider registration: `internal/providers/hetzner`
  - shared user-data fallback remains in `internal/pullpreview/user_data.go`
  - Hetzner custom user-data in `Provider.BuildUserData`
- Defaults:
  - location: `nbg1`
  - image: `ubuntu-24.04`
  - server type: `cpx21`
  - username: `root`
- SSH keys are cached for re-entry via `PULLPREVIEW_SSH_KEYS_CACHE_DIR`.
- `down` currently accepts both normalized instance names and compose context names (`pullpreview-*`) through normalization in `RunDown`.
- Lifecycle cleanup follows best-effort ordering in provider:
  - recreate missing cache/server state when stale
  - validate SSH, recreate instance if cache/validation fails
  - destroy server before cleanup paths on failure

## Key directories
- `cmd/pullpreview`: CLI
- `internal/pullpreview`: core orchestration
- `internal/providers`: provider registry and concrete providers
- `internal/github`: GitHub API wrapper
- `internal/license`: license check client
- `dist/`: bundled Linux amd64 binary used by the action

## Repo-local skill
- `skills/pullpreview-demo-flow/SKILL.md`: repeatable end-to-end demo capture workflow (PR open/label/deploy/view deployment/unlabel/destroy) with strict screenshot requirements and fixed demo PR title.

## Review status (current branch)
- Live provider validation has been run against Hetzner using `.env` with explicit `HETZNER_LOCATION=ash`, `HETZNER_SERVER_TYPE=cpx21`, `HETZNER_IMAGE=ubuntu-24.04`.
- `up`, `down`, and `list` flows have been exercised.
- Follow-up cleanup items:
  - tighten `RunDown` context-name parser to avoid stripping legitimate names that resemble context suffix format
  - make create-failure cleanup continue best-effort cache/key cleanup if server delete fails
