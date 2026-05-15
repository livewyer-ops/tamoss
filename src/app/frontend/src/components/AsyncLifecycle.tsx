import Badge from "@/components/Badge";
import { formatDate, formatRelativeTime } from "@/utils/format";

type LifecycleState = "pending" | "active" | "complete" | "error";

interface LifecycleStep {
  id: string;
  label: string;
  description: string;
  state: LifecycleState;
  timestamp?: string | null;
}

const stateMeta: Record<
  LifecycleState,
  { dot: string; badge: "default" | "info" | "success" | "danger" }
> = {
  pending: { dot: "bg-gray-300", badge: "default" },
  active: { dot: "bg-blue-500", badge: "info" },
  complete: { dot: "bg-green-500", badge: "success" },
  error: { dot: "bg-red-500", badge: "danger" },
};

export default function AsyncLifecycle({
  title = "Lifecycle",
  steps,
}: {
  title?: string;
  steps: readonly LifecycleStep[];
}) {
  if (!steps.length) return null;

  return (
    <section className="tamoss-panel rounded-2xl p-6">
      <h2 className="mb-4 text-lg font-semibold text-gray-900">{title}</h2>
      <div className="space-y-4">
        {steps.map((step, index) => {
          const meta = stateMeta[step.state];
          return (
            <div key={step.id} className="flex gap-4">
              <div className="flex w-6 flex-col items-center">
                <div className={`mt-1 h-3 w-3 rounded-full ${meta.dot}`} />
                {index < steps.length - 1 && (
                  <div className="mt-2 h-full w-px bg-gray-300" />
                )}
              </div>
              <div className="pb-4">
                <div className="flex flex-wrap items-center gap-2">
                  <p className="text-sm font-semibold text-gray-900">
                    {step.label}
                  </p>
                  <Badge variant={meta.badge}>{step.state}</Badge>
                </div>
                <p className="mt-1 text-sm text-gray-600">{step.description}</p>
                {step.timestamp && (
                  <p className="mt-1 text-xs text-gray-500">
                    {formatDate(step.timestamp)} (
                    {formatRelativeTime(step.timestamp)})
                  </p>
                )}
              </div>
            </div>
          );
        })}
      </div>
    </section>
  );
}
