import {
  ChromingTheme,
  FileFormatType,
  MainMediaType,
  OmakasePlayer,
  PlayerAudioMode,
  PlayerEventType,
} from "@byomakase/omakase-player/dist/omakase-player.es.js";
import "@byomakase/omakase-player/dist/omakase-player.css";
import {
  createSynchronizedAudioSidecar,
  type SynchronizedAudioSidecar,
} from "@/player/audio-sidecar";
import { installSensitiveConsoleErrorRedaction } from "@/player/console-redaction";
import {
  descriptorMediaUrls,
  type MediaPreviewDescriptor,
} from "@/player/descriptor";
import { compilePlaybackPlan } from "@/player/hls-manifest";

const AUDIO_ONLY_TIMELINE_FRAME_RATE = 25;

interface SubscriptionLike {
  unsubscribe(): void;
}

interface ObservableLike<T> {
  subscribe(observer: {
    next?: (value: T) => void;
    error?: (error: unknown) => void;
    complete?: () => void;
  }): SubscriptionLike;
}

export type PlaybackPhase =
  | "loading"
  | "ready"
  | "playing"
  | "paused"
  | "buffering"
  | "ended"
  | "error";

export interface PlaybackSnapshot {
  phase: PlaybackPhase;
  currentTime: number;
  duration: number;
  message?: string;
  warning?: string;
}

export interface OmakasePreviewHandle {
  ready: Promise<void>;
  selectAudioTrack(flowId: string): void;
  destroy(): void;
}

export interface CreateOmakasePreviewOptions {
  descriptor: MediaPreviewDescriptor;
  playerElementId: string;
  timelineElementId: string;
  onChange(snapshot: PlaybackSnapshot): void;
}

export class PreviewPlaybackError extends Error {
  constructor() {
    super("Omakase could not load the selected media window.");
    this.name = "PreviewPlaybackError";
  }
}

