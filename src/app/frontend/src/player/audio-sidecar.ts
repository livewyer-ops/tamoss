import Hls, { type ErrorData, Events } from "hls.js";

export const AUDIO_SIDECAR_READY_TIMEOUT_MS = 15_000;
const MAX_DRIFT_SECONDS = 0.25;

export interface SynchronizedAudioSidecarOptions {
  container: HTMLElement;
  mainMedia: HTMLMediaElement;
  playlistUrl: string;
  offsetSeconds: number;
  label: string;
  enabled?: boolean;
  onError?(error: AudioSidecarError): void;
}

export interface SynchronizedAudioSidecar {
  ready: Promise<void>;
  setEnabled(enabled: boolean): void;
  destroy(): void;
}

export type AudioSidecarErrorCode = "load-failed" | "timeout" | "unsupported";

export class AudioSidecarError extends Error {
  constructor(
    public readonly code: AudioSidecarErrorCode,
    message: string,
  ) {
    super(message);
    this.name = "AudioSidecarError";
  }
}

function abortError(): DOMException {
  return new DOMException(
    "The synchronized audio track was removed.",
    "AbortError",
  );
}

function safePause(media: HTMLMediaElement): void {
  try {
    media.pause();
  } catch {
    // A detached or not-yet-initialized media element is already stopped.
  }
}

function safePlay(media: HTMLMediaElement): void {
  try {
    const result = media.play();
    if (result) void result.catch(() => undefined);
  } catch {
    // The next user-initiated main-media play event will retry playback.
  }
}

