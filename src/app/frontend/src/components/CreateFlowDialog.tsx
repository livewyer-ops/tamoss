import { useState, useEffect } from "react";
import { useApi } from "@/contexts/ApiContext";
import type { Flow, FlowCollectionItem, FlowSegment } from "@/types/tams";
import { formatFormat, formatCodec } from "@/utils/format";
import { segmentToWritePayload } from "@/utils/segment-payload";

interface CreateFlowDialogProps {
  open: boolean;
  onClose: () => void;
  flows: Flow[];
  onCreated: (flowId: string) => void;
}

const COPY_SEGMENT_PAGE_SIZE = "300";

export default function CreateFlowDialog({
  open,
  onClose,
  flows,
  onCreated,
}: CreateFlowDialogProps) {
  const api = useApi();
  const [label, setLabel] = useState("");
  const [description, setDescription] = useState("");
  const [templateFlowId, setTemplateFlowId] = useState("");
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) {
      setLabel("");
      setDescription("");
      setTemplateFlowId("");
      setError(null);
    }
  }, [open]);

  if (!open) return null;

  const selectedTemplate = flows.find((f) => f.id === templateFlowId);

  const handleCreate = async () => {
    if (!label.trim() || !selectedTemplate) return;
    setCreating(true);
    setError(null);
    try {
      const newFlowId = crypto.randomUUID();
      let templateSegments: FlowSegment[] = [];
      const segResult = await api.getFlowSegments(selectedTemplate.id, {
        include_object_timerange: true,
        limit: COPY_SEGMENT_PAGE_SIZE,
      });
      if (segResult.nextKey) {
        throw new Error(
          `Template has more than ${COPY_SEGMENT_PAGE_SIZE} segments.`,
        );
      }
      templateSegments = segResult.data;

      const data: Partial<Flow> = {
        source_id: selectedTemplate.source_id,
        format: selectedTemplate.format,
        label: label.trim(),
        description: description.trim() || undefined,
        codec: selectedTemplate.codec,
        container: selectedTemplate.container,
        essence_parameters: selectedTemplate.essence_parameters,
        segment_duration: selectedTemplate.segment_duration,
        avg_bit_rate: selectedTemplate.avg_bit_rate,
        max_bit_rate: selectedTemplate.max_bit_rate,
      };
      await api.createFlow(newFlowId, data);

      if (templateSegments.length > 0) {
        await api.addFlowSegments(
          newFlowId,
          templateSegments.map(segmentToWritePayload),
        );
      }

      if (selectedTemplate.format === "urn:x-nmos:format:multi") {
        const collection: FlowCollectionItem[] = await api
          .getFlowCollection(selectedTemplate.id)
          .catch(() => []);
        if (collection.length > 0) {
          await api.setFlowCollection(
            newFlowId,
            collection.map((c) => ({ id: c.id, role: c.role ?? "" })),
          );
        }
      }

      onCreated(newFlowId);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create flow");
    } finally {
      setCreating(false);
    }
  };

  const canCreate = label.trim().length > 0 && Boolean(selectedTemplate);

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div className="mx-4 w-full max-w-lg rounded-xl bg-white p-6 shadow-xl">
        <h2 className="mb-4 text-lg font-semibold text-gray-900">
          Create New Flow
        </h2>

        <div className="space-y-4">
          <div>
            <label
              htmlFor="template-flow"
              className="block text-sm font-medium text-gray-700"
            >
              Copy properties from <span className="text-red-500">*</span>
            </label>
            <select
              id="template-flow"
              value={templateFlowId}
              onChange={(e) => setTemplateFlowId(e.target.value)}
              disabled={creating}
              className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-tams-500 focus:outline-none focus:ring-1 focus:ring-tams-500 disabled:opacity-50"
            >
              <option value="">Select a flow...</option>
              {flows.map((f) => (
                <option key={f.id} value={f.id}>
                  {f.label || f.id} ({formatFormat(f.format)}
                  {f.codec ? ` - ${formatCodec(f.codec)}` : ""})
                </option>
              ))}
            </select>
          </div>

          <div>
            <label
              htmlFor="new-flow-label"
              className="block text-sm font-medium text-gray-700"
            >
              Label <span className="text-red-500">*</span>
            </label>
            <input
              id="new-flow-label"
              type="text"
              value={label}
              onChange={(e) => setLabel(e.target.value)}
              placeholder="Flow label..."
              disabled={creating}
              className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-tams-500 focus:outline-none focus:ring-1 focus:ring-tams-500 disabled:opacity-50"
            />
          </div>

          <div>
            <label
              htmlFor="new-flow-description"
              className="block text-sm font-medium text-gray-700"
            >
              Description
            </label>
            <textarea
              id="new-flow-description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Optional description..."
              rows={2}
              disabled={creating}
              className="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-tams-500 focus:outline-none focus:ring-1 focus:ring-tams-500 disabled:opacity-50"
            />
          </div>

          {error && <p className="text-sm text-red-600">{error}</p>}
        </div>

        <div className="mt-6 flex justify-end gap-3">
          <button
            onClick={onClose}
            disabled={creating}
            className="rounded-lg border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
          >
            Cancel
          </button>
          <button
            onClick={handleCreate}
            disabled={creating || !canCreate}
            className="rounded-lg bg-tams-600 px-4 py-2 text-sm font-medium text-white shadow-sm hover:bg-tams-700 disabled:opacity-50"
          >
            {creating ? "Creating..." : "Create"}
          </button>
        </div>
      </div>
    </div>
  );
}
