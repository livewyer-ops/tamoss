import type { StorageBackend } from "@/types/tams";
import {
  storageBackendDetail,
  storageBackendDisplay,
  storageBackendLabel,
} from "@/utils/storageBackends";

interface StorageBackendSelectorProps {
  id?: string;
  label?: string;
  value: string;
  onChange: (value: string) => void;
  backends?: StorageBackend[] | null;
  disabled?: boolean;
  includeAllOption?: boolean;
  allLabel?: string;
  className?: string;
}

export default function StorageBackendSelector({
  id,
  label,
  value,
  onChange,
  backends,
  disabled = false,
  includeAllOption = true,
  allLabel = "Default backend",
  className = "",
}: StorageBackendSelectorProps) {
  return (
    <div className={className}>
      {label && id && (
        <label
          htmlFor={id}
          className="block text-sm font-medium text-lw-ink-700"
        >
          {label}
        </label>
      )}
      <select
        id={id}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        disabled={disabled || (backends?.length ?? 0) === 0}
        className="mt-1 block w-full rounded-md border border-lw-ink-200 px-3 py-2 text-sm shadow-sm focus:border-tams-500 focus:outline-none focus:ring-1 focus:ring-tams-500 disabled:bg-lw-ink-50 disabled:opacity-70"
      >
        {includeAllOption && <option value="">{allLabel}</option>}
        {backends?.map((backend) => (
          <option key={backend.id ?? backend.label} value={backend.id ?? ""}>
            {storageBackendDisplay(backend)}
            {backend.default_storage ? " - default" : ""}
          </option>
        ))}
      </select>
    </div>
  );
}

interface StorageBackendMultiSelectProps {
  label?: string;
  values: string[];
  onChange: (values: string[]) => void;
  backends?: StorageBackend[] | null;
  disabled?: boolean;
}

export function StorageBackendMultiSelect({
  label,
  values,
  onChange,
  backends,
  disabled = false,
}: StorageBackendMultiSelectProps) {
  const selected = new Set(values);

  return (
    <div>
      {label && (
        <p className="mb-2 text-sm font-medium text-lw-ink-700">{label}</p>
      )}
      {(backends?.length ?? 0) === 0 ? (
        <p className="text-sm text-lw-ink-500">
          No storage backends advertised.
        </p>
      ) : (
        <div className="grid gap-2 sm:grid-cols-2">
          {backends?.map((backend) => {
            const storageId = backend.id ?? "";
            return (
              <label
                key={storageId || backend.label}
                className="flex items-start gap-2 rounded-lg border border-lw-ink-100 px-3 py-2 text-sm text-lw-ink-700"
              >
                <input
                  type="checkbox"
                  checked={selected.has(storageId)}
                  disabled={disabled || !storageId}
                  onChange={(event) => {
                    const next = new Set(selected);
                    if (event.target.checked) next.add(storageId);
                    else next.delete(storageId);
                    onChange(Array.from(next));
                  }}
                  className="mt-0.5 h-4 w-4 rounded border-lw-ink-200 text-tams-600 focus:ring-tams-500"
                />
                <span>
                  <span className="block font-medium">
                    {storageBackendLabel(backend)}
                    {backend.default_storage ? " (default)" : ""}
                  </span>
                  <span className="block text-xs text-lw-ink-500">
                    {storageBackendDetail(backend) || backend.id}
                  </span>
                </span>
              </label>
            );
          })}
        </div>
      )}
    </div>
  );
}
