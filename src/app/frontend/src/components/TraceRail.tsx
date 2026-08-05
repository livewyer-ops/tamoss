import { Link } from "react-router";

interface TraceRailItem {
  label: string;
  value: string;
  to?: string;
  tone?: "default" | "accent";
}

export default function TraceRail({
  title,
  items,
}: {
  title: string;
  items: TraceRailItem[];
}) {
  if (!items.length) return null;

  return (
    <section className="tamoss-panel rounded-2xl p-6">
      <h2 className="mb-4 text-lg font-semibold text-lw-ink-900">{title}</h2>
      <div className="flex flex-col gap-3">
        {items.map((item, index) => {
          const content = item.to ? (
            <Link
              to={item.to}
              className={`rounded-xl border px-3 py-2.5 text-sm font-medium transition-colors ${
                item.tone === "accent"
                  ? "border-tams-200 bg-tams-50 text-tams-900 hover:bg-tams-100"
                  : "border-lw-ink-100 bg-white text-lw-ink-700 hover:bg-lw-ink-50"
              }`}
            >
              <span className="block text-[0.68rem] uppercase tracking-[0.18em] text-lw-ink-400">
                {item.label}
              </span>
              <span className="mt-1 block break-all font-mono text-xs text-lw-ink-800">
                {item.value}
              </span>
            </Link>
          ) : (
            <div className="rounded-xl border border-lw-ink-100 bg-lw-ink-50 px-3 py-2.5 text-sm text-lw-ink-700">
              <span className="block text-[0.68rem] uppercase tracking-[0.18em] text-lw-ink-400">
                {item.label}
              </span>
              <span className="mt-1 block break-all font-mono text-xs text-lw-ink-800">
                {item.value}
              </span>
            </div>
          );

          return (
            <div
              key={`${item.label}-${index}`}
              className="flex items-center gap-3"
            >
              <div className="flex w-7 flex-col items-center">
                <div
                  className={`h-2.5 w-2.5 rounded-full ${item.tone === "accent" ? "bg-tams-500" : "bg-lw-ink-300"}`}
                />
                {index < items.length - 1 && (
                  <div className="mt-1 h-8 w-px bg-lw-ink-200" />
                )}
              </div>
              <div className="min-w-0 flex-1">{content}</div>
            </div>
          );
        })}
      </div>
    </section>
  );
}
