# Operator RBAC Notes

The generated manager role is intentionally explicit: no wildcard verbs are
used. Backend-provider permissions are limited to the CRDs and core resources
the controller emits:

- `postgresql.cnpg.io/clusters` and `postgresql.cnpg.io/scheduledbackups` for
  CNPG-managed Postgres and managed backup schedules.
- `rustfs.com/tenants` for RustFS Operator-managed S3.

No kube-linter RBAC exception is currently required for these rules.

## Authentik Platform Namespace Policy

`TAMOSS_AUTHENTIK_PLATFORM_NAMESPACES` is enforced in controller code. In the
cluster-wide install, the manager ServiceAccount still has broad Secret
permissions because tenant workloads, generated credentials, schema state, and
platform Authentik API tokens all use the same core `secrets` resource type.
Use explicit per-namespace RoleBindings for tighter installations.
