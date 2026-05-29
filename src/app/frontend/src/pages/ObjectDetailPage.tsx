import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useApi } from "@/contexts/ApiContext";
import { useApiQuery } from "@/hooks/useApiQuery";
import { usePageTitle } from "@/hooks/usePageTitle";
import LoadingSpinner from "@/components/LoadingSpinner";
import ErrorMessage from "@/components/ErrorMessage";
import CopyButton from "@/components/CopyButton";
import EmptyState from "@/components/EmptyState";
import Badge from "@/components/Badge";
import TraceRail from "@/components/TraceRail";
import StateStrip from "@/components/StateStrip";
import ApiReferencePanel from "@/components/ApiReferencePanel";
import RawPayload from "@/components/RawPayload";
import SectionHeading from "@/components/SectionHeading";
import ConfirmAction from "@/components/ConfirmAction";
import StorageBackendSelector from "@/components/StorageBackendSelector";
import { formatTimerange } from "@/utils/format";
import type { ObjectUrl } from "@/types/tams";

const OBJECT_DETAIL_PAGE_SIZE = "50";

export default function ObjectDetailPage() {
  usePageTitle("Media Object");
  const { objectId } = useParams<{ objectId: string }>();
  const api = useApi();
  const [pageStack, setPageStack] = useState<string[]>([]);
  const [storageId, setStorageId] = useState("");
  const [deleteTarget, setDeleteTarget] = useState<ObjectUrl | null>(null);
  const [deleteBusy, setDeleteBusy] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);
  const currentPage = pageStack[pageStack.length - 1];

  useEffect(() => {
    setPageStack([]);
  }, [objectId, storageId]);

  const storageBackends = useApiQuery(() => api.getStorageBackends(), [api]);

  const object = useApiQuery(
    () =>
      objectId
        ? api.getObject(objectId, {
            limit: OBJECT_DETAIL_PAGE_SIZE,
            ...(storageId ? { accept_storage_ids: storageId } : {}),
            ...(currentPage ? { page: currentPage } : {}),
            verbose_storage: true,
          })
        : Promise.resolve(null),
    [api, objectId, currentPage, storageId],
  );

  async function handleDeleteObjectInstance() {
    if (!objectId || !deleteTarget) return;
    setDeleteBusy(true);
    setDeleteError(null);
    try {
      await api.deleteObjectInstance(
        objectId,
        deleteTarget.storage_id
          ? { storage_id: deleteTarget.storage_id }
          : { label: deleteTarget.label },
      );
      setDeleteTarget(null);
      object.refetch();
    } catch (error) {
      setDeleteError(
        error instanceof Error
          ? error.message
          : "Object instance delete failed",
      );
    } finally {
      setDeleteBusy(false);
    }
  }

  if (object.loading) {
    return <LoadingSpinner message="Loading object..." />;
  }

  if (object.error) {
    return (
      <div className="p-4 sm:p-6 lg:p-8">
        <ErrorMessage message={object.error} onRetry={object.refetch} />
      </div>
    );
  }

  if (!object.data) {
    return (
      <div className="p-4 sm:p-6 lg:p-8">
        <EmptyState
          title="Object not found"
          description="No object payload was returned for this identifier."
        />
      </div>
    );
  }

  const objectPage = object.data;
  const mediaObject = objectPage.data;
  const referencedFlows = mediaObject.referenced_by_flows ?? [];

  return (
    <div className="p-4 sm:p-6 lg:p-8">
      <nav className="mb-6" aria-label="Breadcrumb">
        <ol className="flex items-center gap-2 text-sm text-gray-500">
          <li>
            <Link to="/objects" className="hover:text-gray-700">
              Objects
            </Link>
          </li>
          <li aria-hidden="true">/</li>
          <li className="font-medium text-gray-900">{mediaObject.id}</li>
        </ol>
      </nav>

      <div className="mb-8 tamoss-panel rounded-2xl p-6">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <h1 className="text-xl font-bold text-gray-900 sm:text-2xl">
              Media Object
            </h1>
            <div className="mt-2 flex items-center gap-2">
              <code className="text-sm text-gray-500">{mediaObject.id}</code>
              <CopyButton text={mediaObject.id} label="Copy ID" />
            </div>
          </div>
          {mediaObject.first_referenced_by_flow && (
            <Link
              to={`/flows/${mediaObject.first_referenced_by_flow}`}
              className="rounded-lg bg-tams-600 px-4 py-2 text-sm font-medium text-white shadow-sm hover:bg-tams-700"
            >
              Open first flow
            </Link>
          )}
        </div>

        <dl className="mt-6 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <div>
            <dt className="text-sm font-medium text-gray-500">Timerange</dt>
            <dd className="mt-1 font-mono text-sm text-gray-900">
              {formatTimerange(mediaObject.timerange)}
            </dd>
          </div>
          <div>
            <dt className="text-sm font-medium text-gray-500">
              Key frame count
            </dt>
            <dd className="mt-1 text-sm text-gray-900">
              {mediaObject.key_frame_count ?? "N/A"}
            </dd>
          </div>
          <div>
            <dt className="text-sm font-medium text-gray-500">
              Referenced by flows
            </dt>
            <dd className="mt-1 text-sm text-gray-900">
              {referencedFlows.length}
            </dd>
          </div>
        </dl>
      </div>

      <StateStrip
        title="Object State"
        refreshedAt={null}
        items={[
          {
            label: "referenced flows",
            value: String(referencedFlows.length),
            variant: referencedFlows.length > 0 ? "info" : "warning",
          },
          {
            label: "urls",
            value: String(mediaObject.get_urls?.length ?? 0),
            variant:
              (mediaObject.get_urls?.length ?? 0) > 0 ? "success" : "warning",
          },
          {
            label: "keyframes",
            value: String(mediaObject.key_frame_count ?? 0),
            variant:
              (mediaObject.key_frame_count ?? 0) > 0 ? "default" : "warning",
          },
        ]}
      />

      <SectionHeading
        eyebrow="Summary"
        title="Object Overview"
        description="Inspect media object state, referencing flows, storage relationships, and the exact API payload."
      />

      <div className="grid gap-6 lg:grid-cols-2">
        <section>
          <TraceRail
            title="Relationships"
            items={[
              ...(mediaObject.first_referenced_by_flow
                ? [
                    {
                      label: "First Flow",
                      value: mediaObject.first_referenced_by_flow,
                      to: `/flows/${mediaObject.first_referenced_by_flow}`,
                    },
                  ]
                : []),
              {
                label: "Object",
                value: mediaObject.id,
                to: `/objects/${mediaObject.id}`,
                tone: "accent",
              },
              {
                label: "Storage",
                value: "Inspect backend and URL behavior",
                to: "/service",
              },
            ]}
          />
        </section>

        <section>
          <div className="tamoss-panel rounded-2xl p-6 h-full">
            <SectionHeading title="Referenced Flows" />
            {referencedFlows.length ? (
              <div className="space-y-2">
                {referencedFlows.map((flowId) => (
                  <Link
                    key={flowId}
                    to={`/flows/${flowId}`}
                    className="block rounded-lg border border-gray-200 px-3 py-2 text-sm text-tams-600 hover:bg-gray-50 hover:text-tams-700"
                  >
                    {flowId}
                  </Link>
                ))}
              </div>
            ) : (
              <p className="text-sm text-gray-500">
                No flows currently reference this object.
              </p>
            )}
            <div className="mt-4 flex flex-wrap items-center justify-between gap-3 border-t border-gray-100 pt-4">
              <span className="text-xs text-gray-500">
                Page limit {objectPage.limit ?? OBJECT_DETAIL_PAGE_SIZE}
              </span>
              <div className="flex gap-2">
                <button
                  type="button"
                  onClick={() => setPageStack((pages) => pages.slice(0, -1))}
                  disabled={pageStack.length === 0 || object.loading}
                  className="rounded-md border border-gray-300 px-3 py-1.5 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
                >
                  Previous
                </button>
                <button
                  type="button"
                  onClick={() => {
                    if (objectPage.nextKey) {
                      setPageStack((pages) => [...pages, objectPage.nextKey!]);
                    }
                  }}
                  disabled={!objectPage.nextKey || object.loading}
                  className="rounded-md border border-gray-300 px-3 py-1.5 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
                >
                  Next
                </button>
              </div>
            </div>
          </div>
        </section>
      </div>

      <section className="mt-6 tamoss-panel rounded-2xl p-6">
        <div className="mb-4 flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
          <SectionHeading
            title="Playback URLs"
            description="Use these URLs to verify object reachability and playback behavior."
          />
          <StorageBackendSelector
            id="object-storage-filter"
            label="Storage backend"
            value={storageId}
            onChange={setStorageId}
            backends={storageBackends.data}
            includeAllOption
            allLabel="All storage backends"
            className="min-w-64"
          />
        </div>
        {deleteError && (
          <div className="mb-4 rounded-lg border border-rose-200 bg-rose-50 p-3 text-sm text-rose-700">
            {deleteError}
          </div>
        )}
        {mediaObject.get_urls?.length ? (
          <div className="space-y-3">
            {mediaObject.get_urls.map((entry, index) => (
              <div
                key={`${entry.url}-${index}`}
                className="rounded-lg border border-gray-200 p-4"
              >
                <div className="grid gap-4 lg:grid-cols-[220px_minmax(0,1fr)_auto] lg:items-center">
                  <div>
                    <div className="flex flex-wrap items-center gap-2">
                      {entry.label && (
                        <Badge variant="info">{entry.label}</Badge>
                      )}
                      {entry.presigned ? (
                        <Badge variant="warning">Presigned</Badge>
                      ) : (
                        <Badge variant="default">Direct</Badge>
                      )}
                      {entry.controlled && (
                        <Badge variant="success">Controlled</Badge>
                      )}
                    </div>
                    <dl className="mt-3 space-y-1 text-xs text-gray-500">
                      <div>
                        <dt className="inline font-medium text-gray-700">
                          Storage ID:
                        </dt>{" "}
                        <dd className="inline font-mono">
                          {entry.storage_id ?? "N/A"}
                        </dd>
                      </div>
                      {entry.provider && (
                        <div>
                          <dt className="inline font-medium text-gray-700">
                            Provider:
                          </dt>{" "}
                          <dd className="inline">{entry.provider}</dd>
                        </div>
                      )}
                      {entry.region && (
                        <div>
                          <dt className="inline font-medium text-gray-700">
                            Region:
                          </dt>{" "}
                          <dd className="inline">{entry.region}</dd>
                        </div>
                      )}
                    </dl>
                  </div>

                  <div className="min-w-0 overflow-hidden rounded-lg bg-gray-50 px-3 py-3">
                    <div className="overflow-x-auto">
                      <code className="whitespace-nowrap text-xs text-gray-600">
                        {entry.url}
                      </code>
                    </div>
                  </div>

                  <div className="flex flex-wrap items-center gap-2 lg:flex-col lg:items-stretch">
                    <CopyButton text={entry.url} label="Copy URL" />
                    <a
                      href={entry.url}
                      target="_blank"
                      rel="noreferrer"
                      className="rounded-md border border-gray-300 px-3 py-1.5 text-center text-xs font-medium text-gray-700 hover:bg-gray-50"
                    >
                      Open URL
                    </a>
                    {(entry.storage_id || entry.label) && (
                      <button
                        type="button"
                        onClick={() => {
                          setDeleteTarget(entry);
                          setDeleteError(null);
                        }}
                        className="rounded-md border border-red-300 px-3 py-1.5 text-center text-xs font-medium text-red-600 hover:bg-red-50"
                      >
                        Delete instance
                      </button>
                    )}
                  </div>
                </div>
              </div>
            ))}
          </div>
        ) : (
          <EmptyState
            title="No URLs"
            description="This object does not currently expose any GET URLs."
          />
        )}
      </section>

      <ConfirmAction
        open={deleteTarget !== null}
        variant="danger"
        title="Delete object storage instance?"
        description={
          deleteTarget
            ? `This deletes the ${deleteTarget.label ?? deleteTarget.storage_id ?? "selected"} object instance from this media object. If this is the final instance, the API will reject the request and the flow segment deletion path must be used instead.`
            : undefined
        }
        confirmLabel="Confirm delete"
        busy={deleteBusy}
        busyLabel="Deleting..."
        onConfirm={handleDeleteObjectInstance}
        onCancel={() => {
          setDeleteTarget(null);
          setDeleteError(null);
        }}
      />

      <div className="mt-6 grid gap-6 lg:grid-cols-2">
        <ApiReferencePanel
          title="API Reference"
          method="GET"
          path={`/objects/${mediaObject.id}`}
        />

        <RawPayload
          title="Raw Object Payload"
          description="Inspect the exact object response returned by the API."
          json={JSON.stringify(mediaObject, null, 2)}
        />
      </div>
    </div>
  );
}
