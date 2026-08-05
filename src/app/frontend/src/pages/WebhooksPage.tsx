import { useCallback, useState } from "react";
import { useSearchParams } from "react-router";
import { useApi } from "@/contexts/ApiContext";
import { useApiQuery } from "@/hooks/useApiQuery";
import { usePageTitle } from "@/hooks/usePageTitle";
import { useWebhookMutations } from "@/hooks/useWebhookMutations";
import LoadingSpinner from "@/components/LoadingSpinner";
import ErrorMessage from "@/components/ErrorMessage";
import EmptyState from "@/components/EmptyState";
import Badge from "@/components/Badge";
import CopyButton from "@/components/CopyButton";
import StateStrip from "@/components/StateStrip";
import ApiReferencePanel from "@/components/ApiReferencePanel";
import AsyncLifecycle from "@/components/AsyncLifecycle";
import WebhookForm from "@/components/webhooks/WebhookForm";
import WebhookToolbar from "@/components/webhooks/WebhookToolbar";
import {
  WEBHOOK_PAGE_SIZE,
  buildWebhookLifecycle,
  statusVariants,
} from "@/pages/webhooksModel";
import type { WebhookDetail } from "@/types/tams";

export default function WebhooksPage() {
  usePageTitle("Webhooks");
  const api = useApi();
  const [searchParams, setSearchParams] = useSearchParams();
  const [allWebhooks, setAllWebhooks] = useState<WebhookDetail[]>([]);
  const [nextKey, setNextKey] = useState<string | undefined>();
  const [loadingMore, setLoadingMore] = useState(false);
  const webhooks = useApiQuery(async () => {
    const result = await api.getWebhooks({ limit: WEBHOOK_PAGE_SIZE });
    setAllWebhooks(result.data);
    setNextKey(result.nextKey);
    return result;
  }, [api]);
  const storageBackends = useApiQuery(() => api.getStorageBackends(), [api]);
  const [creating, setCreating] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(
    searchParams.get("webhook") ?? null,
  );
  const [filter, setFilter] = useState(searchParams.get("q") ?? "");
  const [statusFilter, setStatusFilter] = useState(
    searchParams.get("status") ?? "",
  );
  const [deleteConfirmId, setDeleteConfirmId] = useState<string | null>(null);
  const [compactMode, setCompactMode] = useState(false);
  const syncSelectedWebhook = useCallback(
    (webhookId: string | null) => {
      setSelectedId(webhookId);
      const next = new URLSearchParams(searchParams);
      if (webhookId) next.set("webhook", webhookId);
      else next.delete("webhook");
      setSearchParams(next, { replace: true });
    },
    [searchParams, setSearchParams],
  );
  const handleFilterChange = useCallback(
    (value: string) => {
      setFilter(value);
      const next = new URLSearchParams(searchParams);
      if (value) next.set("q", value);
      else next.delete("q");
      setSearchParams(next, { replace: true });
    },
    [searchParams, setSearchParams],
  );
  const handleStatusFilterChange = useCallback(
    (value: string) => {
      setStatusFilter(value);
      const next = new URLSearchParams(searchParams);
      if (value) next.set("status", value);
      else next.delete("status");
      setSearchParams(next, { replace: true });
    },
    [searchParams, setSearchParams],
  );
  const selectedWebhook = useApiQuery(
    () => (selectedId ? api.getWebhook(selectedId) : Promise.resolve(null)),
    [api, selectedId],
  );
  const handleWebhookCreated = useCallback(
    (created: WebhookDetail) => {
      setCreating(false);
      syncSelectedWebhook(created.id ?? null);
    },
    [syncSelectedWebhook],
  );
  const handleWebhookUpdated = useCallback(() => {
    setEditingId(null);
  }, []);
  const handleWebhookDeleted = useCallback(
    (webhookId: string) => {
      if (editingId === webhookId) {
        setEditingId(null);
      }
      if (selectedId === webhookId) {
        syncSelectedWebhook(null);
      }
      if (deleteConfirmId === webhookId) {
        setDeleteConfirmId(null);
      }
    },
    [deleteConfirmId, editingId, selectedId, syncSelectedWebhook],
  );
  const {
    busy,
    createWebhook: handleCreate,
    updateWebhook: handleUpdate,
    deleteWebhook: handleDelete,
  } = useWebhookMutations({
    api,
    refreshWebhooks: webhooks.refetch,
    refreshWebhookDetail: selectedWebhook.refetch,
    onCreated: handleWebhookCreated,
    onUpdated: handleWebhookUpdated,
    onDeleted: handleWebhookDeleted,
  });

  const loadMore = useCallback(async () => {
    if (!nextKey || loadingMore) return;
    setLoadingMore(true);
    try {
      const result = await api.getWebhooks({
        limit: WEBHOOK_PAGE_SIZE,
        page: nextKey,
      });
      setAllWebhooks((previous) => [...previous, ...result.data]);
      setNextKey(result.nextKey);
    } finally {
      setLoadingMore(false);
    }
  }, [api, nextKey, loadingMore]);

  const filteredWebhooks = allWebhooks.filter((webhook) => {
    const matchesFilter =
      !filter ||
      webhook.url.toLowerCase().includes(filter.toLowerCase()) ||
      webhook.id?.toLowerCase().includes(filter.toLowerCase()) ||
      webhook.events.some((eventName) =>
        eventName.toLowerCase().includes(filter.toLowerCase()),
      );
    const matchesStatus = !statusFilter || webhook.status === statusFilter;
    return matchesFilter && matchesStatus;
  });

  return (
    <div className="p-4 sm:p-6 lg:p-8">
      <div className="mb-6 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-xl font-bold text-gray-900 sm:text-2xl">
            Webhooks
          </h1>
          <p className="mt-1 text-sm text-gray-500">
            Register and monitor outbound event notifications
          </p>
        </div>
        <WebhookToolbar
          filter={filter}
          statusFilter={statusFilter}
          compactMode={compactMode}
          creating={creating}
          onFilterChange={handleFilterChange}
          onStatusFilterChange={handleStatusFilterChange}
          onRefresh={webhooks.refetch}
          onToggleCompactMode={() => setCompactMode((previous) => !previous)}
          onToggleCreating={() => setCreating((previous) => !previous)}
        />
      </div>

      <StateStrip
        title="Webhook State"
        refreshedAt={allWebhooks.length >= 0 ? new Date().toISOString() : null}
        items={[
          {
            label: "active",
            value: String(
              allWebhooks.filter(
                (item) =>
                  item.status === "created" || item.status === "started",
              ).length,
            ),
            variant: "success",
          },
          {
            label: "disabled",
            value: String(
              allWebhooks.filter((item) => item.status === "disabled").length,
            ),
            variant: "warning",
          },
          {
            label: "errors",
            value: String(
              allWebhooks.filter((item) => item.status === "error").length,
            ),
            variant: allWebhooks.some((item) => item.status === "error")
              ? "danger"
              : "default",
          },
        ]}
      />

      {creating && (
        <section className="mb-8 tamoss-panel rounded-2xl p-6">
          <h2 className="mb-4 text-lg font-semibold text-gray-900">
            Register webhook
          </h2>
          <WebhookForm
            submitLabel="Create webhook"
            busy={busy}
            storageBackends={storageBackends.data}
            onSubmit={handleCreate}
            onCancel={() => setCreating(false)}
          />
        </section>
      )}

      {webhooks.loading && <LoadingSpinner message="Loading webhooks..." />}
      {webhooks.error && (
        <ErrorMessage
          title="Webhook list failed to load"
          message={webhooks.error}
          onRetry={webhooks.refetch}
        />
      )}

      {!webhooks.loading &&
        !webhooks.error &&
        filteredWebhooks.length === 0 && (
          <EmptyState
            title="No webhooks"
            description="Create a webhook to receive flow and source events from this TAMOSS instance."
          />
        )}

      {filteredWebhooks.length ? (
        <div className="grid gap-6 lg:grid-cols-[1.2fr_1fr]">
          <div className="overflow-hidden tamoss-panel rounded-2xl">
            <div className="grid grid-cols-[2.1fr_1.2fr_1fr_1fr_auto] gap-3 border-b border-gray-200 bg-gray-50 px-4 py-3 text-xs font-medium uppercase tracking-wider text-gray-500">
              <div>Webhook</div>
              <div>Events</div>
              <div>State</div>
              <div>Selectors</div>
              <div className="text-right">Actions</div>
            </div>
            <div className="max-h-[70vh] overflow-auto divide-y divide-gray-100">
              {filteredWebhooks.map((webhook) => {
                const isEditing = editingId === webhook.id;
                return (
                  <div
                    key={webhook.id}
                    className={`${selectedId === webhook.id ? "bg-tams-50" : "hover:bg-gray-50"}`}
                  >
                    {isEditing ? (
                      <div className="px-4 py-4">
                        <h2 className="mb-4 text-lg font-semibold text-gray-900">
                          Edit webhook
                        </h2>
                        <WebhookForm
                          initial={webhook}
                          submitLabel="Save changes"
                          busy={busy}
                          storageBackends={storageBackends.data}
                          onSubmit={(payload) =>
                            handleUpdate(webhook.id ?? "", payload)
                          }
                          onCancel={() => setEditingId(null)}
                        />
                      </div>
                    ) : (
                      <>
                        <div
                          className={`grid grid-cols-[2.1fr_1.2fr_1fr_1fr_auto] gap-3 ${compactMode ? "px-4 py-2" : "px-4 py-3"}`}
                        >
                          <div className="min-w-0">
                            <p className="truncate text-sm font-semibold text-gray-900">
                              {webhook.url}
                            </p>
                            {webhook.id && (
                              <p className="mt-1 font-mono text-xs text-gray-500">
                                {webhook.id}
                              </p>
                            )}
                          </div>
                          <div className="flex flex-wrap gap-2">
                            {webhook.events
                              .slice(0, compactMode ? 2 : 3)
                              .map((eventName) => (
                                <Badge key={eventName} variant="info">
                                  {eventName}
                                </Badge>
                              ))}
                            {webhook.events.length > (compactMode ? 2 : 3) && (
                              <Badge variant="default">
                                +{webhook.events.length - (compactMode ? 2 : 3)}
                              </Badge>
                            )}
                          </div>
                          <div className="space-y-2">
                            {webhook.status && (
                              <Badge
                                variant={
                                  statusVariants[webhook.status] ?? "default"
                                }
                              >
                                {webhook.status}
                              </Badge>
                            )}
                            {webhook.error?.summary && (
                              <p className="max-w-xs text-xs text-red-700">
                                {webhook.error.summary}
                              </p>
                            )}
                          </div>
                          <div className="space-y-1 text-xs text-gray-500">
                            <p>flow filters: {webhook.flow_ids?.length ?? 0}</p>
                            <p>
                              source filters: {webhook.source_ids?.length ?? 0}
                            </p>
                            <p>
                              storage filters:{" "}
                              {webhook.accept_storage_ids?.length ?? 0}
                            </p>
                            <p>presigned: {webhook.presigned ? "yes" : "no"}</p>
                          </div>
                          <div className="inline-flex flex-wrap justify-end gap-2 text-right">
                            <button
                              onClick={() =>
                                syncSelectedWebhook(webhook.id ?? null)
                              }
                              className="rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm font-medium text-gray-700 shadow-sm hover:bg-gray-50"
                            >
                              Inspect
                            </button>
                            <button
                              onClick={() => setEditingId(webhook.id ?? null)}
                              className="rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm font-medium text-gray-700 shadow-sm hover:bg-gray-50"
                            >
                              Edit
                            </button>
                            <button
                              onClick={() =>
                                setDeleteConfirmId(webhook.id ?? null)
                              }
                              className="rounded-lg border border-red-300 bg-white px-3 py-2 text-sm font-medium text-red-600 shadow-sm hover:bg-red-50"
                            >
                              Delete
                            </button>
                          </div>
                        </div>

                        {deleteConfirmId === webhook.id && (
                          <div className="border-t border-red-100 px-4 pb-4 pt-2">
                            <div className="rounded-lg border border-red-200 bg-red-50 p-4">
                              <p className="text-sm font-medium text-red-900">
                                Delete this webhook?
                              </p>
                              <p className="mt-1 text-sm text-red-700">
                                This removes the registration from TAMOSS and
                                stops future deliveries.
                              </p>
                              <p className="mt-1 text-sm text-red-700">
                                Historical delivery state may still remain in
                                backend records, but this endpoint will no
                                longer receive new events.
                              </p>
                              <div className="mt-3 flex gap-2">
                                <button
                                  onClick={() => handleDelete(webhook.id ?? "")}
                                  disabled={busy}
                                  className="rounded-lg bg-red-600 px-3 py-2 text-sm font-medium text-white hover:bg-red-700 disabled:opacity-50"
                                >
                                  {busy ? "Deleting..." : "Confirm delete"}
                                </button>
                                <button
                                  onClick={() => setDeleteConfirmId(null)}
                                  disabled={busy}
                                  className="rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
                                >
                                  Cancel
                                </button>
                              </div>
                            </div>
                          </div>
                        )}
                      </>
                    )}
                  </div>
                );
              })}
            </div>

            {nextKey && (
              <div className="border-t border-gray-200 p-4 flex justify-center">
                <button
                  onClick={loadMore}
                  disabled={loadingMore}
                  className="rounded-lg bg-white px-4 py-2 text-sm font-medium text-gray-700 border border-gray-300 hover:bg-gray-50 disabled:opacity-50"
                >
                  {loadingMore ? "Loading..." : "Load more webhooks"}
                </button>
              </div>
            )}
          </div>

          <aside className="tamoss-panel rounded-2xl p-6">
            <h2 className="mb-4 text-lg font-semibold text-gray-900">
              Webhook detail
            </h2>
            {!selectedId && (
              <EmptyState
                title="Select a webhook"
                description="Choose a webhook to inspect the latest server-side payload and status."
              />
            )}
            {selectedId && selectedWebhook.loading && (
              <LoadingSpinner message="Loading webhook detail..." />
            )}
            {selectedId && selectedWebhook.error && (
              <ErrorMessage
                title="Webhook detail failed to load"
                message={selectedWebhook.error}
                onRetry={selectedWebhook.refetch}
              />
            )}
            {selectedWebhook.data && (
              <>
                <div className="mb-5">
                  <AsyncLifecycle
                    title="Webhook Lifecycle"
                    steps={buildWebhookLifecycle(selectedWebhook.data)}
                  />
                </div>
                <div className="space-y-3 text-sm text-gray-600">
                  <p>
                    <span className="font-medium text-gray-900">URL:</span>{" "}
                    {selectedWebhook.data.url}
                  </p>
                  <p>
                    <span className="font-medium text-gray-900">Status:</span>{" "}
                    {selectedWebhook.data.status ?? "Unknown"}
                  </p>
                  <p>
                    <span className="font-medium text-gray-900">Events:</span>{" "}
                    {selectedWebhook.data.events.join(", ")}
                  </p>
                  <p>
                    <span className="font-medium text-gray-900">
                      Flow filters:
                    </span>{" "}
                    {selectedWebhook.data.flow_ids?.join(", ") ?? "None"}
                  </p>
                  <p>
                    <span className="font-medium text-gray-900">
                      Source filters:
                    </span>{" "}
                    {selectedWebhook.data.source_ids?.join(", ") ?? "None"}
                  </p>
                  <p>
                    <span className="font-medium text-gray-900">
                      Storage filters:
                    </span>{" "}
                    {selectedWebhook.data.accept_storage_ids?.join(", ") ??
                      "None"}
                  </p>
                </div>
                {selectedWebhook.data.error && (
                  <div className="mt-4 rounded-lg bg-red-50 p-4 text-sm text-red-700">
                    <p className="font-medium">
                      {selectedWebhook.data.error.type ?? "Webhook error"}
                    </p>
                    <p className="mt-1">
                      {selectedWebhook.data.error.summary ?? "Unknown error"}
                    </p>
                  </div>
                )}
                <div className="mt-5 rounded-lg bg-gray-950 p-4">
                  <div className="mb-3 flex items-center justify-between gap-3">
                    <p className="text-sm font-medium text-white">
                      Raw payload
                    </p>
                    <CopyButton
                      text={JSON.stringify(selectedWebhook.data, null, 2)}
                      label="Copy JSON"
                    />
                  </div>
                  <pre className="overflow-x-auto text-xs text-gray-200">
                    {JSON.stringify(selectedWebhook.data, null, 2)}
                  </pre>
                </div>
                {selectedWebhook.data.id && (
                  <div className="mt-5">
                    <ApiReferencePanel
                      method="GET"
                      path={`/service/webhooks/${selectedWebhook.data.id}`}
                    />
                  </div>
                )}
              </>
            )}
          </aside>
        </div>
      ) : null}
    </div>
  );
}
