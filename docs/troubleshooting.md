# Troubleshooting

Common issues and fixes for TAMOSS. For deployment procedures see
[deployment.md](./deployment.md); for remote production operations see
[production.md](./production.md).

The sections are ordered from shared symptoms to target-specific checks:

- [Common](#common)
- [Kind](#kind)
- [Remote](#remote)

## Common

### API or frontend will not start under `task dev`

Stop the Kind stack first if it is running; the native dev dependency stack uses
the same local PostgreSQL and RustFS ports.

```bash
# Check the dev dependency containers
docker ps --filter name=tams-

# Restart the dev deps from a clean slate
docker compose -f deploy/compose/docker-compose.yaml down --volumes
task deps
```

### UI shows "Failed to fetch flows"

Check that the API is reachable from the UI path in use.

For `task dev`:

```bash
curl http://localhost:8000/healthz
```

For Kubernetes deployments, inspect API pods:

```bash
kubectl --kubeconfig /path/to/kubeconfig -n <namespace> \
  logs -l app.kubernetes.io/component=api --tail=100
```

### Browser playback preview issues

- Under `task dev`, RustFS is at <http://localhost:9000>.
- Under `task up`, RustFS is at <https://s3.tamoss.localtest.me>.
- Remote deployments should use the public S3 hostname configured in the site
  values.
- If object URLs contain the wrong host, check `TAMOSS_S3_PUBLIC_ENDPOINT` and
  the Helm values under `tams.backends.s3.endpoint.public`.
- Check the browser console for HLS.js errors.
- Confirm the selected flow has registered segments with `get_urls`.
- Confirm object URLs are CORS-enabled and reachable from the browser.

### Auth errors or 401 responses

Check the API token and OAuth settings for the active deployment. The static API
token environment variable is `TAMOSS_API_TOKEN`; local development can require
auth by setting `TAMOSS_AUTH_REQUIRED=1`.

Helm-managed installs inject the token from a Kubernetes Secret. With the
default release name and namespace, read it with:

```bash
kubectl --kubeconfig /path/to/kubeconfig -n tams \
  get secret tams-api-token \
  -o jsonpath='{.data.TAMOSS_API_TOKEN}' | base64 --decode; echo
```

For OAuth2 failures:

- Verify the token endpoint URL.
- Verify the OAuth client secret.
- Verify the client ID.
- If scopes are required, include the configured scopes in the token request.
- Check the API pod has the expected issuer and JWKS settings.

### Database connection errors

For `task dev`, check the dev dependency database:

```bash
docker logs tams-db | tail -20
```

Recreate dev dependencies if local state is disposable:

```bash
docker compose -f deploy/compose/docker-compose.yaml down --volumes
task deps
```

For Kubernetes deployments:

- Check the PostgreSQL pod is ready.
- Check the database secret name and keys match the Helm values.
- Check NetworkPolicy does not block API or worker pods from PostgreSQL.
- Check PVC binding if PostgreSQL pods are pending.

### Pods are pending

- Check PVC binding and StorageClass.
- Check resource requests against node capacity.
- Check node selectors, tolerations, and affinity.
- Check namespace quotas and limit ranges.

```bash
kubectl --kubeconfig /path/to/kubeconfig -n <namespace> \
  get pods,pvc,events --sort-by=.metadata.creationTimestamp
```

### Pods are crashlooping

- Check container logs.
- Check secret names and keys.
- Check database connectivity.
- Check S3 credentials and endpoint reachability.
- Check OAuth issuer/JWKS configuration.

```bash
kubectl --kubeconfig /path/to/kubeconfig -n <namespace> \
  describe pod <pod-name>

kubectl --kubeconfig /path/to/kubeconfig -n <namespace> \
  logs <pod-name> --previous
```

### Import errors

Run Python commands against the `src` project. The Task runner already does
this.

```bash
uv run --project src uvicorn tamoss.app:app --reload
```

## Kind

### Local hostnames do not load

The Kind path uses `*.tamoss.localtest.me`, which should resolve to
`127.0.0.1`. If a network, resolver, or browser blocks that domain, add an
explicit hosts-file entry:

```text
127.0.0.1  app.tamoss.localtest.me api.tamoss.localtest.me s3.tamoss.localtest.me auth.tamoss.localtest.me
```

### Browser warns about TLS

Kind uses local self-signed TLS. Accept the browser warning for the local
hostnames, or use `curl -k` for local command-line probes.

### Kind API or UI route is not working

Check Traefik, Gateway, HTTPRoutes, and application pods:

```bash
kubectl --kubeconfig tams.kubeconfig get pods -A
kubectl --kubeconfig tams.kubeconfig -n tams get gateway,httproute
kubectl --kubeconfig tams.kubeconfig -n traefik logs -l app.kubernetes.io/name=traefik --tail=100
kubectl --kubeconfig tams.kubeconfig -n tams logs -l app.kubernetes.io/component=api --tail=100
```

### Reapply the Kind stack

After changing values or local images:

```bash
task deploy:kind KUBECONFIG=tams.kubeconfig
```

For a clean rebuild and full E2E gate:

```bash
task e2e
```

### Tear down Kind and local state

```bash
task down
```

This removes the Kind cluster, local dependency containers, generated
kubeconfig, and local state.

## Remote

### Helmfile release depends on an undefined release

This usually means a site overlay disabled a cluster service that another
release still lists in `needs`.

- Enable the dependency.
- Disable the dependent integration.
- Or point the dependent chart at an externally managed service.

For example, if Traefik is externally managed, the overlay must consistently
disable repo-owned Traefik installation while also avoiding Helmfile dependencies
on the disabled release.

### Authentik readiness wait is skipped

`task deploy:remote` can print:

```text
Skipping Authentik readiness wait; tests/targets/remote.env not found
```

That means the deploy task could not find the optional remote E2E target file.
The Helmfile apply may still have completed successfully.

Create the file from the example if you want the readiness helper and deployed
tests to know the remote URLs and credentials:

```bash
cp tests/targets/remote.env.example tests/targets/remote.env
```

### DNS does not resolve

- Check authoritative DNS for `app`, `api`, `auth`, and `s3`.
- Check the Gateway or LoadBalancer has an external address.
- Check external DNS controller annotations in the site overlay.
- Check the DNS zone and wildcard record match the configured hostnames.

```bash
kubectl --kubeconfig /path/to/remote.kubeconfig -n <namespace> get gateway,httproute
kubectl --kubeconfig /path/to/remote.kubeconfig -n <ingress-namespace> get svc
```

### TLS fails

- Check the Gateway listener TLS secret.
- Check certificate issuer status.
- Check certificate SANs include all required hostnames.
- Check whether the client trusts the issuing CA.

```bash
kubectl --kubeconfig /path/to/remote.kubeconfig -n <namespace> get certificate,secret
kubectl --kubeconfig /path/to/remote.kubeconfig -n <namespace> describe certificate <name>
```

### External clients report TLS socket disconnects

- Check the hostname resolves from the client environment.
- Check the route terminates TLS with a valid certificate.
- Check any corporate proxy or firewall path.
- Confirm the client is using the direct API hostname for API calls.
- Confirm the API route is not protected by browser-oriented forward auth.

### S3 CORS errors

- Confirm the browser uses the public S3 endpoint, not the internal service URL.
- Confirm the public endpoint is reachable from the browser.
- Confirm CORS allows the UI origin, requested method, and requested headers.
- Confirm presigned URLs include the headers the browser actually sends.
- Confirm `Content-Type` is allowed when uploads sign `content-type`.

### Remote tests fail with EOF or TLS EOF errors

If remote acceptance tests report `EOF`, `unexpected EOF`, or TLS EOF errors
during API calls or S3 fixture upload, first verify cluster stability. These
errors are commonly caused by node restarts, ingress disruption, or storage
remounts rather than application-level API responses.

```bash
kubectl --kubeconfig /path/to/remote.kubeconfig get nodes -o wide
kubectl --kubeconfig /path/to/remote.kubeconfig describe node <node-name>
kubectl --kubeconfig /path/to/remote.kubeconfig get pods -A -o wide
kubectl --kubeconfig /path/to/remote.kubeconfig -n <namespace> \
  get events --sort-by=.lastTimestamp
```

Do not lower acceptance thresholds to hide this class of failure. Rerun deployed
tests only after the node is stable, storage volumes are mounted, and the
configured release namespace endpoints are populated.

### Run remote deployed checks

After `tests/targets/remote.env` is configured:

```bash
task e2e:deployed \
  DEPLOY_ENV=remote \
  KUBECONFIG=/path/to/remote.kubeconfig
```

## Related

- [deployment.md](./deployment.md): Deployment options and setup
- [production.md](./production.md): Remote production operations
- [usage.md](./usage.md): Using the UI and API
- [CONTRIBUTING.md](../CONTRIBUTING.md): Developer onboarding guide
- [configuration.md](./configuration.md): Environment variable reference
