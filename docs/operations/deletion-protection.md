# Deletion Protection

`Tamoss`, `StorageBackend`, `TamossHibernate`, and `TamossResume` resources
are protected by admission webhooks. Kubernetes delete requests are rejected
until the confirmation annotation is present.

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

For managed RustFS, the operator can remove the managed bucket and database
registration during finalization.

For `external-s3`, deletion removes only TAMOSS database registration and
operator state. The external bucket, credentials, lifecycle rules, CORS rules,
backups, and provider configuration remain outside TAMOSS.

## TamossHibernate and TamossResume

```bash
kubectl --kubeconfig "$KUBECONFIG" -n tams annotate tamosshibernate snapshot \
  confirmation.tamoss.livewyer.io/deletion=true --overwrite
kubectl --kubeconfig "$KUBECONFIG" -n tams delete tamosshibernate snapshot
```

Deleting an operation that has not reached `Completed` marks the parent
`Tamoss` lifecycle `Failed` and lets normal reconciliation take over again;
see [Hibernate and Resume](hibernate-resume.md) for the recovery flow.

## Break Glass

If the validating webhook is unavailable and blocks recovery, fix or reapply the
operator first. Removing the webhook bypasses destructive-action protection and
should be treated as break-glass only.
