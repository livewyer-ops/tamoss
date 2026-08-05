import { useEffect, useState, useCallback, useMemo } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";
import { useApi } from "@/contexts/ApiContext";
import { useApiQuery } from "@/hooks/useApiQuery";
import { useInfiniteScroll } from "@/hooks/useInfiniteScroll";
import { usePageTitle } from "@/hooks/usePageTitle";
import LoadingSpinner from "@/components/LoadingSpinner";
import ErrorMessage from "@/components/ErrorMessage";
import EmptyState from "@/components/EmptyState";
import Badge from "@/components/Badge";
import CopyButton from "@/components/CopyButton";
import CopyViewLinkButton from "@/components/CopyViewLinkButton";
import TagList from "@/components/TagList";
import CreateFlowDialog from "@/components/CreateFlowDialog";
import {
  formatFormat,
  formatCodec,
  formatDate,
  formatResolution,
  formatFrameRate,
  formatBitRate,
  truncateId,
} from "@/utils/format";
import type { Flow } from "@/types/tams";

const FILTER_DEBOUNCE_MS = 300;
const FLOW_PAGE_SIZE = "50";

function useDebouncedValue<T>(value: T, delay: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(value), delay);
    return () => window.clearTimeout(timer);
  }, [value, delay]);
  return debounced;
}

