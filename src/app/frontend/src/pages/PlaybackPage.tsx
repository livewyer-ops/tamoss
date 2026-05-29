import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useApi } from "@/contexts/ApiContext";
import { useApiQuery } from "@/hooks/useApiQuery";
import { usePageTitle } from "@/hooks/usePageTitle";
import {
  buildHlsManifest,
  buildMultiFlowManifest,
  computeSegmentsDuration,
  parseTimerange,
  revokeManifestUrl,
} from "@/utils/hls-manifest";
import LoadingSpinner from "@/components/LoadingSpinner";
import ErrorMessage from "@/components/ErrorMessage";
import Badge from "@/components/Badge";
import CopyButton from "@/components/CopyButton";
import StorageBackendSelector from "@/components/StorageBackendSelector";
import {
  formatCodec,
  formatFormat,
  formatFrameRate,
  formatResolution,
  truncateId,
} from "@/utils/format";
import type { Flow, FlowCollectionItem, FlowSegment } from "@/types/tams";

type PlaybackState = "idle" | "loading" | "ready" | "unsupported" | "error";
type PlaybackSegment = FlowSegment & {
  playbackFlowId?: string;
  playbackRole?: string;
};
type MultiPlaybackChild = FlowCollectionItem & {
  flow: Flow | null;
  segments: FlowSegment[];
};

const MULTI_FORMAT = "urn:x-nmos:format:multi";
const VIDEO_FORMAT = "urn:x-nmos:format:video";
const AUDIO_FORMAT = "urn:x-nmos:format:audio";
const EMPTY_SEGMENTS: FlowSegment[] = [];
const EMPTY_CHILDREN: MultiPlaybackChild[] = [];
const PLAYBACK_FLOW_PAGE_SIZE = "300";
const PLAYBACK_SEGMENT_PAGE_SIZE = "300";
const AUDIO_SYNC_TOLERANCE_SECONDS = 0.35;

type HlsModule = typeof import("hls.js");

let hlsModulePromise: Promise<HlsModule> | null = null;

function loadHlsModule(): Promise<HlsModule> {
  hlsModulePromise ??= import("hls.js").catch((err: unknown) => {
    hlsModulePromise = null;
    throw err;
  });
  return hlsModulePromise;
}

function firstPlayableUrl(segment: FlowSegment): string | null {
  return segment.get_urls?.[0]?.url ?? null;
}

function playableSegments(segments: FlowSegment[]): FlowSegment[] {
  return segments.filter((segment) => firstPlayableUrl(segment));
}

function roleOrFormatMatches(
  child: MultiPlaybackChild,
  targetRole: "video" | "audio",
  targetFormat: string,
): boolean {
  const role = child.role?.toLowerCase() ?? "";
  if (role.includes(targetRole)) return true;
  return child.flow?.format === targetFormat;
}

function flowOptionLabel(flow: Flow): string {
  const label = flow.label || truncateId(flow.id);
  const technical = [
    flow.format ? formatFormat(flow.format) : null,
    flow.codec ? formatCodec(flow.codec) : null,
  ]
    .filter(Boolean)
    .join(" / ");
  return technical ? `${label} (${technical})` : label;
}

