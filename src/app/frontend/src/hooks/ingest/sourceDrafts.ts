import type { Source } from "@/types/tams";

const CODEC_MIME_MAP: Record<string, string> = {
  h264: "video/h264",
  h265: "video/h265",
  hevc: "video/h265",
  vp8: "video/vp8",
  vp9: "video/vp9",
  av1: "video/av1",
  mpeg2video: "video/mpeg",
  aac: "audio/aac",
  mp3: "audio/mpeg",
  opus: "audio/opus",
  vorbis: "audio/vorbis",
  flac: "audio/flac",
  pcm_s16le: "audio/x-raw-int",
  pcm_s24le: "audio/x-raw-int",
};

export const SOURCE_FORMAT = {
  audio: "urn:x-nmos:format:audio",
  multi: "urn:x-nmos:format:multi",
  video: "urn:x-nmos:format:video",
} as const;

type SourceFormat = (typeof SOURCE_FORMAT)[keyof typeof SOURCE_FORMAT];
export type TrackMode = "video" | "audio";

export interface SourceDraft {
  id: string;
  format: SourceFormat;
  label: string;
  description?: string;
}

export interface SourceMetadata {
  label?: string;
  description?: string;
}

export interface TrackSourceResolution {
  videoSourceId?: string;
  audioSourceId?: string;
  createParentMultiFlow: boolean;
  sourceMetadata: Record<string, SourceMetadata>;
}

export function codecToMime(
  codec: string | undefined,
  isVideo: boolean,
): string | undefined {
  if (!codec) return undefined;
  const mapped = CODEC_MIME_MAP[codec.toLowerCase()];
  if (mapped) return mapped;
  return `${isVideo ? "video" : "audio"}/${codec.toLowerCase()}`;
}

export function createIngestId(): string {
  return crypto.randomUUID();
}

export function existingChildSourceId(
  source: { id: string; source_collection?: Source["source_collection"] },
  role: TrackMode,
): string | undefined {
  return source.source_collection?.find(
    (item) => item.role === role && item.id !== source.id,
  )?.id;
}

export function sourceMetadata(
  source: Pick<Source, "label" | "description">,
): SourceMetadata {
  return {
    label: source.label,
    description: source.description,
  };
}
