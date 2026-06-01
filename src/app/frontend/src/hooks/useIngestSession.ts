import { useState, useCallback, useRef } from "react";
import { useApi } from "@/contexts/ApiContext";
import { ffmpegService, type ProbeResult } from "@/services/ffmpeg-service";
import type { FlowSegmentWrite, Source } from "@/types/tams";
import {
  sampleDurationNanoseconds,
  secondsToNanoseconds,
  timerangeFromNanoseconds,
  timestampFromNanoseconds,
} from "@/utils/tams-time";
import {
  SOURCE_FORMAT,
  codecToMime,
  createIngestId,
  existingChildSourceId,
  sourceMetadata,
  type SourceDraft,
  type SourceMetadata,
  type TrackMode,
  type TrackSourceResolution,
} from "@/hooks/ingest/sourceDrafts";

export type { SourceDraft } from "@/hooks/ingest/sourceDrafts";

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

function measuredDurationNanoseconds(probe: ProbeResult): bigint {
  return probe.durationNanoseconds ?? secondsToNanoseconds(probe.duration);
}

function ingestDurationNanoseconds(
  probe: ProbeResult,
  segmentCount: number,
  targetSegmentDurationNanoseconds: bigint,
): bigint {
  const measured = measuredDurationNanoseconds(probe);
  if (measured > 0n) return measured;
  return targetSegmentDurationNanoseconds * BigInt(segmentCount);
}

function segmentTimingNanoseconds(
  index: number,
  segmentCount: number,
  totalDurationNanoseconds: bigint,
  targetSegmentDurationNanoseconds: bigint,
): { startNanoseconds: bigint; endNanoseconds: bigint } {
  const useTargetDuration =
    targetSegmentDurationNanoseconds * BigInt(Math.max(segmentCount - 1, 0)) <
    totalDurationNanoseconds;

  const startNanoseconds = useTargetDuration
    ? targetSegmentDurationNanoseconds * BigInt(index)
    : (totalDurationNanoseconds * BigInt(index)) / BigInt(segmentCount);
  const endNanoseconds =
    index === segmentCount - 1
      ? totalDurationNanoseconds
      : useTargetDuration
        ? targetSegmentDurationNanoseconds * BigInt(index + 1)
        : (totalDurationNanoseconds * BigInt(index + 1)) / BigInt(segmentCount);

  if (endNanoseconds <= startNanoseconds) {
    return {
      startNanoseconds:
        targetSegmentDurationNanoseconds * BigInt(Math.max(index, 0)),
      endNanoseconds:
        targetSegmentDurationNanoseconds * BigInt(Math.max(index + 1, 1)),
    };
  }

  return { startNanoseconds, endNanoseconds };
}

function buildSegmentRegistration(
  objectId: string,
  mode: "video" | "audio",
  flowStartNanoseconds: bigint,
  flowEndNanoseconds: bigint,
  sourceProbe: ProbeResult,
): FlowSegmentWrite {
  const durationNanoseconds = flowEndNanoseconds - flowStartNanoseconds;
  const registration: FlowSegmentWrite = {
    object_id: objectId,
    timerange: timerangeFromNanoseconds(
      flowStartNanoseconds,
      flowEndNanoseconds,
    ),
    object_timerange: timerangeFromNanoseconds(0n, durationNanoseconds),
    ts_offset: timestampFromNanoseconds(flowStartNanoseconds),
  };

  const frameRate = mode === "video" ? sourceProbe.frameRate : undefined;
  const lastDuration = frameRate
    ? sampleDurationNanoseconds(frameRate)
    : undefined;
  if (lastDuration !== undefined) {
    registration.last_duration = timestampFromNanoseconds(lastDuration);
  }
  return registration;
}

interface AllocatedObject {
  objectId: string;
  storageId: string;
}

interface IngestSession {
  sourceId: string | null;
  segmentDuration: number;
  files: IngestFile[];
  running: boolean;
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
      id: createIngestId(),
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

  const cleanupAllocatedObjects = useCallback(
    async (allocatedObjects: AllocatedObject[]) => {
      await Promise.allSettled(
        allocatedObjects.map((object) =>
          api.deleteObjectInstance(object.objectId, {
            storage_id: object.storageId,
          }),
        ),
      );
    },
    [api],
  );

