import { useEffect, useState } from "react";
import { useApi } from "@/contexts/ApiContext";
import { useApiQuery } from "@/hooks/useApiQuery";
import { usePageTitle } from "@/hooks/usePageTitle";
import ErrorMessage from "@/components/ErrorMessage";
import Badge from "@/components/Badge";
import Skeleton from "@/components/Skeleton";
import StateStrip from "@/components/StateStrip";
import DashboardAttentionPanel from "@/components/dashboard/DashboardAttentionPanel";
import { formatDate, formatRelativeTime } from "@/utils/format";
import { Link } from "react-router";
import {
  DASHBOARD_COLLECTION_PAGE_SIZE,
  buildRecentActivity,
  getSystemState,
} from "@/pages/dashboardModel";

export default function DashboardPage() {
  usePageTitle("Dashboard");
  const api = useApi();
  const [snapshotAt, setSnapshotAt] = useState<string | null>(null);

  const service = useApiQuery(() => api.getService(), [api]);
  const health = useApiQuery(() => api.getHealth(), [api]);
  const sources = useApiQuery(
    () => api.getSources({ limit: DASHBOARD_COLLECTION_PAGE_SIZE }),
    [api],
  );
  const flows = useApiQuery(
    () => api.getFlows({ limit: DASHBOARD_COLLECTION_PAGE_SIZE }),
    [api],
  );
  const backends = useApiQuery(() => api.getStorageBackends(), [api]);
  const webhooks = useApiQuery(
    () => api.getWebhooks({ limit: DASHBOARD_COLLECTION_PAGE_SIZE }),
    [api],
  );
  const deletions = useApiQuery(() => api.getDeletionRequests(), [api]);

  const loading =
    health.loading ||
    service.loading ||
    sources.loading ||
    flows.loading ||
    webhooks.loading ||
    deletions.loading;
  const erroredWebhooks = (webhooks.data?.data ?? []).filter(
    (webhook) => webhook.status === "error",
  );
  const activeDeletions = (deletions.data ?? []).filter(
    (request) => request.status === "created" || request.status === "started",
  );
  const recentActivity = buildRecentActivity({
    flows: flows.data?.data ?? [],
    sources: sources.data?.data ?? [],
    deletions: deletions.data ?? [],
  });
  const systemState = getSystemState({
    healthError: Boolean(health.error),
    serviceError: Boolean(service.error),
    backendError: Boolean(backends.error),
    erroredWebhooks: erroredWebhooks.length,
    activeDeletions: activeDeletions.length,
  });
  const attentionItems = [
    ...(health.error
      ? [
          {
            label: "Health check failed to load",
            to: "/service",
            variant: "danger" as const,
          },
        ]
      : []),
    ...(service.error
      ? [
          {
            label: "Service metadata failed to load",
            to: "/service",
            variant: "danger" as const,
          },
        ]
      : []),
    ...(backends.error
      ? [
          {
            label: "Storage backends failed to load",
            to: "/service",
            variant: "danger" as const,
          },
        ]
      : []),
    ...erroredWebhooks.slice(0, 3).map((webhook) => ({
      label: `Webhook error: ${webhook.url}`,
      to: "/webhooks",
      variant: "warning" as const,
    })),
    ...activeDeletions.slice(0, 3).map((request) => ({
      label: `Deletion in progress: ${request.flow_id}`,
      to: "/deletions",
      variant: "info" as const,
    })),
  ];

  useEffect(() => {
    if (!loading) {
      setSnapshotAt(new Date().toISOString());
    }
  }, [
    loading,
    health.data,
    service.data,
    sources.data,
    flows.data,
    backends.data,
    webhooks.data,
    deletions.data,
  ]);

  const [onboardingDismissed, setOnboardingDismissed] = useState(false);
  useEffect(() => {
    setOnboardingDismissed(
      window.localStorage.getItem("tamoss.onboarded") === "1",
    );
  }, []);
  const dismissOnboarding = () => {
    window.localStorage.setItem("tamoss.onboarded", "1");
    setOnboardingDismissed(true);
  };
  const isFirstRun =
    !sources.loading &&
    !flows.loading &&
    (sources.data?.data.length ?? 0) === 0 &&
    (flows.data?.data.length ?? 0) === 0;
  const showOnboarding = isFirstRun && !onboardingDismissed;

  return (
    <div className="p-4 sm:p-6 lg:p-8">
      <div className="mb-6 sm:mb-8">
        <div className="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
          <div>
            <div className="flex items-center gap-3">
              <h1 className="text-2xl font-extrabold text-lw-ink-900 sm:text-3xl">
                Dashboard
              </h1>
              <Badge variant={systemState.variant}>{systemState.label}</Badge>
            </div>
            <p className="mt-2 max-w-2xl text-sm leading-6 text-lw-ink-500">
              Operational summary of your TAMOSS service instance
            </p>
          </div>
          <div className="flex flex-wrap gap-2">
            <Link
              to="/service"
              className="tamoss-button-primary px-4 py-2.5 text-sm font-semibold"
            >
              Inspect Service
            </Link>
          </div>
        </div>
      </div>

      {showOnboarding && (
        <div className="tamoss-panel mb-6 rounded-2xl p-6">
          <div className="flex items-start justify-between gap-4">
            <div>
              <p className="text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-tams-700">
                Welcome
              </p>
              <h2 className="mt-1 text-xl font-semibold text-lw-ink-900">
                Set up your TAMOSS instance
              </h2>
              <p className="mt-1 text-sm leading-6 text-lw-ink-500">
                No flows or sources yet. Pick a place to start.
              </p>
            </div>
            <button
              type="button"
              onClick={dismissOnboarding}
              className="text-xs font-medium text-lw-ink-500 hover:text-lw-ink-900"
            >
              Dismiss
            </button>
          </div>
          <div className="mt-5 grid gap-3 sm:grid-cols-3">
            <Link
              to="/service"
              className="rounded-xl border border-lw-ink-100 p-4 transition-colors hover:bg-lw-ink-50"
            >
              <p className="text-sm font-semibold text-lw-ink-900">
                Browse the API
              </p>
              <p className="mt-1 text-xs leading-5 text-lw-ink-500">
                Inspect service metadata, routes, and storage backends.
              </p>
            </Link>
            <Link
              to="/ingest"
              className="rounded-xl border border-lw-ink-100 p-4 transition-colors hover:bg-lw-ink-50"
            >
              <p className="text-sm font-semibold text-lw-ink-900">
                Ingest your first media
              </p>
              <p className="mt-1 text-xs leading-5 text-lw-ink-500">
                Allocate storage, upload, and register flows.
              </p>
            </Link>
            <a
              href="https://github.com/bbc/tams"
              target="_blank"
              rel="noreferrer"
              className="rounded-xl border border-lw-ink-100 p-4 transition-colors hover:bg-lw-ink-50"
            >
              <p className="text-sm font-semibold text-lw-ink-900">
                Read the spec
              </p>
              <p className="mt-1 text-xs leading-5 text-lw-ink-500">
                BBC TAMS API specification on GitHub.
              </p>
            </a>
          </div>
        </div>
      )}

      <StateStrip
        title="System State"
        refreshedAt={snapshotAt}
        items={[
          {
            label: "healthz",
            value: health.error
              ? "failing"
              : (health.data ?? "").trim().toLowerCase() === "ok"
                ? "ok"
                : "unknown",
            variant: health.error ? "danger" : "success",
          },
          {
            label: "service",
            value: service.error ? "failing" : "reachable",
            variant: service.error ? "danger" : "success",
          },
          {
            label: "storage",
            value: backends.error
              ? "failing"
              : backends.data?.length
                ? "configured"
                : "missing",
            variant: backends.error
              ? "danger"
              : backends.data?.length
                ? "success"
                : "warning",
          },
          {
            label: "warnings",
            value: String(erroredWebhooks.length + activeDeletions.length),
            variant:
              erroredWebhooks.length + activeDeletions.length > 0
                ? "warning"
                : "success",
          },
        ]}
      />

      <div className="mb-8 grid gap-4 lg:grid-cols-4">
        <Link
          to="/ingest"
          className="tamoss-panel rounded-2xl p-4 transition-colors hover:bg-lw-ink-50"
        >
          <p className="text-sm font-semibold uppercase tracking-[0.18em] text-tams-700">
            Start ingest
          </p>
          <p className="mt-2 text-sm leading-6 text-lw-ink-600">
            Allocate storage, upload media, and register new flows.
          </p>
        </Link>
        <Link
          to="/deletions"
          className="tamoss-panel rounded-2xl p-4 transition-colors hover:bg-lw-ink-50"
        >
          <p className="text-sm font-semibold uppercase tracking-[0.18em] text-tams-700">
            Inspect queue
          </p>
          <p className="mt-2 text-sm leading-6 text-lw-ink-600">
            Review active and errored deletion requests.
          </p>
        </Link>
        <Link
          to="/webhooks"
          className="tamoss-panel rounded-2xl p-4 transition-colors hover:bg-lw-ink-50"
        >
          <p className="text-sm font-semibold uppercase tracking-[0.18em] text-tams-700">
            Trace webhooks
          </p>
          <p className="mt-2 text-sm leading-6 text-lw-ink-600">
            Inspect webhook health, payloads, and operator filters.
          </p>
        </Link>
        <Link
          to="/service"
          className="tamoss-panel rounded-2xl p-4 transition-colors hover:bg-lw-ink-50"
        >
          <p className="text-sm font-semibold uppercase tracking-[0.18em] text-tams-700">
            Check service
          </p>
          <p className="mt-2 text-sm leading-6 text-lw-ink-600">
            Verify service profile, routes, and storage backend state.
          </p>
        </Link>
      </div>

      <DashboardAttentionPanel items={attentionItems} />

      {service.error && (
        <ErrorMessage message={service.error} onRetry={service.refetch} />
      )}

      <div className="grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-3">
        <Link
          to="/sources"
          className="tamoss-panel rounded-2xl p-6 transition-colors hover:bg-lw-ink-50"
        >
          <div className="flex items-center gap-3">
            <svg
              className="h-5 w-5 text-lw-ink-500"
              fill="none"
              viewBox="0 0 24 24"
              strokeWidth={1.5}
              stroke="currentColor"
              aria-hidden="true"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10"
              />
            </svg>
            <div>
              {sources.loading && !sources.data ? (
                <Skeleton className="mb-1 h-7 w-12" />
              ) : (
                <p className="text-2xl font-bold text-gray-900">
                  {sources.data?.data.length !== undefined
                    ? `${sources.data.data.length}${sources.data.nextKey ? "+" : ""}`
                    : "-"}
                </p>
              )}
              <p className="text-sm text-gray-500">Sources</p>
            </div>
          </div>
        </Link>

        <Link
          to="/flows"
          className="tamoss-panel rounded-2xl p-6 transition-colors hover:bg-lw-ink-50"
        >
          <div className="flex items-center gap-3">
            <svg
              className="h-5 w-5 text-lw-ink-500"
              fill="none"
              viewBox="0 0 24 24"
              strokeWidth={1.5}
              stroke="currentColor"
              aria-hidden="true"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="M7 4V2m0 2a2 2 0 100 4m0-4a2 2 0 110 4m0 0v14m0-4a2 2 0 100-4m0 4a2 2 0 110-4m10-2a2 2 0 100-4m0 4a2 2 0 110-4m0 0V6"
              />
            </svg>
            <div>
              {flows.loading && !flows.data ? (
                <Skeleton className="mb-1 h-7 w-12" />
              ) : (
                <p className="text-2xl font-bold text-gray-900">
                  {flows.data?.data.length !== undefined
                    ? `${flows.data.data.length}${flows.data.nextKey ? "+" : ""}`
                    : "-"}
                </p>
              )}
              <p className="text-sm text-gray-500">Flows</p>
            </div>
          </div>
        </Link>

        <div className="tamoss-panel rounded-2xl p-6">
          <div className="flex items-center gap-3">
            <svg
              className="h-5 w-5 text-lw-ink-500"
              fill="none"
              viewBox="0 0 24 24"
              strokeWidth={1.5}
              stroke="currentColor"
              aria-hidden="true"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2"
              />
            </svg>
            <div>
              {backends.loading && !backends.data ? (
                <Skeleton className="mb-1 h-7 w-12" />
              ) : (
                <p className="text-2xl font-bold text-gray-900">
                  {backends.data?.length ?? "-"}
                </p>
              )}
              <p className="text-sm text-gray-500">Storage Backends</p>
            </div>
          </div>
        </div>

        <Link
          to="/webhooks"
          className="tamoss-panel rounded-2xl p-6 transition-colors hover:bg-lw-ink-50"
        >
          <div className="flex items-center gap-3">
            <svg
              className="h-5 w-5 text-lw-ink-500"
              fill="none"
              viewBox="0 0 24 24"
              strokeWidth={1.5}
              stroke="currentColor"
              aria-hidden="true"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="M7.5 8.25h9m-9 3h9m-9 3h5.25M6.75 3h10.5A2.25 2.25 0 0119.5 5.25v13.5A2.25 2.25 0 0117.25 21H6.75A2.25 2.25 0 014.5 18.75V5.25A2.25 2.25 0 016.75 3z"
              />
            </svg>
            <div>
              {webhooks.loading && !webhooks.data ? (
                <Skeleton className="mb-1 h-7 w-12" />
              ) : (
                <p className="text-2xl font-bold text-gray-900">
                  {webhooks.data?.data.length ?? "-"}
                </p>
              )}
              <p className="text-sm text-gray-500">Webhooks</p>
            </div>
          </div>
        </Link>

        <Link
          to="/deletions"
          className="tamoss-panel rounded-2xl p-6 transition-colors hover:bg-lw-ink-50"
        >
          <div className="flex items-center gap-3">
            <svg
              className="h-5 w-5 text-lw-ink-500"
              fill="none"
              viewBox="0 0 24 24"
              strokeWidth={1.5}
              stroke="currentColor"
              aria-hidden="true"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"
              />
            </svg>
            <div>
              {deletions.loading && !deletions.data ? (
                <Skeleton className="mb-1 h-7 w-12" />
              ) : (
                <p className="text-2xl font-bold text-gray-900">
                  {deletions.data?.length ?? "-"}
                </p>
              )}
              <p className="text-sm text-gray-500">Deletion Requests</p>
            </div>
          </div>
        </Link>
      </div>

      <div className="mt-8 grid gap-6 lg:grid-cols-2">
        <div className="tamoss-panel rounded-2xl p-6">
          <div className="mb-4 flex items-center justify-between gap-4">
            <h2 className="text-lg font-semibold text-gray-900">
              Recent activity
            </h2>
            <Badge variant="default">{recentActivity.length} items</Badge>
          </div>
          {recentActivity.length ? (
            <div className="space-y-2">
              {recentActivity.map((item) => (
                <Link
                  key={item.key}
                  to={item.to}
                  className="flex items-start justify-between rounded-lg border border-gray-100 px-3 py-3 hover:bg-gray-50"
                >
                  <div>
                    <div className="flex items-center gap-2">
                      <Badge variant={item.variant}>{item.subtitle}</Badge>
                      <span className="text-sm font-medium text-gray-900">
                        {item.title}
                      </span>
                    </div>
                    {item.created && (
                      <p className="mt-1 text-xs text-gray-500">
                        {formatDate(item.created)} (
                        {formatRelativeTime(item.created)})
                      </p>
                    )}
                  </div>
                  <span className="text-xs font-medium text-tams-600">
                    Inspect
                  </span>
                </Link>
              ))}
            </div>
          ) : (
            <p className="text-sm text-gray-500">
              No recent activity to display.
            </p>
          )}
        </div>

        <div className="tamoss-panel rounded-2xl p-6">
          <h2 className="mb-4 text-lg font-semibold text-gray-900">
            Webhook Status
          </h2>
          <div className="flex flex-wrap gap-2">
            {(["created", "started", "disabled", "error"] as const).map(
              (status) => {
                const count =
                  webhooks.data?.data.filter(
                    (webhook) => webhook.status === status,
                  ).length ?? 0;
                return (
                  <Badge key={status} variant={count > 0 ? "info" : "default"}>
                    {status}: {count}
                  </Badge>
                );
              },
            )}
          </div>
        </div>

        <div className="tamoss-panel rounded-2xl p-6">
          <h2 className="mb-4 text-lg font-semibold text-gray-900">
            Deletion Queue
          </h2>
          <div className="flex flex-wrap gap-2">
            {(["created", "started", "done", "error"] as const).map(
              (status) => {
                const count =
                  deletions.data?.filter((request) => request.status === status)
                    .length ?? 0;
                return (
                  <Badge key={status} variant={count > 0 ? "info" : "default"}>
                    {status}: {count}
                  </Badge>
                );
              },
            )}
          </div>
        </div>
      </div>

      <div className="mt-8 grid gap-6 lg:grid-cols-2">
        <div className="tamoss-panel rounded-2xl p-6">
          <div className="mb-4 flex items-center justify-between gap-4">
            <h2 className="text-lg font-semibold text-gray-900">
              Webhook issues
            </h2>
            <Link
              to="/webhooks"
              className="text-sm font-medium text-tams-600 hover:text-tams-700"
            >
              Inspect webhooks
            </Link>
          </div>
          {erroredWebhooks.length ? (
            <div className="space-y-2">
              {erroredWebhooks.slice(0, 5).map((webhook) => (
                <div
                  key={webhook.id}
                  className="rounded-lg bg-red-50 px-3 py-2"
                >
                  <p className="text-sm font-medium text-red-900">
                    {webhook.url}
                  </p>
                  <p className="mt-1 text-xs text-red-700">
                    {webhook.error?.summary ?? "Unknown webhook error"}
                  </p>
                </div>
              ))}
            </div>
          ) : (
            <p className="text-sm text-gray-500">No webhook errors reported.</p>
          )}
        </div>

        <div className="tamoss-panel rounded-2xl p-6">
          <div className="mb-4 flex items-center justify-between gap-4">
            <h2 className="text-lg font-semibold text-gray-900">
              Active deletions
            </h2>
            <Link
              to="/deletions"
              className="text-sm font-medium text-tams-600 hover:text-tams-700"
            >
              Open queue
            </Link>
          </div>
          {activeDeletions.length ? (
            <div className="space-y-2">
              {activeDeletions.slice(0, 5).map((request) => (
                <div
                  key={request.id}
                  className="rounded-lg bg-gray-50 px-3 py-2"
                >
                  <p className="text-sm font-medium text-gray-900">
                    {request.flow_id}
                  </p>
                  <p className="mt-1 text-xs text-gray-500">
                    {request.status} · {request.timerange_to_delete}
                  </p>
                </div>
              ))}
            </div>
          ) : (
            <p className="text-sm text-gray-500">
              No active deletion requests.
            </p>
          )}
        </div>
      </div>
    </div>
  );
}
