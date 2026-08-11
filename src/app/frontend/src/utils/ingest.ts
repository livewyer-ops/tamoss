import type { IngestRunPhase } from "@/control/ingest";

export function ingestPhaseLabel(phase: IngestRunPhase) {
  return phase === "PartiallySucceeded" ? "Partially succeeded" : phase;
}

export function ingestPhaseTone(phase: IngestRunPhase) {
  if (phase === "Succeeded") return "success" as const;
  if (phase === "Failed") return "error" as const;
  if (phase === "PartiallySucceeded") return "warning" as const;
  if (phase === "Cancelled") return "neutral" as const;
  return "info" as const;
}

export function ingestPhaseDescription(phase: IngestRunPhase) {
  const descriptions: Record<IngestRunPhase, string> = {
    Pending: "Waiting for the target instance or ingest prerequisites.",
    Queued: "The ingest workload is waiting to start.",
    Running: "Tamsin is processing the run.",
    Succeeded: "Every input completed successfully.",
    PartiallySucceeded: "Processing completed with one or more input failures.",
    Failed: "The run stopped without completing its inputs.",
    Cancelled: "Cancellation completed and the ingest workload has stopped.",
  };
  return descriptions[phase];
}

export function formatIngestBytes(value: number) {
  if (!Number.isFinite(value) || value <= 0) return "0 B";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  const unit = Math.min(
    Math.floor(Math.log(value) / Math.log(1024)),
    units.length - 1,
  );
  const amount = value / 1024 ** unit;
  return `${amount >= 10 || unit === 0 ? amount.toFixed(0) : amount.toFixed(1)} ${units[unit]}`;
}

const conditionExplanations: Record<string, string> = {
  CancellationRequested: "The ingest workload is terminating.",
  Cancelled: "The run was cancelled and its workload stopped.",
  CredentialProfileResolverUnavailable:
    "Credential profiles are not available on this installation.",
  ExitCodePending: "The workload stopped before its exit status was observed.",
  IngestAuthenticationUnavailable:
    "The target ingest credential is not available.",
  IngestEndpointInvalid:
    "The resolved ingest endpoint did not pass validation.",
  IngestEndpointResolutionFailed:
    "The target ingest endpoint could not be resolved.",
  IngestEndpointResolverUnavailable:
    "Endpoint resolution is not available on this installation.",
  IngestFailed: "Tamsin reported a run-wide failure.",
  IngestJobMissing: "The recorded ingest workload no longer exists.",
  IngestPartiallySucceeded:
    "One or more inputs failed after processing completed.",
  IngestRunning: "Tamsin is processing media.",
  IngestStorageBackendIdentityPending:
    "The selected storage backend is waiting for registration.",
  IngestStorageBackendNotFound: "The selected storage backend was not found.",
  IngestStorageBackendNotReady: "The selected storage backend is not ready.",
  IngestStorageBackendTargetMismatch:
    "The selected storage backend belongs to another instance.",
  IngestStorageBackendUsageInvalid:
    "The selected storage backend cannot store media.",
  IngestSucceeded: "Tamsin completed every input successfully.",
  InputResolutionFailed: "The approved input reference could not be resolved.",
  InputResolverUnavailable:
    "Input resolution is not available on this installation.",
  InvalidResolvedInput: "The resolved input plan did not pass validation.",
  JobCreated: "The ingest workload was created.",
  JobOwnershipConflict: "The observed workload does not belong to this run.",
  JobQueued: "The ingest workload is waiting to be scheduled.",
  JobStarting: "The ingest workload is starting.",
  ResultVerificationPending:
    "Processing finished, but the durable result is still being verified.",
  RetryParentConfigurationMismatch:
    "The retry does not match its parent run configuration.",
  RetryParentInvalid: "The referenced retry parent is invalid.",
  RetryParentNotComplete: "The referenced retry parent is not terminal.",
  RetryParentNotFound: "The referenced retry parent was not found.",
  RetryParentReplaced: "The referenced retry parent was replaced.",
  RetryParentTargetMismatch:
    "The retry parent belongs to another target instance.",
  TamossNotFound: "The target TAMOSS instance was not found.",
  TamossNotReady: "The target TAMOSS instance is not ready.",
  TamsinImageNotImmutable: "The configured Tamsin image is not immutable.",
  TamsinRuntimeUnavailable:
    "A Tamsin runtime is not configured on this installation.",
};

export function ingestConditionExplanation(reason?: string) {
  if (!reason) return "No reason has been reported.";
  return (
    conditionExplanations[reason] ??
    "No additional explanation is available for this condition."
  );
}
