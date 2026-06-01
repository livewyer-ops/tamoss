# Platform

Runtime platform installation is rendered from the Helm chart in `chart/`.
Environment compositions select platform behavior through their
`platform-values.yaml` file, for example:

```bash
helm dependency build ./deploy/platform/chart
helm template tamoss-platform ./deploy/platform/chart \
  --namespace tamoss-platform \
  --values deploy/environments/kind-multi-server/platform-values.yaml \
  | kubectl apply --server-side --force-conflicts -f -
```

Most platform prerequisites are upstream Helm chart dependencies pinned in
`chart/Chart.yaml` and recorded in `dependencies.yaml`. Traefik CRDs and the
RustFS Operator manifest remain checked-in chart files because their lifecycle
is intentionally explicit.

TLS issuer creation is selected with `tls.mode`:

- `selfSigned` renders a self-signed `ClusterIssuer`.
- `public` renders an ACME `ClusterIssuer` using HTTP-01.
- `existing` renders no issuer and expects the named ClusterIssuer to exist.
- `disabled` renders no issuer for BYO TLS Secret workflows.