  const createTrackFlow = useCallback(
    async (
      file: File,
      sourceId: string,
      mode: "video" | "audio",
      probe: ProbeResult,
    ): Promise<string> => {
      const flowId = createIngestId();
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
        tags: {
          "tamoss-ingest": "managed-browser",
          "tamoss-ingest-timing": "source-derived",
        },
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

      return flowId;
    },
    [api],
  );

  const registerTrackSegments = useCallback(
    async (
      file: File,
      fileId: string,
      flowId: string,
      mode: "video" | "audio",
      probe: ProbeResult,
      segDuration: number,
      progressBase: number,
      progressShare: number,
      storageId?: string,
    ): Promise<void> => {
      updateFile(fileId, { status: "segmenting" });
      const blobs = await ffmpegService.segment(file, segDuration, mode);
      if (blobs.length === 0) {
        throw new Error(`Managed ingest produced no ${mode} segments.`);
      }

      // Allocate storage for all segments at once
      updateFile(fileId, { status: "uploading" });
      const objectIds = blobs.map(() => createIngestId());
      const segmentRegistrations: FlowSegmentWrite[] = [];
      const allocatedObjects: AllocatedObject[] = [];
      const targetSegmentDurationNanoseconds =
        secondsToNanoseconds(segDuration);
      if (targetSegmentDurationNanoseconds <= 0n) {
        throw new Error("Managed ingest requires a positive segment duration.");
      }
      const totalDurationNanoseconds = ingestDurationNanoseconds(
        probe,
        blobs.length,
        targetSegmentDurationNanoseconds,
      );

      try {
        const allocation = await api.allocateStorage(flowId, objectIds, {
          storageId,
        });
        const mediaObjects = allocation.media_objects;

        for (let i = 0; i < blobs.length; i++) {
          if (abortRef.current) throw new Error("Aborted");

          const obj = mediaObjects[i];
          const allocatedStorageId = obj.storage_id ?? storageId;
          if (!allocatedStorageId) {
            throw new Error(
              "Storage allocation did not include a storage backend.",
            );
          }
          allocatedObjects.push({
            objectId: obj.object_id,
            storageId: allocatedStorageId,
          });
          await api.uploadRaw(obj.put_url, blobs[i]);

          // Per-segment ffmpeg.wasm probes are full decode passes in the browser.
          // Use the source probe and generated segment count for registration timing.
          const timing = segmentTimingNanoseconds(
            i,
            blobs.length,
            totalDurationNanoseconds,
            targetSegmentDurationNanoseconds,
          );
          segmentRegistrations.push(
            buildSegmentRegistration(
              obj.object_id,
              mode,
              timing.startNanoseconds,
              timing.endNanoseconds,
              probe,
            ),
          );

          const pct = progressBase + ((i + 1) / blobs.length) * progressShare;
          updateFile(fileId, { progress: Math.round(pct) });
        }

        // Register all segments at once
        updateFile(fileId, { status: "registering" });
        if (segmentRegistrations.length > 0) {
          await api.addFlowSegments(flowId, segmentRegistrations);
        }
      } catch (err) {
        await cleanupAllocatedObjects(allocatedObjects);
        throw err;
      }
    },
    [api, cleanupAllocatedObjects, updateFile],
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

      const sourceId = createIngestId();
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
    async (sourceOverride?: string | SourceDraft, storageId?: string) => {
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
            videoFlowId = await createTrackFlow(
              ingestFile.file,
              trackSources.videoSourceId ?? sourceId,
              "video",
              probe,
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
            audioFlowId = await createTrackFlow(
              ingestFile.file,
              trackSources.audioSourceId ?? sourceId,
              "audio",
              probe,
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
            multiFlowId = createIngestId();
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

          if (probe.hasVideo && videoFlowId) {
            const share = hasBoth ? 45 : 90;
            await registerTrackSegments(
              ingestFile.file,
              ingestFile.id,
              videoFlowId,
              "video",
              probe,
              session.segmentDuration,
              5,
              share,
              storageId,
            );
          }

          if (probe.hasAudio && audioFlowId) {
            const base = hasBoth ? 50 : 5;
            const share = hasBoth ? 45 : 90;
            await registerTrackSegments(
              ingestFile.file,
              ingestFile.id,
              audioFlowId,
              "audio",
              probe,
              session.segmentDuration,
              base,
              share,
              storageId,
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
      createTrackFlow,
      registerTrackSegments,
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
