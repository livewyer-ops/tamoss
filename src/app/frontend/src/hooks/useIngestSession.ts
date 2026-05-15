import { useState, useCallback, useRef } from "react";
import { useApi } from "@/contexts/ApiContext";
import { ffmpegService, type ProbeResult } from "@/services/ffmpeg-service";
import type { FlowSegmentWrite, Source } from "@/types/tams";
import { buildTimerange } from "@/utils/hls-manifest";

export type IngestFileStatus =
  | "pending"
  | "probing"
  | "segmenting"
  | "uploading"
  | "registering"
  | "done"
  | "error";

export interface IngestFile {
  file: File;
  id: string;
  status: IngestFileStatus;
  error?: string;
  tracks: { hasVideo: boolean; hasAudio: boolean };
  progress: number;
  videoFlowId?: string;
  audioFlowId?: string;
  multiFlowId?: string;
}

interface IngestSession {
  sourceId: string | null;
  segmentDuration: number;
  files: IngestFile[];
  running: boolean;
}

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

const SOURCE_FORMAT = {
  audio: "urn:x-nmos:format:audio",
  multi: "urn:x-nmos:format:multi",
  video: "urn:x-nmos:format:video",
} as const;

type SourceFormat = (typeof SOURCE_FORMAT)[keyof typeof SOURCE_FORMAT];
type TrackMode = "video" | "audio";

export interface SourceDraft {
  id: string;
  format: SourceFormat;
  label: string;
  description?: string;
}

interface SourceMetadata {
  label?: string;
  description?: string;
}

interface TrackSourceResolution {
  videoSourceId?: string;
  audioSourceId?: string;
  createParentMultiFlow: boolean;
  sourceMetadata: Record<string, SourceMetadata>;
}

function codecToMime(
  codec: string | undefined,
  isVideo: boolean,
): string | undefined {
  if (!codec) return undefined;
  const mapped = CODEC_MIME_MAP[codec.toLowerCase()];
  if (mapped) return mapped;
  return `${isVideo ? "video" : "audio"}/${codec.toLowerCase()}`;
}

function uuid(): string {
  return crypto.randomUUID();
}

function existingChildSourceId(
  source: { id: string; source_collection?: Source["source_collection"] },
  role: TrackMode,
): string | undefined {
  return source.source_collection?.find(
    (item) => item.role === role && item.id !== source.id,
  )?.id;
}

function sourceMetadata(
  source: Pick<Source, "label" | "description">,
): SourceMetadata {
  return {
    label: source.label,
    description: source.description,
  };
}

