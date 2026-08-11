import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, RefreshCw } from "lucide-react";
import { Link, useParams, useSearchParams } from "react-router";
import {
  Button,
  Page,
  PageHeader,
  Panel,
  QueryMessage,
  StatusBadge,
  surfaceStyles,
} from "@/components/Surface";
import { useApi } from "@/contexts/ApiContext";
import { usePageTitle } from "@/hooks/usePageTitle";
import { flowStatusLabel, flowStatusTone } from "@/utils/flow-status";
import {
  formatBitRate,
  formatCodec,
  formatDate,
  formatFormat,
  formatFrameRate,
  formatResolution,
} from "@/utils/format";

export default function FlowDetailPage() {
  usePageTitle("Flow");
  const { flowId = "" } = useParams();
  const [params, setParams] = useSearchParams();
  const api = useApi();
  const cursor = params.get("segments") ?? undefined;
  const flow = useQuery({
    queryKey: ["api", "flow", flowId],
    queryFn: () => api.getFlow(flowId, { include_timerange: true }),
    enabled: Boolean(flowId),
  });
  const segments = useQuery({
    queryKey: ["api", "flow", flowId, "segments", cursor],
    queryFn: () =>
      api.getFlowSegments(flowId, {
        limit: "50",
        page: cursor,
        include_object_timerange: true,
        presigned: false,
      }),
    enabled: Boolean(flowId),
  });
  const essence = flow.data?.essence_parameters;

  return (
    <Page>
      <PageHeader
        title={flow.data?.label || "Flow"}
        description={flowId}
        actions={
          <Link className={surfaceStyles.button} to="/flows">
            <ArrowLeft size={14} aria-hidden="true" /> Flows
          </Link>
        }
      />
      {flow.isLoading ? (
        <Panel>
          <QueryMessage loading />
        </Panel>
      ) : flow.error ? (
        <Panel>
          <QueryMessage error={flow.error} onRetry={() => flow.refetch()} />
        </Panel>
      ) : flow.data ? (
        <div className={surfaceStyles.stack}>
          <div className={surfaceStyles.grid2}>
            <Panel title="Identity">
              <dl className={surfaceStyles.definitionList}>
                <dt>ID</dt>
                <dd className={surfaceStyles.mono}>{flow.data.id}</dd>
                <dt>Source</dt>
                <dd>
                  <Link
                    to={`/sources/${flow.data.source_id}`}
                    className={surfaceStyles.mono}
                  >
                    {flow.data.source_id}
                  </Link>
                </dd>
                <dt>Format</dt>
                <dd>
                  <StatusBadge tone="info">
                    {formatFormat(flow.data.format)}
                  </StatusBadge>
                </dd>
                <dt>Codec</dt>
                <dd>{formatCodec(flow.data.codec)}</dd>
                <dt>Container</dt>
                <dd>{flow.data.container || "-"}</dd>
                <dt>Status</dt>
                <dd>
                  <StatusBadge tone={flowStatusTone(flow.data.status)}>
                    {flowStatusLabel(flow.data.status)}
                  </StatusBadge>
                </dd>
                <dt>Profile</dt>
                <dd>
                  {flow.data.profile_id ? (
                    <Link
                      className={surfaceStyles.mono}
                      to={`/profiles/${flow.data.profile_id}`}
                    >
                      {flow.data.profile_id}
                    </Link>
                  ) : (
                    "Direct metadata"
                  )}
                </dd>
                <dt>Access</dt>
                <dd>
                  {flow.data.read_only ? (
                    <StatusBadge tone="warning">Read only</StatusBadge>
                  ) : (
                    <StatusBadge tone="success">Writable</StatusBadge>
                  )}
                </dd>
              </dl>
            </Panel>
            <Panel title="Media properties">
              <dl className={surfaceStyles.definitionList}>
                <dt>Timerange</dt>
                <dd className={surfaceStyles.mono}>
                  {flow.data.timerange || "Empty"}
                </dd>
                <dt>Resolution</dt>
                <dd>
                  {formatResolution(
                    essence?.frame_width,
                    essence?.frame_height,
                  )}
                </dd>
                <dt>Frame rate</dt>
                <dd>{formatFrameRate(essence?.frame_rate)}</dd>
                <dt>Average bitrate</dt>
                <dd>{formatBitRate(flow.data.avg_bit_rate)}</dd>
                <dt>Maximum bitrate</dt>
                <dd>{formatBitRate(flow.data.max_bit_rate)}</dd>
                <dt>Created</dt>
                <dd>{formatDate(flow.data.created)}</dd>
              </dl>
            </Panel>
          </div>
          <Panel
            title="Segments"
            actions={
              <Button type="button" onClick={() => segments.refetch()}>
                <RefreshCw size={14} aria-hidden="true" /> Refresh
              </Button>
            }
          >
            {segments.isLoading ? (
              <QueryMessage loading />
            ) : segments.error ? (
              <QueryMessage
                error={segments.error}
                onRetry={() => segments.refetch()}
              />
            ) : !segments.data?.data.length ? (
              <QueryMessage empty={{ title: "This flow has no segments" }} />
            ) : (
              <div className={surfaceStyles.tableWrap}>
                <table className={surfaceStyles.table}>
                  <thead>
                    <tr>
                      <th>Object</th>
                      <th>Segment timerange</th>
                      <th>Object timerange</th>
                      <th>Initialisation Object</th>
                      <th>Locations</th>
                    </tr>
                  </thead>
                  <tbody>
                    {segments.data.data.map((segment) => (
                      <tr key={`${segment.object_id}-${segment.timerange}`}>
                        <td>
                          <Link
                            className={`${surfaceStyles.resourceLink} ${surfaceStyles.mono}`}
                            to={`/objects/${encodeURIComponent(segment.object_id)}`}
                          >
                            {segment.object_id}
                          </Link>
                        </td>
                        <td className={surfaceStyles.mono}>
                          {segment.timerange}
                        </td>
                        <td className={surfaceStyles.mono}>
                          {segment.object_timerange || "-"}
                        </td>
                        <td>
                          {segment.init_object ? (
                            <>
                              <Link
                                className={`${surfaceStyles.resourceLink} ${surfaceStyles.mono}`}
                                to={`/objects/${encodeURIComponent(segment.init_object.object_id)}`}
                              >
                                {segment.init_object.object_id}
                              </Link>
                              <div className={surfaceStyles.secondary}>
                                {segment.init_object.get_urls?.length ?? 0}{" "}
                                locations
                              </div>
                            </>
                          ) : (
                            "-"
                          )}
                        </td>
                        <td>{segment.get_urls?.length ?? 0}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            <footer className={surfaceStyles.pager}>
              <span>
                {segments.data?.data.length ?? 0} segments on this page
              </span>
              <div className={surfaceStyles.toolbar}>
                <Button
                  type="button"
                  disabled={!cursor}
                  onClick={() =>
                    setParams((current) => {
                      const next = new URLSearchParams(current);
                      next.delete("segments");
                      return next;
                    })
                  }
                >
                  First page
                </Button>
                <Button
                  type="button"
                  disabled={!segments.data?.nextKey}
                  onClick={() =>
                    setParams((current) => {
                      const next = new URLSearchParams(current);
                      if (segments.data?.nextKey)
                        next.set("segments", segments.data.nextKey);
                      return next;
                    })
                  }
                >
                  Next
                </Button>
              </div>
            </footer>
          </Panel>
        </div>
      ) : null}
    </Page>
  );
}
