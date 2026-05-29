import { useState, useEffect } from "react";
import { useApi } from "@/contexts/ApiContext";
import type { Flow, FlowCollectionItem } from "@/types/tams";
import { formatFormat, formatCodec } from "@/utils/format";

const FLOW_SELECTOR_PAGE_SIZE = "300";

interface ManageChildFlowsDialogProps {
  open: boolean;
  onClose: () => void;
  flow: Flow;
  onSaved: () => void;
}

export default function ManageChildFlowsDialog({
  open,
  onClose,
  flow,
  onSaved,
}: ManageChildFlowsDialogProps) {
  const api = useApi();
  const [children, setChildren] = useState<FlowCollectionItem[]>([]);
  const [availableFlows, setAvailableFlows] = useState<Flow[]>([]);
  const [addFlowId, setAddFlowId] = useState("");
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) {
      setAddFlowId("");
      setError(null);
      setChildren([]);
      setAvailableFlows([]);
      return;
    }
    setLoading(true);
    Promise.all([
      api.getFlowCollection(flow.id).catch(() => [] as FlowCollectionItem[]),
      api.getFlows({ limit: FLOW_SELECTOR_PAGE_SIZE }),
    ])
      .then(([collection, flowsResult]) => {
        setChildren(collection);
        const childIds = new Set(collection.map((c) => c.id));
        const eligible = flowsResult.data.filter(
          (f) =>
            f.id !== flow.id &&
            !childIds.has(f.id) &&
            (f.format === "urn:x-nmos:format:video" ||
              f.format === "urn:x-nmos:format:audio"),
        );
        setAvailableFlows(eligible);
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, [api, open, flow.id]);

  if (!open) return null;

  const handleAdd = () => {
    if (!addFlowId) return;
    const existing = children.find((c) => c.id === addFlowId);
    if (existing) return;
    const selected = availableFlows.find((f) => f.id === addFlowId);
    const role = guessRole(selected);
    setChildren((prev) => [...prev, { id: addFlowId, role }]);
    setAvailableFlows((prev) => prev.filter((f) => f.id !== addFlowId));
    setAddFlowId("");
  };

  const handleRemove = (id: string) => {
    const removed = children.find((c) => c.id === id);
    setChildren((prev) => prev.filter((c) => c.id !== id));
    if (removed) {
      // Re-add to available list if we have its details
      const flowDetail = availableFlows.find((f) => f.id === id);
      if (!flowDetail) {
        // We don't have the full Flow object anymore; next open will reload
      }
    }
  };

  const handleRoleChange = (id: string, newRole: string) => {
    setChildren((prev) =>
      prev.map((c) => (c.id === id ? { ...c, role: newRole } : c)),
    );
  };

  const handleSave = async () => {
    // Auto-add pending selection if user hasn't clicked Add
    let items = [...children];
    if (addFlowId) {
      const existing = items.find((c) => c.id === addFlowId);
      if (!existing) {
        const selected = availableFlows.find((f) => f.id === addFlowId);
        items = [...items, { id: addFlowId, role: guessRole(selected) }];
      }
    }

    // Validate all items have roles
    const missing = items.find((c) => !c.role?.trim());
    if (missing) {
      setError("All child flows must have a role assigned.");
      return;
    }

    const payload = items.map((c) => ({ id: c.id, role: c.role ?? "" }));
    setSaving(true);
    setError(null);
    try {
      await api.setFlowCollection(flow.id, payload);
      onSaved();
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to update collection",
      );
    } finally {
      setSaving(false);
    }
  };

  const canSave = children.length > 0 || addFlowId;

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div className="mx-4 w-full max-w-lg rounded-xl bg-white p-6 shadow-xl">
        <h2 className="mb-4 text-lg font-semibold text-gray-900">
          Edit Flow Collection
        </h2>

        {loading ? (
          <p className="text-sm text-gray-500">Loading...</p>
        ) : (
          <div className="space-y-4">
            {children.length > 0 && (
              <div className="space-y-2">
                <h3 className="text-sm font-medium text-gray-700">
                  Collection items
                </h3>
                {children.map((child) => (
                  <div
                    key={child.id}
                    className="flex items-center gap-2 rounded-lg bg-gray-50 px-3 py-2"
                  >
                    <div className="min-w-0 flex-1">
                      <code className="text-xs text-gray-700">
                        {child.id.substring(0, 16)}...
                      </code>
                    </div>
                    <input
                      type="text"
                      value={child.role ?? ""}
                      onChange={(e) =>
                        handleRoleChange(child.id, e.target.value)
                      }
                      placeholder="role"
                      disabled={saving}
                      className="w-24 rounded border border-gray-300 px-2 py-1 text-xs focus:border-tams-500 focus:outline-none focus:ring-1 focus:ring-tams-500 disabled:opacity-50"
                    />
                    <button
                      onClick={() => handleRemove(child.id)}
                      disabled={saving}
                      className="text-xs text-red-600 hover:text-red-800 disabled:opacity-50"
                    >
                      Remove from collection
                    </button>
                  </div>
                ))}
              </div>
            )}

            {availableFlows.length > 0 ? (
              <div className="flex items-end gap-2">
                <div className="flex-1">
                  <label className="mb-1 block text-sm font-medium text-gray-700">
                    Add child flow
                  </label>
                  <select
                    value={addFlowId}
                    onChange={(e) => setAddFlowId(e.target.value)}
                    disabled={saving}
                    className="block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-tams-500 focus:outline-none focus:ring-1 focus:ring-tams-500 disabled:opacity-50"
                  >
                    <option value="">Select a flow...</option>
                    {availableFlows.map((f) => (
                      <option key={f.id} value={f.id}>
                        {f.label || f.id} ({formatFormat(f.format)}
                        {f.codec ? ` - ${formatCodec(f.codec)}` : ""})
                      </option>
                    ))}
                  </select>
                </div>
                <button
                  onClick={handleAdd}
                  disabled={saving || !addFlowId}
                  className="rounded-lg bg-gray-100 px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-200 disabled:opacity-50"
                >
                  Add
                </button>
              </div>
            ) : children.length === 0 ? (
              <p className="text-sm text-gray-500">
                No video or audio flows available to add.
              </p>
            ) : null}

            {error && <p className="text-sm text-red-600">{error}</p>}
          </div>
        )}

        <div className="mt-6 flex justify-end gap-3">
          <button
            onClick={onClose}
            disabled={saving}
            className="rounded-lg border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
          >
            Cancel
          </button>
          <button
            onClick={handleSave}
            disabled={saving || loading || !canSave}
            className="rounded-lg bg-tams-600 px-4 py-2 text-sm font-medium text-white shadow-sm hover:bg-tams-700 disabled:opacity-50"
          >
            {saving ? "Saving..." : "Save"}
          </button>
        </div>
      </div>
    </div>
  );
}

function guessRole(flow?: Flow): string {
  if (!flow?.format) return "";
  if (flow.format === "urn:x-nmos:format:video") return "video";
  if (flow.format === "urn:x-nmos:format:audio") return "audio";
  return "";
}
