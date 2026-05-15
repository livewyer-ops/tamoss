# Troubleshooting

Common issues and fixes for TAMOSS. For development-specific setup see
[CONTRIBUTING.md](../CONTRIBUTING.md).

The product runs on Kind/Helmfile (`task up`). `task dev` runs the API and
frontend natively against a thin docker compose stack of dependencies (Postgres
+ RustFS S3) — see `deploy/compose/docker-compose.yaml`.

## API or frontend won't start under `task dev`

Stop the Kind stack first if it is running; the native dev dependency stack
uses the same local PostgreSQL and RustFS ports.

```bash
# Check the dev dependency containers
docker ps --filter name=tams-

# Restart the dev deps from a clean slate
docker compose -f deploy/compose/docker-compose.yaml down --volumes
task deps
```

## UI shows "Failed to fetch flows"

For `task dev` (native API):

```bash
curl http://localhost:8000/healthz
```

For the Kind stack, check the deployed API:

```bash
kubectl --kubeconfig tams.kubeconfig logs -n tams deployment/tams -f
```

## Browser playback preview issues

- Under `task dev`: RustFS is at <http://localhost:9000>.
- Under `task up` (Kind): RustFS is at <https://s3.tamoss.localtest.me>.
- If object URLs contain the wrong host, check `TAMOSS_S3_PUBLIC_ENDPOINT`.
- Check the browser console for HLS.js errors.
- Confirm the selected flow has registered segments with `get_urls`.
- Confirm the object URLs are CORS-enabled and reachable from the browser.

## Remote tests fail with EOF or TLS EOF errors

If remote acceptance tests report `EOF`, `unexpected EOF`, or TLS EOF errors
during API calls or S3 fixture upload, first verify cluster stability. These
errors are commonly caused by node restarts, ingress disruption, or storage
remounts rather than application-level API responses.

```bash
kubectl --kubeconfig /path/to/remote.kubeconfig get nodes -o wide
kubectl --kubeconfig /path/to/remote.kubeconfig describe node <node-name>
kubectl --kubeconfig /path/to/remote.kubeconfig get pods -A -o wide
kubectl --kubeconfig /path/to/remote.kubeconfig -n <release-namespace> get events --sort-by=.lastTimestamp
```

Do not lower acceptance thresholds to hide this class of failure. Rerun
`task test:deployed DEPLOY_ENV=remote` only after the node is stable, storage
volumes are mounted, and the configured release namespace endpoints are
populated.

## Database connection errors

```bash
# Check the dev-deps database container is ready
docker logs tams-db | tail -20

# Recreate dev deps (drops the database volume)
docker compose -f deploy/compose/docker-compose.yaml down --volumes
task deps
```

## Import errors

Ensure you run Python commands against the `src` project. The Task runner already does this.

```bash
uv run --project src uvicorn tamoss.app:app --reload
```

## Auth errors (401 responses)

Check `src/app/tamoss/auth.py` and `src/app/tamoss/settings.py`. The static API
token variable is `TAMOSS_API_TOKEN`.
Set `TAMOSS_AUTH_REQUIRED=1` when local development should reject anonymous
requests.

Helm-managed installs inject the token from a Kubernetes Secret. With the
default release name and namespace, read it with:

```bash
kubectl -n tams get secret tams-api-token \
  -o jsonpath='{.data.TAMOSS_API_TOKEN}' | base64 --decode
```

## Related

- [deployment.md](./deployment.md): Deployment options and setup
- [usage.md](./usage.md): Using the UI and API
- [CONTRIBUTING.md](../CONTRIBUTING.md): Developer onboarding guide
- [configuration.md](./configuration.md): Environment variable reference
