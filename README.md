# PullPreview

A GitHub Action that starts live environments for your pull requests and branches.

## Spin environments in one click

Once installed in your repository, this action is triggered any time a change
is made to Pull Requests labelled with the `pullpreview` label, or one of the
*always-on* branches.

When triggered, it will:

1. Check out the repository code
2. Provision a cheap AWS Lightsail instance, with docker and docker-compose set up
3. Continuously deploy the specified pull requests and branches, using your docker-compose file(s)
4. Report the preview instance URL in the GitHub UI

It is designed to be the **no-nonsense, cheap, and secure** alternative to
services that require access to your code and force your app to fit within
their specific deployment system and/or require a specific config file.

<img src="img/2-add-label.png">
<img src="img/3-deploy-starts.png">
<img src="img/5-view-deployment.png">
<img src="img/6-deploy-next-commit-pending.png">

## Useful for the entire team

* **Product Owners**: Interact with a new feature as it's built, give valuable feedback earlier, reduce wasted development time.
* **Developers**: Show your work in progress, find bugs early, deliver the right feature.
* **Ops**: Concentrate on high value tasks, not maintaining staging environments.
* **CTOs**: Don't let your code run on third-party servers: your code always stays private on either GitHub's or your servers.

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

<img src="img/4-view-logs.png" />
<img src="img/8-list-deployments.png" />

## Installation & Usage

* [Getting Started](https://github.com/pullpreview/action/wiki/Getting-Started)
* Action [Inputs](https://github.com/pullpreview/action/wiki/Inputs) / [Outputs](https://github.com/pullpreview/action/wiki/Outputs)
* Handling [Seed Data](https://github.com/pullpreview/action/wiki/Seed-Data)
* [Workflow Examples](https://github.com/pullpreview/action/wiki/Workflow-Examples)
* [FAQ](https://github.com/pullpreview/action/wiki/FAQ)

&rarr; Please see the [wiki](https://github.com/pullpreview/action/wiki) for the full documentation.

## Is this free?

The code for this Action is completely open source, and licensed under the
Prosperity Public License (see LICENSE).

If you are a non-profit individual, then it is free to run (in that case, please tell me
so and/or pass the word around!).

In all other cases, you must buy a license. More details on [pullpreview.com](https://pullpreview.com).

Thanks for reading until the end!
