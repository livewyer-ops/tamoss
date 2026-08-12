# Deletion Protection

`Tamoss`, `StorageBackend`, `FlowProfile`, and `TamossHibernate` resources are
protected by admission webhooks. The webhooks reject Kubernetes delete
requests until the confirmation annotation is present.

## Tamoss

```bash
kubectl --kubeconfig "$KUBECONFIG" -n tams annotate tamoss tamoss-kind \
  confirmation.tamoss.livewyer.io/deletion=true --overwrite
kubectl --kubeconfig "$KUBECONFIG" -n tams delete tamoss tamoss-kind
```

After the delete is accepted, the operator finalizer cleans up operator-owned
instance resources.

Shared platform services remain in place for other instances.

## StorageBackend

```bash
kubectl --kubeconfig "$KUBECONFIG" -n tams annotate storagebackend archive \
  confirmation.tamoss.livewyer.io/deletion=true --overwrite
kubectl --kubeconfig "$KUBECONFIG" -n tams delete storagebackend archive
```

For managed [RustFS](https://github.com/rustfs/rustfs), the operator can
remove the managed bucket and database
registration when its finalizer runs.

For `external-s3`, deletion removes only TAMOSS database registration and
operator state. The external bucket, credentials, lifecycle rules, CORS rules,
backups, and provider configuration remain outside TAMOSS.

## FlowProfile

```bash
kubectl --kubeconfig "$KUBECONFIG" -n tams annotate flowprofile hd-avc \
  confirmation.tamoss.livewyer.io/deletion=true --overwrite
kubectl --kubeconfig "$KUBECONFIG" -n tams delete flowprofile hd-avc
```

The operator removes the TAMS Profile only when no Flow references it. An
in-use resource remains terminating with `DeletionBlocked=True`; do not remove
its finaliser manually. See [Manage Flow Profiles](manage-flow-profiles.md).

## TamossHibernate

```bash
kubectl --kubeconfig "$KUBECONFIG" -n tams annotate tamosshibernate snapshot \
  confirmation.tamoss.livewyer.io/deletion=true --overwrite
kubectl --kubeconfig "$KUBECONFIG" -n tams delete tamosshibernate snapshot
```

Operator-materialised operations (from `spec.hibernation.enabled`) already
carry the confirmation annotation, so the operator can abort its own
cycles. Deleting an operation that has not reached `Completed` marks the parent
`Tamoss` lifecycle `Failed` and lets normal reconciliation take over again;
see [Hibernate and Resume](hibernate-resume.md) for the recovery flow.

## Webhook Removal

If the validating webhook is unavailable and blocks recovery, fix or reapply the
operator first. Removing the webhook bypasses destructive-action protection;
treat it as a last resort.
