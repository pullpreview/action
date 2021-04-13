## master

* Properly handle `workflow_dispatch` events.

## v5 - 20210218

* Add support for custom DNS.
* Switch to human-readable subdomains that include the PR title inside.
* Add the `pr_number` to the instance tags.

## v4 - 20201117

* Add support and examples for synchronize, reopened events on PRs.
* Add `PULLPREVIEW_FIRST_RUN` env variable when running Compose.
* Use `ServerAliveInterval=15` SSH option to force SSH heartbeat and avoid connection closing if remote server doesn't send any output for a while.
* Always launch an instance in the first AZ found, instead of taking one at random.

## v3 - 20200509

* Support for source IP filtering with the `cdrs` option

## v3 - 20200507

* Support for pretty URLs in `*.my.pullpreview.com`
* Support for automatic Let's Encrypt TLS certificate generation

## v3 - 20200504

* Support for specifying instance type

## v3 - 20200428

* Support for always on branches

## v3 - 20200421

* Support for GitHub deployments API
* Initial release with PR support
