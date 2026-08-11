# API Reference

TAMOSS implements the BBC TAMS v8.2 API. The upstream specification is the core
contract for sources, flows, flow segments, tags, storage backends, webhooks,
objects, and deletion workflows.

- Upstream specification: <https://github.com/bbc/tams>
- Local interactive docs: <https://api.tamoss.localtest.me/docs>

## Product Health Endpoints

These endpoints are TAMOSS operational endpoints, not BBC TAMS resources:

| Endpoint | Purpose |
| --- | --- |
| `/healthz` | Process health. |
| `/readyz` | Readiness for serving traffic. |
| `/service` | Product service metadata. |
| `/service/storage-backends` | Registered TAMS storage backend metadata. |

## Authentication

The API accepts the operator-generated token as a bearer token:

```bash
curl -k -H "Authorization: Bearer $TAMOSS_TOKEN" \
  https://api.tamoss.localtest.me/service
```

OAuth2/OIDC bearer tokens are accepted when the selected profile or external
configuration enables OAuth validation.

## Presigned URLs

Presigned URLs are temporary credentials. Do not paste complete URLs into
public issues, logs, or documentation.
