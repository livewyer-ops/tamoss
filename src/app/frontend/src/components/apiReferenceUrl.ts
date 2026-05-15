import { config } from "@/config";

export function buildApiReferenceUrl(
  path: string,
  apiUrl = config.apiUrl,
  origin = window.location.origin,
): string {
  const rel = path.startsWith("/") ? path.slice(1) : path;
  const base = apiUrl.endsWith("/") ? apiUrl : `${apiUrl}/`;
  const absoluteBase =
    base.startsWith("http://") || base.startsWith("https://")
      ? base
      : new URL(base, origin).toString();
  return new URL(rel, absoluteBase).toString();
}
