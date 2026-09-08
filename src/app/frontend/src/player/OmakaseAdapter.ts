import {
  ChromingTheme,
  FileFormatType,
  MainMediaType,
  OmakasePlayer,
  PlayerAudioMode,
  PlayerEventType,
} from "@byomakase/omakase-player/dist/omakase-player.es.js";
import "@byomakase/omakase-player/dist/omakase-player.css";
import { type ErrorData, Events } from "hls.js";
import { installSensitiveConsoleErrorRedaction } from "@/player/console-redaction";
import {
  descriptorMediaUrls,
  type MediaPreviewDescriptor,
} from "@/player/descriptor";
import { compilePlaybackPlan, PlaybackPlanError } from "@/player/hls-manifest";
import { halfOpenTimerange } from "@/utils/tams-time";

const AUDIO_ONLY_TIMELINE_FRAME_RATE = 25;
export const MEDIA_READY_TIMEOUT_MS = 30_000;
const START_BUFFER_SECONDS = 8;
const LOW_BUFFER_SECONDS = 1;

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

export interface PreviewAudioTrack {
  flowId: string;
  label: string;
}

export interface OmakasePreviewHandle {
  ready: Promise<void>;
  /**
   * Alternate audio renditions in the HLS master. Audio-only plans retain
   * their existing single-rendition behaviour.
   */
  audioTracks: readonly PreviewAudioTrack[];
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
  // The descriptor preserves the TAMS form of the Flow timerange, including an
  // inclusive end or a single instant. The manifest compiler works in half-open
  // nanoseconds, so convert exactly at this boundary instead of relaxing either
  // contract. A window with no playable duration never reaches the player: the
  // preview reports it as absent media rather than as a playback failure.
  const playbackWindow = halfOpenTimerange(descriptor.initialTimerange);
  if (!playbackWindow) {
    throw new PlaybackPlanError(
      "no-playable-media",
      "The selected media window has no playable duration.",
    );
  }
  const plan = compilePlaybackPlan({
    tracks: descriptor.tracks,
    initialTimerange: playbackWindow.timerange,
  });
  const releaseConsoleRedaction = installSensitiveConsoleErrorRedaction(
    descriptorMediaUrls(descriptor),
  );
  const subscriptions: SubscriptionLike[] = [];
  const pendingRejects = new Set<(reason: unknown) => void>();
  let selectedAudioFlowId =
    plan.kind === "hls" ? plan.audioTracks[0]?.flowId : undefined;
  let player: OmakasePlayer;
  try {
    player = new OmakasePlayer({
      playerHtmlElementId: playerElementId,
      playerAudioMode: PlayerAudioMode.SINGLE,
      chromingTheme: ChromingTheme.DEFAULT,
      playerControllerConfig: {
        [MainMediaType.HLS]: {
          hlsConfig: { maxBufferLength: 30, backBufferLength: 30 },
        },
      },
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
  let loadTimeout: number | undefined;
  let playTimeout: number | undefined;
  let loaded = false;
  let bufferedReady = false;
  let wantsPlay = false;
  let started = false;
  let holding = true;
  let playPending = false;
  let switchingAudio = false;
  let resolveBuffered: (() => void) | undefined;
  const domEvents = new AbortController();
  const container = document.getElementById(playerElementId);
  const bufferReady = new Promise<void>((resolve, reject) => {
    resolveBuffered = resolve;
    pendingRejects.add(reject);
  });
  // Teardown can precede main-media loading and the later await of this promise.
  void bufferReady.catch(() => undefined);
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
    for (const button of container?.querySelectorAll("omakase-play-button") ??
      []) {
      button.toggleAttribute("mediapaused", !wantsPlay);
      button.setAttribute("aria-label", wantsPlay ? "pause" : "play");
    }
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
        case PlayerEventType.PLAYER_MAIN_MEDIA_LOAD_ERROR:
          reportPlaybackFailure();
          break;
        case PlayerEventType.PLAYER_PLAY:
          checkBuffer();
          break;
        case PlayerEventType.PLAYER_PAUSE:
          checkBuffer();
          break;
        case PlayerEventType.PLAYER_PLAYBACK_CHANGE:
          currentTime = event.data.playerPlayback.currentTime;
          checkBuffer();
          break;
        case PlayerEventType.PLAYER_ENDED:
          wantsPlay = false;
          emit("ended");
          break;
        case PlayerEventType.PLAYER_PLAYBACK_PROGRESS:
          currentTime = event.data.currentTime;
          checkBuffer();
          break;
      }
    },
  });
  subscriptions.push(eventSubscription);
  emit("loading");

  const dispose = (reason: unknown = abortError()) => {
    if (destroyed) return;
    destroyed = true;
    window.clearTimeout(loadTimeout);
    window.clearTimeout(playTimeout);
    window.clearInterval(bufferInterval);
    domEvents.abort();
    for (const reject of pendingRejects) reject(reason);
    pendingRejects.clear();
    pauseMainMedia(player);
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
    dispose(new PreviewPlaybackError());
  };

  function checkBuffer() {
    const media = player.player.htmlMediaElement;
    if (destroyed || !loaded || !media) return;
    currentTime = media.currentTime;
    if (media.ended) {
      wantsPlay = false;
      emit("ended");
      return;
    }
    const remaining = Math.max(0, duration - currentTime);
    const rate = Math.max(0.1, media.playbackRate);
    if (plan.kind === "hls") {
      const hls = player.player.getPlaybackEngine(MainMediaType.HLS).hls;
      if (hls)
        hls.config.maxBufferLength = Math.max(30, START_BUFFER_SECONDS * rate);
    }
    const ahead = bufferedAhead(media);
    const reserve = Math.min(START_BUFFER_SECONDS * rate, remaining);
    const ready =
      !media.seeking &&
      !switchingAudio &&
      media.readyState >= 3 &&
      ahead + 0.05 >= reserve;
    if (
      wantsPlay &&
      (media.seeking ||
        switchingAudio ||
        ahead + 0.05 < Math.min(LOW_BUFFER_SECONDS * rate, remaining))
    ) {
      holding = true;
    }
    if (!bufferedReady && ready) {
      bufferedReady = true;
      window.clearTimeout(loadTimeout);
      loadTimeout = undefined;
      resolveBuffered?.();
    }
    if (holding && !ready) {
      if (!media.paused) media.pause();
      if (bufferedReady && wantsPlay && loadTimeout === undefined) {
        loadTimeout = window.setTimeout(
          reportPlaybackFailure,
          MEDIA_READY_TIMEOUT_MS,
        );
      }
      emit(wantsPlay ? "buffering" : bufferedReady ? "paused" : "loading");
      return;
    }
    holding = false;
    if (bufferedReady) {
      window.clearTimeout(loadTimeout);
      loadTimeout = undefined;
    }
    if (wantsPlay && media.paused && !playPending) {
      playPending = true;
      started = true;
      playTimeout = window.setTimeout(
        reportPlaybackFailure,
        MEDIA_READY_TIMEOUT_MS,
      );
      const subscription = player.player.play().subscribe({
        error: () => {
          window.clearTimeout(playTimeout);
          playPending = false;
          wantsPlay = false;
          emit("paused");
        },
        complete: () => {
          window.clearTimeout(playTimeout);
          playPending = false;
          if (!wantsPlay) pauseMainMedia(player);
        },
      });
      subscriptions.push(subscription);
    }
    if (!wantsPlay && !media.paused) media.pause();
    emit(
      wantsPlay
        ? media.paused || media.readyState < 3
          ? "buffering"
          : "playing"
        : started
          ? "paused"
          : "ready",
    );
  }

  const requestPlayback = (play: boolean) => {
    if (destroyed) return;
    wantsPlay = play;
    if (play) {
      // Resume Web Audio in the user gesture, before waiting for network data.
      void player.player.audio.audioContext.resume().catch(() => undefined);
      holding = true;
      if (loaded && player.player.htmlMediaElement?.ended) {
        subscriptions.push(
          player.player.seekTo(0).subscribe({ error: reportPlaybackFailure }),
        );
      }
    } else {
      started = true;
      window.clearTimeout(playTimeout);
      pauseMainMedia(player);
      if (bufferedReady) {
        window.clearTimeout(loadTimeout);
        loadTimeout = undefined;
      }
    }
    emit(play ? "buffering" : "paused");
    checkBuffer();
  };
  // Keep user intent separate from the native pauses used to build a reserve.
  const onControl = (event: Event) => {
    const playButton = event
      .composedPath()
      .some(
        (node) =>
          node instanceof HTMLElement && node.tagName === "OMAKASE-PLAY-BUTTON",
      );
    const keyboard = event instanceof KeyboardEvent;
    if (
      event.type === "mediaplayrequest" ||
      event.type === "mediapauserequest"
    ) {
      event.preventDefault();
      event.stopImmediatePropagation();
      requestPlayback(event.type === "mediaplayrequest");
    } else if (
      playButton &&
      (!keyboard || event.key === " " || event.key === "Enter")
    ) {
      event.preventDefault();
      event.stopImmediatePropagation();
      if (event.type !== "keydown") requestPlayback(!wantsPlay);
    }
  };
  for (const type of [
    "click",
    "keydown",
    "keyup",
    "mediaplayrequest",
    "mediapauserequest",
  ]) {
    container?.addEventListener(type, onControl, {
      capture: true,
      signal: domEvents.signal,
    });
  }
  const bufferInterval = window.setInterval(checkBuffer, 100);

  const frameRate =
    resolveFrameRate(descriptor) ??
    (plan.kind === "hls" && !descriptor.video && !descriptor.muxed
      ? AUDIO_ONLY_TIMELINE_FRAME_RATE
      : undefined);
  loadTimeout = window.setTimeout(
    reportPlaybackFailure,
    MEDIA_READY_TIMEOUT_MS,
  );
  const ready = (async () => {
    await observeOne(
      player.loadMainMedia(
        plan.kind === "hls" && plan.audioTracks.length === 0
          ? plan.mainUrl
          : plan.url,
        {
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
        },
      ),
    );
    if (destroyed) throw abortError();
    duration = safeDuration(player);
    loaded = true;
    if (plan.kind === "hls") {
      const hls = player.player.getPlaybackEngine(MainMediaType.HLS).hls;
      if (!hls) throw new Error("HLS playback engine is unavailable");
      const onError = (_event: Events.ERROR, data: ErrorData) => {
        if (data.fatal) reportPlaybackFailure();
      };
      const onSwitching = () => {
        switchingAudio = true;
        holding = true;
        checkBuffer();
      };
      const onSwitched = () => {
        switchingAudio = false;
        checkBuffer();
      };
      hls.on(Events.ERROR, onError);
      hls.on(Events.AUDIO_TRACK_SWITCHING, onSwitching);
      hls.on(Events.AUDIO_TRACK_SWITCHED, onSwitched);
      subscriptions.push({
        unsubscribe() {
          hls.off(Events.ERROR, onError);
          hls.off(Events.AUDIO_TRACK_SWITCHING, onSwitching);
          hls.off(Events.AUDIO_TRACK_SWITCHED, onSwitched);
        },
      });
      const selected = plan.audioTracks.findIndex(
        (track) => track.flowId === selectedAudioFlowId,
      );
      if (selected >= 0 && hls.audioTrack !== selected)
        hls.audioTrack = selected;
    }
    checkBuffer();
    await bufferReady;
    if (destroyed) throw abortError();
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
        "Timeline visualisation is unavailable for this media.",
      ]
        .filter(Boolean)
        .join(" ");
      checkBuffer();
    }
  })().catch((error: unknown) => {
    if (isAbortError(error) || (destroyed && phase !== "error")) {
      throw abortError();
    }
    reportPlaybackFailure();
    throw new PreviewPlaybackError();
  });

  return {
    ready,
    audioTracks: plan.kind === "hls" ? plan.audioTracks : [],
    selectAudioTrack(flowId: string) {
      if (
        plan.kind !== "hls" ||
        !plan.audioTracks.some((track) => track.flowId === flowId)
      ) {
        return;
      }
      selectedAudioFlowId = flowId;
      if (loaded) {
        const hls = player.player.getPlaybackEngine(MainMediaType.HLS).hls;
        const index = plan.audioTracks.findIndex(
          (track) => track.flowId === flowId,
        );
        if (hls && hls.audioTrack !== index) hls.audioTrack = index;
      }
    },
    destroy: dispose,
  };
}

function bufferedAhead(media: HTMLMediaElement): number {
  for (let index = 0; index < media.buffered.length; index++) {
    if (
      media.buffered.start(index) <= media.currentTime + 0.05 &&
      media.buffered.end(index) > media.currentTime
    ) {
      return media.buffered.end(index) - media.currentTime;
    }
  }
  return 0;
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
