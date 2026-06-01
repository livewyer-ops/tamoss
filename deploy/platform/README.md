# Platform

Runtime platform installation is owned by the Helm chart in `chart/`.
Environment compositions select platform behavior through their
`platform-values.yaml` file, for example:

```bash
helm upgrade --install tamoss-platform ./deploy/platform/chart \
  --namespace tamoss-platform \
  --create-namespace \
  --values deploy/environments/kind-multi-server/platform-values.yaml \
  --wait
```

The `components/` tree remains as the checked-in vendoring input for upstream
manifests. Use `task operator:platform:vendor` to refresh those files from the
versions recorded in `dependencies.yaml`.
