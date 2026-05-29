import { useState, useCallback, useEffect } from "react";
import { useParams, Link, useNavigate } from "react-router-dom";
import { useApi } from "@/contexts/ApiContext";
import { useToast } from "@/hooks/useToast";
import { useApiQuery } from "@/hooks/useApiQuery";
import { usePageTitle } from "@/hooks/usePageTitle";
import LoadingSpinner from "@/components/LoadingSpinner";
import ErrorMessage from "@/components/ErrorMessage";
import Badge from "@/components/Badge";
import CopyButton from "@/components/CopyButton";
import InlineEditField from "@/components/InlineEditField";
import EditableTagList from "@/components/EditableTagList";
import AddSegmentsDialog from "@/components/AddSegmentsDialog";
import ManageChildFlowsDialog from "@/components/ManageChildFlowsDialog";
import ConfirmAction from "@/components/ConfirmAction";
import RawPayload from "@/components/RawPayload";
import TraceRail from "@/components/TraceRail";
import { useInfiniteScroll } from "@/hooks/useInfiniteScroll";
import StateStrip from "@/components/StateStrip";
import ApiReferencePanel from "@/components/ApiReferencePanel";
import SectionHeading from "@/components/SectionHeading";
import StorageBackendSelector from "@/components/StorageBackendSelector";
import FlowMetadataPanel from "@/components/flow-detail/FlowMetadataPanel";
import SegmentTable from "@/components/flow-detail/SegmentTable";
import {
  findStorageBackend,
  storageBackendDisplay,
} from "@/utils/storageBackends";
import {
  formatFormat,
  formatCodec,
  formatResolution,
  formatFrameRate,
  formatBitRate,
} from "@/utils/format";
import {
  SEGMENT_PAGE_SIZE,
  deletionRequestPath,
  estimateDeleteScope,
} from "@/pages/flowDetailModel";
import type {
  DeletionRequest,
  Flow,
  FlowSegment,
  FlowCollectionItem,
  StorageAllocation,
} from "@/types/tams";

