# TAMOSS Stack Chart

`tams-stack` is the Helm umbrella chart. It keeps the API, storage dependencies, and
deployment addons in one chart while exposing two values profiles.

## Profiles

- Lite is the smallest operable TAMOSS stack: API Deployment and Service,
  generated API token, PostgreSQL StatefulSet, and standalone single-replica
  RustFS Deployment.
- Full is the cloud-native stack with addons: Lite core plus worker and UI
  Deployments, UI Service, Gateway API routes, Gateway API ReferenceGrant,
  Traefik middleware references, and forward-auth integration points.

Lite intentionally does not render UI, worker, Ingress, HTTPRoute, Gateway,
GatewayClass, Middleware, TraefikService, or distributed RustFS resources.

Bootstrap rows are required for an operable empty store, including the default
storage backend. Fixture rows are optional demo media metadata for development.
The profile defaults leave `platform.dbInit.loadFixtures=false`; enable it and
pass `db/fixtures.sql` only for local demo or CI scenarios that need those rows.

Lite:

```bash
helm dependency build ./deploy/charts/tams-stack

helm install tams ./deploy/charts/tams-stack \
  --namespace tams \
  --create-namespace \
  -f ./deploy/charts/tams-stack/values-lite.yaml \
  --set-file platform.dbInit.schemaSql=db/schema.sql \
  --set-file platform.dbInit.bootstrapSql=db/bootstrap.sql
```

Full:

```bash
helm dependency build ./deploy/charts/tams-stack

helm template tams ./deploy/charts/tams-stack \
  --namespace tams \
  -f ./deploy/charts/tams-stack/values-full.yaml \
  --set-file platform.dbInit.schemaSql=db/schema.sql \
  --set-file platform.dbInit.bootstrapSql=db/bootstrap.sql
```

The Full profile assumes Gateway API CRDs, including `ReferenceGrant`, a
compatible Gateway controller, and a forward-auth provider already exist. Use
`task up` for the local Full path because Helmfile installs those cluster
services around this chart.

Validate the profile contract and Helmfile kind/remote rendering:

```bash
task deploy:validate
```
