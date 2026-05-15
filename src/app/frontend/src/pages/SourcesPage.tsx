import { useEffect, useState, useCallback, useMemo } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
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
import { formatFormat, formatDate, truncateId } from "@/utils/format";
import type { Source } from "@/types/tams";

const SOURCE_PAGE_SIZE = "50";

export default function SourcesPage() {
  usePageTitle("Sources");
  const api = useApi();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const [filter, setFilter] = useState(searchParams.get("q") ?? "");
  const [formatFilter, setFormatFilter] = useState(
    searchParams.get("format") ?? "",
  );
  const [allSources, setAllSources] = useState<Source[]>([]);
  const [nextKey, setNextKey] = useState<string | undefined>();
  const [loadingMore, setLoadingMore] = useState(false);
  const [compactMode, setCompactMode] = useState(false);

  useEffect(() => {
    const saved = window.localStorage.getItem("tamoss.sources.compact");
    setCompactMode(saved === "1");
  }, []);

  useEffect(() => {
    window.localStorage.setItem(
      "tamoss.sources.compact",
      compactMode ? "1" : "0",
    );
  }, [compactMode]);

  const { loading, error, refetch } = useApiQuery(async () => {
    const params: Record<string, string> = { limit: SOURCE_PAGE_SIZE };
    if (formatFilter) params.format = formatFilter;
    const result = await api.getSources(params);
    setAllSources(result.data);
    setNextKey(result.nextKey);
    return result;
  }, [api, formatFilter]);

  const loadMore = useCallback(async () => {
    if (!nextKey || loadingMore) return;
    setLoadingMore(true);
    try {
      const params: Record<string, string> = {
        limit: SOURCE_PAGE_SIZE,
        page: nextKey,
      };
      if (formatFilter) params.format = formatFilter;
      const result = await api.getSources(params);
      setAllSources((prev) => [...prev, ...result.data]);
      setNextKey(result.nextKey);
    } finally {
      setLoadingMore(false);
    }
  }, [api, nextKey, loadingMore, formatFilter]);

  const sentinelRef = useInfiniteScroll(!!nextKey && !loadingMore, loadMore);

  const filtered = useMemo(() => {
    if (!filter) return allSources;
    const needle = filter.toLowerCase();
    return allSources.filter(
      (s) =>
        s.id.toLowerCase().includes(needle) ||
        s.label?.toLowerCase().includes(needle) ||
        s.format?.toLowerCase().includes(needle),
    );
  }, [allSources, filter]);

  return (
    <div className="p-4 sm:p-6 lg:p-8">
      <div className="mb-6 flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-xl font-bold text-gray-900 sm:text-2xl">
            Sources
          </h1>
          <p className="mt-1 text-sm text-gray-500">
            Media sources registered in the TAMOSS service
          </p>
        </div>
        <div className="flex items-center gap-3">
          <label htmlFor="source-filter" className="sr-only">
            Filter sources
          </label>
          <input
            id="source-filter"
            type="search"
            placeholder="Filter sources..."
            value={filter}
            onChange={(e) => {
              const value = e.target.value;
              setFilter(value);
              const next = new URLSearchParams(searchParams);
              if (value) next.set("q", value);
              else next.delete("q");
              setSearchParams(next, { replace: true });
            }}
            className="rounded-lg border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-tams-500 focus:outline-none focus:ring-1 focus:ring-tams-500"
          />
          <label htmlFor="source-format-filter" className="sr-only">
            Filter sources by format
          </label>
          <select
            id="source-format-filter"
            value={formatFilter}
            onChange={(e) => {
              const value = e.target.value;
              setFormatFilter(value);
              const next = new URLSearchParams(searchParams);
              if (value) next.set("format", value);
              else next.delete("format");
              setSearchParams(next, { replace: true });
            }}
            className="rounded-lg border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-tams-500 focus:outline-none focus:ring-1 focus:ring-tams-500"
          >
            <option value="">All formats</option>
            <option value="urn:x-nmos:format:video">Video</option>
            <option value="urn:x-nmos:format:audio">Audio</option>
            <option value="urn:x-nmos:format:multi">Multi</option>
            <option value="urn:x-nmos:format:data">Data</option>
          </select>
          <button
            onClick={refetch}
            className="rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm font-medium text-gray-700 shadow-sm hover:bg-gray-50"
            aria-label="Refresh sources"
          >
            Refresh
          </button>
          <CopyViewLinkButton />
          <button
            onClick={() => setCompactMode((previous) => !previous)}
            className="rounded-lg border border-gray-300 bg-white px-3 py-2 text-sm font-medium text-gray-700 shadow-sm hover:bg-gray-50"
          >
            {compactMode ? "Comfortable rows" : "Compact rows"}
          </button>
        </div>
      </div>

      {loading && <LoadingSpinner message="Loading sources..." />}
      {error && <ErrorMessage message={error} onRetry={refetch} />}

      {!loading && !error && filtered.length === 0 && (
        <EmptyState
          title="No sources found"
          description={
            filter
              ? "Try adjusting your filter"
              : "No sources are registered yet"
          }
        />
      )}

      {!loading && filtered.length > 0 && (
        <div className="overflow-hidden tamoss-panel rounded-2xl">
          <div className="max-h-[70vh] overflow-auto">
            <table className="min-w-full divide-y divide-gray-200">
              <thead className="bg-gray-50">
                <tr>
                  <th className="sticky top-0 z-10 bg-gray-50 px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">
                    Source
                  </th>
                  <th className="sticky top-0 z-10 bg-gray-50 px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">
                    Format
                  </th>
                  <th className="sticky top-0 z-10 bg-gray-50 px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">
                    Description
                  </th>
                  <th className="sticky top-0 z-10 bg-gray-50 px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">
                    Tags
                  </th>
                  <th className="sticky top-0 z-10 bg-gray-50 px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">
                    State
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100">
                {filtered.map((source) => (
                  <tr
                    key={source.id}
                    role="link"
                    tabIndex={0}
                    onClick={() => navigate(`/sources/${source.id}`)}
                    onKeyDown={(event) => {
                      if (event.key === "Enter" || event.key === " ") {
                        event.preventDefault();
                        navigate(`/sources/${source.id}`);
                      }
                    }}
                    className="cursor-pointer hover:bg-gray-50 focus:bg-gray-50 focus:outline-none focus-visible:bg-gray-50"
                  >
                    <td
                      className={
                        compactMode
                          ? "px-4 py-2 align-top"
                          : "px-4 py-3 align-top"
                      }
                    >
                      <div className="min-w-0">
                        <p className="truncate text-sm font-semibold text-gray-900">
                          {source.label || truncateId(source.id)}
                        </p>
                        <div className="mt-1 flex items-center gap-2">
                          <code className="text-xs text-gray-400">
                            {truncateId(source.id, 12)}
                          </code>
                          <CopyButton text={source.id} label="Copy ID" />
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
                      {source.format && (
                        <Badge variant="primary">
                          {formatFormat(source.format)}
                        </Badge>
                      )}
                    </td>
                    <td
                      className={
                        compactMode
                          ? "px-4 py-2 align-top text-sm text-gray-500"
                          : "px-4 py-3 align-top text-sm text-gray-500"
                      }
                    >
                      <p className="line-clamp-2">
                        {source.description || "N/A"}
                      </p>
                    </td>
                    <td
                      className={
                        compactMode
                          ? "px-4 py-2 align-top"
                          : "px-4 py-3 align-top"
                      }
                    >
                      <TagList tags={source.tags} />
                    </td>
                    <td
                      className={
                        compactMode
                          ? "px-4 py-2 align-top text-xs text-gray-400"
                          : "px-4 py-3 align-top text-xs text-gray-400"
                      }
                    >
                      {source.created && (
                        <p>Created {formatDate(source.created)}</p>
                      )}
                      {source.updated && (
                        <p>Updated {formatDate(source.updated)}</p>
                      )}
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
    </div>
  );
}
