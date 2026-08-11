import { ArrowLeft, CircleStop, RefreshCw } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Link, useParams } from "react-router";
import {
  Button,
  Page,
  PageHeader,
  Panel,
  QueryMessage,
  StatusBadge,
  surfaceStyles,
} from "@/components/Surface";
import type { IngestRunCondition } from "@/control/ingest";
import {
  useCancelIngestRun,
  useConsoleSession,
  useIngestRun,
} from "@/control/useIngestRuns";
import { usePageTitle } from "@/hooks/usePageTitle";
import { formatDate } from "@/utils/format";
import {
  formatIngestBytes,
  ingestConditionExplanation,
  ingestPhaseDescription,
  ingestPhaseLabel,
  ingestPhaseTone,
} from "@/utils/ingest";
import styles from "./IngestRunDetailPage.module.css";

function conditionTone(condition: IngestRunCondition) {
  if (condition.type === "Ready") {
    return condition.status === "True" ? "success" : "error";
  }
  if (condition.type === "Degraded" && condition.status === "True") {
    return "error" as const;
  }
  if (condition.status === "Unknown") return "neutral" as const;
  return condition.status === "True" ? ("info" as const) : ("warning" as const);
}

function isAbortError(value: unknown) {
  return value instanceof DOMException && value.name === "AbortError";
}

