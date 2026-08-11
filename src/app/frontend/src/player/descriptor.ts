import type {
  FlowParams,
  FlowSegmentParams,
  TamossApiClient,
} from "@/api/client";
import type { ApiRequestOptions } from "@/api/transport";
import { type SanitizedMediaUrl, sanitizeMediaUrl } from "@/player/url-policy";
import type {
  Flow,
  FlowCollectionItem,
  FlowSegment,
  PaginatedResponse,
} from "@/types/tams";
import {
  timerangeFromNanoseconds,
  timestampFromNanoseconds,
} from "@/utils/tams-time";

const AUDIO_FORMAT = "urn:x-nmos:format:audio";
const DATA_FORMAT = "urn:x-nmos:format:data";
const IMAGE_FORMAT = "urn:x-tam:format:image";
const MULTI_FORMAT = "urn:x-nmos:format:multi";
const VIDEO_FORMAT = "urn:x-nmos:format:video";

const NANOS_PER_SECOND = 1_000_000_000n;
const DEFAULT_WINDOW_SECONDS = 10 * 60;
const MAX_COLLECTION_TRACKS = 16;
const MAX_SEGMENTS_PER_TRACK = 300;
const MAX_SEGMENTS_PER_DESCRIPTOR = 2_000;

export type PreviewTrackKind = "video" | "audio" | "image" | "data" | "muxed";

type FlowSegmentInitObject = NonNullable<FlowSegment["init_object"]>;

export type SanitizedInitObject = Omit<FlowSegmentInitObject, "get_urls"> & {
  get_urls: SanitizedMediaUrl[];
};

export type SanitizedFlowSegment = Omit<
  FlowSegment,
  "get_urls" | "init_object"
> & {
  get_urls: SanitizedMediaUrl[];
  init_object?: SanitizedInitObject;
};

export interface PreviewTrack {
  kind: PreviewTrackKind;
  role?: string;
  flow: Flow;
  segments: SanitizedFlowSegment[];
  truncated: boolean;
  rejectedUrlCount: number;
}

export interface MediaPreviewDescriptor {
  rootFlow: Flow;
  tracks: PreviewTrack[];
  video?: PreviewTrack;
  audio: PreviewTrack[];
  images: PreviewTrack[];
  data: PreviewTrack[];
  muxed?: PreviewTrack;
  initialTimerange: string;
  segmentCount: number;
  truncated: boolean;
  flowsSegments: Map<string, SanitizedFlowSegment[]>;
}

export interface BuildMediaPreviewDescriptorOptions {
  signal?: AbortSignal;
  pageLimit?: number;
  windowSeconds?: number;
  pageSegmentBudget?: number;
  locationOrigin?: string;
}

export type PreviewDescriptorErrorCode =
  | "collection-cycle"
  | "collection-too-large"
  | "duplicate-collection-member"
  | "empty-collection"
  | "invalid-timerange"
  | "missing-container"
  | "missing-media-url"
  | "nested-collection"
  | "too-many-video-tracks"
  | "unsupported-flow-format";

export class PreviewDescriptorError extends Error {
  constructor(
    public readonly code: PreviewDescriptorErrorCode,
    message: string,
    public readonly context: {
      flowId?: string;
      objectId?: string;
      trackKind?: PreviewTrackKind;
    } = {},
  ) {
    super(message);
    this.name = "PreviewDescriptorError";
  }
}

export interface PreviewDescriptorApi {
  getFlow(
    flowId: string,
    params?: FlowParams,
    options?: ApiRequestOptions,
  ): Promise<Flow>;
  getFlowCollection(
    flowId: string,
    options?: ApiRequestOptions,
  ): Promise<FlowCollectionItem[]>;
  getFlowSegments(
    flowId: string,
    params?: FlowSegmentParams,
    options?: ApiRequestOptions,
  ): Promise<PaginatedResponse<FlowSegment>>;
}

interface TrackCandidate {
  kind: PreviewTrackKind;
  flow: Flow;
  role?: string;
}

