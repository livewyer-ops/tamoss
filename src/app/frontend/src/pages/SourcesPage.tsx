import { ArrowRight, FilterX, ListFilter, Search } from "lucide-react";
import { type FormEvent, useCallback, useEffect, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";
import { CatalogPager } from "@/components/CatalogToolbar";
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
import { useCursorPage } from "@/hooks/useCursorPage";
import { usePageTitle } from "@/hooks/usePageTitle";
import { formatDate, formatFormat } from "@/utils/format";

const DEFAULT_LIMIT = "50";

export default function SourcesPage() {
  usePageTitle("Sources");
  const api = useApi();
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const [lookup, setLookup] = useState("");
  const label = params.get("label") ?? "";
  const format = params.get("format") ?? "";
  const limit = params.get("limit") ?? DEFAULT_LIMIT;
  const cursor = params.get("cursor") ?? undefined;
  const [draftLabel, setDraftLabel] = useState(label);
  const [draftFormat, setDraftFormat] = useState(format);
  const [draftLimit, setDraftLimit] = useState(limit);

  useEffect(() => {
    setDraftLabel(label);
    setDraftFormat(format);
    setDraftLimit(limit);
  }, [format, label, limit]);

  const setCursor = useCallback(
    (value?: string) => {
      setParams((current) => {
        const next = new URLSearchParams(current);
        value ? next.set("cursor", value) : next.delete("cursor");
        return next;
      });
    },
    [setParams],
  );

  const load = useCallback(
    (page: string | undefined, signal: AbortSignal) =>
      api.getSources(
        {
          limit,
          page,
          label: label || undefined,
          format: format || undefined,
        },
        { signal },
      ),
    [api, format, label, limit],
  );
  const catalog = useCursorPage({ cursor, load, onCursorChange: setCursor });

  function applyFilters(event: FormEvent) {
    event.preventDefault();
    setParams((current) => {
      const next = new URLSearchParams(current);
      draftLabel ? next.set("label", draftLabel) : next.delete("label");
      draftFormat ? next.set("format", draftFormat) : next.delete("format");
      next.set("limit", draftLimit);
      next.delete("cursor");
      return next;
    });
    catalog.resetHistory();
  }

  function clearFilters() {
    setDraftLabel("");
    setDraftFormat("");
    setParams(new URLSearchParams({ limit: draftLimit }));
    catalog.resetHistory();
  }

  function inspect(event: FormEvent) {
    event.preventDefault();
    if (lookup.trim())
      navigate(`/sources/${encodeURIComponent(lookup.trim())}`);
  }

  return (
    <Page>
      <PageHeader
        title="Sources"
        actions={
          <form onSubmit={inspect} className={surfaceStyles.toolbar}>
            <label className="srOnly" htmlFor="source-lookup">
              Source ID
            </label>
            <input
              id="source-lookup"
              className={`${surfaceStyles.input} ${surfaceStyles.mono}`}
              placeholder="Exact source ID"
              value={lookup}
              onChange={(event) => setLookup(event.target.value)}
            />
            <Button type="submit" disabled={!lookup.trim()}>
              <Search size={14} aria-hidden="true" /> Inspect
            </Button>
          </form>
        }
      />
      <Panel
        title="Source catalogue"
        actions={
          <form className={surfaceStyles.toolbar} onSubmit={applyFilters}>
            <label className="srOnly" htmlFor="source-label">
              Exact label
            </label>
            <input
              id="source-label"
              className={surfaceStyles.input}
              placeholder="Exact label"
              value={draftLabel}
              onChange={(event) => setDraftLabel(event.target.value)}
            />
            <label className="srOnly" htmlFor="source-format">
              Format
            </label>
            <select
              id="source-format"
              className={surfaceStyles.select}
              value={draftFormat}
              onChange={(event) => setDraftFormat(event.target.value)}
            >
              <option value="">All formats</option>
              <option value="urn:x-nmos:format:video">Video</option>
              <option value="urn:x-nmos:format:audio">Audio</option>
              <option value="urn:x-nmos:format:multi">Multi</option>
              <option value="urn:x-nmos:format:data">Data</option>
            </select>
            <label className="srOnly" htmlFor="source-limit">
              Rows per page
            </label>
            <select
              id="source-limit"
              className={surfaceStyles.select}
              value={draftLimit}
              onChange={(event) => setDraftLimit(event.target.value)}
            >
              <option value="25">25 rows</option>
              <option value="50">50 rows</option>
              <option value="100">100 rows</option>
            </select>
            <Button type="submit">
              <ListFilter size={14} aria-hidden="true" /> Apply
            </Button>
            {label || format || draftLabel || draftFormat ? (
              <Button type="button" onClick={clearFilters}>
                <FilterX size={14} aria-hidden="true" /> Clear
              </Button>
            ) : null}
          </form>
        }
      >
        {catalog.loading || catalog.error ? (
          <QueryMessage
            loading={catalog.loading}
            error={catalog.error}
            onRetry={catalog.refresh}
          />
        ) : catalog.data.length === 0 ? (
          <QueryMessage
            empty={{
              title: "No sources found",
            }}
          />
        ) : (
          <div className={surfaceStyles.tableWrap}>
            <table className={surfaceStyles.table}>
              <thead>
                <tr>
                  <th>Source</th>
                  <th>Format</th>
                  <th>Description</th>
                  <th>Tags</th>
                  <th>Updated</th>
                </tr>
              </thead>
              <tbody>
                {catalog.data.map((source) => (
                  <tr key={source.id}>
                    <td>
                      <Link
                        className={surfaceStyles.resourceLink}
                        to={`/sources/${source.id}`}
                      >
                        {source.label || "Unlabelled source"}{" "}
                        <ArrowRight size={12} aria-hidden="true" />
                      </Link>
                      <div
                        className={`${surfaceStyles.secondary} ${surfaceStyles.mono}`}
                      >
                        {source.id}
                      </div>
                    </td>
                    <td>
                      <StatusBadge tone="info">
                        {formatFormat(source.format)}
                      </StatusBadge>
                    </td>
                    <td>
                      {source.description || (
                        <span className={surfaceStyles.secondary}>
                          No description
                        </span>
                      )}
                    </td>
                    <td>
                      {Object.keys(source.tags ?? {})
                        .slice(0, 3)
                        .join(", ") || (
                        <span className={surfaceStyles.secondary}>None</span>
                      )}
                    </td>
                    <td>{formatDate(source.updated ?? source.created)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <CatalogPager
          itemCount={catalog.data.length}
          hasPrevious={catalog.hasPrevious}
          hasNext={catalog.hasNext}
          loading={catalog.loading}
          onPrevious={catalog.previous}
          onNext={catalog.next}
          onRefresh={catalog.refresh}
        />
      </Panel>
    </Page>
  );
}