export default function FlowsPage() {
  usePageTitle("Flows");
  const api = useApi();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const [filter, setFilter] = useState(searchParams.get("q") ?? "");
  const [formatFilter, setFormatFilter] = useState(
    searchParams.get("format") ?? "",
  );
  const [codecFilter, setCodecFilter] = useState(
    searchParams.get("codec") ?? "",
  );
  const [labelFilter, setLabelFilter] = useState(
    searchParams.get("label") ?? "",
  );
  const [timerangeFilter, setTimerangeFilter] = useState(
    searchParams.get("timerange") ?? "",
  );
  const [allFlows, setAllFlows] = useState<Flow[]>([]);
  const [nextKey, setNextKey] = useState<string | undefined>();
  const [loadingMore, setLoadingMore] = useState(false);
  const [showCreateDialog, setShowCreateDialog] = useState(false);
  const [compactMode, setCompactMode] = useState(false);

  const debouncedCodec = useDebouncedValue(
    codecFilter.trim(),
    FILTER_DEBOUNCE_MS,
  );
  const debouncedLabel = useDebouncedValue(
    labelFilter.trim(),
    FILTER_DEBOUNCE_MS,
  );
  const debouncedTimerange = useDebouncedValue(
    timerangeFilter.trim(),
    FILTER_DEBOUNCE_MS,
  );
  const debouncedFilter = useDebouncedValue(filter, FILTER_DEBOUNCE_MS);

  const activeFilterCount = useMemo(
    () =>
      [debouncedCodec, debouncedLabel, debouncedTimerange].filter(Boolean)
        .length,
    [debouncedCodec, debouncedLabel, debouncedTimerange],
  );

  useEffect(() => {
    const saved = window.localStorage.getItem("tamoss.flows.compact");
    setCompactMode(saved === "1");
  }, []);

  useEffect(() => {
    window.localStorage.setItem(
      "tamoss.flows.compact",
      compactMode ? "1" : "0",
    );
  }, [compactMode]);

  useEffect(() => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev);
        const sync = (key: string, value: string) => {
          if (value) next.set(key, value);
          else next.delete(key);
        };
        sync("q", debouncedFilter);
        sync("format", formatFilter);
        sync("codec", debouncedCodec);
        sync("label", debouncedLabel);
        sync("timerange", debouncedTimerange);
        return next;
      },
      { replace: true },
    );
  }, [
    debouncedFilter,
    formatFilter,
    debouncedCodec,
    debouncedLabel,
    debouncedTimerange,
    setSearchParams,
  ]);

  const { loading, error, refetch } = useApiQuery(async () => {
    const params: Record<string, string> = { limit: FLOW_PAGE_SIZE };
    if (formatFilter) params.format = formatFilter;
    if (debouncedCodec) params.codec = debouncedCodec;
    if (debouncedLabel) params.label = debouncedLabel;
    if (debouncedTimerange) params.timerange = debouncedTimerange;
    const result = await api.getFlows(params);
    setAllFlows(result.data);
    setNextKey(result.nextKey);
    return result;
  }, [api, formatFilter, debouncedCodec, debouncedLabel, debouncedTimerange]);

  const loadMore = useCallback(async () => {
    if (!nextKey || loadingMore) return;
    setLoadingMore(true);
    try {
      const params: Record<string, string> = {
        limit: FLOW_PAGE_SIZE,
        page: nextKey,
      };
      if (formatFilter) params.format = formatFilter;
      if (debouncedCodec) params.codec = debouncedCodec;
      if (debouncedLabel) params.label = debouncedLabel;
      if (debouncedTimerange) params.timerange = debouncedTimerange;
      const result = await api.getFlows(params);
      setAllFlows((prev) => [...prev, ...result.data]);
      setNextKey(result.nextKey);
    } finally {
      setLoadingMore(false);
    }
  }, [
    api,
    nextKey,
    loadingMore,
    formatFilter,
    debouncedCodec,
    debouncedLabel,
    debouncedTimerange,
  ]);

  const resetServerFilters = () => {
    setCodecFilter("");
    setLabelFilter("");
    setTimerangeFilter("");
  };

  const sentinelRef = useInfiniteScroll(!!nextKey && !loadingMore, loadMore);

  const filtered = useMemo(() => {
    if (!filter) return allFlows;
    const needle = filter.toLowerCase();
    return allFlows.filter(
      (f) =>
        f.id.toLowerCase().includes(needle) ||
        f.label?.toLowerCase().includes(needle) ||
        f.codec?.toLowerCase().includes(needle),
    );
  }, [allFlows, filter]);

  return (
    <div className="p-4 sm:p-6 lg:p-8">
      <div className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-xl font-bold text-lw-ink-900 sm:text-2xl">
            Flows
          </h1>
          <p className="mt-2 text-sm leading-6 text-lw-ink-500">
            Encoded media flows in the TAMOSS service
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <label htmlFor="flow-filter" className="sr-only">
            Search flows
          </label>
          <input
            id="flow-filter"
            type="search"
            placeholder="Search by id, label, codec..."
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            className="tamoss-toolbar-control w-56 px-3 py-2.5 text-sm focus:border-tams-400 focus:outline-none focus:ring-2 focus:ring-tams-200"
          />
          <label htmlFor="format-filter" className="sr-only">
            Filter by format
          </label>
          <select
            id="format-filter"
            value={formatFilter}
            onChange={(e) => setFormatFilter(e.target.value)}
            className="tamoss-toolbar-control px-3 py-2.5 text-sm focus:border-tams-400 focus:outline-none focus:ring-2 focus:ring-tams-200"
          >
            <option value="">All formats</option>
            <option value="urn:x-nmos:format:video">Video</option>
            <option value="urn:x-nmos:format:audio">Audio</option>
            <option value="urn:x-nmos:format:multi">Multi</option>
            <option value="urn:x-nmos:format:data">Data</option>
          </select>

          <details className="relative">
            <summary className="tamoss-button-secondary inline-flex cursor-pointer select-none items-center gap-2 px-3 py-2.5 text-sm font-medium [&::-webkit-details-marker]:hidden">
              <span>Filters</span>
              {activeFilterCount > 0 && (
                <span className="inline-flex h-5 min-w-[1.25rem] items-center justify-center rounded-full bg-tams-700 px-1.5 text-[0.65rem] font-semibold text-white">
                  {activeFilterCount}
                </span>
              )}
            </summary>
            <div className="tamoss-panel absolute right-0 z-20 mt-2 w-80 rounded-2xl p-4">
              <div className="space-y-3">
                <div>
                  <label
                    htmlFor="codec-filter"
                    className="block text-xs font-medium text-lw-ink-700"
                  >
                    Exact codec
                  </label>
                  <input
                    id="codec-filter"
                    type="text"
                    value={codecFilter}
                    onChange={(e) => setCodecFilter(e.target.value)}
                    placeholder="e.g. video/h264"
                    className="tamoss-toolbar-control mt-1 w-full px-3 py-2 text-sm focus:border-tams-400 focus:outline-none focus:ring-2 focus:ring-tams-200"
                  />
                </div>
                <div>
                  <label
                    htmlFor="label-filter"
                    className="block text-xs font-medium text-lw-ink-700"
                  >
                    Exact label
                  </label>
                  <input
                    id="label-filter"
                    type="text"
                    value={labelFilter}
                    onChange={(e) => setLabelFilter(e.target.value)}
                    className="tamoss-toolbar-control mt-1 w-full px-3 py-2 text-sm focus:border-tams-400 focus:outline-none focus:ring-2 focus:ring-tams-200"
                  />
                </div>
                <div>
                  <label
                    htmlFor="timerange-filter"
                    className="block text-xs font-medium text-lw-ink-700"
                  >
                    Timerange
                  </label>
                  <input
                    id="timerange-filter"
                    type="text"
                    value={timerangeFilter}
                    onChange={(e) => setTimerangeFilter(e.target.value)}
                    aria-describedby="timerange-filter-help"
                    placeholder="[10:0_20:0]"
                    className="tamoss-toolbar-control mt-1 w-full px-3 py-2 font-mono text-sm focus:border-tams-400 focus:outline-none focus:ring-2 focus:ring-tams-200"
                  />
                  <p
                    id="timerange-filter-help"
                    className="mt-1 text-[0.65rem] leading-4 text-lw-ink-500"
                  >
                    Inclusive bracket form, e.g.{" "}
                    <code className="font-mono">[10:0_20:0]</code> or{" "}
                    <code className="font-mono">_20:0)</code>.
                  </p>
                </div>
                {activeFilterCount > 0 && (
                  <button
                    type="button"
                    onClick={resetServerFilters}
                    className="tamoss-button-secondary w-full px-3 py-2 text-sm font-medium"
                  >
                    Reset filters
                  </button>
                )}
              </div>
            </div>
          </details>

          <details className="relative">
            <summary
              className="tamoss-button-secondary inline-flex cursor-pointer select-none items-center px-2 py-2.5 text-sm font-medium [&::-webkit-details-marker]:hidden"
              aria-label="View options"
            >
              <svg
                className="h-5 w-5"
                fill="none"
                viewBox="0 0 24 24"
                strokeWidth={1.6}
                stroke="currentColor"
                aria-hidden="true"
              >
                <path
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  d="M12 6.75a.75.75 0 110-1.5.75.75 0 010 1.5zm0 6a.75.75 0 110-1.5.75.75 0 010 1.5zm0 6a.75.75 0 110-1.5.75.75 0 010 1.5z"
                />
              </svg>
            </summary>
            <div className="tamoss-panel absolute right-0 z-20 mt-2 w-56 rounded-2xl p-2 text-sm">
              <button
                type="button"
                onClick={refetch}
                className="block w-full rounded-lg px-3 py-2 text-left text-sm font-medium text-lw-ink-800 hover:bg-lw-ink-50"
              >
                Refresh
              </button>
              <div className="px-3 py-1">
                <CopyViewLinkButton />
              </div>
              <button
                type="button"
                onClick={() => setCompactMode((previous) => !previous)}
                className="block w-full rounded-lg px-3 py-2 text-left text-sm font-medium text-lw-ink-800 hover:bg-lw-ink-50"
              >
                {compactMode ? "Comfortable rows" : "Compact rows"}
              </button>
            </div>
          </details>

          <button
            onClick={() => setShowCreateDialog(true)}
            className="tamoss-button-primary px-4 py-2.5 text-sm font-semibold"
          >
            Create Flow
          </button>
        </div>
      </div>

      {loading && <LoadingSpinner message="Loading flows..." />}
      {error && <ErrorMessage message={error} onRetry={refetch} />}

      {!loading && !error && filtered.length === 0 && (
        <EmptyState
          title="No flows found"
          description={
            filter || formatFilter
              ? "Try adjusting your filters"
              : "No flows are registered yet"
          }
        />
      )}

      {!loading && filtered.length > 0 && (
        <div className="tamoss-panel overflow-hidden rounded-2xl">
          <div className="max-h-[70vh] overflow-auto">
            <table className="min-w-full divide-y divide-lw-ink-100">
              <thead className="bg-lw-ink-50/80">
                <tr>
                  <th className="sticky top-0 z-10 bg-lw-ink-50/95 px-4 py-3 text-left text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-lw-ink-500">
                    Flow
                  </th>
                  <th className="sticky top-0 z-10 bg-lw-ink-50/95 px-4 py-3 text-left text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-lw-ink-500">
                    Format
                  </th>
                  <th className="sticky top-0 z-10 bg-lw-ink-50/95 px-4 py-3 text-left text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-lw-ink-500">
                    Technical
                  </th>
                  <th className="sticky top-0 z-10 bg-lw-ink-50/95 px-4 py-3 text-left text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-lw-ink-500">
                    State
                  </th>
                  <th className="sticky top-0 z-10 bg-lw-ink-50/95 px-4 py-3 text-left text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-lw-ink-500">
                    Tags
                  </th>
                  <th className="sticky top-0 z-10 bg-lw-ink-50/95 px-4 py-3 text-right text-[0.68rem] font-semibold uppercase tracking-[0.18em] text-lw-ink-500">
                    Actions
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-lw-ink-100/80">
                {filtered.map((flow) => (
                  <tr
                    key={flow.id}
                    role="link"
                    tabIndex={0}
                    onClick={() => navigate(`/flows/${flow.id}`)}
                    onKeyDown={(event) => {
                      if (event.key === "Enter" || event.key === " ") {
                        event.preventDefault();
                        navigate(`/flows/${flow.id}`);
                      }
                    }}
                    className="cursor-pointer hover:bg-lw-ink-50 focus:bg-lw-ink-50 focus:outline-none focus-visible:bg-lw-ink-50"
                  >
                    <td
                      className={
                        compactMode
                          ? "px-4 py-2 align-top"
                          : "px-4 py-3 align-top"
                      }
                    >
                      <div className="min-w-0">
                        <p className="truncate text-sm font-semibold text-lw-ink-900">
                          {flow.label || truncateId(flow.id)}
                        </p>
                        <div className="mt-1 flex items-center gap-2">
                          <code className="text-xs text-lw-ink-400">
                            {truncateId(flow.id, 12)}
                          </code>
                          <CopyButton text={flow.id} label="Copy ID" />
                        </div>
                      </div>
                    </td>
                    <td
                      className={
                        compactMode
                          ? "px-4 py-2 align-top"
                          : "px-4 py-3 align-top"
                      }
                    >
                      <div className="flex flex-wrap gap-2">
                        {flow.format && (
                          <Badge variant="primary">
                            {formatFormat(flow.format)}
                          </Badge>
                        )}
                        {flow.codec && (
                          <Badge variant="info">
                            {formatCodec(flow.codec)}
                          </Badge>
                        )}
                      </div>
                    </td>
                    <td
                      className={
                        compactMode
                          ? "px-4 py-2 align-top text-xs text-gray-500"
                          : "px-4 py-3 align-top text-xs text-gray-500"
                      }
                    >
                      <div className="space-y-1">
                        {flow.essence_parameters?.frame_width && (
                          <p>
                            {formatResolution(
                              flow.essence_parameters.frame_width,
                              flow.essence_parameters.frame_height,
                            )}
                          </p>
                        )}
                        {flow.essence_parameters?.frame_rate && (
                          <p>
                            {formatFrameRate(
                              flow.essence_parameters.frame_rate,
                            )}
                          </p>
                        )}
                        {flow.essence_parameters?.sample_rate && (
                          <p>{flow.essence_parameters.sample_rate} Hz</p>
                        )}
                        {flow.essence_parameters?.channels && (
                          <p>{flow.essence_parameters.channels}ch</p>
                        )}
                        {flow.avg_bit_rate && (
                          <p>{formatBitRate(flow.avg_bit_rate)}</p>
                        )}
                      </div>
                    </td>
                    <td
                      className={
                        compactMode
                          ? "px-4 py-2 align-top"
                          : "px-4 py-3 align-top"
                      }
                    >
                      <div className="space-y-1">
                        {flow.read_only && (
                          <Badge variant="warning">Read-only</Badge>
                        )}
                        {flow.created && (
                          <p className="text-xs text-gray-400">
                            Created {formatDate(flow.created)}
                          </p>
                        )}
                        {flow.timerange && flow.timerange !== "_" && (
                          <p className="font-mono text-xs text-gray-500">
                            {flow.timerange}
                          </p>
                        )}
                      </div>
                    </td>
                    <td
                      className={
                        compactMode
                          ? "px-4 py-2 align-top"
                          : "px-4 py-3 align-top"
                      }
                    >
                      <TagList tags={flow.tags} />
                    </td>
                    <td
                      className={
                        compactMode
                          ? "px-4 py-2 align-top text-right"
                          : "px-4 py-3 align-top text-right"
                      }
                    >
                      <Link
                        to={`/playback?flow=${flow.id}`}
                        onClick={(event) => event.stopPropagation()}
                        className="rounded-md border border-tams-300 px-2 py-1 text-xs font-medium text-tams-700 hover:bg-tams-50"
                      >
                        Preview
                      </Link>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {nextKey && (
            <div className="flex flex-col items-center gap-2 border-t border-gray-200 p-4">
              <div
                ref={sentinelRef}
                aria-hidden="true"
                className="h-px w-full"
              />
              <button
                onClick={loadMore}
                disabled={loadingMore}
                className="rounded-lg bg-white px-4 py-2 text-sm font-medium text-gray-700 border border-gray-300 hover:bg-gray-50 disabled:opacity-50"
              >
                {loadingMore ? "Loading..." : "Load more"}
              </button>
            </div>
          )}
        </div>
      )}

      <CreateFlowDialog
        open={showCreateDialog}
        onClose={() => setShowCreateDialog(false)}
        flows={allFlows}
        onCreated={(newFlowId) => {
          setShowCreateDialog(false);
          navigate(`/flows/${newFlowId}`);
        }}
      />
    </div>
  );
}
