# API Reference

TAMOSS implements the BBC TAMS v8.2 API. The upstream specification is the core
contract for sources, flows, flow segments, tags, storage backends, webhooks,
objects, and deletion workflows.

- Upstream specification: <https://github.com/bbc/tams/tree/8.2>
- Local interactive docs: <https://api.tamoss.localtest.me/docs>

## Capability Map

| Capability | Contract surface | Explanation |
| --- | --- | --- |
| Flow Profiles | `/service/profiles`, `profile_id` on Flows | [Flow Profiles](../concepts/flow-profiles.md) |
| Flow lifecycle status | `status` on Flows and Flow list filters | Upstream OpenAPI specification |
| Initialisation Objects | `init_segments`, `init_object_id`, nested `init_object` | [Initialisation Objects](../concepts/initialisation-objects.md) |
| Deterministic listings | Endpoint-specific `sort_by`, `reverse_order`, and paging headers | Upstream OpenAPI specification |
| Collection membership | Ordered optional-role collections and `collected_by_ids` filters | Upstream OpenAPI specification |
| Storage selection | Presigned selection plus storage tag value and existence filters | [Storage Backends](../concepts/storage-backends.md) |
| Service metadata | `/service` | BBC TAMS service identity and capabilities |
| Storage backend catalogue | `/service/storage-backends` | Registered TAMS storage backend metadata |

The upstream OpenAPI document is authoritative for request and response fields.
Capability pages explain TAMOSS persistence and lifecycle choices without
duplicating that schema.

## Product Health Endpoints

These endpoints are TAMOSS operational endpoints, not BBC TAMS resources:

| Endpoint | Purpose |
| --- | --- |
| `/healthz` | Process health. |
| `/readyz` | Readiness for serving traffic. |

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