export function createSynchronizedAudioSidecar(
  options: SynchronizedAudioSidecarOptions,
): SynchronizedAudioSidecar {
  const { container, mainMedia, offsetSeconds, playlistUrl } = options;
  const audio = document.createElement("audio");
  audio.hidden = true;
  audio.setAttribute("aria-hidden", "true");
  audio.tabIndex = -1;
  audio.preload = "auto";
  container.append(audio);

  let enabled = options.enabled ?? true;
  let destroyed = false;
  let settled = false;
  let mediaReady = false;
  let manifestReady = false;
  let readyForPlayback = false;
  let hls: Hls | undefined;
  let resolveReady: (() => void) | undefined;
  let rejectReady: ((reason: unknown) => void) | undefined;

  const ready = new Promise<void>((resolve, reject) => {
    resolveReady = resolve;
    rejectReady = reject;
  });

  const listeners: Array<{
    event: keyof HTMLMediaElementEventMap;
    listener: EventListener;
  }> = [];

  const addMainListener = (
    event: keyof HTMLMediaElementEventMap,
    listener: EventListener,
  ): void => {
    mainMedia.addEventListener(event, listener);
    listeners.push({ event, listener });
  };

  const targetTime = (): number => {
    const requested = mainMedia.currentTime + offsetSeconds;
    if (!Number.isFinite(requested)) return 0;
    const duration = audio.duration;
    if (Number.isFinite(duration) && duration >= 0) {
      return Math.min(Math.max(requested, 0), duration);
    }
    return Math.max(requested, 0);
  };

  const synchronizeTime = (force: boolean): void => {
    const target = targetTime();
    if (
      !force &&
      Number.isFinite(audio.currentTime) &&
      Math.abs(audio.currentTime - target) <= MAX_DRIFT_SECONDS
    ) {
      return;
    }
    try {
      audio.currentTime = target;
    } catch {
      // Media metadata may not be available yet; readiness retries the seek.
    }
  };

  const synchronizeProperties = (): void => {
    try {
      audio.defaultPlaybackRate = mainMedia.defaultPlaybackRate;
      audio.playbackRate = mainMedia.playbackRate;
      audio.volume = mainMedia.volume;
      audio.muted = enabled ? mainMedia.muted : true;
    } catch {
      // Browser media implementations can reject updates before attachment.
    }
  };

  const synchronizePlayback = (forceTime: boolean): void => {
    if (destroyed) return;
    synchronizeProperties();
    synchronizeTime(forceTime);
    if (!enabled || !readyForPlayback || mainMedia.paused || mainMedia.ended) {
      safePause(audio);
      return;
    }
    safePlay(audio);
  };

  const removeDomListeners = (): void => {
    for (const { event, listener } of listeners.splice(0)) {
      mainMedia.removeEventListener(event, listener);
    }
    audio.removeEventListener("canplay", onCanPlay);
  };

  const teardown = (): void => {
    removeDomListeners();
    safePause(audio);
    hls?.destroy();
    hls = undefined;
    audio.removeAttribute("src");
    audio.load();
    audio.remove();
  };

  const fail = (error: AudioSidecarError): void => {
    if (destroyed) return;
    const wasReady = settled;
    settled = true;
    destroyed = true;
    clearTimeout(timeout);
    teardown();
    if (wasReady) options.onError?.(error);
    else rejectReady?.(error);
    rejectReady = undefined;
    resolveReady = undefined;
  };

  const finishReady = (): void => {
    if (destroyed || settled || !manifestReady || !mediaReady) return;
    settled = true;
    readyForPlayback = true;
    clearTimeout(timeout);
    synchronizePlayback(true);
    resolveReady?.();
    rejectReady = undefined;
    resolveReady = undefined;
  };

  function onCanPlay(): void {
    mediaReady = true;
    finishReady();
  }

  const onPlay = (): void => synchronizePlayback(true);
  const onPause = (): void => synchronizePlayback(false);
  const onSeek = (): void => synchronizePlayback(true);
  const onTimeUpdate = (): void => synchronizeTime(false);
  const onRateChange = (): void => synchronizeProperties();
  const onVolumeChange = (): void => synchronizeProperties();

  addMainListener("play", onPlay);
  addMainListener("pause", onPause);
  addMainListener("ended", onPause);
  addMainListener("seeking", onSeek);
  addMainListener("seeked", onSeek);
  addMainListener("timeupdate", onTimeUpdate);
  addMainListener("ratechange", onRateChange);
  addMainListener("volumechange", onVolumeChange);
  audio.addEventListener("canplay", onCanPlay);
  synchronizeProperties();

  const timeout = window.setTimeout(() => {
    fail(
      new AudioSidecarError(
        "timeout",
        "The synchronized audio track did not become ready in time.",
      ),
    );
  }, AUDIO_SIDECAR_READY_TIMEOUT_MS);

  if (!Number.isFinite(offsetSeconds)) {
    fail(
      new AudioSidecarError(
        "load-failed",
        "The synchronized audio track could not be loaded.",
      ),
    );
  } else if (!Hls.isSupported()) {
    fail(
      new AudioSidecarError(
        "unsupported",
        "Synchronized audio playback is not supported by this browser.",
      ),
    );
  } else {
    try {
      hls = new Hls();
      hls.on(Events.MEDIA_ATTACHED, () => {
        if (destroyed) return;
        try {
          hls?.loadSource(playlistUrl);
        } catch {
          fail(
            new AudioSidecarError(
              "load-failed",
              "The synchronized audio track could not be loaded.",
            ),
          );
        }
      });
      hls.on(Events.MANIFEST_PARSED, () => {
        manifestReady = true;
        finishReady();
      });
      hls.on(Events.ERROR, (_event, data: ErrorData) => {
        if (!data.fatal) return;
        fail(
          new AudioSidecarError(
            "load-failed",
            "The synchronized audio track could not be loaded.",
          ),
        );
      });
      hls.attachMedia(audio);
    } catch {
      fail(
        new AudioSidecarError(
          "load-failed",
          "The synchronized audio track could not be loaded.",
        ),
      );
    }
  }

  return {
    ready,
    setEnabled(nextEnabled: boolean) {
      if (destroyed || enabled === nextEnabled) return;
      enabled = nextEnabled;
      synchronizePlayback(true);
    },
    destroy() {
      if (destroyed) return;
      destroyed = true;
      readyForPlayback = false;
      clearTimeout(timeout);
      teardown();
      if (!settled) {
        settled = true;
        rejectReady?.(abortError());
        rejectReady = undefined;
        resolveReady = undefined;
      }
    },
  };
}