export default function PlaybackPage() {
  usePageTitle("Playback");
  const api = useApi();
  const videoRef = useRef<HTMLVideoElement>(null);
  const audioRef = useRef<HTMLAudioElement>(null);
  const hlsRef = useRef<{ destroy: () => void } | null>(null);
  const audioHlsRef = useRef<{ destroy: () => void } | null>(null);
  const manifestRef = useRef<string[]>([]);
  const [searchParams, setSearchParams] = useSearchParams();
  const activeFlowId = searchParams.get("flow") ?? "";
  const activeStorageId = searchParams.get("storage") ?? "";
  const [selectedFlowId, setSelectedFlowId] = useState(activeFlowId);
  const [selectedStorageId, setSelectedStorageId] = useState(activeStorageId);
  const [manifestUrl, setManifestUrl] = useState<string | null>(null);
  const [audioManifestUrl, setAudioManifestUrl] = useState<string | null>(null);
  const [playbackState, setPlaybackState] = useState<PlaybackState>("idle");
  const [playbackError, setPlaybackError] = useState<string | null>(null);

  useEffect(() => {
    setSelectedFlowId(activeFlowId);
  }, [activeFlowId]);

  useEffect(() => {
    setSelectedStorageId(activeStorageId);
  }, [activeStorageId]);

  const flows = useApiQuery(
    () => api.getFlows({ limit: PLAYBACK_FLOW_PAGE_SIZE }),
    [api],
  );

  const storageBackends = useApiQuery(() => api.getStorageBackends(), [api]);

  const flow = useApiQuery(
    () => (activeFlowId ? api.getFlow(activeFlowId) : Promise.resolve(null)),
    [api, activeFlowId],
  );

  const segmentQuery = useApiQuery(
    () =>
      activeFlowId && flow.data && flow.data.format !== MULTI_FORMAT
        ? api.getFlowSegments(activeFlowId, {
            ...(activeStorageId ? { accept_storage_ids: activeStorageId } : {}),
            include_object_timerange: true,
            limit: PLAYBACK_SEGMENT_PAGE_SIZE,
            presigned: "true",
            verbose_storage: true,
          })
        : Promise.resolve({ data: [] as FlowSegment[], nextKey: undefined }),
    [api, activeFlowId, activeStorageId, flow.data?.format],
  );

  const multiPlaybackQuery = useApiQuery(async () => {
    if (!activeFlowId || flow.data?.format !== MULTI_FORMAT) {
      return { children: [] as MultiPlaybackChild[] };
    }

    const collection = await api.getFlowCollection(activeFlowId);
    const children = await Promise.all(
      collection.map(async (item) => {
        const [childFlow, childSegments] = await Promise.all([
          api.getFlow(item.id).catch(() => null),
          api.getFlowSegments(item.id, {
            ...(activeStorageId ? { accept_storage_ids: activeStorageId } : {}),
            include_object_timerange: true,
            limit: PLAYBACK_SEGMENT_PAGE_SIZE,
            presigned: "true",
            verbose_storage: true,
          }),
        ]);
        return {
          ...item,
          flow: childFlow,
          segments: childSegments.data,
        };
      }),
    );

    return { children };
  }, [api, activeFlowId, activeStorageId, flow.data?.format]);

  const selectedFlow = flow.data;
  const isMultiFlow = selectedFlow?.format === MULTI_FORMAT;
  const singleSegments = segmentQuery.data?.data ?? EMPTY_SEGMENTS;
  const multiChildren = multiPlaybackQuery.data?.children ?? EMPTY_CHILDREN;
  const videoSegments = useMemo<FlowSegment[]>(
    () =>
      multiChildren
        .filter((child) => roleOrFormatMatches(child, "video", VIDEO_FORMAT))
        .flatMap((child) => child.segments),
    [multiChildren],
  );
  const audioSegments = useMemo<FlowSegment[]>(
    () =>
      multiChildren
        .filter((child) => roleOrFormatMatches(child, "audio", AUDIO_FORMAT))
        .flatMap((child) => child.segments),
    [multiChildren],
  );
  const segments = useMemo<PlaybackSegment[]>(() => {
    if (!isMultiFlow) return singleSegments;
    return multiChildren.flatMap((child) =>
      child.segments.map((segment) => ({
        ...segment,
        playbackFlowId: child.id,
        playbackRole: child.role ?? child.flow?.format,
      })),
    );
  }, [isMultiFlow, multiChildren, singleSegments]);
  const playable = playableSegments(segments);
  const totalDuration = computeSegmentsDuration(segments);

  useEffect(() => {
    if (manifestRef.current.length > 0) {
      manifestRef.current.forEach(revokeManifestUrl);
      manifestRef.current = [];
    }
    setManifestUrl(null);
    setAudioManifestUrl(null);
    setPlaybackState(activeFlowId ? "loading" : "idle");
    setPlaybackError(null);

    const loadingSegments = isMultiFlow
      ? multiPlaybackQuery.loading
      : segmentQuery.loading;
    const segmentError = isMultiFlow
      ? multiPlaybackQuery.error
      : segmentQuery.error;
    if (
      !activeFlowId ||
      flow.loading ||
      loadingSegments ||
      flow.error ||
      segmentError
    ) {
      return;
    }

    const nextManifest = isMultiFlow
      ? buildMultiFlowManifest(videoSegments, audioSegments)
      : buildHlsManifest(segments, selectedFlow?.codec ?? undefined);
    const nextManifestUrl =
      typeof nextManifest === "string"
        ? nextManifest
        : nextManifest?.primaryUrl;
    if (!nextManifest || !nextManifestUrl) {
      setPlaybackState("idle");
      return;
    }

    const nextManifestUrls =
      typeof nextManifest === "string" ? [nextManifest] : nextManifest.urls;
    manifestRef.current = nextManifestUrls;
    setManifestUrl(nextManifestUrl);
    setAudioManifestUrl(
      typeof nextManifest === "string" ? null : nextManifest.audioUrl,
    );

    return () => {
      nextManifestUrls.forEach(revokeManifestUrl);
      manifestRef.current = manifestRef.current.filter(
        (url) => !nextManifestUrls.includes(url),
      );
    };
  }, [
    activeFlowId,
    audioSegments,
    flow.error,
    flow.loading,
    isMultiFlow,
    multiPlaybackQuery.error,
    multiPlaybackQuery.loading,
    segmentQuery.error,
    segmentQuery.loading,
    segments,
    selectedFlow?.codec,
    videoSegments,
  ]);

  useEffect(() => {
    const video = videoRef.current;
    if (!video || !manifestUrl) return;
    const activeVideo = video;
    const activeManifestUrl = manifestUrl;

    let cancelled = false;
    let nativeLoadedHandler: (() => void) | null = null;
    setPlaybackState("loading");
    setPlaybackError(null);

    async function attachPlayer() {
      try {
        const { default: Hls } = await loadHlsModule();
        if (cancelled) return;

        if (hlsRef.current) {
          hlsRef.current.destroy();
          hlsRef.current = null;
        }

        if (Hls.isSupported()) {
          const hls = new Hls({ enableWorker: true });
          hlsRef.current = hls;

          hls.on(Hls.Events.MANIFEST_PARSED, () => {
            if (!cancelled) setPlaybackState("ready");
          });
          hls.on(
            Hls.Events.ERROR,
            (
              _event,
              data: { fatal: boolean; type?: string; details?: string },
            ) => {
              if (!data.fatal || cancelled) return;
              setPlaybackState("error");
              setPlaybackError(
                `HLS error: ${data.type ?? "unknown"} ${data.details ?? ""}`.trim(),
              );
              hls.destroy();
              hlsRef.current = null;
            },
          );
          hls.loadSource(activeManifestUrl);
          hls.attachMedia(activeVideo);
          return;
        }

        if (activeVideo.canPlayType("application/vnd.apple.mpegurl")) {
          nativeLoadedHandler = () => {
            if (!cancelled) setPlaybackState("ready");
          };
          activeVideo.addEventListener("loadedmetadata", nativeLoadedHandler, {
            once: true,
          });
          activeVideo.src = activeManifestUrl;
          return;
        }

        setPlaybackState("unsupported");
        setPlaybackError("This browser cannot play HLS previews.");
      } catch (err) {
        if (!cancelled) {
          setPlaybackState("error");
          setPlaybackError(
            err instanceof Error
              ? err.message
              : "Playback initialisation failed",
          );
        }
      }
    }

    attachPlayer();

    return () => {
      cancelled = true;
      if (nativeLoadedHandler) {
        activeVideo.removeEventListener("loadedmetadata", nativeLoadedHandler);
      }
      if (hlsRef.current) {
        hlsRef.current.destroy();
        hlsRef.current = null;
      }
      activeVideo.removeAttribute("src");
    };
  }, [manifestUrl]);

  useEffect(() => {
    const audio = audioRef.current;
    if (!audio || !audioManifestUrl) return;
    const activeAudio = audio;
    const activeManifestUrl = audioManifestUrl;

    let cancelled = false;

    async function attachAudioPlayer() {
      try {
        const { default: Hls } = await loadHlsModule();
        if (cancelled) return;

        if (audioHlsRef.current) {
          audioHlsRef.current.destroy();
          audioHlsRef.current = null;
        }

        if (Hls.isSupported()) {
          const hls = new Hls({ enableWorker: true });
          audioHlsRef.current = hls;

          hls.on(
            Hls.Events.ERROR,
            (
              _event,
              data: { fatal: boolean; type?: string; details?: string },
            ) => {
              if (!data.fatal || cancelled) return;
              setPlaybackError(
                `Audio HLS error: ${data.type ?? "unknown"} ${
                  data.details ?? ""
                }`.trim(),
              );
              hls.destroy();
              audioHlsRef.current = null;
            },
          );
          hls.loadSource(activeManifestUrl);
          hls.attachMedia(activeAudio);
          return;
        }

        if (activeAudio.canPlayType("application/vnd.apple.mpegurl")) {
          activeAudio.src = activeManifestUrl;
        }
      } catch (err) {
        if (!cancelled) {
          setPlaybackError(
            err instanceof Error
              ? err.message
              : "Audio playback initialisation failed",
          );
        }
      }
    }

    attachAudioPlayer();

    return () => {
      cancelled = true;
      if (audioHlsRef.current) {
        audioHlsRef.current.destroy();
        audioHlsRef.current = null;
      }
      if (!activeAudio.paused) {
        activeAudio.pause();
      }
      activeAudio.removeAttribute("src");
    };
  }, [audioManifestUrl]);

  useEffect(() => {
    const video = videoRef.current;
    const audio = audioRef.current;
    if (!video || !audio || !audioManifestUrl) return;

    const activeVideo = video;
    const activeAudio = audio;

    function syncAudioTime(force = false) {
      const videoTime = activeVideo.currentTime;
      const audioTime = activeAudio.currentTime;
      if (!Number.isFinite(videoTime) || !Number.isFinite(audioTime)) return;
      if (
        force ||
        Math.abs(audioTime - videoTime) > AUDIO_SYNC_TOLERANCE_SECONDS
      ) {
        try {
          activeAudio.currentTime = videoTime;
        } catch {
          // The audio element may not have metadata yet; the next sync tick
          // will retry once hls.js has buffered enough media.
        }
      }
    }

    function syncAudioProperties() {
      activeAudio.muted = activeVideo.muted;
      activeAudio.volume = activeVideo.volume;
      activeAudio.playbackRate = activeVideo.playbackRate;
    }

    function playAudio() {
      syncAudioProperties();
      syncAudioTime(true);
      void activeAudio.play().catch((err: unknown) => {
        if (err instanceof DOMException && err.name === "AbortError") return;
        setPlaybackError(
          err instanceof Error ? err.message : "Audio playback failed",
        );
      });
    }

    function pauseAudio() {
      if (!activeAudio.paused) {
        activeAudio.pause();
      }
    }

    function syncAndResumeAudio() {
      syncAudioTime(true);
      if (!activeVideo.paused && !activeVideo.ended) {
        playAudio();
      }
    }

    function seekAudio() {
      syncAudioTime(true);
    }

    function correctAudioDrift() {
      syncAudioTime(false);
    }

    syncAudioProperties();
    activeVideo.addEventListener("play", playAudio);
    activeVideo.addEventListener("pause", pauseAudio);
    activeVideo.addEventListener("ended", pauseAudio);
    activeVideo.addEventListener("seeking", seekAudio);
    activeVideo.addEventListener("seeked", syncAndResumeAudio);
    activeVideo.addEventListener("ratechange", syncAudioProperties);
    activeVideo.addEventListener("volumechange", syncAudioProperties);
    activeVideo.addEventListener("timeupdate", correctAudioDrift);

    return () => {
      activeVideo.removeEventListener("play", playAudio);
      activeVideo.removeEventListener("pause", pauseAudio);
      activeVideo.removeEventListener("ended", pauseAudio);
      activeVideo.removeEventListener("seeking", seekAudio);
      activeVideo.removeEventListener("seeked", syncAndResumeAudio);
      activeVideo.removeEventListener("ratechange", syncAudioProperties);
      activeVideo.removeEventListener("volumechange", syncAudioProperties);
      activeVideo.removeEventListener("timeupdate", correctAudioDrift);
      pauseAudio();
    };
  }, [audioManifestUrl]);

  const loading =
    flow.loading ||
    (isMultiFlow ? multiPlaybackQuery.loading : segmentQuery.loading);
  const error =
    flow.error || (isMultiFlow ? multiPlaybackQuery.error : segmentQuery.error);
  const firstUrl = playable[0] ? firstPlayableUrl(playable[0]) : null;

  return (
    <div className="p-4 sm:p-6 lg:p-8">
      <div className="mb-6 flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
        <div>
          <div className="flex flex-wrap items-center gap-3">
            <h1 className="text-xl font-bold text-lw-ink-900 sm:text-2xl">
              Playback Preview
            </h1>
            <Badge variant="warning">Preview</Badge>
          </div>
          <p className="mt-2 max-w-3xl text-sm leading-6 text-lw-ink-500">
            Browser preview is an addon workflow that builds an HLS playlist
            from TAMS segment object URLs. It verifies object retrieval paths;
            it is not a replacement for a broadcast playout system.
          </p>
        </div>
        <div className="flex w-full flex-col gap-2 sm:flex-row lg:w-auto lg:items-end">
          <label htmlFor="playback-flow-select" className="sr-only">
            Select flow for playback
          </label>
          <select
            id="playback-flow-select"
            value={selectedFlowId}
            onChange={(event) => setSelectedFlowId(event.target.value)}
            className="tamoss-toolbar-control min-w-0 px-3 py-2.5 text-sm focus:border-tams-400 focus:outline-none focus:ring-2 focus:ring-tams-200 sm:min-w-96"
          >
            <option value="">Choose a flow...</option>
            {flows.data?.data.map((candidate) => (
              <option key={candidate.id} value={candidate.id}>
                {flowOptionLabel(candidate)}
              </option>
            ))}
          </select>
          <StorageBackendSelector
            id="playback-storage-select"
            label="Storage backend"
            value={selectedStorageId}
            onChange={setSelectedStorageId}
            backends={storageBackends.data}
            includeAllOption
            allLabel="All storage backends"
            className="sm:min-w-72"
          />
          <button
            onClick={() => {
              if (selectedFlowId) {
                const next: Record<string, string> = { flow: selectedFlowId };
                if (selectedStorageId) next.storage = selectedStorageId;
                setSearchParams(next);
              }
            }}
            disabled={!selectedFlowId}
            className="tamoss-button-primary px-4 py-2.5 text-sm font-semibold disabled:opacity-50"
          >
            Load Preview
          </button>
        </div>
      </div>

      {flows.error && (
        <div className="mb-4">
          <ErrorMessage message={flows.error} onRetry={flows.refetch} />
        </div>
      )}

      {!activeFlowId && (
        <div className="tamoss-panel rounded-2xl p-8 text-center">
          <p className="text-sm font-medium text-lw-ink-700">
            Select a flow to preview.
          </p>
          <p className="mt-2 text-sm text-lw-ink-500">
            The preview uses existing segment `get_urls`; flows without playable
            URLs remain inspectable.
          </p>
        </div>
      )}

      {activeFlowId && loading && (
        <LoadingSpinner message="Loading playback data..." />
      )}

      {activeFlowId && error && (
        <ErrorMessage
          title="Playback data failed to load"
          message={error}
          onRetry={() => {
            flow.refetch();
            if (isMultiFlow) {
              multiPlaybackQuery.refetch();
            } else {
              segmentQuery.refetch();
            }
          }}
        />
      )}

      {activeFlowId && !loading && !error && selectedFlow && (
        <div className="grid gap-6 lg:grid-cols-[minmax(0,2fr)_minmax(22rem,1fr)]">
          <section className="tamoss-panel overflow-hidden rounded-2xl">
            <div className="flex flex-wrap items-center gap-2 border-b border-lw-ink-100 px-4 py-3 sm:px-5">
              <span className="font-semibold text-lw-ink-900">
                {selectedFlow.label || truncateId(selectedFlow.id)}
              </span>
              {selectedFlow.format && (
                <Badge variant="primary">
                  {formatFormat(selectedFlow.format)}
                </Badge>
              )}
              {selectedFlow.codec && (
                <Badge variant="info">{formatCodec(selectedFlow.codec)}</Badge>
              )}
              <Link
                to={`/flows/${selectedFlow.id}`}
                className="ml-auto text-xs font-medium text-tams-700 hover:text-tams-800"
              >
                Inspect Flow
              </Link>
            </div>

            <div className="bg-black">
              {manifestUrl ? (
                <>
                  <video
                    ref={videoRef}
                    className="aspect-video w-full"
                    controls
                    playsInline
                    aria-label="TAMS HLS playback preview"
                  />
                  <audio
                    ref={audioRef}
                    aria-hidden="true"
                    className="hidden"
                    preload="auto"
                  />
                </>
              ) : (
                <div className="flex aspect-video items-center justify-center px-6 text-center">
                  <div>
                    <p className="text-sm font-semibold text-white">
                      No playable segment URLs
                    </p>
                    <p className="mt-2 max-w-lg text-sm text-white/70">
                      {isMultiFlow
                        ? "The multi flow loaded, but none of its child flows exposed a browser-reachable `get_url`."
                        : "The flow loaded, but no segment exposed a browser-reachable `get_url`."}{" "}
                      Inspect the segment list and object storage backend
                      configuration.
                    </p>
                  </div>
                </div>
              )}
            </div>

            <div className="flex flex-wrap items-center gap-3 px-4 py-3 text-sm text-lw-ink-600 sm:px-5">
              {playbackState === "ready" && (
                <Badge variant="success">Preview ready</Badge>
              )}
              {playbackState === "loading" && manifestUrl && (
                <Badge variant="warning">Preparing preview</Badge>
              )}
              {playbackState === "unsupported" && (
                <Badge variant="danger">Unsupported browser</Badge>
              )}
              {playbackState === "error" && (
                <Badge variant="danger">Preview error</Badge>
              )}
              <span>
                {playable.length} playable segment
                {playable.length !== 1 ? "s" : ""}
              </span>
              {isMultiFlow && (
                <span>
                  {multiChildren.length} child flow
                  {multiChildren.length !== 1 ? "s" : ""}
                </span>
              )}
              {totalDuration > 0 && (
                <span>{Math.round(totalDuration)}s indexed duration</span>
              )}
              {firstUrl && (
                <a
                  href={firstUrl}
                  target="_blank"
                  rel="noreferrer"
                  className="font-medium text-tams-700 hover:text-tams-800"
                >
                  Open first object URL
                </a>
              )}
            </div>

            {playbackError && (
              <div className="px-4 pb-4 sm:px-5">
                <ErrorMessage
                  title="Playback preview failed"
                  message={playbackError}
                />
              </div>
            )}
          </section>

          <aside className="space-y-6">
            <section className="tamoss-panel rounded-2xl p-5">
              <h2 className="text-sm font-semibold uppercase tracking-[0.16em] text-lw-ink-500">
                Flow Summary
              </h2>
              <dl className="mt-4 space-y-3 text-sm">
                <div>
                  <dt className="text-lw-ink-400">Flow ID</dt>
                  <dd className="mt-1 flex items-center gap-2 font-mono text-xs text-lw-ink-800">
                    <span className="truncate">{selectedFlow.id}</span>
                    <CopyButton text={selectedFlow.id} label="Copy ID" />
                  </dd>
                </div>
                {selectedFlow.essence_parameters?.frame_width && (
                  <div>
                    <dt className="text-lw-ink-400">Resolution</dt>
                    <dd className="mt-1 text-lw-ink-800">
                      {formatResolution(
                        selectedFlow.essence_parameters.frame_width,
                        selectedFlow.essence_parameters.frame_height,
                      )}
                    </dd>
                  </div>
                )}
                {selectedFlow.essence_parameters?.frame_rate && (
                  <div>
                    <dt className="text-lw-ink-400">Frame rate</dt>
                    <dd className="mt-1 text-lw-ink-800">
                      {formatFrameRate(
                        selectedFlow.essence_parameters.frame_rate,
                      )}
                    </dd>
                  </div>
                )}
                {selectedFlow.timerange && selectedFlow.timerange !== "_" && (
                  <div>
                    <dt className="text-lw-ink-400">Timerange</dt>
                    <dd className="mt-1 font-mono text-xs text-lw-ink-800">
                      {selectedFlow.timerange}
                    </dd>
                  </div>
                )}
              </dl>
            </section>

            <section className="tamoss-panel rounded-2xl p-5">
              <div className="flex items-center justify-between">
                <h2 className="text-sm font-semibold uppercase tracking-[0.16em] text-lw-ink-500">
                  Segments
                </h2>
                <span className="text-xs text-lw-ink-400">
                  {segments.length} indexed
                </span>
              </div>
              <div className="mt-4 max-h-[26rem] space-y-2 overflow-auto pr-1">
                {segments.length === 0 && (
                  <p className="text-sm text-lw-ink-500">
                    {isMultiFlow
                      ? "No segments are registered on this multi flow's child flows."
                      : "No segments are registered for this flow."}
                  </p>
                )}
                {segments.map((segment) => {
                  const { duration } = parseTimerange(segment.timerange);
                  const url = firstPlayableUrl(segment);
                  return (
                    <div
                      key={`${segment.playbackFlowId ?? selectedFlow.id}-${segment.object_id}-${segment.timerange}`}
                      className="rounded-xl border border-lw-ink-100 bg-white/80 p-3"
                    >
                      <div className="flex items-start justify-between gap-3">
                        <div className="min-w-0">
                          <p className="font-mono text-xs text-lw-ink-800">
                            {segment.timerange}
                          </p>
                          {segment.playbackFlowId && (
                            <Link
                              to={`/flows/${segment.playbackFlowId}`}
                              className="mt-1 block truncate font-mono text-xs text-tams-700 hover:text-tams-800"
                            >
                              {segment.playbackRole
                                ? `${segment.playbackRole}: `
                                : ""}
                              {segment.playbackFlowId}
                            </Link>
                          )}
                          <Link
                            to={`/objects/${segment.object_id}`}
                            className="mt-1 block truncate font-mono text-xs text-tams-700 hover:text-tams-800"
                          >
                            {segment.object_id}
                          </Link>
                        </div>
                        <Badge variant={url ? "success" : "warning"}>
                          {url ? "URL" : "No URL"}
                        </Badge>
                      </div>
                      <div className="mt-2 flex flex-wrap items-center gap-2 text-xs text-lw-ink-500">
                        {duration > 0 && <span>{duration.toFixed(1)}s</span>}
                        {url && <CopyButton text={url} label="Copy URL" />}
                        {url && (
                          <a
                            href={url}
                            target="_blank"
                            rel="noreferrer"
                            className="font-medium text-tams-700"
                          >
                            Open
                          </a>
                        )}
                      </div>
                    </div>
                  );
                })}
              </div>
            </section>
          </aside>
        </div>
      )}
    </div>
  );
}
