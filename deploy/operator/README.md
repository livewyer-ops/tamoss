# Development operator manifests

The checked-in `install.yaml` and Kustomization use development images and a
development release catalogue, including in release-tag checkouts.
For a released installation, use the release's published `install.yaml` asset
through an [environment operator overlay](../../docs/operations/install.md).
The release workflow bundles the operator image digest and matching catalogue.

Regenerate the development manifest with:

```bash
task operator:install-manifest
```

Do not hand-edit `install.yaml`; update the operator source manifests instead.
