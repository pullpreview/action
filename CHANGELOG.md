## master

## v5 - updated on 20221108

- Fix pr_push event

## v5 - updated on 20221104

- Return :ignored action when no other action can be guessed.
- Update system packages before running docker-compose.
- Prune volumes before starting.
- Add automated cleanup for dangling resources, to be launched as a scheduled job.

## v5 - updated on 20211201

- Fix issue with alpine ruby no longer having the correct ciphers for connecting to lightsail instances.
- Properly handle `repository_dispatch` events.
- Add support for private registries, e.g. `registries: docker://${{ secrets.GHCR_PAT }}@ghcr.io` in workflow file.

## v5 - 20210218

- Add support for custom DNS.
- Switch to human-readable subdomains that include the PR title inside.
- Add the `pr_number` to the instance tags.

## v4 - 20201117

- Add support and examples for synchronize, reopened events on PRs.
- Add `PULLPREVIEW_FIRST_RUN` env variable when running Compose.
- Use `ServerAliveInterval=15` SSH option to force SSH heartbeat and avoid connection closing if remote server doesn't send any output for a while.
- Always launch an instance in the first AZ found, instead of taking one at random.

## v3 - 20200509

- Support for source IP filtering with the `cdrs` option

## v3 - 20200507

- Support for pretty URLs in `*.my.pullpreview.com`
- Support for automatic Let's Encrypt TLS certificate generation

## v3 - 20200504

- Support for specifying instance type

## v3 - 20200428

- Support for always on branches

## v3 - 20200421

- Support for GitHub deployments API
- Initial release with PR support
