/**
 * Centralized runtime configuration.
 *
 * In production, values come from window.__TAMOSS_CONFIG__ (generated in
 * runtime-config.js by docker-entrypoint.sh from TAMOSS_API_URL).
 *
 * In development, values fall back to Vite env variables (VITE_*).
 *
 * Note: there is no longer a frontend ``s3Endpoint`` — presigned URLs are
 * generated server-side against the configured ``TAMOSS_S3_PUBLIC_ENDPOINT``
 * and are browser-reachable as returned.
 */

interface TamossConfig {
  apiUrl: string;
  apiToken: string;
}

interface RuntimeConfig {
  apiUrl: string;
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
  /** Base URL for API requests (e.g. https://api.example.com) */
  apiUrl:
    resolve(runtimeConfig.apiUrl) ||
    (import.meta.env.VITE_API_URL as string) ||
    "/api",

  /**
   * Dev-only API token sent as Authorization: Bearer header.
   */
  apiToken: (import.meta.env.VITE_API_TOKEN as string) || "",
};
