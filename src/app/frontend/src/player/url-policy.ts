import type { ObjectUrl } from "@/types/tams";

export type MediaRequestCredentials = "same-origin" | "omit";

export interface SanitizedMediaUrl {
  url: string;
  credentials: MediaRequestCredentials;
  presigned: boolean;
  label?: string;
  storageId?: string;
}

const ENCODED_CONTROL_CHARACTER =
  /%(?:0[0-9a-f]|1[0-9a-f]|7f|8[0-9a-f]|9[0-9a-f])/iu;

function hasControlCharacter(value: string): boolean {
  for (const character of value) {
    const codePoint = character.codePointAt(0);
    if (
      codePoint !== undefined &&
      (codePoint <= 0x1f || (codePoint >= 0x7f && codePoint <= 0x9f))
    ) {
      return true;
    }
  }
  return false;
}

function isLoopbackHostname(hostname: string): boolean {
  const normalized = hostname.toLowerCase().replace(/\.$/u, "");
  return (
    normalized === "localhost" ||
    normalized.endsWith(".localhost") ||
    normalized === "[::1]" ||
    normalized === "::1" ||
    /^127(?:\.\d{1,3}){3}$/u.test(normalized)
  );
}

function parsePolicyOrigin(locationOrigin: string): URL {
  try {
    const origin = new URL(locationOrigin);
    if (origin.protocol !== "http:" && origin.protocol !== "https:") {
      throw new Error();
    }
    return origin;
  } catch {
    throw new Error("The media URL policy origin is invalid.");
  }
}

/**
 * Applies the browser media boundary without logging or returning rejected URL
 * values. Callers discard an undefined result and report only track metadata.
 */
export function sanitizeMediaUrl(
  candidate: ObjectUrl,
  locationOrigin: string,
): SanitizedMediaUrl | undefined {
  const rawUrl = candidate.url;
  if (
    typeof rawUrl !== "string" ||
    rawUrl.length === 0 ||
    hasControlCharacter(rawUrl) ||
    ENCODED_CONTROL_CHARACTER.test(rawUrl) ||
    rawUrl.includes("#")
  ) {
    return undefined;
  }

  const pageOrigin = parsePolicyOrigin(locationOrigin);
  let parsed: URL;
  try {
    parsed = new URL(rawUrl, pageOrigin);
  } catch {
    return undefined;
  }

  if (
    (parsed.protocol !== "http:" && parsed.protocol !== "https:") ||
    parsed.username.length > 0 ||
    parsed.password.length > 0 ||
    parsed.hash.length > 0
  ) {
    return undefined;
  }

  const sameOrigin = parsed.origin === pageOrigin.origin;
  if (
    parsed.protocol === "http:" &&
    !sameOrigin &&
    !isLoopbackHostname(parsed.hostname)
  ) {
    return undefined;
  }

  const presigned = candidate.presigned === true;
  if (presigned && sameOrigin) {
    // Native media and same-origin XHR always carry ambient cookies. Until the
    // player owns a credential-omitting loader, do not claim that boundary.
    return undefined;
  }
  return {
    url: parsed.toString(),
    credentials: !presigned && sameOrigin ? "same-origin" : "omit",
    presigned,
    ...(candidate.label ? { label: candidate.label } : {}),
    ...(candidate.storage_id ? { storageId: candidate.storage_id } : {}),
  };
}
