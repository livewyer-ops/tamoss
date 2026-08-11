import type { PreviewTrack } from "@/player/descriptor";

const NANOS_PER_SECOND = 1_000_000_000n;
const SYNTHETIC_EPOCH_NANOSECONDS = 946_684_800n * NANOS_PER_SECOND;
const HLS_MIME_TYPE = "application/vnd.apple.mpegurl";

export interface ParsedTamsTimerange {
  startNanoseconds: bigint;
  endNanoseconds: bigint;
  durationNanoseconds: bigint;
}

export interface BlobUrlApi {
  createObjectURL(blob: Blob): string;
  revokeObjectURL(url: string): void;
}

export interface HlsPlaybackPlan {
  kind: "hls";
  url: string;
  mainUrl: string;
  audioSidecars: ReadonlyArray<{
    flowId: string;
    label: string;
    offsetSeconds: number;
    url: string;
  }>;
  trimmed: boolean;
  masterManifest: string;
  mediaManifests: ReadonlyMap<string, string>;
  dispose(): void;
}

export interface DirectPlaybackPlan {
  kind: "direct";
  url: string;
  mediaKind: "video" | "audio";
  mimeType: string;
  dispose(): void;
}

export type PlaybackPlan = HlsPlaybackPlan | DirectPlaybackPlan;

export type PlaybackPlanErrorCode =
  | "ambiguous-primary"
  | "invalid-timerange"
  | "invalid-url"
  | "missing-init-object"
  | "missing-url"
  | "no-common-media-window"
  | "no-playable-media"
  | "unsupported-container";

export class PlaybackPlanError extends Error {
  constructor(
    public readonly code: PlaybackPlanErrorCode,
    message: string,
  ) {
    super(message);
    this.name = "PlaybackPlanError";
  }
}

export class BlobUrlRegistry {
  private readonly urls = new Set<string>();

  constructor(private readonly urlApi: BlobUrlApi = URL) {}

  create(content: string, type = HLS_MIME_TYPE): string {
    const url = this.urlApi.createObjectURL(new Blob([content], { type }));
    this.urls.add(url);
    return url;
  }

  revokeAll(): void {
    for (const url of this.urls) this.urlApi.revokeObjectURL(url);
    this.urls.clear();
  }
}

interface TimedSegment {
  startNanoseconds: bigint;
  endNanoseconds: bigint;
  durationNanoseconds: bigint;
  timestampOffsetNanoseconds: bigint;
  url: string;
  initUrl?: string;
}

interface MasterTrack {
  bitrate: bigint;
  label: string;
  uri: string;
}

interface MasterManifestInput {
  primary?: MasterTrack;
  audio: MasterTrack[];
}

function fail(code: PlaybackPlanErrorCode, message: string): never {
  throw new PlaybackPlanError(code, message);
}

export function parseTamsTimestamp(timestamp: string): bigint {
  const match = /^(-?)(\d+):(\d{1,9})$/u.exec(timestamp);
  if (!match) {
    return fail("invalid-timerange", "A media timerange is invalid.");
  }
  const nanoseconds = BigInt(match[3]);
  if (nanoseconds >= NANOS_PER_SECOND) {
    return fail("invalid-timerange", "A media timerange is invalid.");
  }
  const absolute = BigInt(match[2]) * NANOS_PER_SECOND + nanoseconds;
  return match[1] === "-" ? -absolute : absolute;
}

export function parseTamsTimerange(timerange: string): ParsedTamsTimerange {
  const match = /^\[(-?\d+:\d{1,9})_(-?\d+:\d{1,9})\)$/u.exec(timerange);
  if (!match) {
    return fail(
      "invalid-timerange",
      "Media playback requires a bounded half-open timerange.",
    );
  }
  const startNanoseconds = parseTamsTimestamp(match[1]);
  const endNanoseconds = parseTamsTimestamp(match[2]);
  if (endNanoseconds <= startNanoseconds) {
    return fail(
      "invalid-timerange",
      "Media playback requires a positive timerange.",
    );
  }
  return {
    startNanoseconds,
    endNanoseconds,
    durationNanoseconds: endNanoseconds - startNanoseconds,
  };
}

