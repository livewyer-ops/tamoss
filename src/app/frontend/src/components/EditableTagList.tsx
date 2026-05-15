import { useState } from "react";
import Badge from "./Badge";

interface EditableTagListProps {
  tags?: Record<string, unknown>;
  onAdd: (key: string, value: string) => Promise<void>;
  onDelete: (key: string) => Promise<void>;
  disabled?: boolean;
}

function resolveTagValue(value: unknown): string {
  if (typeof value === "string") return value;
  if (Array.isArray(value)) return value.map(String).join(", ");
  if (value && typeof value === "object") {
    const obj = value as Record<string, unknown>;
    if ("actual_instance" in obj) {
      return resolveTagValue(obj.actual_instance);
    }
    return JSON.stringify(value);
  }
  return String(value);
}

export default function EditableTagList({
  tags,
  onAdd,
  onDelete,
  disabled = false,
}: EditableTagListProps) {
  const [confirmKey, setConfirmKey] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [newKey, setNewKey] = useState("");
  const [newValue, setNewValue] = useState("");
  const [adding, setAdding] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const entries = tags ? Object.entries(tags) : [];

  const handleDelete = async (key: string) => {
    setDeleting(true);
    setError(null);
    try {
      await onDelete(key);
      setConfirmKey(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Delete failed");
    } finally {
      setDeleting(false);
    }
  };

  const handleAdd = async () => {
    if (!newKey.trim()) return;
    setAdding(true);
    setError(null);
    try {
      await onAdd(newKey.trim(), newValue.trim());
      setNewKey("");
      setNewValue("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Add failed");
    } finally {
      setAdding(false);
    }
  };

  return (
    <div className="space-y-3">
      {entries.length === 0 && disabled && (
        <span className="text-sm text-gray-400">No tags</span>
      )}

      <div className="flex flex-wrap gap-1.5">
        {entries.map(([key, value]) => {
          const displayValue = resolveTagValue(value);
          const isConfirming = confirmKey === key;

          if (isConfirming) {
            return (
              <span
                key={key}
                className="inline-flex items-center gap-1 rounded-full bg-red-100 px-2.5 py-0.5 text-xs font-medium text-red-700"
              >
                Delete "{key}"?
                <button
                  onClick={() => handleDelete(key)}
                  disabled={deleting}
                  className="ml-0.5 font-semibold hover:text-red-900 disabled:opacity-50"
                >
                  Yes
                </button>
                <button
                  onClick={() => setConfirmKey(null)}
                  disabled={deleting}
                  className="font-semibold hover:text-red-900 disabled:opacity-50"
                >
                  No
                </button>
              </span>
            );
          }

          return (
            <Badge key={key} variant="default">
              <span className="font-semibold">{key}:</span>{" "}
              <span className="ml-1">{displayValue}</span>
              {!disabled && (
                <button
                  onClick={() => setConfirmKey(key)}
                  className="ml-1.5 text-gray-400 hover:text-gray-600"
                  aria-label={`Delete tag ${key}`}
                >
                  &times;
                </button>
              )}
            </Badge>
          );
        })}
      </div>

      {!disabled && (
        <div className="flex items-center gap-2">
          <input
            type="text"
            value={newKey}
            onChange={(e) => setNewKey(e.target.value)}
            placeholder="Key"
            disabled={adding}
            className="w-28 rounded-md border border-gray-300 px-2 py-1 text-xs shadow-sm focus:border-tams-500 focus:outline-none focus:ring-1 focus:ring-tams-500 disabled:opacity-50"
          />
          <input
            type="text"
            value={newValue}
            onChange={(e) => setNewValue(e.target.value)}
            placeholder="Value"
            disabled={adding}
            onKeyDown={(e) => {
              if (e.key === "Enter") handleAdd();
            }}
            className="w-40 rounded-md border border-gray-300 px-2 py-1 text-xs shadow-sm focus:border-tams-500 focus:outline-none focus:ring-1 focus:ring-tams-500 disabled:opacity-50"
          />
          <button
            onClick={handleAdd}
            disabled={adding || !newKey.trim()}
            className="rounded-md bg-tams-600 px-2.5 py-1 text-xs font-medium text-white hover:bg-tams-700 disabled:opacity-50"
          >
            {adding ? "Adding..." : "Add"}
          </button>
        </div>
      )}

      {error && <p className="text-xs text-red-600">{error}</p>}
    </div>
  );
}
