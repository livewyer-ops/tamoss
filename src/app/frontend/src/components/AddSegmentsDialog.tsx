import { useState, useEffect } from "react";
import { useApi } from "@/contexts/ApiContext";
import type { Flow, FlowSegment } from "@/types/tams";
import { formatFormat } from "@/utils/format";
import { segmentToWritePayload } from "@/utils/segment-payload";

const COMPATIBLE_FLOW_PAGE_SIZE = "300";
const COPY_SEGMENT_PAGE_SIZE = "300";

interface AddSegmentsDialogProps {
  open: boolean;
  onClose: () => void;
  flow: Flow;
  onAdded: () => void;
}

export default function AddSegmentsDialog({
  open,
  onClose,
  flow,
  onAdded,
}: AddSegmentsDialogProps) {
  const api = useApi();
  const [compatibleFlows, setCompatibleFlows] = useState<Flow[]>([]);
  const [selectedFlowId, setSelectedFlowId] = useState("");
  const [segments, setSegments] = useState<FlowSegment[]>([]);
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set());
  const [loadingFlows, setLoadingFlows] = useState(false);
  const [loadingSegments, setLoadingSegments] = useState(false);
  const [adding, setAdding] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) {
      setSelectedFlowId("");
      setSegments([]);
      setSelectedIds(new Set());
      setError(null);
      return;
    }
    setLoadingFlows(true);
    const params: Record<string, string> = { limit: COMPATIBLE_FLOW_PAGE_SIZE };
    if (flow.format) params.format = flow.format;
    if (flow.source_id) params.source_id = flow.source_id;
    api
      .getFlows(params)
      .then((res) => {
        const compatible = res.data.filter(
          (f) =>
            f.id !== flow.id &&
            f.format === flow.format &&
            f.source_id === flow.source_id,
        );
        setCompatibleFlows(compatible);
      })
      .catch(() => {})
      .finally(() => setLoadingFlows(false));
  }, [api, open, flow.id, flow.format, flow.source_id]);

  useEffect(() => {
    if (!selectedFlowId) {
      setSegments([]);
      setSelectedIds(new Set());
      return;
    }
    setLoadingSegments(true);
    api
      .getFlowSegments(selectedFlowId, {
        include_object_timerange: true,
        limit: COPY_SEGMENT_PAGE_SIZE,
      })
      .then((res) => setSegments(res.data))
      .catch(() => setSegments([]))
      .finally(() => setLoadingSegments(false));
  }, [api, selectedFlowId]);

  if (!open) return null;

  const toggleSegment = (index: number) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(index)) next.delete(index);
      else next.add(index);
      return next;
    });
  };

  const toggleAll = () => {
    if (selectedIds.size === segments.length) {
      setSelectedIds(new Set());
    } else {
      setSelectedIds(new Set(segments.map((_, i) => i)));
    }
  };

  const handleAdd = async () => {
    if (selectedIds.size === 0) return;
    setAdding(true);
    setError(null);
    try {
      const selected = segments
        .filter((_, i) => selectedIds.has(i))
        .map(segmentToWritePayload);
      await api.addFlowSegments(flow.id, selected);
      onAdded();
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to add segments");
    } finally {
      setAdding(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div className="mx-4 w-full max-w-2xl rounded-xl bg-white p-6 shadow-xl">
        <h2 className="mb-4 text-lg font-semibold text-gray-900">
          Add Segments
        </h2>

        <div className="space-y-4">
          <div>
            <label
              htmlFor="add-segments-source-flow"
              className="block text-sm font-medium text-gray-700"
            >
              Source flow (same source &amp; format: {formatFormat(flow.format)}
              )
            </label>
            <select
              id="add-segments-source-flow"
              value={selectedFlowId}
              onChange={(e) => setSelectedFlowId(e.target.value)}
              disabled={adding || loadingFlows}
              className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-tams-500 focus:outline-none focus:ring-1 focus:ring-tams-500 disabled:opacity-50"
            >
              <option value="">Select a flow...</option>
              {compatibleFlows.map((f) => (
                <option key={f.id} value={f.id}>
                  {f.label || f.id}
                  {f.codec ? ` (${f.codec})` : ""}
                </option>
              ))}
            </select>
            {!loadingFlows && compatibleFlows.length === 0 && (
              <p className="mt-1 text-xs text-gray-500">
                No compatible flows found (same source_id and format).
              </p>
            )}
          </div>

          {loadingSegments && (
            <p className="text-sm text-gray-500">Loading segments...</p>
          )}

          {!loadingSegments && selectedFlowId && segments.length === 0 && (
            <p className="text-sm text-gray-500">No segments in this flow.</p>
          )}

          {segments.length > 0 && (
            <div className="max-h-64 overflow-auto rounded-lg border border-gray-200">
              <table className="min-w-full divide-y divide-gray-200">
                <thead className="sticky top-0 bg-gray-50">
                  <tr>
                    <th className="px-2 py-2">
                      <input
                        type="checkbox"
                        checked={
                          selectedIds.size === segments.length &&
                          segments.length > 0
                        }
                        onChange={toggleAll}
                        className="h-4 w-4 rounded border-gray-300 text-tams-600 focus:ring-tams-500"
                      />
                    </th>
                    <th className="px-3 py-2 text-left text-xs font-medium uppercase text-gray-500">
                      Timerange
                    </th>
                    <th className="px-3 py-2 text-left text-xs font-medium uppercase text-gray-500">
                      Object ID
                    </th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-100">
                  {segments.map((seg, i) => (
                    <tr
                      key={`${seg.object_id}-${i}`}
                      className={selectedIds.has(i) ? "bg-tams-50" : ""}
                    >
                      <td className="px-2 py-2">
                        <input
                          type="checkbox"
                          checked={selectedIds.has(i)}
                          onChange={() => toggleSegment(i)}
                          className="h-4 w-4 rounded border-gray-300 text-tams-600 focus:ring-tams-500"
                        />
                      </td>
                      <td className="whitespace-nowrap px-3 py-2 font-mono text-xs text-gray-900">
                        {seg.timerange}
                      </td>
                      <td className="px-3 py-2 font-mono text-xs text-gray-500">
                        {seg.object_id.substring(0, 12)}...
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {error && <p className="text-sm text-red-600">{error}</p>}
        </div>

        <div className="mt-6 flex justify-end gap-3">
          <button
            onClick={onClose}
            disabled={adding}
            className="rounded-lg border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
          >
            Cancel
          </button>
          <button
            onClick={handleAdd}
            disabled={adding || selectedIds.size === 0}
            className="rounded-lg bg-tams-600 px-4 py-2 text-sm font-medium text-white shadow-sm hover:bg-tams-700 disabled:opacity-50"
          >
            {adding
              ? "Adding..."
              : `Add ${selectedIds.size} segment${selectedIds.size !== 1 ? "s" : ""}`}
          </button>
        </div>
      </div>
    </div>
  );
}