function playableUrl(track: PreviewTrack, segmentIndex: number): string {
  const url = track.segments[segmentIndex]?.get_urls[0]?.url;
  if (!url) {
    return fail("missing-url", "A media segment has no playable URL.");
  }
  if (url.includes("\r") || url.includes("\n")) {
    return fail("invalid-url", "A media segment URL is invalid.");
  }
  try {
    const parsed = new URL(url);
    if (parsed.protocol !== "https:" && parsed.protocol !== "http:") {
      return fail("invalid-url", "A media segment URL is invalid.");
    }
  } catch {
    return fail("invalid-url", "A media segment URL is invalid.");
  }
  return url;
}

function initialisationUrl(
  track: PreviewTrack,
  segmentIndex: number,
): string | undefined {
  const url = track.segments[segmentIndex]?.init_object?.get_urls[0]?.url;
  if (!url) return undefined;
  if (url.includes("\r") || url.includes("\n") || url.includes('"')) {
    return fail("invalid-url", "An initialisation Object URL is invalid.");
  }
  try {
    const parsed = new URL(url);
    if (parsed.protocol !== "https:" && parsed.protocol !== "http:") {
      return fail("invalid-url", "An initialisation Object URL is invalid.");
    }
  } catch {
    return fail("invalid-url", "An initialisation Object URL is invalid.");
  }
  return url;
}

function timedSegments(track: PreviewTrack): TimedSegment[] {
  const parsed = track.segments.map((segment, index) => {
    const timerange = parseTamsTimerange(segment.timerange);
    const initUrl = initialisationUrl(track, index);
    return {
      ...timerange,
      timestampOffsetNanoseconds: segment.ts_offset
        ? parseTamsTimestamp(segment.ts_offset)
        : 0n,
      url: playableUrl(track, index),
      ...(initUrl ? { initUrl } : {}),
    };
  });
  parsed.sort((left, right) => {
    if (left.startNanoseconds < right.startNanoseconds) return -1;
    if (left.startNanoseconds > right.startNanoseconds) return 1;
    return 0;
  });
  for (let index = 1; index < parsed.length; index += 1) {
    if (parsed[index].startNanoseconds < parsed[index - 1].endNanoseconds) {
      fail("invalid-timerange", "Media segment timeranges overlap.");
    }
  }
  return parsed;
}

function decimalSeconds(nanoseconds: bigint): string {
  const whole = nanoseconds / NANOS_PER_SECOND;
  const fraction = (nanoseconds % NANOS_PER_SECOND)
    .toString()
    .padStart(9, "0")
    .replace(/0+$/u, "");
  return fraction ? `${whole}.${fraction}` : whole.toString();
}

function ceilSeconds(nanoseconds: bigint): bigint {
  return (nanoseconds + NANOS_PER_SECOND - 1n) / NANOS_PER_SECOND;
}

function floorDivision(dividend: bigint, divisor: bigint): bigint {
  const quotient = dividend / divisor;
  return dividend < 0n && dividend % divisor !== 0n ? quotient - 1n : quotient;
}

function programDateTime(
  segmentStartNanoseconds: bigint,
  initialStartNanoseconds: bigint,
): string {
  const synthetic =
    SYNTHETIC_EPOCH_NANOSECONDS +
    segmentStartNanoseconds -
    initialStartNanoseconds;
  const seconds = floorDivision(synthetic, NANOS_PER_SECOND);
  const fraction = synthetic - seconds * NANOS_PER_SECOND;
  const milliseconds = seconds * 1_000n;
  if (
    milliseconds < BigInt(Number.MIN_SAFE_INTEGER) ||
    milliseconds > BigInt(Number.MAX_SAFE_INTEGER)
  ) {
    return fail(
      "invalid-timerange",
      "A media timerange is outside the preview window.",
    );
  }
  const date = new Date(Number(milliseconds));
  if (!Number.isFinite(date.getTime())) {
    return fail(
      "invalid-timerange",
      "A media timerange is outside the preview window.",
    );
  }
  const iso = date.toISOString();
  return `${iso.slice(0, -5)}.${fraction.toString().padStart(9, "0")}Z`;
}

