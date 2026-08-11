import { useQuery } from "@tanstack/react-query";
import { RefreshCw } from "lucide-react";
import { useId, useLayoutEffect, useRef, useState } from "react";
import {
  Button,
  Panel,
  QueryMessage,
  StatusBadge,
  surfaceStyles,
} from "@/components/Surface";
import { useApi } from "@/contexts/ApiContext";
import {
  buildMediaPreviewDescriptor,
  type MediaPreviewDescriptor,
  type PreviewTrack,
} from "@/player/descriptor";
import { PlaybackPlanError } from "@/player/hls-manifest";
import {
  createOmakasePreview,
  type OmakasePreviewHandle,
  type PlaybackSnapshot,
  type PreviewAudioTrack,
} from "@/player/OmakaseAdapter";
import { formatCodec, formatFormat } from "@/utils/format";
import { halfOpenTimerange } from "@/utils/tams-time";
import styles from "./MediaPreview.module.css";

const INITIAL_PLAYBACK: PlaybackSnapshot = {
  phase: "loading",
  currentTime: 0,
  duration: 0,
};

export default function MediaPreview({ flowId }: { flowId: string }) {
  const api = useApi();
  const reactId = useId().replace(/:/gu, "");
  const playerElementId = `omakase-player-${reactId}`;
  const timelineElementId = `omakase-timeline-${reactId}`;
  const playerHandle = useRef<OmakasePreviewHandle | null>(null);
  const [playback, setPlayback] = useState<PlaybackSnapshot>(INITIAL_PLAYBACK);
  const [audioTracks, setAudioTracks] = useState<readonly PreviewAudioTrack[]>(
    [],
  );
  const [selectedAudioFlowId, setSelectedAudioFlowId] = useState("");
  const preview = useQuery({
    queryKey: ["api", "preview", flowId, "descriptor"],
    queryFn: ({ signal }) =>
      buildMediaPreviewDescriptor(api, flowId, {
        signal,
        locationOrigin: window.location.origin,
      }),
  });

  useLayoutEffect(() => {
    if (!preview.data || !hasPlayableMedia(preview.data)) return;
    setPlayback(INITIAL_PLAYBACK);
    let handle: ReturnType<typeof createOmakasePreview> | undefined;
    try {
      handle = createOmakasePreview({
        descriptor: preview.data,
        playerElementId,
        timelineElementId,
        onChange: setPlayback,
      });
      playerHandle.current = handle;
      // Only the player knows which renditions it can actually switch between.
      setAudioTracks(handle.audioTracks);
      const defaultAudioFlowId = handle.audioTracks[0]?.flowId ?? "";
      setSelectedAudioFlowId(defaultAudioFlowId);
      if (defaultAudioFlowId) handle.selectAudioTrack(defaultAudioFlowId);
      handle.ready.catch(() => undefined);
    } catch (error: unknown) {
      setAudioTracks([]);
      setPlayback({
        phase: "error",
        currentTime: 0,
        duration: 0,
        message:
          error instanceof PlaybackPlanError
            ? error.message
            : "This media window cannot be played by Omakase.",
      });
    }
    return () => {
      if (playerHandle.current === handle) playerHandle.current = null;
      handle?.destroy();
    };
  }, [playerElementId, preview.data, timelineElementId]);

  if (preview.isLoading)
    return (
      <Panel>
        <QueryMessage loading />
      </Panel>
    );
  if (preview.error)
    return (
      <Panel>
        <QueryMessage error={preview.error} onRetry={() => preview.refetch()} />
      </Panel>
    );
  if (!preview.data) return null;

  const descriptor = preview.data;
  const playable = hasPlayableMedia(descriptor);
  return (
    <Panel
      title={descriptor.rootFlow.label || flowId}
      actions={
        <>
          <StatusBadge tone="info">
            {formatFormat(descriptor.rootFlow.format)}
          </StatusBadge>
          <StatusBadge>{formatCodec(descriptor.rootFlow.codec)}</StatusBadge>
          <Button type="button" onClick={() => preview.refetch()}>
            <RefreshCw size={14} aria-hidden="true" /> Refresh window
          </Button>
        </>
      }
    >
      {playable ? (
        <div className={styles.previewSurface}>
          <div className={styles.playerRegion}>
            <section
              id={playerElementId}
              className={styles.player}
              aria-label="Omakase media player"
            />
            <div className={styles.stateBar}>
              <div className={styles.stateControls}>
                <span role="status" aria-live="polite">
                  {phaseLabel(playback.phase)}
                </span>
                {audioTracks.length > 1 ? (
                  <label className={styles.audioSelector}>
                    <span>Audio</span>
                    <select
                      value={selectedAudioFlowId}
                      onChange={(event) => {
                        const flowId = event.target.value;
                        setSelectedAudioFlowId(flowId);
                        playerHandle.current?.selectAudioTrack(flowId);
                      }}
                    >
                      {audioTracks.map((track) => (
                        <option key={track.flowId} value={track.flowId}>
                          {track.label}
                        </option>
                      ))}
                    </select>
                  </label>
                ) : null}
              </div>
              <span className={styles.position}>
                <span className="srOnly">Playback position </span>
                {formatPlaybackTime(playback.currentTime)} /{" "}
                {formatPlaybackTime(playback.duration)}
              </span>
            </div>
            {playback.message ? (
              <p className={styles.playbackError} role="alert">
                {playback.message}
              </p>
            ) : null}
            {playback.warning ? (
              <p className={styles.playbackWarning} role="status">
                {playback.warning}
              </p>
            ) : null}
          </div>
          <div className={styles.timelineRegion}>
            <h3>Timeline</h3>
            <section
              id={timelineElementId}
              className={styles.timeline}
              aria-label="Omakase media timeline"
            />
          </div>
        </div>
      ) : (
        <QueryMessage
          empty={{
            title: "No playable media in this window",
            description:
              "Track metadata remains available below for operational inspection.",
          }}
        />
      )}

      <TrackInventory descriptor={descriptor} />
    </Panel>
  );
}

