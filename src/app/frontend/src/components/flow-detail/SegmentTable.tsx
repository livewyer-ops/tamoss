import { Link } from "react-router-dom";
import CopyButton from "@/components/CopyButton";
import { segmentStorageSummary } from "@/pages/flowDetailModel";
import { parseTimerange } from "@/utils/hls-manifest";
import type { FlowSegment } from "@/types/tams";

export default function SegmentTable({
  segments,
  selectedSegments,
  onToggleSegment,
  onToggleAll,
  readOnly,
  rowKeyPrefix,
  compact = false,
  showDuration = false,
}: {
  segments: FlowSegment[];
  selectedSegments: Set<number>;
  onToggleSegment: (index: number) => void;
  onToggleAll: () => void;
  readOnly: boolean;
  rowKeyPrefix: string;
  compact?: boolean;
  showDuration?: boolean;
}) {
  const thClass = compact
    ? "px-3 py-2 text-left text-xs font-medium uppercase text-lw-ink-400"
    : "px-4 py-3 text-left text-xs font-medium uppercase text-lw-ink-500";
  const tdClass = compact ? "px-3 py-1.5" : "px-4 py-3";
  const checkboxClass = compact ? "w-8 px-2 py-2" : "w-8 px-2 py-3";

  return (
    <div
      className={
        compact
          ? "mt-1 overflow-x-auto rounded-lg border border-lw-ink-100"
          : "overflow-x-auto"
      }
    >
      <table className="min-w-full divide-y divide-lw-ink-100">
        <thead>
          <tr>
            <th className={checkboxClass}>
              <input
                type="checkbox"
                checked={
                  selectedSegments.size === segments.length &&
                  segments.length > 0
                }
                onChange={onToggleAll}
                disabled={readOnly}
                className="h-4 w-4 rounded border-lw-ink-200 text-tams-600 focus:ring-tams-500"
              />
            </th>
            <th className={thClass}>Timerange</th>
            <th className={thClass}>Object ID</th>
            <th className={thClass}>TS Offset</th>
            <th className={thClass}>
              {compact ? "Storage" : "Storage / URLs"}
            </th>
          </tr>
        </thead>
        <tbody
          className={
            compact ? "divide-y divide-lw-ink-50" : "divide-y divide-lw-ink-100"
          }
        >
          {segments.map((segment, index) => {
            const isSelected = selectedSegments.has(index);
            const duration = showDuration
              ? parseTimerange(segment.timerange).duration
              : undefined;

            return (
              <tr
                key={`${rowKeyPrefix}-${segment.object_id}-${index}`}
                className={isSelected ? "bg-tams-50" : ""}
              >
                <td className={checkboxClass}>
                  <input
                    type="checkbox"
                    checked={isSelected}
                    onChange={() => onToggleSegment(index)}
                    disabled={readOnly}
                    className="h-4 w-4 rounded border-lw-ink-200 text-tams-600 focus:ring-tams-500"
                  />
                </td>
                <td
                  className={`${tdClass} whitespace-nowrap font-mono text-xs ${
                    compact ? "text-lw-ink-700" : "text-lw-ink-900"
                  }`}
                >
                  {segment.timerange}
                  {duration !== undefined && (
                    <span className="ml-1 text-lw-ink-400">
                      ({duration.toFixed(0)}s)
                    </span>
                  )}
                </td>
                <td className={tdClass}>
                  <div className="flex items-center gap-1">
                    <Link
                      to={`/objects/${segment.object_id}`}
                      className="font-mono text-xs text-tams-600 hover:text-tams-700"
                    >
                      {segment.object_id.substring(0, compact ? 12 : 12)}...
                    </Link>
                    <CopyButton text={segment.object_id} label="Copy" />
                  </div>
                </td>
                <td className={`${tdClass} font-mono text-xs text-lw-ink-500`}>
                  {segment.ts_offset ?? "N/A"}
                </td>
                <td className={`${tdClass} text-xs text-lw-ink-500`}>
                  {segmentStorageSummary(segment)}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
