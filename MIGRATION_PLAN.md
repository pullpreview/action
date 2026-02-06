# Go Migration Plan (Completed)

## Objectives
- Replace Ruby runtime with Go while preserving functional behavior.
- Keep action inputs/outputs compatible.
- Provide a local operational CLI (`up/down/list/github-sync`).
- Improve reliability with unit and fixture-driven integration tests.

## Current Architecture
- Go CLI: `cmd/pullpreview`
- Core logic: `internal/pullpreview`
- Provider: `internal/providers/lightsail`
- GitHub API wrapper: `internal/github`
- License client: `internal/license`
- Action runtime: `action.yml` composite action invoking bundled binaries from `dist/`

## Packaging Strategy
- Runtime binary is committed in-repo (`dist/pullpreview-linux-amd64`).
- Binary is compressed with `upx` in `make dist`.
- No Docker image build is required during action execution.
- Release/update command:
  - `make dist`
  - source changes must be committed before running `make dist`
  - `make dist` auto-commits the refreshed binary with a standard commit message

## Completed Migration Work
1. Ported command behaviors:
   - `up`, `down`, `list`, `github-sync`
2. Ported provisioning/deploy flow:
   - Lightsail launch/restore, firewall setup, SSH readiness, tarball deploy, update/pre-script execution
3. Ported GitHub orchestration:
   - event-driven deploy/destroy/cleanup logic
   - PR status comments + workflow checks
4. Added PR comment support:
   - always enabled
   - building/ready/error/destroyed comment updates
5. Added deploy heartbeat logging:
   - includes URL, SSH command, authorized users, and key-upload confirmation
6. Removed obsolete runtime artifacts:
   - Ruby entrypoint/code, Gem files, Docker action runtime files

## Validation
- `mise exec -- go test ./...`
- `mise exec -- go test -cover ./...`
- `make dist`
- Live smoke deploy/down:
  - `AWS_PROFILE=runs-on-dev AWS_REGION=us-east-1 mise exec -- go run ./cmd/pullpreview up examples/example-app`
  - `AWS_PROFILE=runs-on-dev AWS_REGION=us-east-1 mise exec -- go run ./cmd/pullpreview down --name <instance>`

## Tooling Note
- Use `mise` for Go commands in this repo.
