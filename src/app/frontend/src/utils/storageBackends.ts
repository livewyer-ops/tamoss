import type { StorageBackend } from "@/types/tams";

function backendLabel(backend: StorageBackend): string {
  return backend.label || backend.id || "Unnamed backend";
}

function backendDetail(backend: StorageBackend): string {
  return [
    backend.provider,
    backend.store_product ?? backend.store_type,
    backend.region,
  ]
    .filter(Boolean)
    .join(" / ");
}

export function storageBackendDisplay(backend?: StorageBackend): string {
  if (!backend) return "Default backend";
  const detail = backendDetail(backend);
  return detail
    ? `${backendLabel(backend)} (${detail})`
    : backendLabel(backend);
}

export function findStorageBackend(
  backends: StorageBackend[] | null | undefined,
  storageId: string | null | undefined,
): StorageBackend | undefined {
  return backends?.find((backend) => backend.id === storageId);
}

export function storageBackendLabel(backend: StorageBackend): string {
  return backendLabel(backend);
}

export function storageBackendDetail(backend: StorageBackend): string {
  return backendDetail(backend);
}