export function useIngestSession() {
  const api = useApi();
  const [session, setSession] = useState<IngestSession>({
    sourceId: null,
    segmentDuration: 6,
    files: [],
    running: false,
  });
  const abortRef = useRef(false);

  const updateFile = useCallback((id: string, updates: Partial<IngestFile>) => {
    setSession((prev) => ({
      ...prev,
      files: prev.files.map((f) => (f.id === id ? { ...f, ...updates } : f)),
    }));
  }, []);

  const addFiles = useCallback((files: File[]) => {
    const newFiles: IngestFile[] = files.map((file) => ({
      file,
      id: uuid(),
      status: "pending" as const,
      tracks: { hasVideo: false, hasAudio: false },
      progress: 0,
    }));
    setSession((prev) => ({ ...prev, files: [...prev.files, ...newFiles] }));
  }, []);

  const removeFile = useCallback((id: string) => {
    setSession((prev) => ({
      ...prev,
      files: prev.files.filter((f) => f.id !== id),
    }));
  }, []);

  const setSourceId = useCallback((sourceId: string | null) => {
    setSession((prev) => ({ ...prev, sourceId }));
  }, []);

  const setSegmentDuration = useCallback((dur: number) => {
    setSession((prev) => ({ ...prev, segmentDuration: Math.max(1, dur) }));
  }, []);

  const reset = useCallback(() => {
    abortRef.current = true;
    setSession({
      sourceId: null,
      segmentDuration: 6,
      files: [],
      running: false,
    });
  }, []);

  const processTrack = useCallback(
    async (
      file: File,
      fileId: string,
      sourceId: string,
      mode: "video" | "audio",
      probe: ProbeResult,
      segDuration: number,
      progressBase: number,
      progressShare: number,
    ): Promise<string> => {
      const flowId = uuid();
      const isVideo = mode === "video";

      const format = isVideo
        ? "urn:x-nmos:format:video"
        : "urn:x-nmos:format:audio";
      const rawCodec = isVideo ? probe.videoCodec : probe.audioCodec;
      const codec = codecToMime(rawCodec, isVideo);

      await api.createFlow(flowId, {
        id: flowId,
        source_id: sourceId,
        format,
        codec,
        container: "video/mp2t",
        label: `${file.name} (${mode})`,
        ...(isVideo && probe.width && probe.height
          ? {
              essence_parameters: {
                frame_width: probe.width,
                frame_height: probe.height,
                ...(probe.frameRate ? { frame_rate: probe.frameRate } : {}),
              },
            }
          : {}),
        ...(!isVideo && probe.sampleRate
          ? {
              essence_parameters: {
                sample_rate: probe.sampleRate,
                channels: probe.channels,
              },
            }
          : {}),
      });

      // Segment the file
      updateFile(fileId, { status: "segmenting" });
      const blobs = await ffmpegService.segment(file, segDuration, mode);

      // Allocate storage for all segments at once
      updateFile(fileId, { status: "uploading" });
      const objectIds = blobs.map(() => uuid());
      const allocation = await api.allocateStorage(flowId, objectIds);
      const mediaObjects = allocation.media_objects;

      // Upload each segment
      const segmentRegistrations: FlowSegmentWrite[] = [];

      for (let i = 0; i < blobs.length; i++) {
        if (abortRef.current) throw new Error("Aborted");

        const obj = mediaObjects[i];
        await api.uploadRaw(obj.put_url, blobs[i]);

        const startSecs = i * segDuration;
        const endSecs = Math.min((i + 1) * segDuration, probe.duration);
        const timerange = buildTimerange(startSecs, endSecs);

        segmentRegistrations.push({ object_id: obj.object_id, timerange });

        const pct = progressBase + ((i + 1) / blobs.length) * progressShare;
        updateFile(fileId, { progress: Math.round(pct) });
      }

      // Register all segments at once
      updateFile(fileId, { status: "registering" });
      if (segmentRegistrations.length > 0) {
        await api.addFlowSegments(flowId, segmentRegistrations);
      }

      return flowId;
    },
    [api, updateFile],
  );

  const ensureChildSource = useCallback(
    (
      parent: Source | SourceDraft,
      role: TrackMode,
      fileName: string,
    ): {
      sourceId: string;
      metadata?: SourceMetadata;
    } => {
      const existing = existingChildSourceId(parent, role);
      if (existing) return { sourceId: existing };

      const sourceId = uuid();
      const baseLabel = parent.label?.trim() || fileName;
      return {
        sourceId,
        metadata: {
          label: `${baseLabel} (${role})`,
          description: parent.description,
        },
      };
    },
    [],
  );

  const resolveTrackSourceIds = useCallback(
    async (
      source: string | SourceDraft,
      fileName: string,
      probe: ProbeResult,
    ): Promise<TrackSourceResolution> => {
      const parentSourceId = typeof source === "string" ? source : source.id;
      const parent =
        typeof source === "string"
          ? await api.getSource(parentSourceId)
          : source;
      const isMultiSource = parent.format === SOURCE_FORMAT.multi;
      const metadata: Record<string, SourceMetadata> = {};

      if (typeof source !== "string") {
        metadata[parentSourceId] = sourceMetadata(source);
      }

      if (!isMultiSource) {
        if (parent.format === SOURCE_FORMAT.video && probe.hasAudio) {
          throw new Error(
            "Selected source is video-only, but the upload contains audio. Use a Multi source for audio/video uploads.",
          );
        }
        if (parent.format === SOURCE_FORMAT.audio && probe.hasVideo) {
          throw new Error(
            "Selected source is audio-only, but the upload contains video. Use a Multi source for audio/video uploads.",
          );
        }
        return {
          videoSourceId: probe.hasVideo ? parentSourceId : undefined,
          audioSourceId: probe.hasAudio ? parentSourceId : undefined,
          createParentMultiFlow: false,
          sourceMetadata: metadata,
        };
      }

      const videoChild = probe.hasVideo
        ? ensureChildSource(parent, "video", fileName)
        : undefined;
      const audioChild = probe.hasAudio
        ? ensureChildSource(parent, "audio", fileName)
        : undefined;

      if (videoChild?.metadata) {
        metadata[videoChild.sourceId] = videoChild.metadata;
      }
      if (audioChild?.metadata) {
        metadata[audioChild.sourceId] = audioChild.metadata;
      }

      return {
        videoSourceId: videoChild?.sourceId,
        audioSourceId: audioChild?.sourceId,
        createParentMultiFlow: true,
        sourceMetadata: metadata,
      };
    },
    [api, ensureChildSource],
  );

  const applySourceMetadata = useCallback(
    async (
      sourceId: string | undefined,
      metadata: SourceMetadata | undefined,
      applied: Set<string>,
    ): Promise<void> => {
      if (!sourceId || !metadata || applied.has(sourceId)) return;

      const label = metadata.label?.trim();
      if (label) {
        await api.updateSourceLabel(sourceId, label);
      }

      const description = metadata.description?.trim();
      if (description) {
        await api.updateSourceDescription(sourceId, description);
      }

      applied.add(sourceId);
    },
    [api],
  );

  const startIngest = useCallback(
    async (sourceOverride?: string | SourceDraft) => {
      abortRef.current = false;
      const source = sourceOverride ?? session.sourceId;
      if (!source) {
        throw new Error("Select or create a source before starting ingest.");
      }
      const sourceId = typeof source === "string" ? source : source.id;
      setSession((prev) => ({ ...prev, running: true }));

      const filesToProcess = session.files.filter(
        (f) => f.status === "pending",
      );
      const appliedSourceMetadata = new Set<string>();

      for (const ingestFile of filesToProcess) {
        if (abortRef.current) break;

        try {
          // Probe
          updateFile(ingestFile.id, { status: "probing", progress: 0 });
          const probe = await ffmpegService.probe(ingestFile.file);
          updateFile(ingestFile.id, {
            tracks: {
              hasVideo: probe.hasVideo,
              hasAudio: probe.hasAudio,
            },
          });

          if (!probe.hasVideo && !probe.hasAudio) {
            updateFile(ingestFile.id, {
              status: "error",
              error: "No video or audio tracks detected",
            });
            continue;
          }

          const hasBoth = probe.hasVideo && probe.hasAudio;
          const trackSources = await resolveTrackSourceIds(
            source,
            ingestFile.file.name,
            probe,
          );
          // Split progress: probing=5%, segmenting+uploading per track, registering=5%
          // For dual track: video gets 45%, audio gets 45%, probe+register=10%
          // For single track: track gets 90%, probe+register=10%

          let videoFlowId: string | undefined;
          let audioFlowId: string | undefined;

          if (probe.hasVideo) {
            const share = hasBoth ? 45 : 90;
            videoFlowId = await processTrack(
              ingestFile.file,
              ingestFile.id,
              trackSources.videoSourceId ?? sourceId,
              "video",
              probe,
              session.segmentDuration,
              5,
              share,
            );
            updateFile(ingestFile.id, { videoFlowId });
            await applySourceMetadata(
              trackSources.videoSourceId ?? sourceId,
              trackSources.sourceMetadata[
                trackSources.videoSourceId ?? sourceId
              ],
              appliedSourceMetadata,
            );
          }

          if (probe.hasAudio) {
            const base = hasBoth ? 50 : 5;
            const share = hasBoth ? 45 : 90;
            audioFlowId = await processTrack(
              ingestFile.file,
              ingestFile.id,
              trackSources.audioSourceId ?? sourceId,
              "audio",
              probe,
              session.segmentDuration,
              base,
              share,
            );
            updateFile(ingestFile.id, { audioFlowId });
            await applySourceMetadata(
              trackSources.audioSourceId ?? sourceId,
              trackSources.sourceMetadata[
                trackSources.audioSourceId ?? sourceId
              ],
              appliedSourceMetadata,
            );
          }

          // Parent multi flows collect the mono-essence child flows; the API
          // derives the matching Source source_collection metadata from this.
          let multiFlowId: string | undefined;
          if (
            trackSources.createParentMultiFlow &&
            (videoFlowId || audioFlowId)
          ) {
            multiFlowId = uuid();
            await api.createFlow(multiFlowId, {
              id: multiFlowId,
              source_id: sourceId,
              format: "urn:x-nmos:format:multi",
              label: `${ingestFile.file.name} (multi)`,
            });
            const flowCollection = [
              videoFlowId ? { id: videoFlowId, role: "video" } : null,
              audioFlowId ? { id: audioFlowId, role: "audio" } : null,
            ].filter(
              (item): item is { id: string; role: string } => item !== null,
            );
            await api.setFlowCollection(multiFlowId, flowCollection);
            updateFile(ingestFile.id, { multiFlowId });
            await applySourceMetadata(
              sourceId,
              trackSources.sourceMetadata[sourceId],
              appliedSourceMetadata,
            );
          }

          updateFile(ingestFile.id, { status: "done", progress: 100 });
        } catch (err) {
          const message = err instanceof Error ? err.message : "Unknown error";
          updateFile(ingestFile.id, { status: "error", error: message });
        }
      }

      setSession((prev) => ({ ...prev, running: false }));
    },
    [
      api,
      session.files,
      session.sourceId,
      session.segmentDuration,
      updateFile,
      processTrack,
      resolveTrackSourceIds,
      applySourceMetadata,
    ],
  );

  return {
    session,
    addFiles,
    removeFile,
    setSourceId,
    setSegmentDuration,
    startIngest,
    reset,
  };
}
