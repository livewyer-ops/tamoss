import { RefreshCw } from "lucide-react";
import {
  Button,
  Page,
  PageHeader,
  Panel,
  QueryMessage,
  StatusBadge,
  surfaceStyles,
} from "@/components/Surface";
import type {
  RuntimeEndpointSlice,
  RuntimeEndpointSlicePort,
  RuntimeResourceCondition,
  RuntimeServicePort,
} from "@/control/runtime";
import { useRuntime } from "@/control/runtime";
import { usePageTitle } from "@/hooks/usePageTitle";
import { formatDate, formatRelativeTime } from "@/utils/format";

function workloadTone(status: string, hibernated: boolean) {
  if (status === "ready") return "success" as const;
  if (status === "unavailable") return "error" as const;
  if (status === "scaledDown" && hibernated) return "info" as const;
  return "warning" as const;
}

function instanceConditionTone(
  type: string,
  status: string,
  hibernated: boolean,
) {
  if (hibernated && (type === "Ready" || type === "Hibernated")) {
    return "info" as const;
  }
  if (type === "Ready") return status === "True" ? "success" : "error";
  if (type === "Degraded" && status === "True") return "error" as const;
  if (type === "Progressing" && status === "True") return "warning" as const;
  return "neutral" as const;
}

function conditionDetails(conditions: RuntimeResourceCondition[]) {
  if (conditions.length === 0) return "-";
  return conditions.map((condition) => (
    <div key={`${condition.type}-${condition.status}`}>
      <strong>{condition.type}</strong>
      <div className={surfaceStyles.secondary}>
        {[condition.reason, condition.message].filter(Boolean).join(": ") ||
          condition.status}
      </div>
    </div>
  ));
}

function diagnosticDetails(reason?: string, message?: string) {
  return [reason, message].filter(Boolean).join(": ") || "-";
}

function servicePorts(ports: RuntimeServicePort[]) {
  if (ports.length === 0) return "-";
  return ports
    .map(
      (port) =>
        `${port.name || "unnamed"} ${port.port}/${port.protocol} -> ${port.targetPort}`,
    )
    .join(", ");
}

function endpointPorts(ports: RuntimeEndpointSlicePort[]) {
  if (ports.length === 0) return "-";
  return ports
    .map(
      (port) =>
        `${port.name || "unnamed"} ${port.port ?? "any"}/${port.protocol || "any"}`,
    )
    .join(", ");
}

function endpointSliceTone(slice: RuntimeEndpointSlice, hibernated: boolean) {
  if (hibernated && slice.totalEndpoints === 0) {
    return "info" as const;
  }
  if (slice.totalEndpoints === 0 || slice.notReadyEndpoints > 0) {
    return "error" as const;
  }
  if (slice.terminatingEndpoints > 0) {
    return "warning" as const;
  }
  return "success" as const;
}

function endpointSliceDetails(
  slice: RuntimeEndpointSlice,
  hibernated: boolean,
) {
  if (hibernated && slice.totalEndpoints === 0) return "Instance hibernated";
  const details = [];
  if (slice.notReadyEndpoints > 0) {
    details.push(`${slice.notReadyEndpoints} not ready`);
  }
  if (slice.terminatingEndpoints > 0) {
    details.push(`${slice.terminatingEndpoints} terminating`);
  }
  return details.join(", ") || "-";
}

