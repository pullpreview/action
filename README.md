# PullPreview Action

Automatic **preview environments**, synchronized on every code change.

* For Product Owners: Interact with a new feature as it's built, give valuable feedback earlier, reduce wasted development time.
* For Developers: Show your work in progress, find bugs early, deliver the right feature.
* For Ops: No more temporary staging environments to provision and maintain. Environments are automatically started and destroyed when needed.
* For CTOs: It's cheap! The code stays on your and GitHub's servers at all times.

## Trigger deployments directly from your Pull Requests

Once installed in your repository, this action is triggered any time a change
is made to Pull Requests labelled with the `pullpreview` label, or one of the
*always-on* branches.

Once triggered it will:

1. Check out the repository code
2. Provision a cheap AWS Lightsail instance, with docker and docker-compose set up
3. Continuously deploy selected Pull Requests and Branches, using the specified docker-compose file(s)
4. Report the preview instance URL in the GitHub UI

It is designed to be the **no-nonsense** alternative to services that require
your app to either fit within their specific system and config files, or try to
force kubernetes on you while all you wanted was to deploy your app for review
and go on with your life.

## Features

Preview environments that:

* work with your **existing tooling**: If your app can be started with
  docker-compose, it can be deployed to preview environments with PullPreview.

* can be **started and destroyed easily**: You can manage preview environments
  by adding or removing the `pullpreview` label on your Pull Requests. You can
set specific branches as always on, for instance to continuously deploy your
master branch.

* are **cheap too run**: Preview environments are launched on AWS Lightsail
  instances, which are both very cheap (10USD per month, proratized to the
duration of the PR), and all costs included (bandwith, storage, etc.)

* take the **privacy** of your code seriously: The workflow happens all within
  a GitHub Action, which means your code never leaves GitHub or your Lightsail
instances.

* make the preview URL **easy to find** for your reviewers: Deployment statuses
  and URLs are visible in the PR checks section, and on the Deployments tab in
the GitHub UI.

* **persist state** across deploys: Any state stored in docker volumes (e.g.
  database data) will be persisted across deploys, making the life of reviewers
easier.

* are **easy to troubleshoot**: You can give specific GitHub users the
  authorization to SSH into the preview instance (with sudo privileges) to
further troubleshoot any issue. The SSH keys that they use to push to GitHub
will automatically be installed on the preview servers.

* are **integrated into the GitHub UI**: Logs for each deployment run are
  available within the Actions section, and direct links to the preview
environments are available in the Checks section of a PR, and in the
Deployments tab of the repository.

## Installation

With PullPreview, the only requisite is to have a working docker-compose file
(all versions supported), that can spin your whole environment.

**Note :** If your compose files requires additional files at runtime that are
not checked into the git repository (typically a `.env` file containing
credentials specific to your preview environment), you can easily generate that
file from GitHub Secrets by using an additional step in the workflow (see
Examples).

To install PullPreview, the workflow is very simple:

1. Add the PullPreview action to a workflow file in your repository, for instance `.github/workflows/pullpreview.yml` (see [example][example]).
2. Add the required `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` secrets in your repository settings.
3. Open a PR, and add the `pullpreview` label on it (you may have to create the label first).
4. The PullPreview Action is triggered, a server provisioned, and the PR is updated with the deployment status and the URL to the preview environment.

[example]: https://github.com/pullpreview/pullpreview-example-rails-app/blob/master/.github/workflows/pullpreview.yml

Note: The AWS user needs to have IAM read/write permissions on EC2 resources.
You may want to create a dedicated AWS sub-account for all your preview
environments.

## Action inputs

* `always_on`: By default PullPreview only deploys Pull Requests. With this
  setting you can also request specific branches to always be deployed.
Defaults to none.

* `admins`: Logins of GitHub users that will have their SSH key installed on
  the instance, comma-separated. Defaults to none, which means you won't have
SSH access. We suggest you set at least one GitHub login (e.g. `crohr`).

* `ports`: Ports to open for external access on the preview server (port 22 is
  always open), comma-separated. Defaults to `80/tcp,443/tcp,1000-10000/tcp`.

* `default_port`: The port to use when building the preview URL. Defaults to `80`.

* `compose_files`: Compose files to use when running docker-compose up,
  comma-separated. Defaults to `docker-compose.yml`.

* `instance_type`: Instance type to use. Defaults to `small_2_0` (1CPU, 2GB
  RAM, 10$/month)

## Action outputs

* `url`: The preview environment URL.

* `host`: The hostname or IP address of the preview server.

* `username`: The username that can be used to SSH into the preview server.

## Examples

A basic workflow file that allows 2 users SSH access into the preview
environments, as well as marking the `master` branch as always on.

```yaml
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
    timeout-minutes: 30
    steps:
    - uses: actions/checkout@v2
    - uses: pullpreview/action@v3
      with:
        admins: crohr,other-github-user
        always_on: master
      env:
        AWS_ACCESS_KEY_ID: "${{ secrets.AWS_ACCESS_KEY_ID }}"
        AWS_SECRET_ACCESS_KEY: "${{ secrets.AWS_SECRET_ACCESS_KEY }}"
        AWS_REGION=us-east-1
```

A workflow file that demonstrates how to use GitHub Secrets to generate a custom .env file for use in your docker-compose YAML file:

```yaml
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
    timeout-minutes: 30
    steps:
    - uses: actions/checkout@v2
    - name: Generate .env file
      env:
        SECRET1: ${{ secrets.SECRET1 }}
        SECRET2: ${{ secrets.SECRET2 }}
      run: |
        echo "VALUE1=$SECRET1" >> .env
        echo "VALUE2=$SECRET2" >> .env
    - uses: pullpreview/action@v3
      env:
        AWS_ACCESS_KEY_ID: "${{ secrets.AWS_ACCESS_KEY_ID }}"
        AWS_SECRET_ACCESS_KEY: "${{ secrets.AWS_SECRET_ACCESS_KEY }}"
```

## Is this free?

The code for this Action is completely open source, and licensed under the
Prosperity Public License (see LICENSE).

If you are a non-profit, then it is free to run (in that case, please tell me
so and/or pass the word around!).

In all other cases, you must buy a license. A license is valid for a year, and
costs 120EUR/year.

Send an email to
[support@pullpreview.com](mailto:support@pullpreview.com?subject=License) for
details.

Thanks!