interface TimerangeBounds {
  start?: bigint;
  end: bigint;
  endInclusive: boolean;
  instantaneous: boolean;
}

function boundedPositiveInteger(
  value: number | undefined,
  fallback: number,
  maximum: number,
): number {
  if (value === undefined) return fallback;
  if (!Number.isSafeInteger(value) || value <= 0) return fallback;
  return Math.min(value, maximum);
}

function parseTimestamp(timestamp: string): bigint | undefined {
  const match = /^(-?)(\d+):(\d{1,9})$/u.exec(timestamp);
  if (!match) return undefined;
  const nanos = BigInt(match[3]);
  if (nanos >= NANOS_PER_SECOND) return undefined;
  const absolute = BigInt(match[2]) * NANOS_PER_SECOND + nanos;
  return match[1] === "-" ? -absolute : absolute;
}

function parseBoundedTimerange(timerange: string): TimerangeBounds | undefined {
  const instant = /^\[(-?\d+:\d{1,9})\]$/u.exec(timerange);
  if (instant) {
    const timestamp = parseTimestamp(instant[1]);
    return timestamp === undefined
      ? undefined
      : {
          start: timestamp,
          end: timestamp,
          endInclusive: true,
          instantaneous: true,
        };
  }

  const range = /^(?:(\[|\()(-?\d+:\d{1,9})?)?_(-?\d+:\d{1,9})(\)|\])$/u.exec(
    timerange,
  );
  if (!range || (range[1] && !range[2])) return undefined;
  const start = range[2] ? parseTimestamp(range[2]) : undefined;
  const end = parseTimestamp(range[3]);
  if (end === undefined || (range[2] && start === undefined)) return undefined;
  if (start !== undefined && start > end) return undefined;
  return {
    ...(start === undefined ? {} : { start }),
    end,
    endInclusive: range[4] === "]",
    instantaneous: false,
  };
}

function recentTimerange(flow: Flow, windowSeconds: number): string {
  if (!flow.timerange) {
    throw new PreviewDescriptorError(
      "invalid-timerange",
      "The preview Flow does not have a bounded timerange.",
      { flowId: flow.id },
    );
  }
  const bounds = parseBoundedTimerange(flow.timerange);
  if (!bounds) {
    throw new PreviewDescriptorError(
      "invalid-timerange",
      "The preview Flow does not have a valid bounded timerange.",
      { flowId: flow.id },
    );
  }

  const windowStart = bounds.end - BigInt(windowSeconds) * NANOS_PER_SECOND;
  const start =
    bounds.start !== undefined && bounds.start > windowStart
      ? bounds.start
      : windowStart;
  if (bounds.instantaneous) {
    return `[${timestampFromNanoseconds(bounds.end)}]`;
  }
  if (bounds.endInclusive) {
    return `[${timestampFromNanoseconds(start)}_${timestampFromNanoseconds(
      bounds.end,
    )}]`;
  }
  return timerangeFromNanoseconds(start, bounds.end);
}

function classifyFlow(flow: Flow, allowMuxed: boolean): PreviewTrackKind {
  switch (flow.format) {
    case VIDEO_FORMAT:
      return "video";
    case AUDIO_FORMAT:
      return "audio";
    case IMAGE_FORMAT:
      return "image";
    case DATA_FORMAT:
      return "data";
    case MULTI_FORMAT:
      if (allowMuxed && flow.container) return "muxed";
      break;
  }
  throw new PreviewDescriptorError(
    "unsupported-flow-format",
    "The Flow format is not supported by the media preview.",
    { flowId: flow.id },
  );
}

function assertImmediateChild(root: Flow, child: Flow): void {
  if (
    child.id === root.id ||
    child.flow_collection?.some((item) => item.id === root.id)
  ) {
    throw new PreviewDescriptorError(
      "collection-cycle",
      "The preview Flow collection contains a cycle.",
      { flowId: child.id },
    );
  }
  if (child.format === MULTI_FORMAT || child.flow_collection?.length) {
    throw new PreviewDescriptorError(
      "nested-collection",
      "Nested Flow collections are not supported by the media preview.",
      { flowId: child.id },
    );
  }
  if (!child.container) {
    throw new PreviewDescriptorError(
      "missing-container",
      "A preview track does not identify a media container.",
      { flowId: child.id },
    );
  }
}

