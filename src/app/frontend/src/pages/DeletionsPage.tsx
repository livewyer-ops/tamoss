import { Link, useSearchParams } from "react-router";
import { useState } from "react";
import { useApi } from "@/contexts/ApiContext";
import { useApiQuery } from "@/hooks/useApiQuery";
import { usePageTitle } from "@/hooks/usePageTitle";
import LoadingSpinner from "@/components/LoadingSpinner";
import EmptyState from "@/components/EmptyState";
import Badge from "@/components/Badge";
import ErrorMessage from "@/components/ErrorMessage";
import CopyViewLinkButton from "@/components/CopyViewLinkButton";
import StateStrip from "@/components/StateStrip";
import ApiReferencePanel from "@/components/ApiReferencePanel";
import AsyncLifecycle from "@/components/AsyncLifecycle";
import {
  formatDate,
  formatRelativeTime,
  formatTimerange,
} from "@/utils/format";

const statusVariants = {
  created: "info",
  started: "warning",
  done: "success",
  error: "danger",
} as const;

function isStale(request: {
  status: string;
  updated?: string;
  created?: string;
}) {
  if (request.status !== "started") return false;
  const ref = request.updated ?? request.created;
  if (!ref) return false;
  const timestamp = Date.parse(ref);
  if (Number.isNaN(timestamp)) return false;
  return Date.now() - timestamp > 15 * 60 * 1000;
}

function buildDeletionLifecycle(request: {
  status: "created" | "started" | "done" | "error";
  created?: string;
  updated?: string;
  error?: { summary?: string };
}) {
  return [
    {
      id: "accepted",
      label: "Request accepted",
      description:
        "The API stored the delete request and queued it for worker processing.",
      state: request.created ? "complete" : "pending",
      timestamp: request.created,
    },
    {
      id: "worker",
      label: "Worker processing",
      description:
        "A background worker is responsible for deleting segments and finalizing flow cleanup.",
      state:
        request.status === "started"
          ? "active"
          : request.status === "done" || request.status === "error"
            ? "complete"
            : "pending",
      timestamp: request.updated,
    },
    {
      id: "outcome",
      label: request.status === "error" ? "Request failed" : "Request finished",
      description:
        request.status === "error"
          ? (request.error?.summary ??
            "The request entered an error state and needs operator attention.")
          : "The request reached its terminal state.",
      state:
        request.status === "done"
          ? "complete"
          : request.status === "error"
            ? "error"
            : request.status === "started"
              ? "active"
              : "pending",
      timestamp:
        request.status === "done" || request.status === "error"
          ? request.updated
          : undefined,
    },
  ] as const;
}

