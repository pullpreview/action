# Go Migration Plan

## Goals
- Replace the Ruby action with a Go codebase while preserving current behavior.
- Provide a local CLI that can deploy to Lightsail: `AWS_PROFILE=runs-on-dev go run ./cmd/pullpreview up path/to/example-app`.
- Add strong unit test coverage and end-to-end tests around GitHub event handling and deployment workflows.
- Keep GitHub Action inputs/outputs compatible.

## Go Environment Note
- Run all Go commands via `mise` to ensure the expected toolchain/version is used.
- Examples:
  - `mise exec -- go test ./...`
  - `mise exec -- go run ./cmd/pullpreview up path/to/example-app`

## Phase 1 — Baseline mapping
1. Inventory current behavior (done in `AGENTS.md`).
2. Identify compatibility requirements:
   - CLI subcommands and flags
   - GitHub event handling and cleanup logic
   - Lightsail provisioning, DNS naming, firewall rules
   - Update/pre-scripts and Docker Compose behavior
   - License checks and GitHub status/deployment updates

## Phase 2 — Go scaffolding
1. Create Go module and `cmd/pullpreview` CLI with subcommands:
   - `up`, `down`, `github-sync`, `list`
2. Implement configuration parsing:
   - Support the current action inputs (comma-separated arrays).
   - Provide sensible defaults (notably instance type `small`).
3. Preserve environment-based configuration:
   - `GITHUB_*`, `AWS_*`, `PULLPREVIEW_*`.

## Phase 3 — Core implementation
1. **Instance & Provider layer**
   - Interface for providers and Lightsail implementation via AWS SDK v2.
   - Port logic for snapshots, firewall rules, access details, and instance listing.
2. **Deployment logic**
   - Tar application contents (excluding `.git`).
   - Render and upload update and pre-scripts.
   - Execute remote update via SSH.
   - Emit outputs to `GITHUB_OUTPUT`.
3. **GitHub integration**
   - Parse event payloads and compute desired action.
   - Set commit statuses and deployment statuses.
   - Cleanup dangling deployments and orphaned environments.
4. **License check**
   - Port the license server check and behavior.

## Phase 4 — Tests
1. **Unit tests**
   - Name normalization and DNS calculations.
   - TTL parsing and expiry logic.
   - GitHub event action resolution with fixtures.
   - Tag generation and deployment naming.
2. **End-to-end tests**
   - Run `github-sync` against fixtures with fake providers and fake GitHub API.
   - Verify outputs, status transitions, and cleanup behavior.
3. **Optional live test (manual)**
   - Run: `AWS_PROFILE=runs-on-dev go run ./cmd/pullpreview up path/to/example-app`.
   - Validate Lightsail instance, SSH access, and deployed URL.

## Phase 5 — Action packaging
1. Replace Ruby container with a Go-based container or a composite action using a bundled Go binary.
2. Keep `action.yml` input/output compatibility.
3. Ensure workflows still trigger via `github-sync` command.

## Deliverables
- Go CLI and supporting packages.
- Updated action packaging.
- Unit and E2E tests.
- Documentation updates as needed.

## Progress Status
- `Phase 1` complete: Ruby behavior captured in `AGENTS.md`.
- `Phase 2` complete: Go module and CLI scaffolding in place.
- `Phase 3` complete for baseline parity:
  - Lightsail provider, deployment flow, GitHub sync, and license check are implemented in Go.
- `Phase 4` in progress with strong coverage improvements:
  - `internal/github`: `98.5%`
  - `internal/license`: `90.9%`
  - `internal/pullpreview`: `55.8%`
  - `internal/providers/lightsail`: `20.1%` (network-heavy package; helper logic covered)
- `Phase 5` baseline complete: action container builds/runs Go binary.

## Validation Notes
- All Go tests pass with `mise exec -- go test ./...`.
- Coverage command: `mise exec -- go test -cover ./...`.
- Live deploy verified with public example app:
  - `AWS_PROFILE=runs-on-dev AWS_REGION=us-east-1 mise exec -- go run ./cmd/pullpreview up examples/example-app`
- Live cleanup verified:
  - `AWS_PROFILE=runs-on-dev AWS_REGION=us-east-1 mise exec -- go run ./cmd/pullpreview down --name local-example-app`