export function createOmakasePreview({
  descriptor,
  playerElementId,
  timelineElementId,
  onChange,
}: CreateOmakasePreviewOptions): OmakasePreviewHandle {
  const plan = compilePlaybackPlan({
    tracks: descriptor.tracks,
    initialTimerange: descriptor.initialTimerange,
  });
  const releaseConsoleRedaction = installSensitiveConsoleErrorRedaction(
    descriptorMediaUrls(descriptor),
  );
  const subscriptions: SubscriptionLike[] = [];
  const audioSidecars = new Map<string, SynchronizedAudioSidecar>();
  const pendingRejects = new Set<(reason: unknown) => void>();
  let selectedAudioFlowId =
    plan.kind === "hls" ? plan.audioSidecars[0]?.flowId : undefined;
  let player: OmakasePlayer;
  try {
    player = new OmakasePlayer({
      playerHtmlElementId: playerElementId,
      playerAudioMode: PlayerAudioMode.SINGLE,
      chromingTheme: ChromingTheme.DEFAULT,
    });
  } catch {
    plan.dispose();
    releaseConsoleRedaction();
    throw new PreviewPlaybackError();
  }
  let destroyed = false;
  let phase: PlaybackPhase = "loading";
  let currentTime = 0;
  let duration = 0;
  let warning =
    plan.kind === "hls" && plan.trimmed
      ? "Playback is limited to the timerange shared by video and audio tracks."
      : undefined;

  const emit = (nextPhase = phase) => {
    phase = nextPhase;
    onChange({
      phase,
      currentTime,
      duration,
      ...(warning ? { warning } : {}),
    });
  };

  const observeOne = <T>(source: ObservableLike<T>): Promise<T> =>
    new Promise<T>((resolve, reject) => {
      let emitted = false;
      const rejectPending = (reason: unknown) => {
        if (emitted) return;
        emitted = true;
        reject(reason);
      };
      pendingRejects.add(rejectPending);
      const subscription = source.subscribe({
        next: (value) => {
          if (emitted) return;
          emitted = true;
          pendingRejects.delete(rejectPending);
          resolve(value);
        },
        error: (error) => {
          if (emitted) return;
          emitted = true;
          pendingRejects.delete(rejectPending);
          reject(error);
        },
        complete: () => {
          if (emitted) return;
          emitted = true;
          pendingRejects.delete(rejectPending);
          reject(new Error("Omakase operation completed without a result"));
        },
      });
      subscriptions.push(subscription);
    });

  const eventSubscription = player.player.onEvent$.subscribe({
    next: (event) => {
      if (destroyed) return;
      switch (event.type) {
        case PlayerEventType.PLAYER_PLAY:
          emit("playing");
          break;
        case PlayerEventType.PLAYER_PAUSE:
          emit("paused");
          break;
        case PlayerEventType.PLAYER_PLAYBACK_CHANGE:
          currentTime = event.data.playerPlayback.currentTime;
          emit(playbackPhase(event.data.playerPlayback, phase));
          break;
        case PlayerEventType.PLAYER_ENDED:
          emit("ended");
          break;
        case PlayerEventType.PLAYER_PLAYBACK_PROGRESS:
          currentTime = event.data.currentTime;
          emit();
          break;
      }
    },
  });
  subscriptions.push(eventSubscription);
  emit("loading");

  const dispose = () => {
    if (destroyed) return;
    destroyed = true;
    for (const reject of pendingRejects) reject(abortError());
    pendingRejects.clear();
    pauseMainMedia(player);
    for (const sidecar of audioSidecars.values()) sidecar.destroy();
    audioSidecars.clear();
    for (const subscription of subscriptions.splice(0)) {
      try {
        subscription.unsubscribe();
      } catch {
        // Continue releasing the remaining player resources.
      }
    }
    queueMicrotask(() => {
      try {
        player.destroy();
      } catch {
        // Third-party teardown is best effort; routing must still release our URLs.
      } finally {
        try {
          plan.dispose();
        } finally {
          releaseConsoleRedaction();
        }
      }
    });
  };

  const reportPlaybackFailure = () => {
    if (destroyed) return;
    phase = "error";
    onChange({
      phase,
      currentTime,
      duration,
      message: "Omakase could not load the selected media window.",
    });
    dispose();
  };

  const frameRate =
    resolveFrameRate(descriptor) ??
    (plan.kind === "hls" && !descriptor.video && !descriptor.muxed
      ? AUDIO_ONLY_TIMELINE_FRAME_RATE
      : undefined);
  const ready = (async () => {
    await observeOne(
      player.loadMainMedia(plan.kind === "hls" ? plan.mainUrl : plan.url, {
        fileFormatType:
          plan.kind === "hls"
            ? FileFormatType.HLS
            : plan.mediaKind === "audio"
              ? FileFormatType.MP4_AUDIO
              : FileFormatType.MP4,
        mainMediaType:
          plan.kind === "hls"
            ? MainMediaType.HLS
            : plan.mediaKind === "audio"
              ? MainMediaType.AUDIO_FILE
              : MainMediaType.MP4,
        ...(frameRate ? { frameRate } : {}),
      }),
    );
    if (destroyed) throw abortError();
    duration = safeDuration(player);
    if (plan.kind === "hls" && plan.audioSidecars.length > 0) {
      const container = document.getElementById(playerElementId);
      const mainMedia =
        container?.querySelector<HTMLMediaElement>("video, audio");
      if (!container || !mainMedia) {
        throw new Error("Omakase media element is unavailable");
      }
      for (const sidecar of plan.audioSidecars) {
        const handle = createSynchronizedAudioSidecar({
          container,
          enabled: sidecar.flowId === selectedAudioFlowId,
          label: sidecar.label,
          mainMedia,
          onError: reportPlaybackFailure,
          offsetSeconds: sidecar.offsetSeconds,
          playlistUrl: sidecar.url,
        });
        audioSidecars.set(sidecar.flowId, handle);
        await handle.ready;
      }
    }
    if (destroyed) throw abortError();
    emit("ready");
    try {
      await observeOne(
        player.createTimeline({
          htmlElementId: timelineElementId,
          scrubberClickSeek: true,
          zoomWheelEnabled: true,
          style: {
            stageMinWidth: 640,
            backgroundFill: "#f7f8f8",
            headerBackgroundFill: "#eef1f2",
            footerBackgroundFill: "#eef1f2",
            playheadFill: "#172126",
            playheadLineWidth: 2,
            playheadPlayProgressFill: "#007b67",
            playheadPlayProgressOpacity: 0.42,
            playheadBufferedFill: "#8f9da2",
            scrubberSouthLineOpacity: 0.2,
          },
        }),
      );
    } catch (error: unknown) {
      if (destroyed || isAbortError(error)) throw abortError();
      warning = [
        warning,
        "Timeline visualization is unavailable for this media.",
      ]
        .filter(Boolean)
        .join(" ");
      emit("ready");
    }
  })().catch((error: unknown) => {
    if (destroyed || isAbortError(error)) throw abortError();
    reportPlaybackFailure();
    throw new PreviewPlaybackError();
  });

  return {
    ready,
    selectAudioTrack(flowId: string) {
      if (
        plan.kind !== "hls" ||
        !plan.audioSidecars.some((sidecar) => sidecar.flowId === flowId)
      ) {
        return;
      }
      selectedAudioFlowId = flowId;
      for (const [sidecarFlowId, sidecar] of audioSidecars) {
        sidecar.setEnabled(sidecarFlowId === flowId);
      }
    },
    destroy: dispose,
  };
}

