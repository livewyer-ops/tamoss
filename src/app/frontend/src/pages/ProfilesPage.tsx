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
import type { FlowEssenceParameters } from "@/types/tams";
import {
  formatBitRate,
  formatCodec,
  formatDate,
  formatFormat,
  formatFrameRate,
  formatResolution,
} from "@/utils/format";

const DEFAULT_LIMIT = "50";

export default function ProfilesPage() {
  usePageTitle("Profiles");
  const api = useApi();
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const [lookup, setLookup] = useState("");
  const cursor = params.get("cursor") ?? undefined;
  const limit = params.get("limit") ?? DEFAULT_LIMIT;
  const label = params.get("label") ?? "";
  const codec = params.get("codec") ?? "";
  const format = params.get("format") ?? "";
  const [draftLabel, setDraftLabel] = useState(label);
  const [draftCodec, setDraftCodec] = useState(codec);
  const [draftFormat, setDraftFormat] = useState(format);
  const [draftLimit, setDraftLimit] = useState(limit);

  useEffect(() => {
    setDraftLabel(label);
    setDraftCodec(codec);
    setDraftFormat(format);
    setDraftLimit(limit);
  }, [codec, format, label, limit]);

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
      api.getProfiles(
        {
          limit,
          page,
          label: label || undefined,
          codec: codec || undefined,
          format: format || undefined,
        },
        { signal },
      ),
    [api, codec, format, label, limit],
  );
  const catalog = useCursorPage({ cursor, load, onCursorChange: setCursor });

  function applyFilters(event: FormEvent) {
    event.preventDefault();
    setParams((current) => {
      const next = new URLSearchParams(current);
      draftLabel ? next.set("label", draftLabel) : next.delete("label");
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
    setDraftCodec("");
    setDraftFormat("");
    setParams(new URLSearchParams({ limit: draftLimit }));
    catalog.resetHistory();
  }

  function inspect(event: FormEvent) {
    event.preventDefault();
    if (lookup.trim()) {
      navigate(`/profiles/${encodeURIComponent(lookup.trim())}`);
    }
  }

  return (
    <Page>
      <PageHeader
        title="Profiles"
        actions={
          <form onSubmit={inspect} className={surfaceStyles.toolbar}>
            <label className="srOnly" htmlFor="profile-lookup">
              Profile ID
            </label>
            <input
              id="profile-lookup"
              className={`${surfaceStyles.input} ${surfaceStyles.mono}`}
              placeholder="Exact profile ID"
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
        title="Profile catalogue"
        actions={
          <form className={surfaceStyles.toolbar} onSubmit={applyFilters}>
            <label className="srOnly" htmlFor="profile-label">
              Exact label
            </label>
            <input
              id="profile-label"
              className={surfaceStyles.input}
              placeholder="Exact label"
              value={draftLabel}
              onChange={(event) => setDraftLabel(event.target.value)}
            />
            <label className="srOnly" htmlFor="profile-codec">
              Exact codec
            </label>
            <input
              id="profile-codec"
              className={surfaceStyles.input}
              placeholder="Exact codec"
              value={draftCodec}
              onChange={(event) => setDraftCodec(event.target.value)}
            />
            <label className="srOnly" htmlFor="profile-format">
              Format
            </label>
            <select
              id="profile-format"
              className={surfaceStyles.select}
              value={draftFormat}
              onChange={(event) => setDraftFormat(event.target.value)}
            >
              <option value="">All formats</option>
              <option value="urn:x-nmos:format:video">Video</option>
              <option value="urn:x-nmos:format:audio">Audio</option>
              <option value="urn:x-tam:format:image">Image</option>
              <option value="urn:x-nmos:format:data">Data</option>
            </select>
            <label className="srOnly" htmlFor="profile-limit">
              Rows per page
            </label>
            <select
              id="profile-limit"
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
            codec ||
            format ||
            draftLabel ||
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
          <QueryMessage empty={{ title: "No profiles found" }} />
        ) : (
          <div className={surfaceStyles.tableWrap}>
            <table className={surfaceStyles.table}>
              <thead>
                <tr>
                  <th>Profile</th>
                  <th>Format</th>
                  <th>Codec</th>
                  <th>Technical</th>
                  <th>Created</th>
                </tr>
              </thead>
              <tbody>
                {catalog.data.map((profile) => {
                  const metadata = profile.flow_metadata;
                  const essence = metadata.essence_parameters as
                    | FlowEssenceParameters
                    | undefined;
                  return (
                    <tr key={profile.id}>
                      <td>
                        <Link
                          className={surfaceStyles.resourceLink}
                          to={`/profiles/${profile.id}`}
                        >
                          {profile.label || "Unlabelled profile"}{" "}
                          <ArrowRight size={12} aria-hidden="true" />
                        </Link>
                        <div
                          className={`${surfaceStyles.secondary} ${surfaceStyles.mono}`}
                        >
                          {profile.id}
                        </div>
                      </td>
                      <td>
                        <StatusBadge tone="info">
                          {formatFormat(metadata.format)}
                        </StatusBadge>
                      </td>
                      <td>{formatCodec(metadata.codec)}</td>
                      <td>
                        <div>
                          {formatResolution(
                            essence?.frame_width,
                            essence?.frame_height,
                          )}
                        </div>
                        <div className={surfaceStyles.secondary}>
                          {formatFrameRate(essence?.frame_rate)} ·{" "}
                          {formatBitRate(metadata.avg_bit_rate)}
                        </div>
                      </td>
                      <td>{formatDate(profile.created)}</td>
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
