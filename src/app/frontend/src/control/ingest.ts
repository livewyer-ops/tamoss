import { config } from "@/config";
import type { components, operations } from "@/control/generated/openapi";

export type IngestRunPhase = components["schemas"]["IngestRunPhase"];

export const INGEST_RUN_PHASES = [
  "Pending",
  "Queued",
  "Running",
  "Succeeded",
  "PartiallySucceeded",
  "Failed",
  "Cancelled",
] as const satisfies readonly IngestRunPhase[];

export type IngestRunCondition = components["schemas"]["IngestRunCondition"];
export type IngestRunDetail =
  operations["getIngestRun"]["responses"][200]["content"]["application/json"];
export type ConsoleSession =
  operations["getConsoleSession"]["responses"][200]["content"]["application/json"];
export type IngestRunListResponse =
  operations["listIngestRuns"]["responses"][200]["content"]["application/json"];
export type CancelIngestRunRequest =
  operations["cancelIngestRun"]["requestBody"]["content"]["application/json"];
export type CancelIngestRunResponse =
  operations["cancelIngestRun"]["responses"][200]["content"]["application/json"];

export interface ConsoleRequestOptions {
  signal?: AbortSignal;
}

export type IngestRunListParams = NonNullable<
  operations["listIngestRuns"]["parameters"]["query"]
>;

export class ConsoleApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message);
    this.name = "ConsoleApiError";
  }
}

function consoleUrl(path: string, params?: Record<string, string | number>) {
  const base = config.controlApiUrl.endsWith("/")
    ? config.controlApiUrl
    : `${config.controlApiUrl}/`;
  const url = new URL(
    path.replace(/^\//, ""),
    new URL(base, window.location.origin),
  );
  for (const [key, value] of Object.entries(params ?? {})) {
    url.searchParams.set(key, String(value));
  }
  return url.toString();
}

async function consoleError(response: Response): Promise<ConsoleApiError> {
  let code = "request_failed";
  let message = "Console request failed.";
  try {
    const body = (await response.json()) as { code?: unknown; error?: unknown };
    if (typeof body.code === "string" && body.code) code = body.code;
    if (typeof body.error === "string" && body.error) message = body.error;
  } catch {
    // Console errors are intentionally projected; an invalid response stays generic.
  }
  return new ConsoleApiError(response.status, code, message);
}

async function getJson<T>(
  path: string,
  options: ConsoleRequestOptions = {},
  params?: Record<string, string | number>,
): Promise<T> {
  const response = await fetch(consoleUrl(path, params), {
    credentials: "same-origin",
    headers: { Accept: "application/json" },
    signal: options.signal,
  });
  if (!response.ok) throw await consoleError(response);
  return (await response.json()) as T;
}

export function getConsoleSession(options: ConsoleRequestOptions = {}) {
  return getJson<ConsoleSession>("session", options);
}

export function getIngestRuns(
  params: IngestRunListParams = {},
  options: ConsoleRequestOptions = {},
) {
  const query: Record<string, string | number> = {};
  if (params.limit !== undefined) query.limit = params.limit;
  if (params.phase) query.phase = params.phase;
  if (params.cursor) query.cursor = params.cursor;
  return getJson<IngestRunListResponse>("ingest-runs", options, query);
}

export function getIngestRun(
  name: string,
  options: ConsoleRequestOptions = {},
) {
  return getJson<IngestRunDetail>(
    `ingest-runs/${encodeURIComponent(name)}`,
    options,
  );
}

export async function cancelIngestRun(
  name: string,
  payload: CancelIngestRunRequest,
  options: ConsoleRequestOptions = {},
): Promise<CancelIngestRunResponse> {
  const response = await fetch(
    consoleUrl(`ingest-runs/${encodeURIComponent(name)}/cancel`),
    {
      method: "POST",
      credentials: "same-origin",
      headers: {
        Accept: "application/json",
        "Content-Type": "application/json",
      },
      body: JSON.stringify(payload),
      signal: options.signal,
    },
  );
  if (!response.ok) throw await consoleError(response);
  return (await response.json()) as CancelIngestRunResponse;
}