export default function IngestRunDetailPage() {
  const { runName = "" } = useParams();
  const run = useIngestRun(runName);
  const session = useConsoleSession();
  const cancel = useCancelIngestRun();
  const [confirming, setConfirming] = useState(false);
  const [feedback, setFeedback] = useState("");
  const cancelTrigger = useRef<HTMLButtonElement>(null);
  const keepRunning = useRef<HTMLButtonElement>(null);
  usePageTitle("Ingest run");

  const detail = run.data;
  const cancelCapability = session.data?.capabilities.ingestRuns.cancel;
  const canCancel = Boolean(
    detail?.cancellable &&
      cancelCapability?.available &&
      cancelCapability.allowed,
  );

  useEffect(() => {
    if (confirming) keepRunning.current?.focus();
  }, [confirming]);

  useEffect(() => {
    if (!canCancel) setConfirming(false);
  }, [canCancel]);

  function openConfirmation() {
    cancel.reset();
    setFeedback("");
    setConfirming(true);
  }

  function closeConfirmation() {
    setConfirming(false);
    cancelTrigger.current?.focus();
  }

  async function requestCancellation() {
    if (!detail) return;
    try {
      const response = await cancel.mutateAsync(detail);
      setConfirming(false);
      setFeedback(
        response.replayed
          ? "Cancellation had already been requested."
          : "Cancellation requested.",
      );
    } catch {
      // The mutation error remains in the confirmation region for retry.
    }
  }

  return (
    <Page>
      <PageHeader
        title={detail?.name || "Ingest run"}
        actions={
          <>
            <Link className={surfaceStyles.button} to="/ingest">
              <ArrowLeft size={14} aria-hidden="true" /> Ingest runs
            </Link>
            {canCancel ? (
              <Button
                ref={cancelTrigger}
                type="button"
                variant="danger"
                onClick={openConfirmation}
              >
                <CircleStop size={14} aria-hidden="true" /> Cancel run
              </Button>
            ) : null}
          </>
        }
      />
      {run.isLoading ? (
        <Panel>
          <QueryMessage loading />
        </Panel>
      ) : run.error ? (
        <Panel>
          <QueryMessage error={run.error} onRetry={() => run.refetch()} />
        </Panel>
      ) : detail ? (
        <div className={surfaceStyles.stack}>
          {feedback ? (
            <div className={styles.feedback} role="status">
              {feedback}
            </div>
          ) : null}
          {confirming ? (
            <section
              className={styles.confirm}
              aria-labelledby="cancel-run-title"
              onKeyDown={(event) => {
                if (event.key === "Escape" && !cancel.isPending) {
                  closeConfirmation();
                }
              }}
            >
              <strong id="cancel-run-title">Cancel this ingest run?</strong>
              <p id="cancel-run-description">
                This one-way request stops the ingest workload. The retained run
                cannot be restarted.
              </p>
              {cancel.error && !isAbortError(cancel.error) ? (
                <div
                  className={`${styles.feedback} ${styles.errorFeedback}`}
                  role="alert"
                >
                  {cancel.error.message}
                </div>
              ) : null}
              <div className={surfaceStyles.toolbar}>
                <Button
                  ref={keepRunning}
                  type="button"
                  onClick={closeConfirmation}
                  disabled={cancel.isPending}
                >
                  Keep running
                </Button>
                <Button
                  type="button"
                  variant="danger"
                  onClick={requestCancellation}
                  disabled={cancel.isPending}
                >
                  <CircleStop size={14} aria-hidden="true" />
                  {cancel.isPending ? "Requesting cancellation" : "Cancel run"}
                </Button>
              </div>
            </section>
          ) : null}

          <Panel
            title="Run status"
            actions={
              <Button
                type="button"
                onClick={() => run.refetch()}
                disabled={run.isFetching}
              >
                <RefreshCw size={14} aria-hidden="true" /> Refresh
              </Button>
            }
          >
            <p className={styles.statusExplanation} role="status">
              <StatusBadge tone={ingestPhaseTone(detail.phase)}>
                {ingestPhaseLabel(detail.phase)}
              </StatusBadge>{" "}
              {ingestPhaseDescription(detail.phase)}
            </p>
            <dl className={surfaceStyles.definitionList}>
              <dt>Desired state</dt>
              <dd>{detail.desiredState}</dd>
              <dt>Created</dt>
              <dd>{formatDate(detail.createdAt)}</dd>
              <dt>Started</dt>
              <dd>{formatDate(detail.startedAt)}</dd>
              <dt>Completed</dt>
              <dd>{formatDate(detail.completedAt)}</dd>
              <dt>Attempt</dt>
              <dd>{detail.attempt || "-"}</dd>
              <dt>Generation</dt>
              <dd>
                {detail.observedGeneration} / {detail.generation} observed
              </dd>
              <dt>UID</dt>
              <dd className={surfaceStyles.mono}>{detail.uid}</dd>
            </dl>
          </Panel>

          <Panel title="Progress">
            <dl className={styles.metricGrid}>
              <div className={styles.metric}>
                <dt>Completed</dt>
                <dd>
                  {detail.progress.inputsCompleted} /{" "}
                  {detail.progress.inputsTotal}
                </dd>
              </div>
              <div className={styles.metric}>
                <dt>Succeeded</dt>
                <dd>{detail.progress.inputsSucceeded}</dd>
              </div>
              <div className={styles.metric}>
                <dt>Failed</dt>
                <dd>{detail.progress.inputsFailed}</dd>
              </div>
              <div className={styles.metric}>
                <dt>Uploaded</dt>
                <dd>{formatIngestBytes(detail.progress.bytesUploaded)}</dd>
              </div>
            </dl>
          </Panel>

          <div className={surfaceStyles.grid2}>
            <Panel title="Configuration">
              <dl className={surfaceStyles.definitionList}>
                <dt>Input type</dt>
                <dd>{detail.inputKind}</dd>
                <dt>Profile</dt>
                <dd>{detail.profile}</dd>
                <dt>Size</dt>
                <dd>{detail.sizeClass}</dd>
                <dt>Storage backend</dt>
                <dd>{detail.options.storageBackend || "Instance default"}</dd>
                <dt>Verify</dt>
                <dd>{detail.options.verify ? "Enabled" : "Disabled"}</dd>
                <dt>Dry run</dt>
                <dd>{detail.options.dryRun ? "Enabled" : "Disabled"}</dd>
                <dt>Maximum inputs</dt>
                <dd>{detail.options.maxInputs}</dd>
                <dt>Concurrency</dt>
                <dd>{detail.options.concurrency || "Managed default"}</dd>
              </dl>
            </Panel>
            <Panel title="Runtime references">
              <dl className={surfaceStyles.definitionList}>
                <dt>Job</dt>
                <dd className={surfaceStyles.mono}>
                  {detail.job?.name || "-"}
                </dd>
                <dt>Tamsin run</dt>
                <dd className={surfaceStyles.mono}>
                  {detail.tamsinRunId || "-"}
                </dd>
                <dt>Retry parent</dt>
                <dd>
                  {detail.retryOf ? (
                    <Link
                      className={surfaceStyles.mono}
                      to={`/ingest/${encodeURIComponent(detail.retryOf.name)}`}
                    >
                      {detail.retryOf.name}
                    </Link>
                  ) : (
                    "-"
                  )}
                </dd>
                <dt>Result verified</dt>
                <dd>{detail.result?.verified ? "Yes" : "No"}</dd>
                <dt>Result type</dt>
                <dd>{detail.result?.mediaType || "-"}</dd>
                <dt>Result size</dt>
                <dd>
                  {detail.result?.size !== undefined
                    ? formatIngestBytes(detail.result.size)
                    : "-"}
                </dd>
              </dl>
            </Panel>
          </div>

          <Panel title="Conditions">
            {detail.conditions.length === 0 ? (
              <QueryMessage empty={{ title: "No conditions reported" }} />
            ) : (
              <div className={surfaceStyles.tableWrap}>
                <table className={surfaceStyles.table}>
                  <caption className="srOnly">Ingest run conditions</caption>
                  <thead>
                    <tr>
                      <th>Condition</th>
                      <th>Status</th>
                      <th>Reason</th>
                      <th>Changed</th>
                    </tr>
                  </thead>
                  <tbody>
                    {detail.conditions.map((condition) => (
                      <tr key={condition.type}>
                        <td>{condition.type}</td>
                        <td>
                          <StatusBadge tone={conditionTone(condition)}>
                            {condition.status}
                          </StatusBadge>
                        </td>
                        <td>
                          <span className={surfaceStyles.mono}>
                            {condition.reason || "Not reported"}
                          </span>
                          <div className={surfaceStyles.secondary}>
                            {ingestConditionExplanation(condition.reason)}
                          </div>
                        </td>
                        <td>{formatDate(condition.lastTransitionTime)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Panel>
        </div>
      ) : null}
    </Page>
  );
}