async function resolveTrackCandidates(
  api: PreviewDescriptorApi,
  root: Flow,
  options: ApiRequestOptions,
): Promise<TrackCandidate[]> {
  if (root.container) {
    return [{ kind: classifyFlow(root, true), flow: root }];
  }
  if (root.format !== MULTI_FORMAT) {
    throw new PreviewDescriptorError(
      "missing-container",
      "The preview Flow does not identify a media container.",
      { flowId: root.id },
    );
  }

  const collection = await api.getFlowCollection(root.id, options);
  if (collection.length === 0) {
    throw new PreviewDescriptorError(
      "empty-collection",
      "The preview Flow collection is empty.",
      { flowId: root.id },
    );
  }
  if (collection.length > MAX_COLLECTION_TRACKS) {
    throw new PreviewDescriptorError(
      "collection-too-large",
      `The media preview supports at most ${MAX_COLLECTION_TRACKS} tracks.`,
      { flowId: root.id },
    );
  }

  const memberIds = new Set<string>();
  for (const member of collection) {
    if (member.id === root.id) {
      throw new PreviewDescriptorError(
        "collection-cycle",
        "The preview Flow collection contains a cycle.",
        { flowId: root.id },
      );
    }
    if (memberIds.has(member.id)) {
      throw new PreviewDescriptorError(
        "duplicate-collection-member",
        "The preview Flow collection contains the same Flow more than once.",
        { flowId: member.id },
      );
    }
    memberIds.add(member.id);
  }

  const children = await Promise.all(
    collection.map((member) =>
      api.getFlow(member.id, { include_timerange: true }, options),
    ),
  );
  const tracks = children.map((child, index) => {
    assertImmediateChild(root, child);
    return {
      kind: classifyFlow(child, false),
      flow: child,
      ...(collection[index].role ? { role: collection[index].role } : {}),
    };
  });
  if (tracks.filter((track) => track.kind === "video").length > 1) {
    throw new PreviewDescriptorError(
      "too-many-video-tracks",
      "The media preview supports at most one video track.",
      { flowId: root.id },
    );
  }
  return tracks;
}

function sanitizeSegments(
  track: TrackCandidate,
  segments: FlowSegment[],
  locationOrigin: string,
): { segments: SanitizedFlowSegment[]; rejectedUrlCount: number } {
  let rejectedUrlCount = 0;
  const sanitizedSegments = segments.map((segment) => {
    const sanitizeUrls = (
      candidates: NonNullable<FlowSegment["get_urls"]>,
    ): SanitizedMediaUrl[] =>
      candidates.flatMap((candidate) => {
        const sanitized = sanitizeMediaUrl(candidate, locationOrigin);
        if (!sanitized?.presigned) {
          rejectedUrlCount += 1;
          return [];
        }
        return [sanitized];
      });
    const sanitizedUrls = sanitizeUrls(segment.get_urls ?? []);
    if (sanitizedUrls.length === 0) {
      throw new PreviewDescriptorError(
        "missing-media-url",
        `A ${track.kind} track segment does not have an allowed media URL.`,
        {
          flowId: track.flow.id,
          objectId: segment.object_id,
          trackKind: track.kind,
        },
      );
    }
    const {
      get_urls: _unsafeUrls,
      init_object: unsafeInitObject,
      ...safeSegment
    } = segment;
    if (!unsafeInitObject) {
      return { ...safeSegment, get_urls: sanitizedUrls };
    }

    const sanitizedInitUrls = sanitizeUrls(unsafeInitObject.get_urls ?? []);
    if (sanitizedInitUrls.length === 0) {
      throw new PreviewDescriptorError(
        "missing-media-url",
        `A ${track.kind} track initialisation Object does not have an allowed media URL.`,
        {
          flowId: track.flow.id,
          objectId: unsafeInitObject.object_id,
          trackKind: track.kind,
        },
      );
    }
    const { get_urls: _unsafeInitUrls, ...safeInitObject } = unsafeInitObject;
    return {
      ...safeSegment,
      get_urls: sanitizedUrls,
      init_object: { ...safeInitObject, get_urls: sanitizedInitUrls },
    };
  });
  return { segments: sanitizedSegments, rejectedUrlCount };
}

