import Badge from "@/components/Badge";
import { formatDate, formatRelativeTime } from "@/utils/format";

interface StateStripItem {
  label: string;
  value: string;
  variant?: "default" | "primary" | "success" | "warning" | "danger" | "info";
}

export default function StateStrip({
  title,
  items,
  refreshedAt,
}: {
  title: string;
  items: StateStripItem[];
  refreshedAt?: string | null;
}) {
  return (
    <div className="tamoss-panel-strong mb-6 rounded-2xl p-5">
      <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <div>
          <p className="text-sm font-semibold uppercase tracking-[0.18em] text-lw-ink-700">
            {title}
          </p>
          {refreshedAt && (
            <p className="mt-1 text-xs text-lw-ink-500">
              Snapshot from {formatDate(refreshedAt)} (
              {formatRelativeTime(refreshedAt)})
            </p>
          )}
        </div>
        <div className="flex flex-wrap gap-2">
          {items.map((item) => (
            <Badge
              key={`${item.label}-${item.value}`}
              variant={item.variant ?? "default"}
            >
              {item.label}: {item.value}
            </Badge>
          ))}
        </div>
      </div>
    </div>
  );
}
