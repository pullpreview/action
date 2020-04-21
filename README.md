# PullPreview Action

This action allows you to automatically deploy your pull requests on preview environments, synchronized with every push.

The workflow is very simple:

1. Add the PullPreview action to a workflow file in your repository (see [example][example]).
2. Add the required `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` secrets in your repository settings.
3. Open a PR, and add the `pullpreview` label on it.
4. After a while, a server will have been provisioned, and the PR status is updated with a link to your preview environment.

[example]: https://github.com/pullpreview/pullpreview-example-rails-app/blob/master/.github/workflows/pullpreview.yml

Note: this will currently spawn 2GB lightsail instances ($10/month), prorated to the duration of the preview.

## How it works

Assuming your application has a working docker-compose file checked in, the action will checkout your repository, launch a server if not already existing, then uploads your code to that server and run `docker-compose up` on it.

It will also set up the server with the public keys for any GitHub nicknames you provide, allowing you to directly SSH into the server and perform any debugging you may need.

## Inputs

### `always_on`

By default PullPreview only deploys Pull Requests. With this setting you can also request specific branches to always be deployed. Defaults to none.

### `admins`

Logins of GitHub users that will have their SSH key installed on the instance, comma-separated. Defaults to none, which means you won't have SSH access. We suggest you set at least one GitHub login (e.g. `crohr`).

### `ports`

Ports to open for external access on the preview server (port 22 is always open), comma-separated. Defaults to `80/tcp,443/tcp,1000-10000/tcp`.

### `default_port`

The port to use when building the preview URL. Defaults to `80`.

### `compose_files`

Compose files to use when running docker-compose up, comma-separated. Defaults to `docker-compose.yml`.

## Outputs

### `url`

The URL of the application on the preview server.

### `host`

The hostname or IP address of the preview server.

### `username`

The username that can be used to SSH into the preview server.

## Example workflow file

```
# .github/workflows/pullpreview.yml
name: PullPreview
on:
  push:
  pull_request:
    types: [labeled, unlabeled, closed]

jobs:
  deploy:
    name: Deploy
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v2
    - uses: pullpreview/action@master
      with:
        admins: crohr
        always_on: master
      env:
        AWS_ACCESS_KEY_ID: "${{ secrets.AWS_ACCESS_KEY_ID }}"
        AWS_SECRET_ACCESS_KEY: "${{ secrets.AWS_SECRET_ACCESS_KEY }}"
        AWS_REGION=us-east-1
```

## Is this free?

If you are using this on a personal GitHub account for non-commercial purposes, it is free to run.

If you are using this on an team GitHub account, there will be a license fee to pay once at some point.
