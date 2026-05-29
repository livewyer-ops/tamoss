import { Link } from "react-router-dom";
import Badge from "@/components/Badge";

export interface DashboardAttentionItem {
  label: string;
  to: string;
  variant: "danger" | "warning" | "info";
}

export default function DashboardAttentionPanel({
  items,
}: {
  items: DashboardAttentionItem[];
}) {
  return (
    <div className="tamoss-panel rounded-2xl p-6">
      <div className="mb-4 flex items-center justify-between gap-4">
        <h2 className="text-lg font-semibold text-lw-ink-900">
          Needs Attention
        </h2>
        <Badge variant={items.length ? "warning" : "success"}>
          {items.length ? `${items.length} item(s)` : "All clear"}
        </Badge>
      </div>
      {items.length ? (
        <div className="space-y-2">
          {items.map((item) => (
            <Link
              key={item.label}
              to={item.to}
              className="flex items-center justify-between rounded-2xl border border-lw-ink-100 bg-white/80 px-4 py-3 hover:bg-lw-ink-50/70"
            >
              <div className="flex items-center gap-3">
                <Badge variant={item.variant}>{item.variant}</Badge>
                <span className="text-sm text-lw-ink-800">{item.label}</span>
              </div>
              <span className="text-xs font-medium text-tams-600">Inspect</span>
            </Link>
          ))}
        </div>
      ) : (
        <p className="text-sm text-lw-ink-500">
          No active warnings. Service metadata, storage, webhooks, and deletion
          queues look healthy.
        </p>
      )}
    </div>
  );
}
