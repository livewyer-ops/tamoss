# Platform

Runtime platform installation is rendered from the Helm chart in `chart/`.
Environment compositions select platform behavior through their
`platform-values.yaml` file, for example:

```bash
helm template tamoss-platform ./deploy/platform/chart \
  --namespace tamoss-platform \
  --values deploy/environments/kind-multi-server/platform-values.yaml \
  | kubectl apply --server-side --force-conflicts -f -
```

The `components/` tree remains as the checked-in vendoring input for upstream
manifests. Use `task operator:platform:vendor` to refresh those files from the
versions recorded in `dependencies.yaml`.

TLS issuer creation is selected with `tls.mode`:

- `selfSigned` renders a self-signed `ClusterIssuer`.
- `public` renders an ACME `ClusterIssuer` using HTTP-01.
- `existing` renders no issuer and expects the named ClusterIssuer to exist.
- `disabled` renders no issuer for BYO TLS Secret workflows.