function TrackInventory({
  descriptor,
}: {
  descriptor: MediaPreviewDescriptor;
}) {
  return (
    <section className={styles.inventory} aria-labelledby="preview-tracks">
      <header className={styles.inventoryHeader}>
        <div>
          <h3 id="preview-tracks">Tracks</h3>
          <p className={styles.window}>{descriptor.initialTimerange}</p>
        </div>
        <div className={styles.inventoryBadges}>
          <StatusBadge>{descriptor.tracks.length} tracks</StatusBadge>
          <StatusBadge>{descriptor.segmentCount} segments</StatusBadge>
          {descriptor.truncated ? (
            <StatusBadge tone="warning">Window truncated</StatusBadge>
          ) : null}
        </div>
      </header>
      <div className={surfaceStyles.tableWrap}>
        <table className={`${surfaceStyles.table} ${styles.trackTable}`}>
          <thead>
            <tr>
              <th>Track</th>
              <th>Format</th>
              <th>Codec</th>
              <th>Segments</th>
              <th>Availability</th>
              <th>Covered timerange</th>
            </tr>
          </thead>
          <tbody>
            {descriptor.tracks.map((track) => (
              <TrackRow key={track.flow.id} track={track} />
            ))}
          </tbody>
        </table>
      </div>
      <details className={styles.segmentDetails}>
        <summary>Segment availability</summary>
        <div className={surfaceStyles.tableWrap}>
          <table className={`${surfaceStyles.table} ${styles.segmentTable}`}>
            <thead>
              <tr>
                <th>Track</th>
                <th>Object</th>
                <th>Timerange</th>
                <th>Storage</th>
              </tr>
            </thead>
            <tbody>
              {descriptor.tracks
                .flatMap((track) =>
                  track.segments.map((segment) => ({ track, segment })),
                )
                .slice(0, 100)
                .map(({ track, segment }) => (
                  <tr
                    key={`${track.flow.id}-${segment.object_id}-${segment.timerange}`}
                  >
                    <td>{track.role || track.kind}</td>
                    <td className={surfaceStyles.mono}>{segment.object_id}</td>
                    <td className={surfaceStyles.mono}>{segment.timerange}</td>
                    <td>
                      {Array.from(
                        new Set(
                          segment.get_urls.map(
                            (location) => location.label || "Unlabelled",
                          ),
                        ),
                      ).join(", ")}
                    </td>
                  </tr>
                ))}
            </tbody>
          </table>
        </div>
        {descriptor.segmentCount > 100 ? (
          <p className={styles.segmentLimit}>
            Showing the first 100 segments in the selected window.
          </p>
        ) : null}
      </details>
    </section>
  );
}

function TrackRow({ track }: { track: PreviewTrack }) {
  const rejected = track.rejectedUrlCount;
  return (
    <tr>
      <td>
        <strong>{track.role || track.kind}</strong>
        <span className={styles.trackId}>{track.flow.id}</span>
      </td>
      <td>{formatFormat(track.flow.format)}</td>
      <td>{formatCodec(track.flow.codec)}</td>
      <td>{track.segments.length}</td>
      <td>
        {track.segments.length ? (
          <StatusBadge tone={rejected ? "warning" : "success"}>
            {rejected ? `${rejected} locations rejected` : "Available"}
          </StatusBadge>
        ) : (
          <StatusBadge tone="error">No segments</StatusBadge>
        )}
      </td>
      <td className={surfaceStyles.mono}>{coveredTimerange(track)}</td>
    </tr>
  );
}

function hasPlayableMedia(descriptor: MediaPreviewDescriptor): boolean {
  // A point timerange carries no playable duration, so the preview reports the
  // window as empty instead of asking the player to fail on it.
  if (!halfOpenTimerange(descriptor.initialTimerange)) return false;
  return Boolean(
    descriptor.muxed?.segments.length ||
      descriptor.video?.segments.length ||
      descriptor.audio.some((track) => track.segments.length),
  );
}

function coveredTimerange(track: PreviewTrack): string {
  const first = track.segments[0]?.timerange;
  const last = track.segments[track.segments.length - 1]?.timerange;
  if (!first || !last) return "-";
  return first === last ? first : `${first} to ${last}`;
}

function phaseLabel(phase: PlaybackSnapshot["phase"]): string {
  switch (phase) {
    case "loading":
      return "Loading media";
    case "ready":
      return "Ready";
    case "playing":
      return "Playing";
    case "paused":
      return "Paused";
    case "buffering":
      return "Buffering";
    case "ended":
      return "Ended";
    case "error":
      return "Playback failed";
  }
}

function formatPlaybackTime(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return "00:00";
  const rounded = Math.floor(seconds);
  const minutes = Math.floor(rounded / 60);
  const remainingSeconds = rounded % 60;
  return `${String(minutes).padStart(2, "0")}:${String(remainingSeconds).padStart(2, "0")}`;
}
