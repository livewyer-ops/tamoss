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
import type { FlowStatus } from "@/types/tams";
import { flowStatusLabel, flowStatusTone } from "@/utils/flow-status";
import {
  formatBitRate,
  formatCodec,
  formatFormat,
  formatFrameRate,
  formatResolution,
} from "@/utils/format";

const DEFAULT_LIMIT = "50";

export default function FlowsPage() {
  usePageTitle("Flows");
  const api = useApi();
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const [lookup, setLookup] = useState("");
  const cursor = params.get("cursor") ?? undefined;
  const limit = params.get("limit") ?? DEFAULT_LIMIT;
  const label = params.get("label") ?? "";
  const sourceId = params.get("source_id") ?? "";
  const profileId = params.get("profile_id") ?? "";
  const status = (params.get("status") ?? "") as FlowStatus | "";
  const codec = params.get("codec") ?? "";
  const format = params.get("format") ?? "";
  const [draftLabel, setDraftLabel] = useState(label);
  const [draftSourceId, setDraftSourceId] = useState(sourceId);
  const [draftProfileId, setDraftProfileId] = useState(profileId);
  const [draftStatus, setDraftStatus] = useState<FlowStatus | "">(status);
  const [draftCodec, setDraftCodec] = useState(codec);
  const [draftFormat, setDraftFormat] = useState(format);
  const [draftLimit, setDraftLimit] = useState(limit);

  useEffect(() => {
    setDraftLabel(label);
    setDraftSourceId(sourceId);
    setDraftProfileId(profileId);
    setDraftStatus(status);
    setDraftCodec(codec);
    setDraftFormat(format);
    setDraftLimit(limit);
  }, [codec, format, label, limit, profileId, sourceId, status]);

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
      api.getFlows(
        {
          limit,
          page,
          label: label || undefined,
          source_id: sourceId || undefined,
          profile_id: profileId || undefined,
          status: status || undefined,
          codec: codec || undefined,
          format: format || undefined,
          include_timerange: true,
        },
        { signal },
      ),
    [api, codec, format, label, limit, profileId, sourceId, status],
  );
  const catalog = useCursorPage({ cursor, load, onCursorChange: setCursor });

  function applyFilters(event: FormEvent) {
    event.preventDefault();
    setParams((current) => {
      const next = new URLSearchParams(current);
      draftLabel ? next.set("label", draftLabel) : next.delete("label");
      draftSourceId
        ? next.set("source_id", draftSourceId)
        : next.delete("source_id");
      draftProfileId
        ? next.set("profile_id", draftProfileId)
        : next.delete("profile_id");
      draftStatus ? next.set("status", draftStatus) : next.delete("status");
      draftCodec ? next.set("codec", draftCodec) : next.delete("codec");
      draftFormat ? next.set("format", draftFormat) : next.delete("format");
      next.set("limit", draftLimit);
      next.delete("cursor");
      return next;
    });
    catalog.resetHistory();
  }

  function clearFilters() {
    setDraftLabel("");
    setDraftSourceId("");
    setDraftProfileId("");
    setDraftStatus("");
    setDraftCodec("");
    setDraftFormat("");
    setParams(new URLSearchParams({ limit: draftLimit }));
    catalog.resetHistory();
  }

  function inspect(event: FormEvent) {
    event.preventDefault();
    if (lookup.trim()) navigate(`/flows/${encodeURIComponent(lookup.trim())}`);
  }

  return (
    <Page>
      <PageHeader
        title="Flows"
        actions={
          <form onSubmit={inspect} className={surfaceStyles.toolbar}>
            <label className="srOnly" htmlFor="flow-lookup">
              Flow ID
            </label>
            <input
              id="flow-lookup"
              className={`${surfaceStyles.input} ${surfaceStyles.mono}`}
              placeholder="Exact flow ID"
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
        title="Flow catalogue"
        actions={
          <form className={surfaceStyles.toolbar} onSubmit={applyFilters}>
            <label className="srOnly" htmlFor="flow-label">
              Exact label
            </label>
            <input
              id="flow-label"
              className={surfaceStyles.input}
              placeholder="Exact label"
              value={draftLabel}
              onChange={(event) => setDraftLabel(event.target.value)}
            />
            <label className="srOnly" htmlFor="flow-source">
              Source ID
            </label>
            <input
              id="flow-source"
              className={`${surfaceStyles.input} ${surfaceStyles.mono}`}
              placeholder="Source ID"
              value={draftSourceId}
              onChange={(event) => setDraftSourceId(event.target.value)}
            />
            <label className="srOnly" htmlFor="flow-profile">
              Profile ID
            </label>
            <input
              id="flow-profile"
              className={`${surfaceStyles.input} ${surfaceStyles.mono}`}
              placeholder="Profile ID"
              value={draftProfileId}
              onChange={(event) => setDraftProfileId(event.target.value)}
            />
            <label className="srOnly" htmlFor="flow-codec">
              Exact codec
            </label>
            <input
              id="flow-codec"
              className={surfaceStyles.input}
              placeholder="Exact codec"
              value={draftCodec}
              onChange={(event) => setDraftCodec(event.target.value)}
            />
            <label className="srOnly" htmlFor="flow-format">
              Format
            </label>
            <select
              id="flow-format"
              className={surfaceStyles.select}
              value={draftFormat}
              onChange={(event) => setDraftFormat(event.target.value)}
            >
              <option value="">All formats</option>
              <option value="urn:x-nmos:format:video">Video</option>
              <option value="urn:x-nmos:format:audio">Audio</option>
              <option value="urn:x-tam:format:image">Image</option>
              <option value="urn:x-nmos:format:multi">Multi</option>
              <option value="urn:x-nmos:format:data">Data</option>
            </select>
            <label className="srOnly" htmlFor="flow-status">
              Status
            </label>
            <select
              id="flow-status"
              className={surfaceStyles.select}
              value={draftStatus}
              onChange={(event) =>
                setDraftStatus(event.target.value as FlowStatus | "")
              }
            >
              <option value="">All statuses</option>
              <option value="awaiting_content">Awaiting content</option>
              <option value="ingesting">Ingesting</option>
              <option value="replication_in_progress">
                Replication in progress
              </option>
              <option value="closed_complete">Closed complete</option>
            </select>
            <label className="srOnly" htmlFor="flow-limit">
              Rows per page
            </label>
            <select
              id="flow-limit"
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
            {label ||
            sourceId ||
            profileId ||
            status ||
            codec ||
            format ||
            draftLabel ||
            draftSourceId ||
            draftProfileId ||
            draftStatus ||
            draftCodec ||
            draftFormat ? (
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
              title: "No flows found",
            }}
          />
        ) : (
          <div className={surfaceStyles.tableWrap}>
            <table className={surfaceStyles.table}>
              <thead>
                <tr>
                  <th>Flow</th>
                  <th>Format</th>
                  <th>Codec</th>
                  <th>Technical</th>
                  <th>Timerange</th>
                  <th>Status</th>
                  <th>Access</th>
                </tr>
              </thead>
              <tbody>
                {catalog.data.map((flow) => {
                  const essence = flow.essence_parameters;
                  return (
                    <tr key={flow.id}>
                      <td>
                        <Link
                          className={surfaceStyles.resourceLink}
                          to={`/flows/${flow.id}`}
                        >
                          {flow.label || "Unlabelled flow"}{" "}
                          <ArrowRight size={12} aria-hidden="true" />
                        </Link>
                        <div
                          className={`${surfaceStyles.secondary} ${surfaceStyles.mono}`}
                        >
                          {flow.id}
                        </div>
                        <div
                          className={`${surfaceStyles.secondary} ${surfaceStyles.mono}`}
                        >
                          Source {flow.source_id}
                        </div>
                      </td>
                      <td>
                        <StatusBadge tone="info">
                          {formatFormat(flow.format)}
                        </StatusBadge>
                      </td>
                      <td>{formatCodec(flow.codec)}</td>
                      <td>
                        <div>
                          {formatResolution(
                            essence?.frame_width,
                            essence?.frame_height,
                          )}
                        </div>
                        <div className={surfaceStyles.secondary}>
                          {formatFrameRate(essence?.frame_rate)} ·{" "}
                          {formatBitRate(flow.avg_bit_rate)}
                        </div>
                      </td>
                      <td className={surfaceStyles.mono}>
                        {flow.timerange || "Empty"}
                      </td>
                      <td>
                        <StatusBadge tone={flowStatusTone(flow.status)}>
                          {flowStatusLabel(flow.status)}
                        </StatusBadge>
                      </td>
                      <td>
                        {flow.read_only ? (
                          <StatusBadge tone="warning">Read only</StatusBadge>
                        ) : (
                          <StatusBadge tone="success">Writable</StatusBadge>
                        )}
                      </td>
                    </tr>
                  );
                })}
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
