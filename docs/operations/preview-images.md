# Publish and Deploy Preview Images

Use this guide to publish private branch images outside the normal Docker Hub
workflow and deploy them to an isolated preview namespace.

For the released upgrade path, use [Upgrades](upgrades.md) instead.

## Prerequisites

- Docker authenticated to the private Google Artifact Registry through
  `task canary:login`.
- A test cluster in the same Google Cloud project whose node service account
  can pull from `europe-west2-docker.pkg.dev/tamoss/tamoss-canary`.
- The normal TAMOSS platform dependencies already installed on that dedicated
  test cluster.
- A clean understanding that these images are private test artefacts, not
  release candidates published through the normal release workflow.

The task refuses Docker Hub destinations, so this manual path cannot overwrite
normal release or pull-request repositories.

## Understand the Publication Boundary

`.github/workflows/docker-hub.yaml` publishes on pushes to `main`, on release
tags, and trusted pull requests from this repository. Forked and Dependabot
pull requests remain build-only because they cannot receive Docker Hub
credentials. A branch push does not enter that workflow unless it is `main`;
use the canary task below when a private branch image is needed before review.

## Publish the Images

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

Record the emitted tag; the deployment uses the same value as the operator's
default operand tag.

## Deploy the Preview Instance

`deploy/instances/preview` applies a `multi-server` instance with the Console
enabled and each component's image repository pinned to the canary registry.

Only the repositories are pinned. Tags are left unset so the operator fills
them from `DefaultOperandTag`, which the canary build stamps with the tag it
just pushed. A preview instance therefore tracks whichever canary build
deployed its operator, and the overlay does not carry a short SHA that changes
on every build.

Use a dedicated test cluster because changing the installed operator affects
every `Tamoss` it watches. Install the current operator resources, switch its
manager image to the published tag, then apply the preview instance:

```bash
export CANARY_REGISTRY=europe-west2-docker.pkg.dev/tamoss/tamoss-canary
export CANARY_TAG="$(task canary:tag)"

kubectl apply --server-side -k deploy/operator
kubectl -n tamoss-system set image deployment/operator-controller-manager \
  manager="$CANARY_REGISTRY/tamoss-operator:$CANARY_TAG"
kubectl -n tamoss-system rollout status \
  deployment/operator-controller-manager --timeout=5m
kubectl apply -k deploy/instances/preview
```

The checked-in overlay uses its own `preview-tams` namespace. It pins operand
repositories to the private registry while the canary operator supplies the
published tag through `DefaultOperandTag`.

## Verify the Deployment

Use `task canary:list` to confirm the private registry tag, then inspect the
resolved operator and operand images:

```bash
kubectl -n preview-tams get tamoss -o wide
kubectl -n preview-tams get tamoss -o jsonpath='{.items[*].status.resolved.images}'
kubectl -n preview-tams wait --for=condition=Ready \
  tamoss/tamoss-preview --timeout=15m
```

Run the applicable deployed checks against the preview target before treating
the branch build as usable.

## Clean Up

Delete the isolated preview namespace after testing. If the cluster is not
being discarded, restore the released operator image as a separate deliberate
step:

```bash
kubectl delete namespace preview-tams
```

Do not use this workflow or cleanup against a namespace or operator installation
that serves a released instance.