function isMpegTransportStream(container: string | undefined): boolean {
  return container?.toLowerCase().endsWith("/mp2t") ?? false;
}

function isMp4(container: string | undefined): boolean {
  return container?.toLowerCase().endsWith("/mp4") ?? false;
}

function trackBitrate(track: PreviewTrack): bigint {
  const kilobits = track.flow.max_bit_rate ?? track.flow.avg_bit_rate;
  return Number.isSafeInteger(kilobits) && (kilobits ?? 0) > 0
    ? BigInt(kilobits as number) * 1_000n
    : 1n;
}

function manifestLabel(value: string): string {
  return value
    .replace(/[\r\n]+/gu, " ")
    .replace(/"/gu, "'")
    .trim();
}

function trackLabel(track: PreviewTrack, fallback: string): string {
  return manifestLabel(track.role || track.flow.label || fallback) || fallback;
}

export function compileHlsMediaManifest(
  track: PreviewTrack,
  initialTimerange: string,
): string {
  const fragmentedMp4 = isMp4(track.flow.container);
  if (!isMpegTransportStream(track.flow.container) && !fragmentedMp4) {
    return fail(
      "unsupported-container",
      "HLS preview requires MPEG transport stream or fragmented MP4 segments.",
    );
  }
  const initialStart = parseTamsTimerange(initialTimerange).startNanoseconds;
  const segments = timedSegments(track);
  if (segments.length === 0) {
    return fail("no-playable-media", "No playable media segments were found.");
  }
  const targetDuration = segments.reduce(
    (maximum, segment) =>
      segment.durationNanoseconds > maximum
        ? segment.durationNanoseconds
        : maximum,
    0n,
  );
  const entries: string[] = [];
  let currentInitUrl: string | undefined;
  for (let index = 0; index < segments.length; index += 1) {
    const segment = segments[index];
    const previous = segments[index - 1];
    if (
      previous &&
      (previous.endNanoseconds !== segment.startNanoseconds ||
        previous.timestampOffsetNanoseconds !==
          segment.timestampOffsetNanoseconds)
    ) {
      entries.push("#EXT-X-DISCONTINUITY");
    }
    if (fragmentedMp4) {
      if (!segment.initUrl) {
        return fail(
          "missing-init-object",
          "Fragmented MP4 preview requires an initialisation Object for every media Object.",
        );
      }
      if (segment.initUrl !== currentInitUrl) {
        entries.push(`#EXT-X-MAP:URI="${segment.initUrl}"`);
        currentInitUrl = segment.initUrl;
      }
    }
    entries.push(
      `#EXT-X-PROGRAM-DATE-TIME:${programDateTime(
        segment.startNanoseconds,
        initialStart,
      )}`,
      `#EXTINF:${decimalSeconds(segment.durationNanoseconds)},`,
      segment.url,
    );
  }
  return [
    "#EXTM3U",
    `#EXT-X-VERSION:${fragmentedMp4 ? 7 : 3}`,
    `#EXT-X-TARGETDURATION:${ceilSeconds(targetDuration)}`,
    "#EXT-X-MEDIA-SEQUENCE:0",
    "#EXT-X-PLAYLIST-TYPE:VOD",
    ...entries,
    "#EXT-X-ENDLIST",
    "",
  ].join("\n");
}

export function compileHlsMasterManifest(input: MasterManifestInput): string {
  if (!input.primary && input.audio.length === 0) {
    return fail("no-playable-media", "No playable media tracks were found.");
  }
  const lines = ["#EXTM3U", "#EXT-X-VERSION:3"];
  if (input.primary) {
    const names = new Map<string, number>();
    for (const [index, track] of input.audio.entries()) {
      const occurrence = (names.get(track.label) ?? 0) + 1;
      names.set(track.label, occurrence);
      const name =
        occurrence === 1 ? track.label : `${track.label} (${occurrence})`;
      lines.push(
        `#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",NAME="${manifestLabel(
          name,
        )}",DEFAULT=${index === 0 ? "YES" : "NO"},AUTOSELECT=YES,URI="${
          track.uri
        }"`,
      );
    }
    const peakAudioBitrate = input.audio.reduce(
      (maximum, track) => (track.bitrate > maximum ? track.bitrate : maximum),
      0n,
    );
    lines.push(
      `#EXT-X-STREAM-INF:BANDWIDTH=${
        input.primary.bitrate + peakAudioBitrate
      }${input.audio.length > 0 ? ',AUDIO="audio"' : ""}`,
      input.primary.uri,
    );
  } else {
    for (const track of input.audio) {
      lines.push(`#EXT-X-STREAM-INF:BANDWIDTH=${track.bitrate}`, track.uri);
    }
  }
  return [...lines, ""].join("\n");
}

function mediaKind(track: PreviewTrack): "video" | "audio" {
  return track.kind === "audio" ? "audio" : "video";
}

function alignSplitEssenceTracks(tracks: PreviewTrack[]): {
  tracks: PreviewTrack[];
  trimmed: boolean;
} {
  const primary = tracks.find(
    (track) => track.kind === "video" || track.kind === "muxed",
  );
  const audio = tracks.filter((track) => track.kind === "audio");
  if (!primary || audio.length === 0) return { tracks, trimmed: false };

  const primarySegments = timedSegments(primary);
  const audioSegments = audio.map((track) => timedSegments(track));
  const latestStart = audioSegments.reduce(
    (latest, segments) =>
      segments[0].startNanoseconds > latest
        ? segments[0].startNanoseconds
        : latest,
    primarySegments[0].startNanoseconds,
  );
  const commonEnd = audioSegments.reduce(
    (earliest, segments) => {
      const end = segments[segments.length - 1].endNanoseconds;
      return end < earliest ? end : earliest;
    },
    primarySegments[primarySegments.length - 1].endNanoseconds,
  );
  const anchor = primarySegments.find(
    (segment) =>
      segment.startNanoseconds >= latestStart &&
      segment.startNanoseconds < commonEnd &&
      audioSegments.every((segments) =>
        segments.some(
          (audioSegment) =>
            audioSegment.startNanoseconds <= segment.startNanoseconds &&
            audioSegment.endNanoseconds > segment.startNanoseconds,
        ),
      ),
  );
  if (!anchor) {
    return fail(
      "no-common-media-window",
      "Video and audio tracks do not share a playable timerange.",
    );
  }

  const primaryWindow: TimedSegment[] = [];
  let primaryEnd = anchor.startNanoseconds;
  for (const segment of primarySegments) {
    if (segment.startNanoseconds < anchor.startNanoseconds) continue;
    if (
      segment.startNanoseconds !== primaryEnd ||
      segment.endNanoseconds > commonEnd
    ) {
      break;
    }
    primaryWindow.push(segment);
    primaryEnd = segment.endNanoseconds;
  }
  if (primaryWindow.length === 0) {
    return fail(
      "no-common-media-window",
      "Video and audio tracks do not share a complete media segment.",
    );
  }

  const aligned = tracks.map((track) => ({
    ...track,
    segments: track.segments.filter((segment) => {
      const timerange = parseTamsTimerange(segment.timerange);
      if (track === primary) {
        return (
          timerange.startNanoseconds >= anchor.startNanoseconds &&
          timerange.endNanoseconds <= primaryEnd
        );
      }
      return (
        timerange.endNanoseconds > anchor.startNanoseconds &&
        timerange.startNanoseconds < primaryEnd
      );
    }),
  }));
  if (
    aligned.some(
      (track) =>
        track.segments.length === 0 ||
        !continuouslyCovers(
          timedSegments(track),
          anchor.startNanoseconds,
          primaryEnd,
        ),
    )
  ) {
    return fail(
      "no-common-media-window",
      "Video and audio tracks do not continuously cover the same timerange.",
    );
  }
  return {
    tracks: aligned,
    trimmed: aligned.some(
      (track, index) => track.segments.length !== tracks[index].segments.length,
    ),
  };
}

function continuouslyCovers(
  segments: TimedSegment[],
  startNanoseconds: bigint,
  endNanoseconds: bigint,
): boolean {
  let coveredUntil = startNanoseconds;
  for (const segment of segments) {
    if (segment.endNanoseconds <= coveredUntil) continue;
    if (segment.startNanoseconds > coveredUntil) return false;
    coveredUntil = segment.endNanoseconds;
    if (coveredUntil >= endNanoseconds) return true;
  }
  return coveredUntil >= endNanoseconds;
}

export function compilePlaybackPlan(
  input: { tracks: readonly PreviewTrack[]; initialTimerange: string },
  urlApi: BlobUrlApi = URL,
): PlaybackPlan {
  parseTamsTimerange(input.initialTimerange);
  const mediaTracks = input.tracks.filter(
    (track) =>
      (track.kind === "video" ||
        track.kind === "audio" ||
        track.kind === "muxed") &&
      track.segments.length > 0,
  );
  if (mediaTracks.length === 0) {
    return fail("no-playable-media", "No playable media tracks were found.");
  }
  const primaries = mediaTracks.filter(
    (track) => track.kind === "video" || track.kind === "muxed",
  );
  if (primaries.length > 1) {
    return fail(
      "ambiguous-primary",
      "Media playback supports at most one primary video track.",
    );
  }

  const mp4Tracks = mediaTracks.filter((track) => isMp4(track.flow.container));
  if (
    mp4Tracks.length === 1 &&
    mediaTracks.length === 1 &&
    mp4Tracks[0].segments.length === 1 &&
    !mp4Tracks[0].segments[0].init_object
  ) {
    const track = mp4Tracks[0];
    timedSegments(track);
    const url = playableUrl(track, 0);
    return {
      kind: "direct",
      url,
      mediaKind: mediaKind(track),
      mimeType: track.flow.container as string,
      dispose() {},
    };
  }
  if (
    mp4Tracks.some((track) =>
      track.segments.some((segment) => !segment.init_object),
    )
  ) {
    return fail(
      "missing-init-object",
      "Fragmented MP4 preview requires an initialisation Object for every media Object.",
    );
  }
  if (
    mediaTracks.some(
      (track) =>
        !isMpegTransportStream(track.flow.container) &&
        !isMp4(track.flow.container),
    )
  ) {
    return fail(
      "unsupported-container",
      "The media container is not supported for preview.",
    );
  }

  const aligned = alignSplitEssenceTracks(mediaTracks);
  const hlsTracks = aligned.tracks;
  const hlsPrimaries = hlsTracks.filter(
    (track) => track.kind === "video" || track.kind === "muxed",
  );

  const registry = new BlobUrlRegistry(urlApi);
  try {
    const mediaManifests = new Map<string, string>();
    const trackUrls = new Map<PreviewTrack, string>();
    for (const track of hlsTracks) {
      const manifest = compileHlsMediaManifest(track, input.initialTimerange);
      mediaManifests.set(track.flow.id, manifest);
      trackUrls.set(track, registry.create(manifest));
    }
    const primary = hlsPrimaries[0];
    const audio = hlsTracks.filter((track) => track.kind === "audio");
    const masterManifest = compileHlsMasterManifest({
      ...(primary
        ? {
            primary: {
              bitrate: trackBitrate(primary),
              label: trackLabel(primary, "Video"),
              uri: trackUrls.get(primary) as string,
            },
          }
        : {}),
      audio: audio.map((track, index) => ({
        bitrate: trackBitrate(track),
        label: trackLabel(track, `Audio ${index + 1}`),
        uri: trackUrls.get(track) as string,
      })),
    });
    const url = registry.create(masterManifest);
    const primaryStart = primary
      ? timedSegments(primary)[0].startNanoseconds
      : undefined;
    let disposed = false;
    return {
      kind: "hls",
      url,
      mainUrl: trackUrls.get(primary ?? audio[0]) as string,
      audioSidecars:
        primary && primaryStart !== undefined
          ? audio.map((track, index) => ({
              flowId: track.flow.id,
              label: trackLabel(track, `Audio ${index + 1}`),
              offsetSeconds:
                Number(
                  primaryStart - timedSegments(track)[0].startNanoseconds,
                ) / 1_000_000_000,
              url: trackUrls.get(track) as string,
            }))
          : [],
      trimmed: aligned.trimmed,
      masterManifest,
      mediaManifests,
      dispose() {
        if (disposed) return;
        disposed = true;
        registry.revokeAll();
      },
    };
  } catch (error) {
    registry.revokeAll();
    throw error;
  }
}
