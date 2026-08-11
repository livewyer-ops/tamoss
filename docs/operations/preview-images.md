# Canary Images

Canary builds provide private, explicitly published images for testing a branch
outside the normal Docker Hub workflow. They go to a private registry which a
test cluster in the same Google Cloud project pulls through its node service
account, so no image pull secret is needed.

This page covers publishing a canary and running an instance on it. For the
released upgrade path see [Upgrades](upgrades.md).

## Publication paths

`.github/workflows/docker-hub.yaml` publishes on pushes to `main`, on release
tags, and trusted pull requests from this repository. Forked and Dependabot
pull requests remain build-only because they cannot receive Docker Hub
credentials. A branch push does not enter that workflow unless it is `main`;
use the canary task below when a private branch image is needed before review.

## Publishing a canary

```bash
task canary:login      # once per machine: configure Docker for the registry host
task canary:tag        # show the tag that would be used
task canary:publish    # build all four images and push them
task canary:list       # confirm what the registry now holds
```

`canary:publish` builds the API, UI, Console API, and operator images for
`linux/amd64` and `linux/arm64`, with SBOM and provenance attestations, and
pushes them to
`europe-west2-docker.pkg.dev/tamoss/tamoss-canary`.

Tags follow the convention already in the registry,
`<base version>-<topic>-<short sha>`, for example
`8.2.0-preview-58bba1d9`. Override any part with `CANARY_BASE_VERSION`,
`CANARY_TOPIC`, or a complete `CANARY_TAG`.

The task refuses to run against Docker Hub. Pointing `CANARY_REGISTRY` at a
public repository fails rather than publishing a withheld artifact.

## Running an instance on a canary

`deploy/instances/preview` applies a `multi-server` instance with the Console
enabled and each component's image repository pinned to the canary registry.

Only the repositories are pinned. Tags are left unset so the operator fills
them from `DefaultOperandTag`, which the canary build stamps with the tag it
just pushed. A preview instance therefore tracks whichever canary build
deployed its operator, and the overlay does not carry a short SHA that changes
on every build.

It uses its own `preview-tams` namespace so a preview never rolls over a
released instance sharing the cluster. Remove the namespace when finished.

Use `task canary:list` to verify the private registry tags. The publishing task
rejects Docker Hub destinations so the manual canary path cannot overwrite the
normal release and pull-request repositories.
