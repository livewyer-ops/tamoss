import { useQuery } from "@tanstack/react-query";
import {
  AlertTriangle,
  ArrowRight,
  Boxes,
  Database,
  Radio,
  Server,
} from "lucide-react";
import { Link } from "react-router";
import {
  Page,
  PageHeader,
  Panel,
  QueryMessage,
  StatusBadge,
  surfaceStyles,
} from "@/components/Surface";
import { useApi } from "@/contexts/ApiContext";
import { useRuntime } from "@/control/runtime";
import { usePageTitle } from "@/hooks/usePageTitle";
import { formatRelativeTime } from "@/utils/format";

export default function DashboardPage() {
  usePageTitle("Overview");
  const api = useApi();
  const health = useQuery({
    queryKey: ["api", "health"],
    queryFn: () => api.getHealth(),
  });
  const sources = useQuery({
    queryKey: ["api", "sources", "summary"],
    queryFn: () => api.getSources({ limit: "50" }),
  });
  const flows = useQuery({
    queryKey: ["api", "flows", "summary"],
    queryFn: () => api.getFlows({ limit: "50", include_timerange: true }),
  });
  const deletions = useQuery({
    queryKey: ["api", "deletions", "summary"],
    queryFn: () =>
      api.getDeletionRequests({
        limit: "100",
        sort_by: "created",
        reverse_order: true,
      }),
  });
  const runtime = useRuntime();
  const activeDeletions =
    deletions.data?.data.filter(
      (item) => !["done", "error"].includes(item.status),
    ).length ?? 0;
  const deletionSummaryPartial = Boolean(deletions.data?.nextKey);
  const unavailable =
    runtime.data?.workloads.filter((item) => item.status === "unavailable")
      .length ?? 0;
  const progressing =
    runtime.data?.workloads.filter((item) => item.status === "progressing")
      .length ?? 0;
  const scaledDown =
    runtime.data?.workloads.filter((item) => item.status === "scaledDown")
      .length ?? 0;
  const hibernated = runtime.data?.instance.phase === "Hibernated";
  const runtimeStale = Boolean(runtime.data?.stale || runtime.error);
  const runtimeServices = runtime.data?.services ?? [];
  const runtimeEndpointSlices = runtime.data?.endpointSlices ?? [];
  const unhealthyRouteNames = new Set(
    runtimeEndpointSlices
      .filter(
        (slice) =>
          slice.notReadyEndpoints > 0 ||
          (!hibernated && slice.totalEndpoints === 0),
      )
      .map((slice) => slice.serviceName || slice.name),
  );
  if (!hibernated) {
    for (const service of runtimeServices) {
      if (!service.selectorComponent) continue;
      if (
        !runtimeEndpointSlices.some(
          (slice) => slice.serviceName === service.name,
        )
      ) {
        unhealthyRouteNames.add(service.name);
      }
    }
  }
  const unhealthyRoutes = unhealthyRouteNames.size;
  const readyWorkloads =
    runtime.data?.workloads.filter((item) => item.status === "ready").length ??
    0;
  const unexpectedScaledDown = hibernated ? 0 : scaledDown;
  const notReady = unavailable + progressing + unexpectedScaledDown;
  const warnings =
    runtime.data?.events
      .filter((item) => item.type === "Warning")
      .slice(0, 5) ?? [];
  const summaryFailures = [
    { name: "TAMS API", failed: health.isError },
    { name: "Sources summary", failed: sources.isError },
    { name: "Flows summary", failed: flows.isError },
    { name: "Deletion summary", failed: deletions.isError },
  ].filter((query) => query.failed);
  const summaryLoading =
    health.isLoading ||
    sources.isLoading ||
    flows.isLoading ||
    deletions.isLoading;

  return (
    <Page>
      <PageHeader title="Overview" />
      <div className={surfaceStyles.stack}>
        <div className={surfaceStyles.grid4}>
          <div className={`${surfaceStyles.panel} ${surfaceStyles.metric}`}>
            <div className={surfaceStyles.metricLabel}>
              <Server size={14} aria-hidden="true" /> TAMS API
            </div>
            <div className={surfaceStyles.metricValue}>
              {health.isLoading
                ? "Checking"
                : health.isError
                  ? "Unavailable"
                  : "Healthy"}
            </div>
            <StatusBadge
              tone={
                health.isError
                  ? "error"
                  : health.isLoading
                    ? "neutral"
                    : "success"
              }
            >
              {health.isError ? "Request failed" : health.data || "Ready"}
            </StatusBadge>
          </div>
          <div className={`${surfaceStyles.panel} ${surfaceStyles.metric}`}>
            <div className={surfaceStyles.metricLabel}>
              <Database size={14} aria-hidden="true" /> Sources
            </div>
            <div className={surfaceStyles.metricValue}>
              {sources.data?.data.length ?? "-"}
            </div>
            <span className={surfaceStyles.secondary}>
              {sources.data?.nextKey ? "At least this many" : "Current catalog"}
            </span>
          </div>
          <div className={`${surfaceStyles.panel} ${surfaceStyles.metric}`}>
            <div className={surfaceStyles.metricLabel}>
              <Radio size={14} aria-hidden="true" /> Flows
            </div>
            <div className={surfaceStyles.metricValue}>
              {flows.data?.data.length ?? "-"}
            </div>
            <span className={surfaceStyles.secondary}>
              {flows.data?.nextKey ? "At least this many" : "Current catalog"}
            </span>
          </div>
          <div className={`${surfaceStyles.panel} ${surfaceStyles.metric}`}>
            <div className={surfaceStyles.metricLabel}>
              <Boxes size={14} aria-hidden="true" /> Runtime
            </div>
            <div className={surfaceStyles.metricValue}>
              {runtime.data
                ? hibernated
                  ? "Paused"
                  : `${readyWorkloads}/${runtime.data.workloads.length}`
                : "-"}
            </div>
            <StatusBadge
              tone={
                !runtime.data
                  ? "neutral"
                  : runtimeStale
                    ? "warning"
                    : hibernated
                      ? "info"
                      : unavailable > 0
                        ? "error"
                        : notReady > 0
                          ? "warning"
                          : "success"
              }
            >
              {!runtime.data
                ? "Unavailable"
                : runtimeStale
                  ? "Stale"
                  : hibernated
                    ? "Hibernated"
                    : unavailable > 0
                      ? `${unavailable} unavailable`
                      : progressing > 0
                        ? `${progressing} progressing`
                        : scaledDown > 0
                          ? `${scaledDown} scaled down`
                          : "All ready"}
            </StatusBadge>
          </div>
        </div>

        <div className={surfaceStyles.grid2}>
          <Panel
            title="Attention required"
            actions={
              <Link className={surfaceStyles.resourceLink} to="/system">
                Runtime <ArrowRight size={13} aria-hidden="true" />
              </Link>
            }
          >
            {runtime.isLoading || summaryLoading ? (
              <QueryMessage loading />
            ) : runtime.error && !runtime.data ? (
              <QueryMessage error={runtime.error} />
            ) : warnings.length === 0 &&
              activeDeletions === 0 &&
              notReady === 0 &&
              unhealthyRoutes === 0 &&
              summaryFailures.length === 0 &&
              !runtimeStale ? (
              <QueryMessage
                empty={{
                  title: "No active warnings",
                }}
              />
            ) : (
              <div className={surfaceStyles.tableWrap}>
                <table className={surfaceStyles.table}>
                  <thead>
                    <tr>
                      <th>Signal</th>
                      <th>Resource</th>
                      <th>Observed</th>
                    </tr>
                  </thead>
                  <tbody>
                    {summaryFailures.map((failure) => (
                      <tr key={failure.name}>
                        <td>
                          <StatusBadge tone="error">Request failed</StatusBadge>
                        </td>
                        <td>{failure.name}</td>
                        <td>Now</td>
                      </tr>
                    ))}
                    {runtimeStale && runtime.data ? (
                      <tr>
                        <td>
                          <StatusBadge tone="warning">Stale</StatusBadge>
                        </td>
                        <td>
                          {runtime.error
                            ? "Runtime refresh failed"
                            : "Runtime snapshot"}
                        </td>
                        <td>{formatRelativeTime(runtime.data.observedAt)}</td>
                      </tr>
                    ) : null}
                    {unavailable > 0 ? (
                      <tr>
                        <td>
                          <StatusBadge tone="error">Unavailable</StatusBadge>
                        </td>
                        <td>
                          {unavailable} workload{unavailable === 1 ? "" : "s"}
                        </td>
                        <td>Now</td>
                      </tr>
                    ) : null}
                    {progressing > 0 ? (
                      <tr>
                        <td>
                          <StatusBadge tone="warning">Progressing</StatusBadge>
                        </td>
                        <td>
                          {progressing} workload{progressing === 1 ? "" : "s"}
                        </td>
                        <td>Now</td>
                      </tr>
                    ) : null}
                    {unexpectedScaledDown > 0 ? (
                      <tr>
                        <td>
                          <StatusBadge tone="warning">Scaled down</StatusBadge>
                        </td>
                        <td>
                          {unexpectedScaledDown} workload
                          {unexpectedScaledDown === 1 ? "" : "s"}
                        </td>
                        <td>Now</td>
                      </tr>
                    ) : null}
                    {unhealthyRoutes > 0 ? (
                      <tr>
                        <td>
                          <StatusBadge tone="error">Routing</StatusBadge>
                        </td>
                        <td>
                          {unhealthyRoutes} unhealthy service route
                          {unhealthyRoutes === 1 ? "" : "s"}
                        </td>
                        <td>Now</td>
                      </tr>
                    ) : null}
                    {activeDeletions > 0 ? (
                      <tr>
                        <td>
                          <StatusBadge tone="warning">Deletion</StatusBadge>
                        </td>
                        <td>
                          {activeDeletions}
                          {deletionSummaryPartial ? "+" : ""} active request
                          {activeDeletions === 1 ? "" : "s"}
                        </td>
                        <td>
                          {deletionSummaryPartial ? "Latest page" : "Now"}
                        </td>
                      </tr>
                    ) : null}
                    {warnings.map((event) => (
                      <tr
                        key={`${event.regarding.kind}-${event.regarding.name}-${event.reason}`}
                      >
                        <td>
                          <StatusBadge tone="warning">
                            <AlertTriangle size={12} aria-hidden="true" />{" "}
                            {event.reason}
                          </StatusBadge>
                        </td>
                        <td>
                          {event.regarding.kind}/{event.regarding.name}
                        </td>
                        <td>{formatRelativeTime(event.lastObservedAt)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Panel>

          <Panel
            title="Active operations"
            actions={
              <Link className={surfaceStyles.resourceLink} to="/ingest">
                All jobs <ArrowRight size={13} aria-hidden="true" />
              </Link>
            }
          >
            {!runtime.data ? (
              <QueryMessage
                empty={{
                  title: "Runtime data unavailable",
                }}
              />
            ) : runtime.data.jobs.filter((job) =>
                ["pending", "running"].includes(job.status),
              ).length === 0 ? (
              <QueryMessage
                empty={{
                  title: "No active jobs",
                }}
              />
            ) : (
              <div className={surfaceStyles.tableWrap}>
                <table className={surfaceStyles.table}>
                  <thead>
                    <tr>
                      <th>Job</th>
                      <th>Component</th>
                      <th>Status</th>
                    </tr>
                  </thead>
                  <tbody>
                    {runtime.data.jobs
                      .filter((job) =>
                        ["pending", "running"].includes(job.status),
                      )
                      .map((job) => (
                        <tr key={job.name}>
                          <td className={surfaceStyles.mono}>{job.name}</td>
                          <td>{job.component}</td>
                          <td>
                            <StatusBadge
                              tone={
                                job.status === "running" ? "info" : "neutral"
                              }
                            >
                              {job.status}
                            </StatusBadge>
                          </td>
                        </tr>
                      ))}
                  </tbody>
                </table>
              </div>
            )}
          </Panel>
        </div>
      </div>
    </Page>
  );
}