export function descriptorMediaUrls(
  descriptor: MediaPreviewDescriptor,
): string[] {
  return descriptor.tracks.flatMap((track) =>
    track.segments.flatMap((segment) => [
      ...segment.get_urls.map((location) => location.url),
      ...(segment.init_object?.get_urls.map((location) => location.url) ?? []),
    ]),
  );
}

function currentLocationOrigin(): string {
  if (typeof window === "undefined" || !window.location?.origin) {
    throw new Error("The browser origin is unavailable for media URL policy.");
  }
  return window.location.origin;
}

export async function buildMediaPreviewDescriptor(
  api: PreviewDescriptorApi | TamossApiClient,
  rootFlowId: string,
  options: BuildMediaPreviewDescriptorOptions = {},
): Promise<MediaPreviewDescriptor> {
  const requestOptions = options.signal ? { signal: options.signal } : {};
  const rootFlow = await api.getFlow(
    rootFlowId,
    { include_timerange: true },
    requestOptions,
  );
  const windowSeconds = boundedPositiveInteger(
    options.windowSeconds,
    DEFAULT_WINDOW_SECONDS,
    DEFAULT_WINDOW_SECONDS,
  );
  const initialTimerange = recentTimerange(rootFlow, windowSeconds);
  const candidates = await resolveTrackCandidates(
    api,
    rootFlow,
    requestOptions,
  );
  const pageSegmentBudget = boundedPositiveInteger(
    options.pageSegmentBudget,
    MAX_SEGMENTS_PER_DESCRIPTOR,
    MAX_SEGMENTS_PER_DESCRIPTOR,
  );
  const requestedPageLimit = boundedPositiveInteger(
    options.pageLimit,
    MAX_SEGMENTS_PER_TRACK,
    MAX_SEGMENTS_PER_TRACK,
  );
  const pageLimit = Math.min(
    requestedPageLimit,
    Math.max(1, Math.floor(pageSegmentBudget / candidates.length)),
  );
  const locationOrigin = options.locationOrigin ?? currentLocationOrigin();

  const responses = await Promise.all(
    candidates.map((candidate) =>
      api.getFlowSegments(
        candidate.flow.id,
        {
          include_object_timerange: true,
          limit: pageLimit,
          presigned: true,
          timerange: initialTimerange,
          verbose_storage: true,
        },
        requestOptions,
      ),
    ),
  );

  const tracks = candidates.map((candidate, index): PreviewTrack => {
    const response = responses[index];
    const boundedSegments = response.data.slice(0, pageLimit);
    const sanitized = sanitizeSegments(
      candidate,
      boundedSegments,
      locationOrigin,
    );
    return {
      ...candidate,
      segments: sanitized.segments,
      rejectedUrlCount: sanitized.rejectedUrlCount,
      truncated:
        response.nextKey !== undefined || response.data.length >= pageLimit,
    };
  });
  const video = tracks.find((track) => track.kind === "video");
  const muxed = tracks.find((track) => track.kind === "muxed");
  const flowsSegments = new Map(
    tracks.map((track) => [track.flow.id, track.segments]),
  );

  return {
    rootFlow,
    tracks,
    ...(video ? { video } : {}),
    audio: tracks.filter((track) => track.kind === "audio"),
    images: tracks.filter((track) => track.kind === "image"),
    data: tracks.filter((track) => track.kind === "data"),
    ...(muxed ? { muxed } : {}),
    initialTimerange,
    segmentCount: tracks.reduce(
      (total, track) => total + track.segments.length,
      0,
    ),
    truncated: tracks.some((track) => track.truncated),
    flowsSegments,
  };
}
