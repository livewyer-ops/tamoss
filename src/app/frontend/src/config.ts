/**
 * Centralized runtime configuration.
 *
 * In production, values come from window.__TAMOSS_CONFIG__ (generated in
 * runtime-config.js by docker-entrypoint.sh).
 *
 * The API path is deliberately fixed to the same origin. The only runtime
 * override is the optional Console API path.
 *
 * Note: there is no longer a frontend ``s3Endpoint`` — presigned URLs are
 * generated server-side against the configured ``TAMOSS_S3_PUBLIC_ENDPOINT``
 * and are browser-reachable as returned.
 */

interface TamossConfig {
  apiUrl: string;
  controlApiUrl: string;
}

interface RuntimeConfig {
  controlApiUrl: string;
}

declare global {
  interface Window {
    __TAMOSS_CONFIG__?: Partial<RuntimeConfig>;
  }
}

/** Return empty string if value is an unexpanded envsubst template variable */
function resolve(value: string | undefined): string {
  if (!value || value.startsWith("$")) return "";
  return value;
}

const runtimeConfig = window.__TAMOSS_CONFIG__ ?? {};

export const config: TamossConfig = {
  /** TAMS is reachable only through the same-origin, read-only proxy. */
  apiUrl: "/api",

  /** Same-origin TAMOSS operational API. */
  controlApiUrl:
    resolve(runtimeConfig.controlApiUrl) ||
    (import.meta.env.VITE_CONTROL_API_URL as string) ||
    "/ui-api/v1",
};
