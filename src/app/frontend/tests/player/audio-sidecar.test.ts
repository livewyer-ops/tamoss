import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const hlsMock = vi.hoisted(() => ({
  events: {
    ERROR: "hlsError",
    MANIFEST_PARSED: "hlsManifestParsed",
    MEDIA_ATTACHED: "hlsMediaAttached",
  },
  instances: [] as unknown[],
  isSupported: vi.fn(() => true),
}));

interface MockHlsInstance {
  attachMedia: ReturnType<typeof vi.fn>;
  destroy: ReturnType<typeof vi.fn>;
  emit(event: string, data?: unknown): void;
  loadSource: ReturnType<typeof vi.fn>;
}

vi.mock("hls.js", () => {
  class MockHls {
    static Events = hlsMock.events;
    static isSupported = hlsMock.isSupported;
    private readonly handlers = new Map<
      string,
      Array<(event: string, data: unknown) => void>
    >();
    attachMedia = vi.fn();
    destroy = vi.fn();
    loadSource = vi.fn();

    constructor() {
      hlsMock.instances.push(this);
    }

    on(event: string, handler: (event: string, data: unknown) => void): void {
      const handlers = this.handlers.get(event) ?? [];
      handlers.push(handler);
      this.handlers.set(event, handlers);
    }

    emit(event: string, data: unknown = {}): void {
      for (const handler of this.handlers.get(event) ?? []) {
        handler(event, data);
      }
    }

    off(event: string, handler: (event: string, data: unknown) => void): void {
      this.handlers.set(
        event,
        (this.handlers.get(event) ?? []).filter(
          (candidate) => candidate !== handler,
        ),
      );
    }
  }

  return { default: MockHls, Events: hlsMock.events };
});

import {
  AUDIO_SIDECAR_READY_TIMEOUT_MS,
  type AudioSidecarError,
  createSynchronizedAudioSidecar,
} from "@/player/audio-sidecar";

function setMediaProperty(
  media: HTMLMediaElement,
  property: string,
  value: unknown,
): void {
  Object.defineProperty(media, property, {
    configurable: true,
    writable: true,
    value,
  });
}

function latestHls(): MockHlsInstance {
  return hlsMock.instances[hlsMock.instances.length - 1] as MockHlsInstance;
}

function makeMainMedia(): HTMLVideoElement {
  const main = document.createElement("video");
  setMediaProperty(main, "currentTime", 10);
  setMediaProperty(main, "paused", true);
  setMediaProperty(main, "ended", false);
  setMediaProperty(main, "playbackRate", 1);
  setMediaProperty(main, "defaultPlaybackRate", 1);
  setMediaProperty(main, "volume", 1);
  setMediaProperty(main, "muted", false);
  return main;
}

async function makeReady(
  handle: ReturnType<typeof createSynchronizedAudioSidecar>,
  audio: HTMLAudioElement,
): Promise<void> {
  latestHls().emit(hlsMock.events.MEDIA_ATTACHED);
  latestHls().emit(hlsMock.events.MANIFEST_PARSED);
  audio.dispatchEvent(new Event("canplay"));
  await handle.ready;
}

beforeEach(() => {
  hlsMock.instances.length = 0;
  hlsMock.isSupported.mockReturnValue(true);
  vi.spyOn(HTMLMediaElement.prototype, "pause").mockImplementation(() => {});
  vi.spyOn(HTMLMediaElement.prototype, "play").mockResolvedValue(undefined);
  vi.spyOn(HTMLMediaElement.prototype, "load").mockImplementation(() => {});
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  document.body.replaceChildren();
});

