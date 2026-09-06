import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, Radio } from "lucide-react";
import { Fragment } from "react";
import { Link, useParams } from "react-router";
import {
  Page,
  PageHeader,
  Panel,
  QueryMessage,
  StatusBadge,
  surfaceStyles,
} from "@/components/Surface";
import { useApi } from "@/contexts/ApiContext";
import { usePageTitle } from "@/hooks/usePageTitle";
import { formatDate, formatFormat } from "@/utils/format";

export default function SourceDetailPage() {
  usePageTitle("Source");
  const { sourceId = "" } = useParams();
  const api = useApi();
  const source = useQuery({
    queryKey: ["api", "source", sourceId],
    queryFn: () => api.getSource(sourceId),
    enabled: Boolean(sourceId),
  });
  const flows = useQuery({
    queryKey: ["api", "flows", { sourceId }],
    queryFn: () =>
      api.getFlows({
        source_id: sourceId,
        limit: "50",
        include_timerange: true,
      }),
    enabled: Boolean(sourceId),
  });

  return (
    <Page>
      <PageHeader
        title={source.data?.label || "Source"}
        description={sourceId}
        actions={
          <Link className={surfaceStyles.button} to="/sources">
            <ArrowLeft size={14} aria-hidden="true" /> Sources
          </Link>
        }
      />
      {source.isLoading ? (
        <Panel>
          <QueryMessage loading />
        </Panel>
      ) : source.error ? (
        <Panel>
          <QueryMessage error={source.error} onRetry={() => source.refetch()} />
        </Panel>
      ) : source.data ? (
        <div className={surfaceStyles.stack}>
          <div className={surfaceStyles.grid2}>
            <Panel title="Metadata">
              <dl className={surfaceStyles.definitionList}>
                <dt>ID</dt>
                <dd className={surfaceStyles.mono}>{source.data.id}</dd>
                <dt>Format</dt>
                <dd>
                  <StatusBadge tone="info">
                    {formatFormat(source.data.format)}
                  </StatusBadge>
                </dd>
                <dt>Description</dt>
                <dd>{source.data.description || "No description"}</dd>
                <dt>Created</dt>
                <dd>{formatDate(source.data.created)}</dd>
                <dt>Updated</dt>
                <dd>{formatDate(source.data.updated)}</dd>
              </dl>
            </Panel>
            <Panel title="Tags">
              {!Object.keys(source.data.tags ?? {}).length ? (
                <QueryMessage empty={{ title: "No tags" }} />
              ) : (
                <dl className={surfaceStyles.definitionList}>
                  {Object.entries(source.data.tags ?? {}).map(
                    ([name, value]) => (
                      <Fragment key={name}>
                        <dt>{name}</dt>
                        <dd>
                          {Array.isArray(value) ? value.join(", ") : value}
                        </dd>
                      </Fragment>
                    ),
                  )}
                </dl>
              )}
            </Panel>
          </div>
          <Panel
            title="Flows"
            actions={
              <Link
                className={surfaceStyles.resourceLink}
                to={`/flows?source_id=${encodeURIComponent(sourceId)}`}
              >
                Open catalogue
              </Link>
            }
          >
            {flows.isLoading ? (
              <QueryMessage loading />
            ) : flows.error ? (
              <QueryMessage error={flows.error} />
            ) : !flows.data?.data.length ? (
              <QueryMessage empty={{ title: "No flows" }} />
            ) : (
              <div className={surfaceStyles.tableWrap}>
                <table className={surfaceStyles.table}>
                  <thead>
                    <tr>
                      <th>Flow</th>
                      <th>Format</th>
                      <th>Codec</th>
                      <th>Timerange</th>
                    </tr>
                  </thead>
                  <tbody>
                    {flows.data.data.map((flow) => (
                      <tr key={flow.id}>
                        <td>
                          <Link
                            className={surfaceStyles.resourceLink}
                            to={`/flows/${flow.id}`}
                          >
                            <Radio size={13} aria-hidden="true" />{" "}
                            {flow.label || flow.id}
                          </Link>
                        </td>
                        <td>{formatFormat(flow.format)}</td>
                        <td>{flow.codec || "-"}</td>
                        <td className={surfaceStyles.mono}>
                          {flow.timerange || "Empty"}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Panel>
        </div>
      ) : null}
    </Page>
  );
}