export default function SystemPage() {
  usePageTitle("Runtime");
  const runtime = useRuntime();
  const snapshot = runtime.data;
  const runtimeStale = Boolean(snapshot?.stale || runtime.error);
  const hibernated = snapshot?.instance.phase === "Hibernated";
  const services = snapshot?.services ?? [];
  const endpointSlices = snapshot?.endpointSlices ?? [];

  return (
    <Page>
      <PageHeader
        title="Runtime"
        actions={
          <Button
            type="button"
            onClick={() => runtime.refetch()}
            disabled={runtime.isFetching}
          >
            <RefreshCw size={14} aria-hidden="true" /> Refresh
          </Button>
        }
      />
      {runtime.isLoading ? (
        <Panel>
          <QueryMessage loading />
        </Panel>
      ) : runtime.error && !snapshot ? (
        <Panel>
          <QueryMessage
            error={runtime.error}
            onRetry={() => runtime.refetch()}
          />
        </Panel>
      ) : snapshot ? (
        <div className={surfaceStyles.stack}>
          {runtimeStale ? (
            <div className={surfaceStyles.callout}>
              Runtime data is stale. The last refresh failed.
            </div>
          ) : null}
          {snapshot.ingestRuntimeTruncated ? (
            <div className={surfaceStyles.callout} role="status">
              Runtime view is partial. Some ingest Jobs, Pods, and Events are
              not shown.
            </div>
          ) : null}
          <Panel
            title={`${snapshot.instance.namespace}/${snapshot.instance.name}`}
            actions={
              <StatusBadge
                tone={
                  snapshot.instance.phase === "Ready"
                    ? "success"
                    : hibernated
                      ? "info"
                      : "warning"
                }
              >
                {snapshot.instance.phase}
              </StatusBadge>
            }
          >
            <dl className={surfaceStyles.definitionList}>
              <dt>Observed</dt>
              <dd>
                {formatDate(snapshot.observedAt)} (
                {formatRelativeTime(snapshot.observedAt)})
              </dd>
              <dt>Generation</dt>
              <dd>
                {snapshot.instance.observedGeneration} /{" "}
                {snapshot.instance.generation}
              </dd>
              <dt>UID</dt>
              <dd className={surfaceStyles.mono}>{snapshot.instance.uid}</dd>
            </dl>
          </Panel>

          {snapshot.instance.conditions.length > 0 ? (
            <Panel title="Instance conditions">
              <div className={surfaceStyles.tableWrap}>
                <table className={surfaceStyles.table}>
                  <thead>
                    <tr>
                      <th>Condition</th>
                      <th>Status</th>
                      <th>Details</th>
                      <th>Changed</th>
                    </tr>
                  </thead>
                  <tbody>
                    {snapshot.instance.conditions.map((condition) => (
                      <tr key={condition.type}>
                        <td>{condition.type}</td>
                        <td>
                          <StatusBadge
                            tone={instanceConditionTone(
                              condition.type,
                              condition.status,
                              hibernated,
                            )}
                          >
                            {condition.status}
                          </StatusBadge>
                        </td>
                        <td>
                          {diagnosticDetails(
                            condition.reason,
                            condition.message,
                          )}
                        </td>
                        <td>
                          {formatRelativeTime(condition.lastTransitionTime)}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </Panel>
          ) : null}

          <Panel title="Workloads">
            {snapshot.workloads.length === 0 ? (
              <QueryMessage empty={{ title: "No workloads observed" }} />
            ) : (
              <div className={surfaceStyles.tableWrap}>
                <table className={surfaceStyles.table}>
                  <thead>
                    <tr>
                      <th>Component</th>
                      <th>Deployment</th>
                      <th>Status</th>
                      <th>Ready</th>
                      <th>Updated</th>
                      <th>Generation</th>
                      <th>Details</th>
                    </tr>
                  </thead>
                  <tbody>
                    {snapshot.workloads.map((workload) => (
                      <tr key={workload.name}>
                        <td>{workload.component}</td>
                        <td className={surfaceStyles.mono}>{workload.name}</td>
                        <td>
                          <StatusBadge
                            tone={workloadTone(workload.status, hibernated)}
                          >
                            {hibernated && workload.status === "scaledDown"
                              ? "Paused"
                              : workload.status}
                          </StatusBadge>
                        </td>
                        <td>
                          {workload.readyReplicas}/{workload.desiredReplicas}
                        </td>
                        <td>{workload.updatedReplicas}</td>
                        <td>
                          {workload.observedGeneration}/{workload.generation}
                        </td>
                        <td>{conditionDetails(workload.conditions)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Panel>

          <Panel title="Services">
            {services.length === 0 ? (
              <QueryMessage empty={{ title: "No services observed" }} />
            ) : (
              <div className={surfaceStyles.tableWrap}>
                <table className={surfaceStyles.table}>
                  <thead>
                    <tr>
                      <th>Component</th>
                      <th>Service</th>
                      <th>Type</th>
                      <th>Selector</th>
                      <th>Port routing</th>
                    </tr>
                  </thead>
                  <tbody>
                    {services.map((service) => (
                      <tr key={service.name}>
                        <td>{service.component || "-"}</td>
                        <td className={surfaceStyles.mono}>{service.name}</td>
                        <td>{service.type}</td>
                        <td>{service.selectorComponent || "-"}</td>
                        <td className={surfaceStyles.mono}>
                          {servicePorts(service.ports)}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Panel>

          <Panel title="EndpointSlices">
            {endpointSlices.length === 0 ? (
              <QueryMessage empty={{ title: "No EndpointSlices observed" }} />
            ) : (
              <div className={surfaceStyles.tableWrap}>
                <table className={surfaceStyles.table}>
                  <thead>
                    <tr>
                      <th>Service</th>
                      <th>EndpointSlice</th>
                      <th>Address type</th>
                      <th>Resolved ports</th>
                      <th>Readiness</th>
                      <th>Details</th>
                    </tr>
                  </thead>
                  <tbody>
                    {endpointSlices.map((slice) => (
                      <tr key={slice.name}>
                        <td className={surfaceStyles.mono}>
                          {slice.serviceName || "-"}
                        </td>
                        <td className={surfaceStyles.mono}>{slice.name}</td>
                        <td>{slice.addressType}</td>
                        <td className={surfaceStyles.mono}>
                          {endpointPorts(slice.ports)}
                        </td>
                        <td>
                          <StatusBadge
                            tone={endpointSliceTone(slice, hibernated)}
                          >
                            {hibernated && slice.totalEndpoints === 0
                              ? "Paused"
                              : `${slice.readyEndpoints}/${slice.totalEndpoints} ready`}
                          </StatusBadge>
                        </td>
                        <td>{endpointSliceDetails(slice, hibernated)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Panel>

          <Panel title="Pods">
            {snapshot.pods.length === 0 ? (
              <QueryMessage empty={{ title: "No pods observed" }} />
            ) : (
              <div className={surfaceStyles.tableWrap}>
                <table className={surfaceStyles.table}>
                  <thead>
                    <tr>
                      <th>Pod</th>
                      <th>Component</th>
                      <th>Phase</th>
                      <th>Ready</th>
                      <th>Restarts</th>
                      <th>Started</th>
                      <th>Details</th>
                    </tr>
                  </thead>
                  <tbody>
                    {snapshot.pods.map((pod) => (
                      <tr key={pod.name}>
                        <td className={surfaceStyles.mono}>{pod.name}</td>
                        <td>{pod.component}</td>
                        <td>
                          <StatusBadge
                            tone={
                              pod.deleting || !pod.ready ? "warning" : "success"
                            }
                          >
                            {pod.deleting ? "Terminating" : pod.phase}
                          </StatusBadge>
                        </td>
                        <td>{pod.ready ? "Yes" : "No"}</td>
                        <td>{pod.restarts}</td>
                        <td>{formatRelativeTime(pod.startedAt)}</td>
                        <td>{diagnosticDetails(pod.reason, pod.message)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Panel>

          <div className={surfaceStyles.grid2}>
            <Panel
              title="Jobs"
              actions={
                snapshot.ingestRuntimeTruncated ? (
                  <StatusBadge tone="warning">Partial</StatusBadge>
                ) : undefined
              }
            >
              {snapshot.jobs.length === 0 ? (
                <QueryMessage empty={{ title: "No jobs observed" }} />
              ) : (
                <div className={surfaceStyles.tableWrap}>
                  <table className={surfaceStyles.table}>
                    <thead>
                      <tr>
                        <th>Job</th>
                        <th>Status</th>
                        <th>Active</th>
                        <th>Failed</th>
                        <th>Details</th>
                      </tr>
                    </thead>
                    <tbody>
                      {snapshot.jobs.map((job) => (
                        <tr key={job.name}>
                          <td className={surfaceStyles.mono}>{job.name}</td>
                          <td>
                            <StatusBadge
                              tone={
                                job.status === "failed"
                                  ? "error"
                                  : job.status === "succeeded"
                                    ? "success"
                                    : "info"
                              }
                            >
                              {job.status}
                            </StatusBadge>
                          </td>
                          <td>{job.active}</td>
                          <td>{job.failed}</td>
                          <td>{conditionDetails(job.conditions)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </Panel>
            <Panel title="Recent Kubernetes events">
              {snapshot.events.length === 0 ? (
                <QueryMessage empty={{ title: "No recent events" }} />
              ) : (
                <div className={surfaceStyles.tableWrap}>
                  <table className={surfaceStyles.table}>
                    <thead>
                      <tr>
                        <th>Reason</th>
                        <th>Regarding</th>
                        <th>Message</th>
                        <th>Last seen</th>
                      </tr>
                    </thead>
                    <tbody>
                      {snapshot.events.slice(0, 20).map((event) => (
                        <tr
                          key={`${event.regarding.kind}-${event.regarding.name}-${event.reason}`}
                        >
                          <td>
                            <StatusBadge
                              tone={
                                event.type === "Warning" ? "warning" : "neutral"
                              }
                            >
                              {event.reason}
                            </StatusBadge>
                          </td>
                          <td>
                            {event.regarding.kind}/{event.regarding.name}
                          </td>
                          <td>{event.message || "-"}</td>
                          <td>{formatRelativeTime(event.lastObservedAt)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}
            </Panel>
          </div>
        </div>
      ) : null}
    </Page>
  );
}
