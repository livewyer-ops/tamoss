import type { FlowSegment } from "@/types/tams";

const TIMERANGE_EPSILON_SECONDS = 1e-9;

/**
 * Parse a TAMS timerange string like "[0:0_10:0)" into start/end seconds.
 *
 * Handles the special representations:
 *   "_"  → unbounded range (all time)  → returns { start: 0, end: 0, duration: 0 }
 *   "()" → empty range                 → returns { start: 0, end: 0, duration: 0 }
 */
export function parseTimerange(tr: string): {
  start: number;
  end: number;
  duration: number;
} {
  if (!tr || tr === "_" || tr === "()") {
    return { start: 0, end: 0, duration: 0 };
  }

  const inner = tr.replace(/[[\]()]/g, "");
  const [startStr, endStr] = inner.split("_");

  function parseTimestamp(ts: string): number {
    if (!ts) return 0;
    const parts = ts.split(":");
    const secs = parseInt(parts[0], 10) || 0;
    const nanos = parseInt(parts[1], 10) || 0;
    return secs + nanos / 1e9;
  }

  const start = parseTimestamp(startStr);
  const end = endStr === undefined ? start : parseTimestamp(endStr);
  return { start, end, duration: end - start };
}

/**
 * Compute the total media duration from segments.
 *
 * Instead of summing individual segment durations (which may have rounding
 * gaps or overlaps), this computes the span from earliest start to latest
 * end across all segments. This reports the Flow extent.
 */
export function computeSegmentsDuration(segments: FlowSegment[]): number {
  if (segments.length === 0) return 0;

  let minStart = Infinity;
  let maxEnd = -Infinity;

  for (const seg of segments) {
    try {
      const { start, end } = parseTimerange(seg.timerange);
      if (start < minStart) minStart = start;
      if (end > maxEnd) maxEnd = end;
    } catch {
      // skip unparseable
    }
  }

  if (!isFinite(minStart) || !isFinite(maxEnd)) return 0;
  return maxEnd - minStart;
}

interface TimedSegment {
  seg: FlowSegment;
  start: number;
  end: number;
  duration: number;
}

/**
 * Sort segments chronologically and return only those with URLs.
 */
function sortedPlayableSegments(segments: FlowSegment[]): FlowSegment[] {
  return sortedTimedSegments(segments, { requireUrl: true }).map(
    (timed) => timed.seg,
  );
}

function sortedTimedSegments(
  segments: FlowSegment[],
  { requireUrl }: { requireUrl: boolean },
): TimedSegment[] {
  return segments
    .filter((segment) => !requireUrl || (segment.get_urls?.length ?? 0) > 0)
    .map((seg) => ({ seg, ...parseTimerange(seg.timerange) }))
    .filter((segment) => Number.isFinite(segment.duration))
    .sort((a, b) => a.start - b.start);
}

/**
 * Build a media playlist (m3u8) string from segments. Does NOT create a blob URL.
 *
 * Segment durations are derived from their individual timeranges. Gaps stay
 * visible as timeline discontinuities instead of being folded into EXTINF.
 */
export function buildMediaPlaylistContent(
  segments: FlowSegment[],
): string | null {
  const parsed = sortedTimedSegments(segments, { requireUrl: true });
  if (parsed.length === 0) return null;

  let maxSegDuration = 0;
  const entries: string[] = [];

  for (let i = 0; i < parsed.length; i++) {
    const current = parsed[i];
    const previous = parsed[i - 1];
    const duration = Math.max(0, current.duration);
    if (duration > maxSegDuration) maxSegDuration = duration;
    if (
      previous &&
      (previous.seg.object_id !== current.seg.object_id ||
        current.start > previous.end + TIMERANGE_EPSILON_SECONDS)
    ) {
      entries.push("#EXT-X-DISCONTINUITY");
    }

    // Server already presigns against the public S3 endpoint, so the URL
    // is browser-reachable as returned.
    const url = current.seg.get_urls![0].url;
    entries.push(`#EXTINF:${duration.toFixed(6)},`);
    entries.push(url);
  }

  const targetSegDuration = Math.max(1, Math.ceil(maxSegDuration));

  return [
    "#EXTM3U",
    "#EXT-X-VERSION:3",
    `#EXT-X-TARGETDURATION:${targetSegDuration}`,
    "#EXT-X-MEDIA-SEQUENCE:0",
    "#EXT-X-PLAYLIST-TYPE:VOD",
    ...entries,
    "#EXT-X-ENDLIST",
  ].join("\n");
}

/**
 * Create a blob URL from manifest content.
 */
function manifestToBlob(content: string): string {
  const blob = new Blob([content], { type: "application/vnd.apple.mpegurl" });
  return URL.createObjectURL(blob);
}

/**
 * Build a single-flow HLS m3u8 manifest from TAMS flow segments.
 * Returns a blob: URL that can be fed to an HLS-capable player.
 */
export function buildHlsManifest(
  segments: FlowSegment[],
  _codec?: string,
): string | null {
  const content = buildMediaPlaylistContent(segments);
  if (!content) return null;
  return manifestToBlob(content);
}

export interface MultiFlowManifest {
  primaryUrl: string;
  audioUrl: string | null;
  urls: string[];
}

/**
 * Build separate HLS media playlists for a multi-flow preview.
 *
 * TAMS child flows commonly store video and audio as independently encoded
 * transport-stream objects with resetting PTS. A single HLS alternate-audio
 * master asks the player to align those timelines internally, which is brittle
 * for this preview workflow. The page attaches the returned video and audio
 * playlists to separate media elements and keeps them in sync explicitly.
 *
 * `primaryUrl` is fed to the visible video element. `audioUrl` is only set
 * when there is a distinct audio playlist to attach to the hidden audio
 * element. All returned `urls` must be revoked on cleanup.
 */
export function buildMultiFlowManifest(
  videoSegments: FlowSegment[],
  audioSegments: FlowSegment[],
): MultiFlowManifest | null {
  const hasVideo = sortedPlayableSegments(videoSegments).length > 0;
  const hasAudio = sortedPlayableSegments(audioSegments).length > 0;

  if (!hasVideo && !hasAudio) return null;

  if (!hasVideo) {
    const content = buildMediaPlaylistContent(audioSegments);
    if (!content) return null;
    const primaryUrl = manifestToBlob(content);
    return { primaryUrl, audioUrl: null, urls: [primaryUrl] };
  }
  if (!hasAudio) {
    const content = buildMediaPlaylistContent(videoSegments);
    if (!content) return null;
    const primaryUrl = manifestToBlob(content);
    return { primaryUrl, audioUrl: null, urls: [primaryUrl] };
  }

  const videoContent = buildMediaPlaylistContent(videoSegments);
  const audioContent = buildMediaPlaylistContent(audioSegments);

  if (!videoContent || !audioContent) return null;

  const videoBlobUrl = manifestToBlob(videoContent);
  const audioBlobUrl = manifestToBlob(audioContent);

  return {
    primaryUrl: videoBlobUrl,
    audioUrl: audioBlobUrl,
    urls: [videoBlobUrl, audioBlobUrl],
  };
}

/**
 * Revoke a blob URL created by buildHlsManifest or buildMultiFlowManifest.
 */
export function revokeManifestUrl(url: string): void {
  if (url.startsWith("blob:")) {
    URL.revokeObjectURL(url);
  }
}
