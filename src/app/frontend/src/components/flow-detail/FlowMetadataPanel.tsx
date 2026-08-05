import { Link } from "react-router";
import InlineEditField from "@/components/InlineEditField";
import SectionHeading from "@/components/SectionHeading";
import type { TamossApiClient } from "@/api/client";
import { formatDate, formatTimerange } from "@/utils/format";
import type { Flow } from "@/types/tams";

interface FlowMetadataPanelProps {
  api: TamossApiClient;
  flow: Flow;
  flowId: string;
  isReadOnly: boolean;
  onRefresh: () => void;
}

export default function FlowMetadataPanel({
  api,
  flow,
  flowId,
  isReadOnly,
  onRefresh,
}: FlowMetadataPanelProps) {
  return (
    <div className="tamoss-panel rounded-2xl p-4 sm:p-6">
      <SectionHeading title="State & Metadata" />

      <h3 className="text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-lw-ink-400">
        Identity
      </h3>
      <dl className="mt-2 space-y-3">
        <div>
          <dt className="text-sm font-medium text-lw-ink-500">Description</dt>
          <dd className="mt-1 text-sm text-lw-ink-900">
            <InlineEditField
              value={flow.description || ""}
              placeholder="Add description..."
              multiline
              disabled={isReadOnly}
              onSave={async (value) => {
                await api.updateFlowDescription(flowId, value);
                onRefresh();
              }}
            />
          </dd>
        </div>
        <div>
          <dt className="text-sm font-medium text-lw-ink-500">Source</dt>
          <dd className="mt-1">
            <Link
              to={`/sources/${flow.source_id}`}
              className="text-sm text-tams-600 hover:text-tams-700"
            >
              {flow.source_id}
            </Link>
          </dd>
        </div>
      </dl>

      <h3 className="mt-6 text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-lw-ink-400">
        Encoding
      </h3>
      <dl className="mt-2 space-y-3">
        <div>
          <dt className="text-sm font-medium text-lw-ink-500">Codec</dt>
          <dd className="mt-1 text-sm text-lw-ink-900">
            <span
              className={
                flow.codec ? "font-mono text-xs" : "text-lw-ink-400 italic"
              }
            >
              {flow.codec || "Not set"}
            </span>
          </dd>
        </div>
        <div>
          <dt className="text-sm font-medium text-lw-ink-500">Container</dt>
          <dd className="mt-1 text-sm text-lw-ink-900">
            <span
              className={
                flow.container ? "font-mono text-xs" : "text-lw-ink-400 italic"
              }
            >
              {flow.container || "Not set"}
            </span>
          </dd>
        </div>
        <div>
          <dt className="text-sm font-medium text-lw-ink-500">
            Average bit rate
          </dt>
          <dd className="mt-1 text-sm text-lw-ink-900">
            <InlineEditField
              value={flow.avg_bit_rate ? String(flow.avg_bit_rate) : ""}
              placeholder="Set average bit rate"
              disabled={isReadOnly}
              onSave={async (value) => {
                const parsed = Number(value);
                if (!Number.isFinite(parsed) || parsed < 0) {
                  throw new Error("Bit rate must be a positive number");
                }
                await api.updateFlowAvgBitRate(flowId, parsed);
                onRefresh();
              }}
            />
          </dd>
        </div>
        <div>
          <dt className="text-sm font-medium text-lw-ink-500">
            Maximum bit rate
          </dt>
          <dd className="mt-1 text-sm text-lw-ink-900">
            <InlineEditField
              value={flow.max_bit_rate ? String(flow.max_bit_rate) : ""}
              placeholder="Set maximum bit rate"
              disabled={isReadOnly}
              onSave={async (value) => {
                const parsed = Number(value);
                if (!Number.isFinite(parsed) || parsed < 0) {
                  throw new Error("Bit rate must be a positive number");
                }
                await api.updateFlowMaxBitRate(flowId, parsed);
                onRefresh();
              }}
            />
          </dd>
        </div>
      </dl>

      <details className="group mt-6">
        <summary className="flex cursor-pointer list-none items-center justify-between text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-lw-ink-400 [&::-webkit-details-marker]:hidden">
          <span>Lifecycle</span>
          <span className="text-lw-ink-400 group-open:hidden">Show</span>
          <span className="hidden text-lw-ink-400 group-open:inline">Hide</span>
        </summary>
        <dl className="mt-2 space-y-3">
          <div>
            <dt className="text-sm font-medium text-lw-ink-500">Timerange</dt>
            <dd className="mt-1 font-mono text-sm text-lw-ink-900">
              {formatTimerange(flow.timerange)}
            </dd>
          </div>
          <div>
            <dt className="text-sm font-medium text-lw-ink-500">Generation</dt>
            <dd className="mt-1 text-sm text-lw-ink-900">
              {flow.generation ?? "N/A"}
            </dd>
          </div>
          <div>
            <dt className="text-sm font-medium text-lw-ink-500">
              Read-only state
            </dt>
            <dd className="mt-1 text-sm text-lw-ink-900">
              {isReadOnly ? "Read-only" : "Writable"}
            </dd>
          </div>
          <div>
            <dt className="text-sm font-medium text-lw-ink-500">Created</dt>
            <dd className="mt-1 text-sm text-lw-ink-900">
              {formatDate(flow.created)}
            </dd>
          </div>
          {flow.created_by && (
            <div>
              <dt className="text-sm font-medium text-lw-ink-500">
                Created By
              </dt>
              <dd className="mt-1 text-sm text-lw-ink-900">
                {flow.created_by}
              </dd>
            </div>
          )}
        </dl>
      </details>
    </div>
  );
}