export default function FlowDetailPage() {
  usePageTitle("Flow");
  const { flowId } = useParams<{ flowId: string }>();
  const api = useApi();
  const pushToast = useToast();
  const navigate = useNavigate();
  const [segments, setSegments] = useState<FlowSegment[]>([]);
  const [segNextKey, setSegNextKey] = useState<string | undefined>();
  const [segLoadingMore, setSegLoadingMore] = useState(false);
  const [deleteConfirm, setDeleteConfirm] = useState(false);
  const [flowDeleting, setFlowDeleting] = useState(false);
  const [showAddSegments, setShowAddSegments] = useState(false);
  const [showChildFlows, setShowChildFlows] = useState(false);
  const [childFlows, setChildFlows] = useState<FlowCollectionItem[]>([]);
  const [childSegmentsMap, setChildSegmentsMap] = useState<
    Record<string, FlowSegment[]>
  >({});
  const [childSegmentErrors, setChildSegmentErrors] = useState<
    Record<string, string>
  >({});
  const [addSegTargetFlow, setAddSegTargetFlow] = useState<Flow | null>(null);

  // Multi-select removal state (main segments)
  const [selectedSegments, setSelectedSegments] = useState<Set<number>>(
    new Set(),
  );
  const [removeConfirm, setRemoveConfirm] = useState(false);
  const [removing, setRemoving] = useState(false);

  // Multi-select removal state (child flow segments)
  const [selectedChildSegments, setSelectedChildSegments] = useState<
    Record<string, Set<number>>
  >({});
  const [removeChildConfirm, setRemoveChildConfirm] = useState<string | null>(
    null,
  );
  const [removingChild, setRemovingChild] = useState(false);

  const [readOnlySaving, setReadOnlySaving] = useState(false);
  const [readOnlyConfirm, setReadOnlyConfirm] = useState<
    "lock" | "unlock" | null
  >(null);
  const [storageCount, setStorageCount] = useState(1);
  const [allocationStorageId, setAllocationStorageId] = useState("");
  const [segmentStorageId, setSegmentStorageId] = useState("");
  const [storageAllocating, setStorageAllocating] = useState(false);
  const [storageError, setStorageError] = useState<string | null>(null);
  const [allocatedObjects, setAllocatedObjects] = useState<
    StorageAllocation["media_objects"]
  >([]);

  const {
    data: flow,
    loading,
    error,
    refetch,
  } = useApiQuery(() => api.getFlow(flowId!), [api, flowId]);

  const storageBackends = useApiQuery(() => api.getStorageBackends(), [api]);

  const segQuery = useApiQuery(async () => {
    const result = await api.getFlowSegments(flowId!, {
      include_object_timerange: true,
      ...(segmentStorageId ? { accept_storage_ids: segmentStorageId } : {}),
      limit: SEGMENT_PAGE_SIZE,
      verbose_storage: true,
    });
    setSegments(result.data);
    setSegNextKey(result.nextKey);
    setSelectedSegments(new Set());
    setRemoveConfirm(false);
    return result;
  }, [api, flowId, segmentStorageId]);

  const collectionQuery = useApiQuery(async () => {
    try {
      const items = await api.getFlowCollection(flowId!);
      setChildFlows(items);
      return items;
    } catch (err) {
      setChildFlows([]);
      setChildSegmentsMap({});
      setChildSegmentErrors({});
      throw err;
    }
  }, [api, flowId]);

  // Fetch segments for each child flow
  useEffect(() => {
    if (childFlows.length === 0) {
      setChildSegmentsMap({});
      setChildSegmentErrors({});
      return;
    }
    let cancelled = false;
    Promise.all(
      childFlows.map(async (c) => {
        try {
          const result = await api.getFlowSegments(c.id, {
            ...(segmentStorageId
              ? { accept_storage_ids: segmentStorageId }
              : {}),
            include_object_timerange: true,
            limit: SEGMENT_PAGE_SIZE,
            verbose_storage: true,
          });
          return [c.id, result.data, null] as const;
        } catch (err) {
          return [
            c.id,
            [] as FlowSegment[],
            err instanceof Error ? err.message : "Unknown error",
          ] as const;
        }
      }),
    ).then((results) => {
      if (cancelled) return;
      setChildSegmentsMap(
        Object.fromEntries(results.map(([id, data]) => [id, data])),
      );
      setChildSegmentErrors(
        Object.fromEntries(
          results
            .filter(([, , err]) => err)
            .map(([id, , err]) => [id, err as string]),
        ),
      );
    });
    return () => {
      cancelled = true;
    };
  }, [api, childFlows, segmentStorageId]);

  const loadMoreSegments = useCallback(async () => {
    if (!segNextKey || segLoadingMore) return;
    setSegLoadingMore(true);
    try {
      const result = await api.getFlowSegments(flowId!, {
        include_object_timerange: true,
        ...(segmentStorageId ? { accept_storage_ids: segmentStorageId } : {}),
        limit: SEGMENT_PAGE_SIZE,
        page: segNextKey,
        verbose_storage: true,
      });
      setSegments((prev) => [...prev, ...result.data]);
      setSegNextKey(result.nextKey);
    } finally {
      setSegLoadingMore(false);
    }
  }, [api, flowId, segNextKey, segLoadingMore, segmentStorageId]);

  const segmentsSentinelRef = useInfiniteScroll(
    !!segNextKey && !segLoadingMore,
    loadMoreSegments,
  );

  const handleDelete = useCallback(async () => {
    if (!flowId) return;
    setFlowDeleting(true);
    try {
      const deleteRequest = await api.deleteFlow(flowId);
      if (deleteRequest?.id) {
        navigate(deletionRequestPath(deleteRequest));
        return;
      }
      navigate("/flows");
    } catch (err) {
      pushToast({
        kind: "error",
        message: err instanceof Error ? err.message : "Delete failed",
      });
    } finally {
      setFlowDeleting(false);
    }
  }, [api, flowId, navigate, pushToast]);

  const handleReadOnlyToggle = useCallback(async () => {
    if (!flowId || !flow) return;
    setReadOnlySaving(true);
    try {
      await api.setFlowReadOnly(flowId, !flow.read_only);
      refetch();
    } catch (err) {
      pushToast({
        kind: "error",
        message:
          err instanceof Error
            ? err.message
            : "Failed to update read-only state",
      });
    } finally {
      setReadOnlySaving(false);
    }
  }, [api, flowId, flow, refetch, pushToast]);

  const handleAllocateStorage = useCallback(async () => {
    if (!flowId) return;
    setStorageAllocating(true);
    setStorageError(null);
    try {
      const result = await api.allocateStorageByCount(flowId, storageCount, {
        storageId: allocationStorageId || undefined,
      });
      setAllocatedObjects(result.media_objects);
    } catch (err) {
      setStorageError(
        err instanceof Error ? err.message : "Failed to allocate storage",
      );
    } finally {
      setStorageAllocating(false);
    }
  }, [api, flowId, storageCount, allocationStorageId]);

  const openAddSegmentsForChild = useCallback(
    async (childId: string) => {
      try {
        const childFlow = await api.getFlow(childId);
        setAddSegTargetFlow(childFlow);
      } catch (err) {
        pushToast({
          kind: "error",
          message:
            err instanceof Error ? err.message : "Failed to load child flow",
        });
      }
    },
    [api, pushToast],
  );

  const refreshChildSegments = useCallback(
    async (childId: string) => {
      try {
        const result = await api.getFlowSegments(childId, {
          ...(segmentStorageId ? { accept_storage_ids: segmentStorageId } : {}),
          include_object_timerange: true,
          limit: SEGMENT_PAGE_SIZE,
          verbose_storage: true,
        });
        setChildSegmentsMap((prev) => ({ ...prev, [childId]: result.data }));
        setChildSegmentErrors((prev) => {
          const next = { ...prev };
          delete next[childId];
          return next;
        });
        setSelectedChildSegments((prev) => {
          const next = { ...prev };
          delete next[childId];
          return next;
        });
        setRemoveChildConfirm(null);
      } catch (err) {
        const message = err instanceof Error ? err.message : "Unknown error";
        setChildSegmentsMap((prev) => ({ ...prev, [childId]: [] }));
        setChildSegmentErrors((prev) => ({ ...prev, [childId]: message }));
        pushToast({
          kind: "error",
          message: `Failed to load child flow segments: ${message}`,
        });
      }
    },
    [api, pushToast, segmentStorageId],
  );

  // Multi-select helpers (main segments)
  const toggleSegment = (i: number) => {
    setSelectedSegments((prev) => {
      const next = new Set(prev);
      if (next.has(i)) next.delete(i);
      else next.add(i);
      return next;
    });
  };

  const toggleAllSegments = () => {
    if (selectedSegments.size === segments.length) {
      setSelectedSegments(new Set());
    } else {
      setSelectedSegments(new Set(segments.map((_, i) => i)));
    }
  };

  // Multi-select helpers (child flow segments)
  const toggleChildSegment = (childId: string, i: number) => {
    setSelectedChildSegments((prev) => {
      const current = prev[childId] ?? new Set<number>();
      const next = new Set(current);
      if (next.has(i)) next.delete(i);
      else next.add(i);
      return { ...prev, [childId]: next };
    });
  };

  const toggleAllChildSegments = (childId: string, count: number) => {
    setSelectedChildSegments((prev) => {
      const current = prev[childId] ?? new Set<number>();
      if (current.size === count) {
        const next = { ...prev };
        delete next[childId];
        return next;
      }
      return {
        ...prev,
        [childId]: new Set(Array.from({ length: count }, (_, i) => i)),
      };
    });
  };

  // Bulk delete handlers. Loops halt on first failure and report both
  // which items were deleted and which were not attempted — the backend
  // has no bulk-delete primitive for a set of timeranges, so partial
  // state is surfaced rather than silently tolerated.
  const handleBulkDeleteSegments = useCallback(async () => {
    if (!flowId || selectedSegments.size === 0) return;
    setRemoving(true);
    const ordered = Array.from(selectedSegments);
    const succeeded: string[] = [];
    const deleteRequests: DeletionRequest[] = [];
    let failedRange: string | null = null;
    let failureError: unknown = null;
    try {
      for (const idx of ordered) {
        const range = segments[idx].timerange;
        try {
          const deleteRequest = await api.deleteFlowSegments(flowId, range);
          succeeded.push(range);
          if (deleteRequest?.id) deleteRequests.push(deleteRequest);
        } catch (err) {
          failedRange = range;
          failureError = err;
          break;
        }
      }
    } finally {
      segQuery.refetch();
      refetch();
      setRemoving(false);
    }
    if (failedRange) {
      const notAttempted = ordered.length - succeeded.length - 1;
      const msg =
        failureError instanceof Error ? failureError.message : "unknown error";
      pushToast({
        kind: "error",
        message:
          `Deleted ${succeeded.length} segment(s). Failed on ${failedRange}: ${msg}. ` +
          `${notAttempted} segment(s) not attempted. Selection preserved.`,
        action: deleteRequests[0]
          ? {
              label: "Open request",
              href: deletionRequestPath(deleteRequests[0]),
            }
          : { label: "Open queue", href: "/deletions" },
      });
      return;
    }
    setSelectedSegments(new Set());
    setRemoveConfirm(false);
    if (deleteRequests.length > 0) {
      pushToast({
        kind: "success",
        message: `Queued ${deleteRequests.length} deletion request(s).`,
        action: {
          label: "Open request",
          href: deletionRequestPath(deleteRequests[0]),
        },
      });
    } else {
      pushToast({
        kind: "success",
        message: `Deleted ${succeeded.length} segment registration(s).`,
      });
    }
  }, [api, flowId, segments, selectedSegments, segQuery, refetch, pushToast]);

  const handleBulkDeleteChildSegments = useCallback(
    async (childId: string) => {
      const selected = selectedChildSegments[childId];
      if (!selected || selected.size === 0) return;
      const childSegs = childSegmentsMap[childId] ?? [];
      setRemovingChild(true);
      const ordered = Array.from(selected);
      const succeeded: string[] = [];
      const deleteRequests: DeletionRequest[] = [];
      let failedRange: string | null = null;
      let failureError: unknown = null;
      try {
        for (const idx of ordered) {
          const range = childSegs[idx].timerange;
          try {
            const deleteRequest = await api.deleteFlowSegments(childId, range);
            succeeded.push(range);
            if (deleteRequest?.id) deleteRequests.push(deleteRequest);
          } catch (err) {
            failedRange = range;
            failureError = err;
            break;
          }
        }
      } finally {
        await refreshChildSegments(childId);
        refetch();
        setRemovingChild(false);
      }
      if (failedRange) {
        const notAttempted = ordered.length - succeeded.length - 1;
        const msg =
          failureError instanceof Error
            ? failureError.message
            : "unknown error";
        pushToast({
          kind: "error",
          message:
            `Deleted ${succeeded.length} segment(s). Failed on ${failedRange}: ${msg}. ` +
            `${notAttempted} segment(s) not attempted.`,
          action: deleteRequests[0]
            ? {
                label: "Open request",
                href: deletionRequestPath(deleteRequests[0]),
              }
            : { label: "Open queue", href: "/deletions" },
        });
        return;
      }
      if (deleteRequests.length > 0) {
        pushToast({
          kind: "success",
          message: `Queued ${deleteRequests.length} deletion request(s).`,
          action: {
            label: "Open request",
            href: deletionRequestPath(deleteRequests[0]),
          },
        });
      } else {
        pushToast({
          kind: "success",
          message: `Deleted ${succeeded.length} segment registration(s).`,
        });
      }
    },
    [
      api,
      childSegmentsMap,
      selectedChildSegments,
      refreshChildSegments,
      refetch,
      pushToast,
    ],
  );

  if (loading) return <LoadingSpinner message="Loading flow..." />;
  if (error)
    return (
      <div className="p-6">
        <ErrorMessage message={error} onRetry={refetch} />
      </div>
    );
  if (!flow) return null;

  const ep = flow.essence_parameters;
  const isReadOnly = !!flow.read_only;
  const isMulti = flow.format === "urn:x-nmos:format:multi";
  const deleteScope = estimateDeleteScope(segments, Boolean(segNextKey));
  const allocationBackend = findStorageBackend(
    storageBackends.data,
    allocationStorageId,
  );
  const readOnlyScope = isReadOnly
    ? "This will unlock the flow and re-enable metadata edits, segment changes, storage allocation, and deletion."
    : "This will lock the flow and prevent metadata edits, segment changes, storage allocation, and deletion until it is made writable again.";

  return (
    <div className="p-4 sm:p-6 lg:p-8">
      <nav className="mb-6" aria-label="Breadcrumb">
        <ol className="flex items-center gap-2 text-sm text-gray-500">
          <li>
            <Link to="/flows" className="hover:text-gray-700">
              Flows
            </Link>
          </li>
          <li aria-hidden="true">/</li>
          <li className="font-medium text-gray-900">{flow.label || flow.id}</li>
        </ol>
      </nav>

      <div className="mb-8 flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <div className="flex flex-wrap items-center gap-3">
            <h1 className="text-xl font-bold text-gray-900 sm:text-2xl">
              <InlineEditField
                value={flow.label || ""}
                placeholder="Unnamed Flow"
                disabled={isReadOnly}
                onSave={async (v) => {
                  await api.updateFlowLabel(flowId!, v);
                  refetch();
                }}
              />
            </h1>
            {flow.format && (
              <Badge variant="primary">{formatFormat(flow.format)}</Badge>
            )}
            {flow.codec && (
              <Badge variant="info">{formatCodec(flow.codec)}</Badge>
            )}
            {isReadOnly && <Badge variant="warning">Read-only</Badge>}
          </div>
          <div className="mt-1 flex items-center gap-2">
            <code className="text-sm text-gray-400">{flow.id}</code>
            <CopyButton text={flow.id} label="Copy" />
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Link
            to={`/playback?flow=${flow.id}`}
            className="rounded-lg border border-tams-300 px-4 py-2 text-sm font-medium text-tams-700 hover:bg-tams-50"
          >
            Preview Playback
          </Link>
          <button
            onClick={() => setReadOnlyConfirm(isReadOnly ? "unlock" : "lock")}
            disabled={readOnlySaving}
            className="rounded-lg border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
          >
            {readOnlySaving
              ? "Saving..."
              : isReadOnly
                ? "Make Writable"
                : "Mark Read-only"}
          </button>
          <button
            onClick={() => setDeleteConfirm(true)}
            disabled={isReadOnly || flowDeleting}
            className="rounded-lg border border-red-300 px-4 py-2 text-sm font-medium text-red-600 hover:bg-red-50 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {flowDeleting ? "Deleting..." : "Delete"}
          </button>
        </div>
      </div>

      <ConfirmAction
        open={readOnlyConfirm !== null}
        variant="warning"
        title={
          readOnlyConfirm === "lock"
            ? "Mark flow read-only?"
            : "Make flow writable?"
        }
        description={readOnlyScope}
        confirmLabel={
          readOnlyConfirm === "lock" ? "Confirm Read-only" : "Confirm Writable"
        }
        busy={readOnlySaving}
        busyLabel="Saving..."
        onConfirm={handleReadOnlyToggle}
        onCancel={() => setReadOnlyConfirm(null)}
      />

      <ConfirmAction
        open={deleteConfirm}
        variant="danger"
        title="Delete flow?"
        description={
          <>
            <p>{deleteScope}</p>
            <p className="mt-2">
              This action may be processed in the background depending on
              deletion size.{" "}
              <Link to="/deletions" className="font-semibold underline">
                Open the deletions queue
              </Link>{" "}
              to track progress.
            </p>
          </>
        }
        confirmLabel="Confirm Delete"
        busy={flowDeleting}
        busyLabel="Deleting..."
        onConfirm={handleDelete}
        onCancel={() => setDeleteConfirm(false)}
      />

      <StateStrip
        title="Flow State"
        refreshedAt={flow.metadata_updated ?? flow.created ?? null}
        items={[
          {
            label: "mutability",
            value: isReadOnly ? "read-only" : "writable",
            variant: isReadOnly ? "warning" : "success",
          },
          {
            label: "segments",
            value: String(segments.length),
            variant: segments.length > 0 ? "info" : "default",
          },
          {
            label: "format",
            value: flow.format ? formatFormat(flow.format) : "unknown",
          },
          {
            label: "objects",
            value: segments.length > 0 ? "linked" : "none",
            variant: segments.length > 0 ? "success" : "warning",
          },
        ]}
      />

      <SectionHeading
        eyebrow="Summary"
        title="Flow Overview"
        description="Inspect flow metadata, relationships, operational controls, segment timeline, and API truth in a consistent order."
      />

      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        <FlowMetadataPanel
          api={api}
          flow={flow}
          flowId={flowId!}
          isReadOnly={isReadOnly}
          onRefresh={refetch}
        />

        <div className="space-y-6">
          <TraceRail
            title="Relationships"
            items={[
              ...(flow.source_id
                ? [
                    {
                      label: "Source",
                      value: flow.source_id,
                      to: `/sources/${flow.source_id}`,
                    },
                  ]
                : []),
              {
                label: "Flow",
                value: flow.id,
                to: `/flows/${flow.id}`,
                tone: "accent",
              },
              ...(segments[0]?.object_id
                ? [
                    {
                      label: "First Object",
                      value: segments[0].object_id,
                      to: `/objects/${segments[0].object_id}`,
                    },
                  ]
                : []),
              {
                label: "Storage",
                value: "Allocate and inspect object upload targets",
                to: "/service",
              },
            ]}
          />

          {ep &&
            (() => {
              const hasVideoEssence = !!(
                ep.frame_width ||
                ep.frame_rate ||
                ep.interlace_mode ||
                ep.colorspace
              );
              const hasAudioEssence = !!(ep.sample_rate || ep.channels);
              const bitDepthInVideo = hasVideoEssence;
              return (
                <div className="tamoss-panel rounded-2xl p-4 sm:p-6">
                  <SectionHeading title="Essence Parameters" />
                  {hasVideoEssence && (
                    <>
                      <h3 className="text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-lw-ink-400">
                        Video
                      </h3>
                      <dl className="mt-2 grid grid-cols-2 gap-3">
                        {ep.frame_width && (
                          <div>
                            <dt className="text-sm font-medium text-gray-500">
                              Resolution
                            </dt>
                            <dd className="mt-1 text-sm text-gray-900">
                              {formatResolution(
                                ep.frame_width,
                                ep.frame_height,
                              )}
                            </dd>
                          </div>
                        )}
                        {ep.frame_rate && (
                          <div>
                            <dt className="text-sm font-medium text-gray-500">
                              Frame Rate
                            </dt>
                            <dd className="mt-1 text-sm text-gray-900">
                              {formatFrameRate(ep.frame_rate)}
                            </dd>
                          </div>
                        )}
                        {ep.interlace_mode && (
                          <div>
                            <dt className="text-sm font-medium text-gray-500">
                              Interlace
                            </dt>
                            <dd className="mt-1 text-sm text-gray-900">
                              {ep.interlace_mode}
                            </dd>
                          </div>
                        )}
                        {ep.colorspace && (
                          <div>
                            <dt className="text-sm font-medium text-gray-500">
                              Colorspace
                            </dt>
                            <dd className="mt-1 text-sm text-gray-900">
                              {ep.colorspace}
                            </dd>
                          </div>
                        )}
                        {bitDepthInVideo && ep.bit_depth && (
                          <div>
                            <dt className="text-sm font-medium text-gray-500">
                              Bit Depth
                            </dt>
                            <dd className="mt-1 text-sm text-gray-900">
                              {ep.bit_depth}-bit
                            </dd>
                          </div>
                        )}
                      </dl>
                    </>
                  )}
                  {hasAudioEssence && (
                    <>
                      <h3
                        className={`${hasVideoEssence ? "mt-6" : ""} text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-lw-ink-400`}
                      >
                        Audio
                      </h3>
                      <dl className="mt-2 grid grid-cols-2 gap-3">
                        {ep.sample_rate && (
                          <div>
                            <dt className="text-sm font-medium text-gray-500">
                              Sample Rate
                            </dt>
                            <dd className="mt-1 text-sm text-gray-900">
                              {ep.sample_rate} Hz
                            </dd>
                          </div>
                        )}
                        {ep.channels && (
                          <div>
                            <dt className="text-sm font-medium text-gray-500">
                              Channels
                            </dt>
                            <dd className="mt-1 text-sm text-gray-900">
                              {ep.channels}
                            </dd>
                          </div>
                        )}
                        {!bitDepthInVideo && ep.bit_depth && (
                          <div>
                            <dt className="text-sm font-medium text-gray-500">
                              Bit Depth
                            </dt>
                            <dd className="mt-1 text-sm text-gray-900">
                              {ep.bit_depth}-bit
                            </dd>
                          </div>
                        )}
                      </dl>
                    </>
                  )}
                  {!hasVideoEssence && !hasAudioEssence && ep.bit_depth && (
                    <dl className="grid grid-cols-2 gap-3">
                      <div>
                        <dt className="text-sm font-medium text-gray-500">
                          Bit Depth
                        </dt>
                        <dd className="mt-1 text-sm text-gray-900">
                          {ep.bit_depth}-bit
                        </dd>
                      </div>
                    </dl>
                  )}
                </div>
              );
            })()}

          <div className="tamoss-panel rounded-2xl p-4 sm:p-6">
            <h2 className="mb-4 text-lg font-semibold text-gray-900">
              Bitrate
            </h2>
            <dl className="grid grid-cols-2 gap-3">
              <div>
                <dt className="text-sm font-medium text-gray-500">Average</dt>
                <dd className="mt-1 text-sm text-gray-900">
                  {formatBitRate(flow.avg_bit_rate)}
                </dd>
              </div>
              <div>
                <dt className="text-sm font-medium text-gray-500">Maximum</dt>
                <dd className="mt-1 text-sm text-gray-900">
                  {formatBitRate(flow.max_bit_rate)}
                </dd>
              </div>
            </dl>
          </div>
        </div>

        <div className="tamoss-panel rounded-2xl p-4 sm:p-6 lg:col-span-2">
          <h2 className="mb-4 text-lg font-semibold text-gray-900">Tags</h2>
          <EditableTagList
            tags={flow.tags}
            disabled={isReadOnly}
            onAdd={async (key, value) => {
              await api.updateFlowTag(flowId!, key, value);
              refetch();
            }}
            onDelete={async (key) => {
              await api.deleteFlowTag(flowId!, key);
              refetch();
            }}
          />
        </div>

        {isMulti && (
          <div className="tamoss-panel rounded-2xl p-4 sm:p-6 lg:col-span-2">
            <div className="mb-4 flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
              <SectionHeading
                title={`Flow Collection (${childFlows.length})`}
              />
              <div className="flex flex-col gap-2 sm:flex-row sm:items-end">
                <StorageBackendSelector
                  id="multi-segment-storage-filter"
                  label="Segment URL backend"
                  value={segmentStorageId}
                  onChange={setSegmentStorageId}
                  backends={storageBackends.data}
                  includeAllOption
                  allLabel="All storage backends"
                  className="min-w-64"
                />
                <button
                  onClick={() => setShowChildFlows(true)}
                  disabled={isReadOnly}
                  className="rounded-lg bg-tams-600 px-3 py-2 text-xs font-medium text-white shadow-sm hover:bg-tams-700 disabled:opacity-50"
                >
                  Edit Collection
                </button>
              </div>
            </div>

            {collectionQuery.loading && (
              <p className="text-sm text-gray-500">Loading...</p>
            )}

            {collectionQuery.error && (
              <ErrorMessage
                message={collectionQuery.error}
                onRetry={collectionQuery.refetch}
              />
            )}

            {!collectionQuery.loading &&
              !collectionQuery.error &&
              childFlows.length === 0 && (
                <p className="text-sm text-gray-500">
                  No flow collection items.
                </p>
              )}

            {childFlows.length > 0 && (
              <div className="space-y-4">
                {childFlows.map((child) => {
                  const childSegs = childSegmentsMap[child.id] ?? [];
                  const childSegmentError = childSegmentErrors[child.id];
                  const childSelected =
                    selectedChildSegments[child.id] ?? new Set<number>();
                  return (
                    <div key={child.id}>
                      <div className="flex items-center justify-between rounded-lg bg-gray-50 px-3 py-2">
                        <div className="flex items-center gap-2">
                          <Link
                            to={`/flows/${child.id}`}
                            className="text-sm text-tams-600 hover:text-tams-700"
                          >
                            {child.id.substring(0, 16)}...
                          </Link>
                          <span className="text-xs text-gray-500">
                            role: {child.role || "N/A"}
                          </span>
                          {childSegs.length > 0 && (
                            <span className="text-xs text-gray-400">
                              {childSegs.length} segment
                              {childSegs.length !== 1 ? "s" : ""}
                            </span>
                          )}
                        </div>
                        <div className="flex items-center gap-2">
                          {childSelected.size > 0 && (
                            <button
                              onClick={() => setRemoveChildConfirm(child.id)}
                              disabled={removingChild || isReadOnly}
                              className="rounded-md border border-red-300 px-2 py-1 text-xs font-medium text-red-600 hover:bg-red-50 disabled:opacity-50"
                            >
                              Delete {childSelected.size} segment
                              {childSelected.size !== 1 ? "s" : ""}
                            </button>
                          )}
                          <button
                            onClick={() => openAddSegmentsForChild(child.id)}
                            disabled={isReadOnly}
                            className="rounded-md bg-tams-600 px-2 py-1 text-xs font-medium text-white hover:bg-tams-700 disabled:opacity-50"
                          >
                            Add Segments
                          </button>
                          <CopyButton text={child.id} label="Copy ID" />
                        </div>
                      </div>
                      {childSegmentError && (
                        <div className="mt-2">
                          <ErrorMessage
                            title="Segment Load Failed"
                            message={childSegmentError}
                            onRetry={() => {
                              void refreshChildSegments(child.id);
                            }}
                          />
                        </div>
                      )}
                      {!childSegmentError && childSegs.length > 0 && (
                        <SegmentTable
                          segments={childSegs}
                          selectedSegments={childSelected}
                          onToggleSegment={(index) =>
                            toggleChildSegment(child.id, index)
                          }
                          onToggleAll={() =>
                            toggleAllChildSegments(child.id, childSegs.length)
                          }
                          readOnly={isReadOnly}
                          rowKeyPrefix={`child-${child.id}`}
                          compact
                          showDuration
                        />
                      )}
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        )}

        {!isMulti && (
          <div className="tamoss-panel rounded-2xl p-4 sm:p-6 lg:col-span-2">
            <div className="mb-4 flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
              <SectionHeading
                title={`Segments (${segments.length}${segNextKey ? "+" : ""})`}
              />
              <div className="flex flex-col gap-2 sm:flex-row sm:items-end">
                <StorageBackendSelector
                  id="segment-storage-filter"
                  label="Segment URL backend"
                  value={segmentStorageId}
                  onChange={setSegmentStorageId}
                  backends={storageBackends.data}
                  includeAllOption
                  allLabel="All storage backends"
                  className="min-w-64"
                />
                {selectedSegments.size > 0 && (
                  <button
                    onClick={() => setRemoveConfirm(true)}
                    disabled={removing || isReadOnly}
                    className="rounded-lg border border-red-300 px-3 py-2 text-xs font-medium text-red-600 hover:bg-red-50 disabled:opacity-50"
                  >
                    Delete {selectedSegments.size} segment
                    {selectedSegments.size !== 1 ? "s" : ""}
                  </button>
                )}
                <button
                  onClick={() => setShowAddSegments(true)}
                  disabled={isReadOnly}
                  className="rounded-lg bg-tams-600 px-3 py-2 text-xs font-medium text-white shadow-sm hover:bg-tams-700 disabled:opacity-50"
                >
                  Add Segments
                </button>
              </div>
            </div>

            {segQuery.loading && (
              <LoadingSpinner message="Loading segments..." />
            )}
            {segQuery.error && (
              <ErrorMessage
                message={segQuery.error}
                onRetry={segQuery.refetch}
              />
            )}

            {!segQuery.loading && segments.length === 0 && (
              <p className="text-sm text-gray-500">No segments available.</p>
            )}

            {segments.length > 0 && (
              <>
                <SegmentTable
                  segments={segments}
                  selectedSegments={selectedSegments}
                  onToggleSegment={toggleSegment}
                  onToggleAll={toggleAllSegments}
                  readOnly={isReadOnly}
                  rowKeyPrefix="flow"
                />

                {segNextKey && (
                  <div className="mt-4 flex flex-col items-center gap-2">
                    <div
                      ref={segmentsSentinelRef}
                      aria-hidden="true"
                      className="h-px w-full"
                    />
                    <button
                      onClick={loadMoreSegments}
                      disabled={segLoadingMore}
                      className="rounded-lg bg-white px-4 py-2 text-sm font-medium text-gray-700 border border-gray-300 hover:bg-gray-50 disabled:opacity-50"
                    >
                      {segLoadingMore ? "Loading..." : "Load more segments"}
                    </button>
                  </div>
                )}
              </>
            )}
          </div>
        )}
      </div>

      <section className="tamoss-panel mt-6 rounded-2xl p-4 sm:p-6">
        <div className="mb-4 flex items-center justify-between gap-4">
          <SectionHeading
            title="Operations"
            description="Allocate upload targets and inspect flow-level operational controls."
          />
        </div>

        <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
          <div>
            <label className="block text-sm font-medium text-gray-700">
              Object count
            </label>
            <input
              type="number"
              min={1}
              value={storageCount}
              onChange={(event) =>
                setStorageCount(Math.max(1, Number(event.target.value) || 1))
              }
              disabled={isReadOnly || storageAllocating}
              className="mt-1 w-32 rounded-lg border border-gray-300 px-3 py-2 text-sm focus:border-tams-500 focus:outline-none focus:ring-1 focus:ring-tams-500 disabled:bg-gray-50"
            />
          </div>
          <StorageBackendSelector
            id="allocation-storage-backend"
            label="Allocation backend"
            value={allocationStorageId}
            onChange={setAllocationStorageId}
            backends={storageBackends.data}
            disabled={isReadOnly || storageAllocating}
            includeAllOption
            allLabel="Default backend"
            className="min-w-64"
          />
          <button
            onClick={handleAllocateStorage}
            disabled={isReadOnly || storageAllocating}
            className="rounded-lg bg-tams-600 px-4 py-2 text-sm font-medium text-white hover:bg-tams-700 disabled:opacity-50"
          >
            {storageAllocating ? "Allocating..." : "Allocate storage"}
          </button>
        </div>

        {storageError && (
          <div className="mt-4 rounded-lg border border-rose-200 bg-rose-50 p-3 text-sm text-rose-700">
            {storageError}
          </div>
        )}

        {allocatedObjects.length > 0 && (
          <div className="mt-4 space-y-3">
            <div className="rounded-lg border border-blue-200 bg-blue-50 p-4 text-sm text-blue-800">
              <p className="font-medium">Storage allocation prepared</p>
              <p className="mt-1">
                These object IDs and PUT URLs reserve upload targets only. Media
                is not registered in a flow until segments are created against
                these object IDs. Allocation used{" "}
                {storageBackendDisplay(allocationBackend)}.
              </p>
            </div>
            {allocatedObjects.map((entry) => (
              <div
                key={entry.object_id}
                className="rounded-lg border border-gray-200 p-4"
              >
                <div>
                  <p className="text-sm font-medium text-gray-900">Object ID</p>
                  <div className="mt-1 flex items-center gap-2">
                    <Link
                      to={`/objects/${entry.object_id}`}
                      className="font-mono text-xs text-tams-600 hover:text-tams-700"
                    >
                      {entry.object_id}
                    </Link>
                    <CopyButton text={entry.object_id} label="Copy ID" />
                  </div>
                </div>
                <div className="mt-3">
                  <p className="text-sm font-medium text-gray-900">PUT URL</p>
                  <div className="mt-1 flex items-start gap-2">
                    <code className="min-w-0 flex-1 break-all text-xs text-gray-500">
                      {entry.put_url.url}
                    </code>
                    <CopyButton text={entry.put_url.url} label="Copy URL" />
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </section>

      <AddSegmentsDialog
        open={showAddSegments}
        onClose={() => setShowAddSegments(false)}
        flow={flow}
        onAdded={() => {
          setShowAddSegments(false);
          segQuery.refetch();
        }}
      />

      {addSegTargetFlow && (
        <AddSegmentsDialog
          open={!!addSegTargetFlow}
          onClose={() => setAddSegTargetFlow(null)}
          flow={addSegTargetFlow}
          onAdded={() => {
            const targetId = addSegTargetFlow.id;
            setAddSegTargetFlow(null);
            refreshChildSegments(targetId);
            refetch();
          }}
        />
      )}

      {isMulti && (
        <ManageChildFlowsDialog
          open={showChildFlows}
          onClose={() => setShowChildFlows(false)}
          flow={flow}
          onSaved={() => {
            setShowChildFlows(false);
            collectionQuery.refetch();
            refetch();
          }}
        />
      )}

      <ConfirmAction
        open={removeConfirm}
        variant="danger"
        title="Delete selected segments?"
        description={`This deletes ${selectedSegments.size} segment registration${selectedSegments.size !== 1 ? "s" : ""} from the flow timeline. Media objects may still remain if referenced by other flows.`}
        confirmLabel="Confirm"
        busy={removing}
        busyLabel="Deleting..."
        onConfirm={handleBulkDeleteSegments}
        onCancel={() => setRemoveConfirm(false)}
      />

      <ConfirmAction
        open={removeChildConfirm !== null}
        variant="danger"
        title="Delete selected child-flow segments?"
        description={(() => {
          const size = removeChildConfirm
            ? (selectedChildSegments[removeChildConfirm]?.size ?? 0)
            : 0;
          return `This deletes ${size} segment registration${size !== 1 ? "s" : ""} from the child flow timeline. Media objects may still remain if referenced elsewhere.`;
        })()}
        confirmLabel="Confirm"
        busy={removingChild}
        busyLabel="Deleting..."
        onConfirm={() => {
          if (removeChildConfirm) {
            handleBulkDeleteChildSegments(removeChildConfirm);
          }
        }}
        onCancel={() => setRemoveChildConfirm(null)}
      />

      <RawPayload
        className="mt-6"
        title="Raw Flow Payload"
        description="Inspect the exact flow response returned by the API."
        json={JSON.stringify(flow, null, 2)}
      />

      <div className="mt-6">
        <ApiReferencePanel
          title="API Reference"
          method="GET"
          path={`/flows/${flow.id}`}
        />
      </div>
    </div>
  );
}