describe("synchronised audio sidecar", () => {
  it("loads a hidden HLS audio element and follows main media state", async () => {
    const container = document.createElement("div");
    const main = makeMainMedia();
    document.body.append(container, main);
    const signedPlaylist =
      "blob:https://app.example/manifest?X-Amz-Signature=private";
    const handle = createSynchronizedAudioSidecar({
      container,
      mainMedia: main,
      playlistUrl: signedPlaylist,
      offsetSeconds: 2,
      label: "English",
    });
    const audio = container.querySelector("audio") as HTMLAudioElement;
    const audioPlay = vi.spyOn(audio, "play").mockResolvedValue(undefined);
    const audioPause = vi.spyOn(audio, "pause").mockImplementation(() => {});

    expect(audio.hidden).toBe(true);
    expect(audio).toHaveAttribute("aria-hidden", "true");
    expect(audio).not.toHaveAttribute("src");
    expect(audio.textContent).toBe("");
    expect(latestHls().attachMedia).toHaveBeenCalledWith(audio);

    await makeReady(handle, audio);

    expect(latestHls().loadSource).toHaveBeenCalledWith(signedPlaylist);
    expect(audio).not.toHaveAttribute("src", signedPlaylist);
    expect(audio.currentTime).toBe(12);

    setMediaProperty(main, "paused", false);
    main.dispatchEvent(new Event("play"));
    expect(audioPlay).toHaveBeenCalled();

    setMediaProperty(main, "currentTime", 20);
    main.dispatchEvent(new Event("seeking"));
    expect(audio.currentTime).toBe(22);

    audio.currentTime = 21.75;
    main.dispatchEvent(new Event("timeupdate"));
    expect(audio.currentTime).toBe(21.75);
    audio.currentTime = 21.749;
    main.dispatchEvent(new Event("timeupdate"));
    expect(audio.currentTime).toBe(22);

    setMediaProperty(main, "playbackRate", 1.5);
    main.dispatchEvent(new Event("ratechange"));
    expect(audio.playbackRate).toBe(1.5);

    setMediaProperty(main, "volume", 0.4);
    setMediaProperty(main, "muted", true);
    main.dispatchEvent(new Event("volumechange"));
    expect(audio.volume).toBe(0.4);
    expect(audio.muted).toBe(true);

    setMediaProperty(main, "paused", true);
    main.dispatchEvent(new Event("pause"));
    expect(audioPause).toHaveBeenCalled();

    handle.destroy();
    expect(latestHls().destroy).toHaveBeenCalledOnce();
    expect(container).toBeEmptyDOMElement();
  });

  it("keeps disabled renditions loaded and switches without mixing", async () => {
    const container = document.createElement("div");
    const main = makeMainMedia();
    setMediaProperty(main, "paused", false);
    setMediaProperty(main, "currentTime", 30);
    setMediaProperty(main, "volume", 0.6);
    const handle = createSynchronizedAudioSidecar({
      container,
      mainMedia: main,
      playlistUrl: "blob:audio-playlist",
      offsetSeconds: -5,
      label: "Commentary",
      enabled: false,
    });
    const audio = container.querySelector("audio") as HTMLAudioElement;
    const play = vi.spyOn(audio, "play").mockResolvedValue(undefined);
    const pause = vi.spyOn(audio, "pause").mockImplementation(() => {});

    await makeReady(handle, audio);
    expect(audio.muted).toBe(true);
    expect(play).not.toHaveBeenCalled();
    expect(pause).toHaveBeenCalled();

    handle.setEnabled(true);
    expect(audio.currentTime).toBe(25);
    expect(audio.volume).toBe(0.6);
    expect(audio.muted).toBe(false);
    expect(play).toHaveBeenCalledOnce();

    handle.setEnabled(false);
    expect(audio.muted).toBe(true);
    expect(pause).toHaveBeenCalled();

    main.dispatchEvent(new Event("play"));
    expect(play).toHaveBeenCalledOnce();

    handle.setEnabled(true);
    expect(play).toHaveBeenCalledTimes(2);
    handle.destroy();
  });

  it("redacts fatal HLS error details and removes the failed sidecar", async () => {
    const container = document.createElement("div");
    const main = makeMainMedia();
    const secretUrl =
      "https://media.example/audio.m3u8?X-Amz-Signature=do-not-log";
    const handle = createSynchronizedAudioSidecar({
      container,
      mainMedia: main,
      playlistUrl: secretUrl,
      offsetSeconds: 0,
      label: "Audio",
    });
    const failed = expect(handle.ready).rejects.toMatchObject({
      code: "load-failed",
      name: "AudioSidecarError",
    });

    latestHls().emit(hlsMock.events.ERROR, {
      fatal: true,
      error: new Error(`request failed: ${secretUrl}`),
      response: { url: secretUrl },
    });
    await failed;

    let error: AudioSidecarError | undefined;
    try {
      await handle.ready;
    } catch (caught) {
      error = caught as AudioSidecarError;
    }
    expect(error?.message).not.toContain(secretUrl);
    expect(error?.stack).not.toContain(secretUrl);
    expect(latestHls().destroy).toHaveBeenCalledOnce();
    expect(container).toBeEmptyDOMElement();
  });

  it("reports fatal HLS errors that occur after readiness", async () => {
    const container = document.createElement("div");
    const main = makeMainMedia();
    const onError = vi.fn();
    const handle = createSynchronizedAudioSidecar({
      container,
      mainMedia: main,
      playlistUrl: "blob:audio-playlist",
      offsetSeconds: 0,
      label: "Audio",
      onError,
    });
    const audio = container.querySelector("audio") as HTMLAudioElement;
    await makeReady(handle, audio);

    latestHls().emit(hlsMock.events.ERROR, {
      fatal: true,
      error: new Error(
        "https://media.example/audio.ts?X-Amz-Signature=do-not-log",
      ),
    });

    expect(onError).toHaveBeenCalledOnce();
    const error = onError.mock.calls[0][0] as AudioSidecarError;
    expect(error).toMatchObject({
      code: "load-failed",
      message: "The synchronised audio track could not be loaded.",
      name: "AudioSidecarError",
    });
    expect(error.stack).not.toContain("X-Amz-Signature");
    expect(latestHls().destroy).toHaveBeenCalledOnce();
    expect(container).toBeEmptyDOMElement();
  });

  it("times out with a sanitized error and performs complete cleanup", async () => {
    vi.useFakeTimers();
    const container = document.createElement("div");
    const main = makeMainMedia();
    const handle = createSynchronizedAudioSidecar({
      container,
      mainMedia: main,
      playlistUrl: "blob:https://app.example/private-playlist",
      offsetSeconds: 0,
      label: "Audio",
    });
    const failed = expect(handle.ready).rejects.toMatchObject({
      code: "timeout",
      name: "AudioSidecarError",
    });

    await vi.advanceTimersByTimeAsync(AUDIO_SIDECAR_READY_TIMEOUT_MS);
    await failed;

    expect(latestHls().destroy).toHaveBeenCalledOnce();
    expect(container).toBeEmptyDOMElement();
    main.dispatchEvent(new Event("play"));
    expect(latestHls().destroy).toHaveBeenCalledOnce();
  });

  it("rejects pending readiness and cleans up when destroyed", async () => {
    const container = document.createElement("div");
    const handle = createSynchronizedAudioSidecar({
      container,
      mainMedia: makeMainMedia(),
      playlistUrl: "blob:audio-playlist",
      offsetSeconds: 0,
      label: "Audio",
    });
    const aborted = expect(handle.ready).rejects.toMatchObject({
      name: "AbortError",
    });

    handle.destroy();
    await aborted;

    expect(latestHls().destroy).toHaveBeenCalledOnce();
    expect(container).toBeEmptyDOMElement();
  });
});
