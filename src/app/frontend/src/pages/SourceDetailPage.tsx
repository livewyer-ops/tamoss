import { useParams, Link } from "react-router";
import { useApi } from "@/contexts/ApiContext";
import { useApiQuery } from "@/hooks/useApiQuery";
import { usePageTitle } from "@/hooks/usePageTitle";
import LoadingSpinner from "@/components/LoadingSpinner";
import ErrorMessage from "@/components/ErrorMessage";
import Badge from "@/components/Badge";
import CopyButton from "@/components/CopyButton";
import InlineEditField from "@/components/InlineEditField";
import EditableTagList from "@/components/EditableTagList";
import TraceRail from "@/components/TraceRail";
import StateStrip from "@/components/StateStrip";
import ApiReferencePanel from "@/components/ApiReferencePanel";
import RawPayload from "@/components/RawPayload";
import SectionHeading from "@/components/SectionHeading";
import { formatFormat, formatDate } from "@/utils/format";

const SOURCE_FLOW_PAGE_SIZE = "300";

export default function SourceDetailPage() {
  usePageTitle("Source");
  const { sourceId } = useParams<{ sourceId: string }>();
  const api = useApi();

  const {
    data: source,
    loading,
    error,
    refetch,
  } = useApiQuery(() => api.getSource(sourceId!), [api, sourceId]);

  const flows = useApiQuery(
    () => api.getFlows({ limit: SOURCE_FLOW_PAGE_SIZE, source_id: sourceId! }),
    [api, sourceId],
  );

  if (loading) return <LoadingSpinner message="Loading source..." />;
  if (error)
    return (
      <div className="p-6">
        <ErrorMessage message={error} onRetry={refetch} />
      </div>
    );
  if (!source) return null;

  const downstreamFlowCount = flows.data
    ? `${flows.data.data.length}${flows.data.nextKey ? "+" : ""}`
    : "0";
  const hasDownstreamFlows = (flows.data?.data.length ?? 0) > 0;

  return (
    <div className="p-4 sm:p-6 lg:p-8">
      <nav className="mb-6" aria-label="Breadcrumb">
        <ol className="flex items-center gap-2 text-sm text-gray-500">
          <li>
            <Link to="/sources" className="hover:text-gray-700">
              Sources
            </Link>
          </li>
          <li aria-hidden="true">/</li>
          <li className="font-medium text-gray-900">
            {source.label || source.id}
          </li>
        </ol>
      </nav>

      <div className="mb-8">
        <div className="flex items-start gap-4">
          <div className="flex-1">
            <div className="flex items-center gap-3">
              <h1 className="text-xl font-bold text-gray-900 sm:text-2xl">
                <InlineEditField
                  value={source.label || ""}
                  placeholder="Unnamed Source"
                  onSave={async (v) => {
                    await api.updateSourceLabel(sourceId!, v);
                    refetch();
                  }}
                />
              </h1>
              {source.format && (
                <Badge variant="primary">{formatFormat(source.format)}</Badge>
              )}
            </div>
            <div className="mt-1 flex items-center gap-2">
              <code className="text-sm text-gray-400">{source.id}</code>
              <CopyButton text={source.id} label="Copy" />
            </div>
          </div>
        </div>
      </div>

      <StateStrip
        title="Source State"
        refreshedAt={source.updated ?? source.created ?? null}
        items={[
          {
            label: "format",
            value: source.format ? formatFormat(source.format) : "unknown",
          },
          {
            label: "downstream flows",
            value: downstreamFlowCount,
            variant: hasDownstreamFlows ? "info" : "default",
          },
          {
            label: "tags",
            value: String(Object.keys(source.tags ?? {}).length),
            variant:
              Object.keys(source.tags ?? {}).length > 0 ? "success" : "default",
          },
        ]}
      />

      <SectionHeading
        eyebrow="Summary"
        title="Source Overview"
        description="Inspect source metadata, downstream flow links, and operational references."
      />

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        <div className="tamoss-panel rounded-2xl p-4 sm:p-6">
          <SectionHeading title="State & Metadata" />
          <dl className="space-y-3">
            <div>
              <dt className="text-sm font-medium text-gray-500">Format</dt>
              <dd className="mt-1 text-sm text-gray-900">
                {source.format ?? "N/A"}
              </dd>
            </div>
            <div>
              <dt className="text-sm font-medium text-gray-500">Description</dt>
              <dd className="mt-1 text-sm text-gray-900">
                <InlineEditField
                  value={source.description || ""}
                  placeholder="Add description..."
                  multiline
                  onSave={async (v) => {
                    await api.updateSourceDescription(sourceId!, v);
                    refetch();
                  }}
                />
              </dd>
            </div>
            <div>
              <dt className="text-sm font-medium text-gray-500">Created</dt>
              <dd className="mt-1 text-sm text-gray-900">
                {formatDate(source.created)}
              </dd>
            </div>
            <div>
              <dt className="text-sm font-medium text-gray-500">Updated</dt>
              <dd className="mt-1 text-sm text-gray-900">
                {formatDate(source.updated)}
              </dd>
            </div>
            {source.created_by && (
              <div>
                <dt className="text-sm font-medium text-gray-500">
                  Created By
                </dt>
                <dd className="mt-1 text-sm text-gray-900">
                  {source.created_by}
                </dd>
              </div>
            )}
          </dl>
        </div>

        <div className="tamoss-panel rounded-2xl p-4 sm:p-6">
          <SectionHeading title="Tags" />
          <EditableTagList
            tags={source.tags}
            onAdd={async (key, value) => {
              await api.updateSourceTag(sourceId!, key, value);
              refetch();
            }}
            onDelete={async (key) => {
              await api.deleteSourceTag(sourceId!, key);
              refetch();
            }}
          />
        </div>

        {source.source_collection && source.source_collection.length > 0 && (
          <div className="tamoss-panel rounded-2xl p-4 sm:p-6">
            <SectionHeading title="Source Collection" />
            <ul className="space-y-2">
              {source.source_collection.map((item) => (
                <li key={item.id} className="flex items-center gap-2">
                  <Link
                    to={`/sources/${item.id}`}
                    className="text-sm text-tams-600 hover:text-tams-700"
                  >
                    {item.id}
                  </Link>
                  {item.role && <Badge variant="default">{item.role}</Badge>}
                </li>
              ))}
            </ul>
          </div>
        )}

        <TraceRail
          title="Relationships"
          items={[
            {
              label: "Source",
              value: source.id,
              to: `/sources/${source.id}`,
              tone: "accent",
            },
            ...(flows.data?.data[0]
              ? [
                  {
                    label: "First Flow",
                    value: flows.data.data[0].id,
                    to: `/flows/${flows.data.data[0].id}`,
                  },
                ]
              : []),
            {
              label: "Objects",
              value: "Follow segment object IDs from a flow",
              to: "/objects",
            },
            {
              label: "Storage",
              value: "Inspect storage backends and object URLs",
              to: "/service",
            },
          ]}
        />

        <div className="tamoss-panel rounded-2xl p-4 sm:p-6 lg:col-span-2">
          <SectionHeading title="Downstream Flows" />
          {flows.loading && <LoadingSpinner message="Loading flows..." />}
          {flows.error && (
            <ErrorMessage message={flows.error} onRetry={flows.refetch} />
          )}
          {flows.data && flows.data.data.length === 0 && (
            <p className="text-sm text-gray-500">
              No flows associated with this source.
            </p>
          )}
          {flows.data && flows.data.data.length > 0 && (
            <div className="space-y-2">
              {flows.data.data.map((flow) => (
                <Link
                  key={flow.id}
                  to={`/flows/${flow.id}`}
                  className="flex items-center justify-between rounded-lg border border-gray-100 p-3 hover:bg-gray-50"
                >
                  <div>
                    <span className="text-sm font-medium text-gray-900">
                      {flow.label || flow.id}
                    </span>
                    {flow.codec && (
                      <Badge variant="default" className="ml-2">
                        {flow.codec}
                      </Badge>
                    )}
                  </div>
                  <span className="text-xs text-gray-400">
                    {formatFormat(flow.format)}
                  </span>
                </Link>
              ))}
            </div>
          )}
        </div>

        <RawPayload
          className="lg:col-span-2"
          title="Raw Source Payload"
          description="Inspect the exact source response returned by the API."
          json={JSON.stringify(source, null, 2)}
        />

        <div className="lg:col-span-2">
          <ApiReferencePanel
            title="API Reference"
            method="GET"
            path={`/sources/${source.id}`}
          />
        </div>
      </div>
    </div>
  );
}