export default function DeletionsPage() {
  usePageTitle("Deletion Requests");
  const api = useApi();
  const [searchParams, setSearchParams] = useSearchParams();
  const [selectedRequestId, setSelectedRequestId] = useState<string | null>(
    searchParams.get("request") ?? null,
  );
  const { data, loading, error, refetch } = useApiQuery(
    () => api.getDeletionRequests(),
    [api],
  );

  const selectedRequest = useApiQuery(
    () =>
      selectedRequestId
        ? api.getDeletionRequest(selectedRequestId)
        : Promise.resolve(null),
    [api, selectedRequestId],
  );

  function syncSelectedRequest(requestId: string | null) {
    setSelectedRequestId(requestId);
    const next = new URLSearchParams(searchParams);
    if (requestId) next.set("request", requestId);
    else next.delete("request");
    setSearchParams(next, { replace: true });
  }

  return (
    <div className="p-4 sm:p-6 lg:p-8">
      <div className="mb-6 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-xl font-bold text-gray-900 sm:text-2xl">
            Deletion Requests
          </h1>
          <p className="mt-1 text-sm text-gray-500">
            Monitor background flow deletion operations
          </p>
        </div>
        <button
          onClick={() => {
            refetch();
            if (selectedRequestId) selectedRequest.refetch();
          }}
          className="rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm font-medium text-gray-700 shadow-sm hover:bg-gray-50"
          aria-label="Refresh deletion requests"
        >
          Refresh
        </button>
        <CopyViewLinkButton />
      </div>

      <StateStrip
        title="Deletion Queue State"
        refreshedAt={data && data.length >= 0 ? new Date().toISOString() : null}
        items={[
          {
            label: "active",
            value: String(
              (data ?? []).filter(
                (item) =>
                  item.status === "created" || item.status === "started",
              ).length,
            ),
            variant: "info",
          },
          {
            label: "errored",
            value: String(
              (data ?? []).filter((item) => item.status === "error").length,
            ),
            variant: (data ?? []).some((item) => item.status === "error")
              ? "danger"
              : "default",
          },
          {
            label: "completed",
            value: String(
              (data ?? []).filter((item) => item.status === "done").length,
            ),
            variant: "success",
          },
        ]}
      />

      {loading && <LoadingSpinner message="Loading deletion requests..." />}

      {error && (
        <div
          className="rounded-lg border border-yellow-200 bg-yellow-50 p-4"
          role="status"
        >
          <div className="flex items-start gap-3">
            <svg
              className="mt-0.5 h-5 w-5 flex-shrink-0 text-yellow-500"
              fill="none"
              viewBox="0 0 24 24"
              strokeWidth={1.5}
              stroke="currentColor"
              aria-hidden="true"
            >
              <path
                strokeLinecap="round"
                strokeLinejoin="round"
                d="M12 9v3.75m-9.303 3.376c-.866 1.5.217 3.374 1.948 3.374h14.71c1.73 0 2.813-1.874 1.948-3.374L13.949 3.378c-.866-1.5-3.032-1.5-3.898 0L2.697 16.126zM12 15.75h.007v.008H12v-.008z"
              />
            </svg>
            <div>
              <p className="text-sm font-medium text-yellow-800">
                Unable to load deletion requests
              </p>
              <p className="mt-1 text-sm text-yellow-700">
                The server returned an error while loading the deletion queue.
              </p>
            </div>
          </div>
        </div>
      )}

      {selectedRequestId && selectedRequest.error && (
        <div className="mb-6">
          <ErrorMessage
            title="Deletion detail failed to load"
            message={selectedRequest.error}
            links={[{ label: "Inspect Flows", to: "/flows" }]}
            onRetry={selectedRequest.refetch}
          />
        </div>
      )}

      {!loading && !error && (data ?? []).length === 0 && (
        <EmptyState
          title="No deletion requests"
          description="There are no active or completed deletion requests"
        />
      )}

      {data && data.length > 0 && (
        <div className="grid gap-6 lg:grid-cols-[1.2fr_1fr]">
          <div className="space-y-3">
            {data.map((req) => (
              <button
                key={req.id}
                type="button"
                onClick={() => syncSelectedRequest(req.id)}
                className={`w-full rounded-xl bg-white p-4 text-left border sm:p-5 ${
                  selectedRequestId === req.id
                    ? "border-tams-400"
                    : "border-lw-ink-100 hover:border-lw-ink-200"
                }`}
              >
                <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between sm:gap-4">
                  <div>
                    <div className="flex items-center gap-3">
                      <code className="text-sm font-medium text-gray-900">
                        {req.id}
                      </code>
                      <Badge variant={statusVariants[req.status] ?? "default"}>
                        {req.status}
                      </Badge>
                      {isStale(req) && <Badge variant="danger">stalled</Badge>}
                      {req.delete_flow && (
                        <Badge variant="danger">Full delete</Badge>
                      )}
                    </div>
                    <div className="mt-2 space-y-1 text-sm text-gray-500">
                      <p>
                        Flow:{" "}
                        <span className="font-mono text-xs text-gray-600">
                          {req.flow_id}
                        </span>
                      </p>
                      <p>
                        Timerange to delete:{" "}
                        <span className="font-mono">
                          {formatTimerange(req.timerange_to_delete)}
                        </span>
                      </p>
                      {req.timerange_remaining && (
                        <p>
                          Remaining:{" "}
                          <span className="font-mono">
                            {formatTimerange(req.timerange_remaining)}
                          </span>
                        </p>
                      )}
                    </div>
                  </div>
                  <div className="text-xs text-gray-400 sm:text-right">
                    {req.created && (
                      <p>Age {formatRelativeTime(req.created)}</p>
                    )}
                    {req.created && <p>Created {formatDate(req.created)}</p>}
                    {req.updated && (
                      <p>
                        Updated {formatDate(req.updated)} (
                        {formatRelativeTime(req.updated)})
                      </p>
                    )}
                    {req.created_by && <p>By {req.created_by}</p>}
                  </div>
                </div>
                {req.error && (
                  <div className="mt-3 rounded-lg bg-red-50 p-3 text-sm text-red-700">
                    {req.error.summary ?? "Unknown error"}
                  </div>
                )}
              </button>
            ))}
          </div>

          <div className="tamoss-panel rounded-2xl p-5">
            <h2 className="mb-4 text-lg font-semibold text-gray-900">
              Request details
            </h2>
            {!selectedRequestId && (
              <EmptyState
                title="Select a request"
                description="Choose a deletion request to inspect its current state and raw payload."
              />
            )}
            {selectedRequestId && selectedRequest.loading && (
              <LoadingSpinner message="Loading request details..." />
            )}
            {selectedRequestId && selectedRequest.error && null}
            {selectedRequest.data && (
              <>
                <div className="mb-5">
                  <AsyncLifecycle
                    title="Delete Request Lifecycle"
                    steps={buildDeletionLifecycle(selectedRequest.data)}
                  />
                </div>
                <div className="space-y-3 text-sm text-gray-600">
                  <p>
                    <span className="font-medium text-gray-900">Flow:</span>{" "}
                    <Link
                      to={`/flows/${selectedRequest.data.flow_id}`}
                      className="text-tams-600 hover:text-tams-700"
                    >
                      {selectedRequest.data.flow_id}
                    </Link>
                  </p>
                  <p>
                    <span className="font-medium text-gray-900">Status:</span>{" "}
                    <Badge
                      variant={
                        statusVariants[selectedRequest.data.status] ?? "default"
                      }
                    >
                      {selectedRequest.data.status}
                    </Badge>
                    {isStale(selectedRequest.data) && (
                      <span className="ml-2">
                        <Badge variant="danger">stalled</Badge>
                      </span>
                    )}
                  </p>
                  <p>
                    <span className="font-medium text-gray-900">Age:</span>{" "}
                    {formatRelativeTime(selectedRequest.data.created)}
                  </p>
                  <p>
                    <span className="font-medium text-gray-900">
                      Delete mode:
                    </span>{" "}
                    {selectedRequest.data.delete_flow
                      ? "Full flow delete"
                      : "Segment delete"}
                  </p>
                  <p>
                    <span className="font-medium text-gray-900">
                      Target timerange:
                    </span>{" "}
                    <span className="font-mono">
                      {formatTimerange(
                        selectedRequest.data.timerange_to_delete,
                      )}
                    </span>
                  </p>
                  {selectedRequest.data.timerange_remaining && (
                    <p>
                      <span className="font-medium text-gray-900">
                        Remaining timerange:
                      </span>{" "}
                      <span className="font-mono">
                        {formatTimerange(
                          selectedRequest.data.timerange_remaining,
                        )}
                      </span>
                    </p>
                  )}
                  {selectedRequest.data.updated && (
                    <p>
                      <span className="font-medium text-gray-900">
                        Last update:
                      </span>{" "}
                      {formatDate(selectedRequest.data.updated)} (
                      {formatRelativeTime(selectedRequest.data.updated)})
                    </p>
                  )}
                </div>

                <div className="mt-5 rounded-lg bg-gray-950 p-4">
                  <pre className="overflow-x-auto text-xs text-gray-200">
                    {JSON.stringify(selectedRequest.data, null, 2)}
                  </pre>
                </div>
                <div className="mt-5">
                  <ApiReferencePanel
                    method="GET"
                    path={`/flow-delete-requests/${selectedRequest.data.id}`}
                  />
                </div>
              </>
            )}
          </div>
        </div>
      )}
    </div>
  );
}
