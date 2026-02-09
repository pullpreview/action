# PullPreview Action — Current Behavior (Go)

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

## Deploy behavior (`up`)
- Launches/restores Lightsail instance and waits for SSH.
- Uploads authorized keys.
- Renders compose config, rewrites relative bind mounts under `app_path` to `/app/...`, and syncs only those bind-mounted local paths to the server via `rsync`.
- Deploys through Docker context to the remote engine.
- Executes `pre_script` inline over SSH before `docker compose up` (script must be self-contained).
- Optional automatic HTTPS proxying via Caddy + Let's Encrypt when `proxy_tls` is set.
  - Format: `service:port` (for example `web:80`).
  - Forces preview URL/output to HTTPS on port `443`.
  - Opens firewall port `443` and suppresses firewall exposure for port `80`.
  - Injects `pullpreview-proxy` service unless host port `443` is already published (then it logs a warning and skips proxy injection).
- Emits periodic heartbeat logs with:
  - preview URL
  - SSH command (`ssh user@ip`)
  - authorized users info
  - key-upload confirmation

## GitHub sync behavior (`github-sync`)
- Handles PR labeled/opened/reopened/synchronize/unlabeled/closed events.
- Handles push events for `always_on` branches.
- Handles scheduled cleanup of dangling labeled preview instances.
- Updates marker-based PR status comments.
- For `admins: "@collaborators/push"`:
  - loads collaborators from GitHub REST API with `affiliation=all` + `permission=push`
  - uses only the first page (up to 100 users)
  - emits a warning if additional pages exist
  - fetches each admin's SSH public keys via GitHub API and forwards keys to the instance
  - uses local key cache directory (`PULLPREVIEW_SSH_KEYS_CACHE_DIR`) to avoid refetching keys across runs
- Always posts/updates marker-based PR status comments per environment/job with building/ready/error/destroyed state and preview URL.

## Action inputs/outputs
- Existing inputs are preserved.
- Additional input:
  - `proxy_tls` (`service:port`, default empty)
- Outputs:
  - `url`
  - `host`
  - `username`

## Key directories
- `cmd/pullpreview`: CLI
- `internal/pullpreview`: core orchestration
- `internal/providers/lightsail`: Lightsail provider
- `internal/github`: GitHub API wrapper
- `internal/license`: license check client
- `dist/`: bundled Linux amd64 binary used by the action

## Repo-local skill
- `skills/pullpreview-demo-flow/SKILL.md`: repeatable end-to-end demo capture workflow (PR open/label/deploy/view deployment/unlabel/destroy) with strict screenshot requirements and fixed demo PR title.
