import { ArrowRight, FilterX, ListFilter } from "lucide-react";
import { type FormEvent, useCallback, useEffect, useState } from "react";
import { Link, useSearchParams } from "react-router";
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
import {
  getIngestRuns,
  INGEST_RUN_PHASES,
  type IngestRunPhase,
} from "@/control/ingest";
import { useCursorPage } from "@/hooks/useCursorPage";
import { usePageTitle } from "@/hooks/usePageTitle";
import { formatDate, formatRelativeTime } from "@/utils/format";
import {
  formatIngestBytes,
  ingestPhaseLabel,
  ingestPhaseTone,
} from "@/utils/ingest";

const DEFAULT_LIMIT = "25";
const LIMITS = ["25", "50", "100"] as const;

function isPhase(value: string): value is IngestRunPhase {
  return INGEST_RUN_PHASES.some((phase) => phase === value);
}

export default function IngestPage() {
  usePageTitle("Ingest runs");
  const [params, setParams] = useSearchParams();
  const phaseParam = params.get("phase") ?? "";
  const phase = isPhase(phaseParam) ? phaseParam : undefined;
  const limitParam = params.get("limit") ?? DEFAULT_LIMIT;
  const limit = LIMITS.some((value) => value === limitParam)
    ? limitParam
    : DEFAULT_LIMIT;
  const cursor = params.get("cursor") ?? undefined;
  const [draftPhase, setDraftPhase] = useState(phase ?? "");
  const [draftLimit, setDraftLimit] = useState(limit);

  useEffect(() => {
    setDraftPhase(phase ?? "");
    setDraftLimit(limit);
  }, [limit, phase]);

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
      getIngestRuns(
        { limit: Number(limit), phase, cursor: page },
        { signal },
      ).then((response) => ({
        data: response.items,
        nextKey: response.page.nextCursor,
        limit: response.page.limit,
      })),
    [limit, phase],
  );
  const runs = useCursorPage({ cursor, load, onCursorChange: setCursor });

  function applyFilters(event: FormEvent) {
    event.preventDefault();
    setParams((current) => {
      const next = new URLSearchParams(current);
      draftPhase ? next.set("phase", draftPhase) : next.delete("phase");
      next.set("limit", draftLimit);
      next.delete("cursor");
      return next;
    });
    runs.resetHistory();
  }

  function clearPhase() {
    setDraftPhase("");
    setParams(new URLSearchParams({ limit: draftLimit }));
    runs.resetHistory();
  }

  return (
    <Page>
      <PageHeader title="Ingest runs" />
      <Panel
        title="Run history"
        actions={
          <form className={surfaceStyles.toolbar} onSubmit={applyFilters}>
            <label className="srOnly" htmlFor="ingest-phase">
              Exact phase
            </label>
            <select
              id="ingest-phase"
              className={surfaceStyles.select}
              value={draftPhase}
              onChange={(event) => setDraftPhase(event.target.value)}
            >
              <option value="">All phases</option>
              {INGEST_RUN_PHASES.map((value) => (
                <option key={value} value={value}>
                  {ingestPhaseLabel(value)}
                </option>
              ))}
            </select>
            <label className="srOnly" htmlFor="ingest-limit">
              Rows per page
            </label>
            <select
              id="ingest-limit"
              className={surfaceStyles.select}
              value={draftLimit}
              onChange={(event) => setDraftLimit(event.target.value)}
            >
              {LIMITS.map((value) => (
                <option key={value} value={value}>
                  {value} rows
                </option>
              ))}
            </select>
            <Button type="submit">
              <ListFilter size={14} aria-hidden="true" /> Apply
            </Button>
            {phase || draftPhase ? (
              <Button type="button" onClick={clearPhase}>
                <FilterX size={14} aria-hidden="true" /> Clear
              </Button>
            ) : null}
          </form>
        }
      >
        {runs.loading || runs.error ? (
          <QueryMessage
            loading={runs.loading}
            error={runs.error}
            onRetry={runs.refresh}
          />
        ) : runs.data.length === 0 ? (
          <QueryMessage
            empty={{
              title:
                runs.hasNext || runs.hasPrevious
                  ? "No matching runs on this page"
                  : "No ingest runs found",
            }}
          />
        ) : (
          <div className={surfaceStyles.tableWrap}>
            <table className={surfaceStyles.table}>
              <caption className="srOnly">Durable ingest runs</caption>
              <thead>
                <tr>
                  <th>Run</th>
                  <th>Phase</th>
                  <th>Progress</th>
                  <th>Profile</th>
                  <th>Attempt</th>
                  <th>Created</th>
                  <th>Completed</th>
                </tr>
              </thead>
              <tbody>
                {runs.data.map((run) => (
                  <tr key={run.uid}>
                    <td>
                      <Link
                        className={surfaceStyles.resourceLink}
                        to={`/ingest/${encodeURIComponent(run.name)}`}
                      >
                        {run.name} <ArrowRight size={12} aria-hidden="true" />
                      </Link>
                      <div className={surfaceStyles.secondary}>
                        {run.sizeClass} size
                      </div>
                    </td>
                    <td>
                      <StatusBadge tone={ingestPhaseTone(run.phase)}>
                        {ingestPhaseLabel(run.phase)}
                      </StatusBadge>
                    </td>
                    <td>
                      {run.progress.inputsCompleted} /{" "}
                      {run.progress.inputsTotal}
                      <div className={surfaceStyles.secondary}>
                        {run.progress.inputsSucceeded} succeeded,{" "}
                        {run.progress.inputsFailed} failed,{" "}
                        {formatIngestBytes(run.progress.bytesUploaded)} uploaded
                      </div>
                    </td>
                    <td>{run.profile}</td>
                    <td>{run.attempt || "-"}</td>
                    <td>
                      {formatRelativeTime(run.createdAt)}
                      <div className={surfaceStyles.secondary}>
                        {formatDate(run.createdAt)}
                      </div>
                    </td>
                    <td>
                      {run.completedAt
                        ? formatRelativeTime(run.completedAt)
                        : "-"}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <CatalogPager
          itemCount={runs.data.length}
          hasPrevious={runs.hasPrevious}
          hasNext={runs.hasNext}
          loading={runs.loading}
          onPrevious={runs.previous}
          onNext={runs.next}
          onRefresh={runs.refresh}
        />
      </Panel>
    </Page>
  );
}