function playbackPhase(
  playback: {
    buffering: boolean;
    ended: boolean;
    paused: boolean;
    pausing: boolean;
    playing: boolean;
    waiting: boolean;
    waitingSyncedMedia: boolean;
  },
  fallback: PlaybackPhase,
): PlaybackPhase {
  if (playback.ended) return "ended";
  if (playback.buffering || playback.waiting || playback.waitingSyncedMedia) {
    return "buffering";
  }
  if (playback.playing) return "playing";
  if (playback.paused || playback.pausing) return "paused";
  return fallback;
}

function resolveFrameRate(
  descriptor: MediaPreviewDescriptor,
): number | string | undefined {
  const videoFlow = descriptor.video?.flow;
  const rate = videoFlow?.essence_parameters?.frame_rate;
  if (rate?.numerator) {
    return rate.denominator && rate.denominator !== 1
      ? `${rate.numerator}/${rate.denominator}`
      : rate.numerator;
  }

  const tagValue = videoFlow?.tags?.nominal_fps;
  const nominalRate = Array.isArray(tagValue) ? tagValue[0] : tagValue;
  return typeof nominalRate === "string" && validFrameRate(nominalRate)
    ? nominalRate
    : undefined;
}

function validFrameRate(value: string): boolean {
  const match = /^(\d+(?:\.\d+)?)(?:\/(\d+))?$/u.exec(value.trim());
  if (!match) return false;
  const numerator = Number(match[1]);
  const denominator = Number(match[2] ?? "1");
  const framesPerSecond = numerator / denominator;
  return (
    Number.isFinite(framesPerSecond) &&
    denominator > 0 &&
    framesPerSecond >= 1 &&
    framesPerSecond <= 240
  );
}

function safeDuration(player: OmakasePlayer): number {
  try {
    const value = player.player.getDuration();
    return Number.isFinite(value) ? value : 0;
  } catch {
    return 0;
  }
}

function pauseMainMedia(player: OmakasePlayer): void {
  try {
    player.player.htmlMediaElement?.pause();
  } catch {
    // Omakase still owns teardown if the browser has already detached media.
  }
}

function abortError(): DOMException {
  return new DOMException("Preview disposed", "AbortError");
}

function isAbortError(error: unknown): boolean {
  return error instanceof DOMException && error.name === "AbortError";
}
