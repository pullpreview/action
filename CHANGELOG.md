## master

## v5.6.0

- Strip admin values
- Reserve 8 chars for user subdomains
- Allow to override some of the compose options
- Fix environment deletion not accessible by integration
- Clear outdated environments

## v5.5.0

- Allow to pass "@collaborators/push" as admins

## v5.4.0

- Use docker v2 `--wait` option

## v5.3.0

- Switch to Amazon Linux 2023 to fix issue with outdated OpenSSH server

## v5.2.5

- Add possibility to auto-stop deploymnents after a while (#48)
- Add concurrency to workflow
- Fix tags (#47)
- Fix bundle selection

## v5.2.4

* Pin to specific ruby image because most recent ruby ships with openssh v9, which is incompatible with openssh v7 on the AWS AMIs.

## v5.2.3

* Update docker-compose to v2.18.1

## v5.2.2

* Update docker-compose to v2.17.3

## v5.2.1

* Fix deletion of instance when using deployment variants
* Allow to pass label as input
* Fix status messages when multi envs
* Ignore errors when removing absent labels
* Additional workflow for testing multi envs

## v5.2.0

- Allow `deployment_variant` config, so that one can launch multiple preview deployments per pull request.
- Refactor code to allow for additional providers.

## v5.1.6 - updated on 20230304

- Update dependency
- Send basic telemetry

## v5.1.5 - updated on 20230110

- Switch to using amazon linux 2 since the old blueprint became unavailable (#34)

## v5.1.4 - updated on 20221108

- Fix pr_push event

## v5.1.3 - updated on 20221104

- Return :ignored action when no other action can be guessed.
- Update system packages before running docker-compose.
- Prune volumes before starting.
- Add automated cleanup for dangling resources, to be launched as a scheduled job.

## v5.1.2 - updated on 20211201

- Fix issue with alpine ruby no longer having the correct ciphers for connecting to lightsail instances.
- Properly handle `repository_dispatch` events.
- Add support for private registries, e.g. `registries: docker://${{ secrets.GHCR_PAT }}@ghcr.io` in workflow file.

## v5.1.1 - 20210218

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
