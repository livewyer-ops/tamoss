import type { FlowSegment } from "@/types/tams";

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
  const end = parseTimestamp(endStr);
  return { start, end, duration: end - start };
}

/**
 * Build a TAMS timerange string like "[0:0_10:0)" from start/end seconds.
 * Inverse of parseTimerange.
 */
export function buildTimerange(startSecs: number, endSecs: number): string {
  const sW = Math.floor(startSecs);
  const sN = Math.round((startSecs - sW) * 1e9);
  const eW = Math.floor(endSecs);
  const eN = Math.round((endSecs - eW) * 1e9);
  return `[${sW}:${sN}_${eW}:${eN})`;
}

/**
 * Compute the total media duration from segments.
 *
 * Instead of summing individual segment durations (which may have rounding
 * gaps or overlaps), this computes the span from earliest start to latest
 * end across all segments.  This gives a consistent total that matches
 * what the HLS player will actually present.
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

/**
 * Sort segments chronologically and return only those with URLs.
 */
function sortedPlayableSegments(segments: FlowSegment[]): FlowSegment[] {
  return segments
    .filter((s) => s.get_urls && s.get_urls.length > 0)
    .sort((a, b) => {
      const ta = parseTimerange(a.timerange);
      const tb = parseTimerange(b.timerange);
      return ta.start - tb.start;
    });
}

/**
 * Build a media playlist (m3u8) string from segments. Does NOT create a blob URL.
 *
 * Segment durations are derived from their individual timeranges. The playlist
 * target is the span from earliest start to latest end, unless an explicit
 * `totalDuration` is supplied. Any difference between that target and the sum
 * of segment durations is applied to the last segment, so contiguous segments
 * with non-zero absolute starts are not padded.
 *
 * When `totalDuration` is provided (e.g. from the union of video+audio spans
 * in a multi-flow), the last segment is extended so the playlist's EXTINF sum
 * exactly matches that target.  This ensures video and audio sub-playlists
 * report the same total to the HLS player.
 */
function buildMediaPlaylistContent(
  segments: FlowSegment[],
  totalDuration?: number,
): string | null {
  const playable = sortedPlayableSegments(segments);
  if (playable.length === 0) return null;

  // Compute the natural span of these segments (earliest start → latest end).
  let minStart = Infinity;
  let maxEnd = -Infinity;
  const parsed = playable.map((seg) => {
    const tr = parseTimerange(seg.timerange);
    if (tr.start < minStart) minStart = tr.start;
    if (tr.end > maxEnd) maxEnd = tr.end;
    return { seg, ...tr };
  });

  // Target duration: explicit override, or the natural span.
  const target = totalDuration ?? maxEnd - minStart;
  const naturalSum = parsed.reduce((acc, p) => acc + p.duration, 0);
  // Apply any target-vs-sum correction to the last segment so the total EXTINF
  // sum matches the target exactly.
  const correction = target - naturalSum;

  let maxSegDuration = 0;
  const entries: string[] = [];

  for (let i = 0; i < parsed.length; i++) {
    let dur = parsed[i].duration;
    // Apply correction to the last segment.
    if (i === parsed.length - 1 && correction !== 0) {
      dur = Math.max(0, dur + correction);
    }
    if (dur > maxSegDuration) maxSegDuration = dur;

    // Server already presigns against the public S3 endpoint, so the URL
    // is browser-reachable as returned.
    const url = parsed[i].seg.get_urls![0].url;
    entries.push(`#EXTINF:${dur.toFixed(6)},`);
    entries.push(url);
  }

  const targetSegDuration = Math.ceil(maxSegDuration);

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

/**
 * Build an HLS master playlist that merges video and audio streams
 * from separate TAMS child flows into a single playable manifest.
 *
 * Both sub-playlists are normalised to the same total duration (the union
 * of video and audio timeranges) so the player shows a consistent timeline.
 *
 * Returns a blob: URL for the master playlist plus an array of
 * sub-playlist blob URLs that must be revoked on cleanup.
 */
export function buildMultiFlowManifest(
  videoSegments: FlowSegment[],
  audioSegments: FlowSegment[],
): { masterUrl: string; subUrls: string[] } | null {
  const hasVideo = sortedPlayableSegments(videoSegments).length > 0;
  const hasAudio = sortedPlayableSegments(audioSegments).length > 0;

  if (!hasVideo && !hasAudio) return null;

  // Compute the union duration across both streams so playlists agree.
  const videoDur = computeSegmentsDuration(videoSegments);
  const audioDur = computeSegmentsDuration(audioSegments);
  const unionDuration = Math.max(videoDur, audioDur);

  // If only one type exists, return a simple playlist for it
  if (!hasVideo) {
    const content = buildMediaPlaylistContent(audioSegments);
    if (!content) return null;
    return { masterUrl: manifestToBlob(content), subUrls: [] };
  }
  if (!hasAudio) {
    const content = buildMediaPlaylistContent(videoSegments);
    if (!content) return null;
    return { masterUrl: manifestToBlob(content), subUrls: [] };
  }

  // Both video and audio: build sub-playlists with matching total duration
  const videoContent = buildMediaPlaylistContent(videoSegments, unionDuration);
  const audioContent = buildMediaPlaylistContent(audioSegments, unionDuration);

  if (!videoContent || !audioContent) return null;

  const subUrls: string[] = [];
  const videoBlobUrl = manifestToBlob(videoContent);
  const audioBlobUrl = manifestToBlob(audioContent);
  subUrls.push(videoBlobUrl, audioBlobUrl);

  const masterLines = [
    "#EXTM3U",
    "#EXT-X-VERSION:4",
    "",
    `#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",NAME="Audio",DEFAULT=YES,AUTOSELECT=YES,URI="${audioBlobUrl}"`,
    "",
    `#EXT-X-STREAM-INF:BANDWIDTH=2000000,AUDIO="audio"`,
    videoBlobUrl,
  ];

  const masterContent = masterLines.join("\n");
  const masterUrl = manifestToBlob(masterContent);

  return { masterUrl, subUrls };
}

/**
 * Build an HLS manifest for a single segment.
 * Returns a blob: URL or null if the segment has no playable URLs.
 */
export function buildSingleSegmentManifest(
  segment: FlowSegment,
): string | null {
  const content = buildMediaPlaylistContent([segment]);
  if (!content) return null;
  return manifestToBlob(content);
}

/**
 * Revoke a blob URL created by buildHlsManifest or buildMultiFlowManifest.
 */
export function revokeManifestUrl(url: string): void {
  if (url.startsWith("blob:")) {
    URL.revokeObjectURL(url);
  }
}
