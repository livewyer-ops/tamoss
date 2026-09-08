import { Events } from "hls.js";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { MediaPreviewDescriptor, PreviewTrack } from "@/player/descriptor";

const mocks = vi.hoisted(() => {
  const instances: MockPlayer[] = [];
  const plan = {
    kind: "hls" as "hls" | "direct",
    url: "blob:master",
    mediaKind: "video" as "video" | "audio",
    mimeType: "video/mp4",
    mainUrl: "blob:video",
    audioTracks: [] as Array<{
      flowId: string;
      label: string;
    }>,
    trimmed: false,
    masterManifest: "#EXTM3U",
    mediaManifests: new Map(),
    dispose: vi.fn(),
  };

  class MockPlayer {
    config: unknown;
    destroy = vi.fn();
    mainMediaElement = document.createElement("video");
    eventUnsubscribe = vi.fn();
    loadUnsubscribe = vi.fn();
    timelineUnsubscribe = vi.fn();
    failLoad = false;
    stallLoad = false;
    failTimeline = false;
    paused = true;
    readyState = 4;
    seeking = false;
    ranges = [[0, 12]];
    hlsEvents = new Map<string, (...args: unknown[]) => void>();
    hls = {
      config: { maxBufferLength: 30 },
      audioTrack: 0,
      on: vi.fn((event: string, listener: (...args: unknown[]) => void) =>
        this.hlsEvents.set(event, listener),
      ),
      off: vi.fn((event: string) => this.hlsEvents.delete(event)),
    };
    eventObserver?: Observer;
    player = {
      getDuration: vi.fn(() => 12),
      getPlaybackEngine: vi.fn(() => ({ hls: this.hls })),
      audio: { audioContext: { resume: vi.fn(() => Promise.resolve()) } },
      play: vi.fn(() => ({
        subscribe: ({ complete }: Observer) => {
          this.paused = false;
          complete?.();
          return { unsubscribe: vi.fn() };
        },
      })),
      seekTo: vi.fn((time: number) => ({
        subscribe: ({ complete }: Observer) => {
          this.mainMediaElement.currentTime = time;
          complete?.();
          return { unsubscribe: vi.fn() };
        },
      })),
      htmlMediaElement: this.mainMediaElement,
      playerLocal: {
        htmlMediaElement: this.mainMediaElement,
      },
      onEvent$: {
        subscribe: vi.fn((observer: Observer) => {
          this.eventObserver = observer;
          return { unsubscribe: this.eventUnsubscribe };
        }),
      },
    };
    loadMainMedia = vi.fn(() => ({
      subscribe: ({ next, error }: Observer) => {
        queueMicrotask(() => {
          if (this.stallLoad) return;
          if (this.failLoad) {
            error?.(
              new Error(
                "https://storage.example/media.ts?X-Amz-Signature=private",
              ),
            );
          } else {
            next?.({});
          }
        });
        return { unsubscribe: this.loadUnsubscribe };
      },
    }));
    createTimeline = vi.fn(() => ({
      subscribe: ({ next, error }: Observer) => {
        queueMicrotask(() => {
          if (this.failTimeline) error?.(new Error("Timeline unavailable"));
          else next?.({});
        });
        return { unsubscribe: this.timelineUnsubscribe };
      },
    }));

    constructor(config: unknown) {
      this.config = config;
      this.mainMediaElement.pause = vi.fn(() => {
        this.paused = true;
      });
      Object.defineProperties(this.mainMediaElement, {
        paused: { get: () => this.paused },
        readyState: { get: () => this.readyState },
        seeking: { get: () => this.seeking },
        buffered: {
          get: () => ({
            length: this.ranges.length,
            start: (i: number) => this.ranges[i][0],
            end: (i: number) => this.ranges[i][1],
          }),
        },
      });
      instances.push(this);
      const playerId = (config as { playerHtmlElementId?: string })
        .playerHtmlElementId;
      if (playerId) {
        document
          .getElementById(playerId)
          ?.append(this.player.playerLocal.htmlMediaElement);
      }
    }

    emitEvent(event: unknown) {
      this.eventObserver?.next?.(event);
    }
  }

  interface Observer {
    next?: (value: unknown) => void;
    error?: (error: unknown) => void;
    complete?: () => void;
  }

  return { MockPlayer, instances, plan };
});

vi.mock("@byomakase/omakase-player/dist/omakase-player.es.js", () => ({
  ChromingTheme: { DEFAULT: "DEFAULT" },
  FileFormatType: { HLS: "HLS", MP4: "MP4", MP4_AUDIO: "MP4_AUDIO" },
  MainMediaType: { HLS: "HLS", MP4: "MP4", AUDIO_FILE: "AUDIO_FILE" },
  OmakasePlayer: mocks.MockPlayer,
  PlayerAudioMode: { SINGLE: "SINGLE" },
  PlayerEventType: {
    PLAYER_MAIN_MEDIA_LOAD_ERROR: "PLAYER_MAIN_MEDIA_LOAD_ERROR",
    PLAYER_PLAY: "PLAYER_PLAY",
    PLAYER_PAUSE: "PLAYER_PAUSE",
    PLAYER_PLAYBACK_CHANGE: "PLAYER_PLAYBACK_CHANGE",
    PLAYER_ENDED: "PLAYER_ENDED",
    PLAYER_PLAYBACK_PROGRESS: "PLAYER_PLAYBACK_PROGRESS",
  },
}));

vi.mock("@/player/hls-manifest", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/player/hls-manifest")>()),
  compilePlaybackPlan: vi.fn(() => mocks.plan),
}));

import { compilePlaybackPlan } from "@/player/hls-manifest";
import {
  createOmakasePreview,
  PreviewPlaybackError,
} from "@/player/OmakaseAdapter";

function descriptor(): MediaPreviewDescriptor {
  const video = {
    kind: "video" as const,
    flow: {
      id: "video-1",
      source_id: "source-1",
      format: "urn:x-nmos:format:video",
      codec: "video/h264",
      container: "video/mp2t",
      timerange: "[100:0_106:0)",
      essence_parameters: {
        frame_rate: { numerator: 25, denominator: 1 },
      },
    },
    segments: [
      {
        object_id: "object-1",
        timerange: "[100:0_106:0)",
        get_urls: [
          {
            url: "https://storage.example/object-1.ts?signature=secret",
            credentials: "omit" as const,
            presigned: true,
          },
        ],
      },
    ],
    truncated: false,
    rejectedUrlCount: 0,
  };
  return {
    rootFlow: video.flow,
    tracks: [video],
    video,
    audio: [],
    images: [],
    data: [],
    initialTimerange: "[100:0_106:0)",
    segmentCount: 1,
    truncated: false,
    flowsSegments: new Map([[video.flow.id, video.segments]]),
  };
}

function audioDescriptor(): MediaPreviewDescriptor {
  const base = descriptor();
  if (!base.video) throw new Error("Video fixture missing");
  const audio: PreviewTrack = {
    ...base.video,
    kind: "audio",
    flow: {
      ...base.video.flow,
      format: "urn:x-nmos:format:audio",
      codec: "audio/aac",
      container: "audio/mp2t",
      essence_parameters: { sample_rate: 48_000 },
    },
  };
  return {
    ...base,
    rootFlow: audio.flow,
    tracks: [audio],
    video: undefined,
    audio: [audio],
    flowsSegments: new Map([[audio.flow.id, audio.segments]]),
  };
}

describe("OmakaseAdapter", () => {
  afterEach(() => vi.useRealTimers());
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML =
      '<div id="player"><omakase-play-button></omakase-play-button></div>';
    mocks.instances.length = 0;
    mocks.plan.audioTracks = [];
    mocks.plan.url = "blob:master";
    mocks.plan.trimmed = false;
    mocks.plan.kind = "hls";
  });

  it("subscribes to load and timeline operations then tears everything down", async () => {
    const onChange = vi.fn();
    const handle = createOmakasePreview({
      descriptor: descriptor(),
      playerElementId: "player",
      timelineElementId: "timeline",
      onChange,
    });
    const player = mocks.instances[0];

    await handle.ready;

    expect(player.config).toMatchObject({
      playerHtmlElementId: "player",
      playerAudioMode: "SINGLE",
      chromingTheme: "DEFAULT",
    });
    expect(player.loadMainMedia).toHaveBeenCalledWith("blob:video", {
      fileFormatType: "HLS",
      frameRate: 25,
      mainMediaType: "HLS",
    });
    expect(player.createTimeline).toHaveBeenCalledWith(
      expect.objectContaining({ htmlElementId: "timeline" }),
    );
    expect(onChange).toHaveBeenLastCalledWith({
      phase: "ready",
      currentTime: 0,
      duration: 12,
    });

    handle.destroy();
    handle.destroy();

    expect(player.eventUnsubscribe).toHaveBeenCalledOnce();
    expect(player.loadUnsubscribe).toHaveBeenCalledOnce();
    expect(player.timelineUnsubscribe).toHaveBeenCalledOnce();
    await vi.waitFor(() => {
      expect(player.destroy).toHaveBeenCalledOnce();
      expect(mocks.plan.dispose).toHaveBeenCalledOnce();
    });
  });

  it("replaces player errors without exposing signed URLs", async () => {
    const onChange = vi.fn();
    const handle = createOmakasePreview({
      descriptor: descriptor(),
      playerElementId: "player",
      timelineElementId: "timeline",
      onChange,
    });
    mocks.instances[0].failLoad = true;

    await expect(handle.ready).rejects.toBeInstanceOf(PreviewPlaybackError);
    const lastSnapshot = onChange.mock.lastCall?.[0];
    expect(lastSnapshot.message).toBe(
      "Omakase could not load the selected media window.",
    );
    expect(JSON.stringify(lastSnapshot)).not.toContain("X-Amz-Signature");

    handle.destroy();
  });

  it.each(["timeout", "load-event"])(
    "ends a stalled load on %s",
    async (failure) => {
      vi.useFakeTimers();
      const onChange = vi.fn();
      const handle = createOmakasePreview({
        descriptor: descriptor(),
        playerElementId: "player",
        timelineElementId: "timeline",
        onChange,
      });
      const player = mocks.instances[0];
      player.stallLoad = true;
      const rejected = expect(handle.ready).rejects.toBeInstanceOf(
        PreviewPlaybackError,
      );
      if (failure === "timeout") {
        await vi.advanceTimersByTimeAsync(30_000);
      } else {
        player.emitEvent({ type: "PLAYER_MAIN_MEDIA_LOAD_ERROR", data: {} });
      }
      await rejected;
      expect(onChange).toHaveBeenLastCalledWith(
        expect.objectContaining({ phase: "error" }),
      );
      await Promise.resolve();
      expect(player.destroy).toHaveBeenCalledOnce();
      expect(mocks.plan.dispose).toHaveBeenCalledOnce();
      expect(vi.getTimerCount()).toBe(0);
    },
  );

  it("keeps loaded media available when the canvas timeline fails", async () => {
    const onChange = vi.fn();
    const handle = createOmakasePreview({
      descriptor: descriptor(),
      playerElementId: "player",
      timelineElementId: "timeline",
      onChange,
    });
    const player = mocks.instances[0];
    player.failTimeline = true;

    await expect(handle.ready).resolves.toBeUndefined();
    expect(onChange).toHaveBeenLastCalledWith({
      phase: "ready",
      currentTime: 0,
      duration: 12,
      warning: "Timeline visualisation is unavailable for this media.",
    });
    expect(player.destroy).not.toHaveBeenCalled();

    handle.destroy();
  });

  it("uses a validated nominal frame-rate tag for VFR media", async () => {
    const tagged = descriptor();
    if (!tagged.video) throw new Error("Video fixture missing");
    tagged.video.flow.essence_parameters = { vfr: true };
    tagged.video.flow.tags = { nominal_fps: "12/1" };
    const handle = createOmakasePreview({
      descriptor: tagged,
      playerElementId: "player",
      timelineElementId: "timeline",
      onChange: vi.fn(),
    });

    await handle.ready;

    expect(mocks.instances[0].loadMainMedia).toHaveBeenCalledWith(
      "blob:video",
      expect.objectContaining({ frameRate: "12/1" }),
    );
    handle.destroy();
  });

  it("supplies an operational timeline timebase for audio-only HLS", async () => {
    const handle = createOmakasePreview({
      descriptor: audioDescriptor(),
      playerElementId: "player",
      timelineElementId: "timeline",
      onChange: vi.fn(),
    });

    await handle.ready;

    expect(mocks.instances[0].loadMainMedia).toHaveBeenCalledWith(
      "blob:video",
      expect.objectContaining({
        fileFormatType: "HLS",
        frameRate: 25,
        mainMediaType: "HLS",
      }),
    );
    handle.destroy();
  });

  it("holds a low buffer and respects pause during recovery", async () => {
    vi.useFakeTimers();
    const onChange = vi.fn();
    const handle = createOmakasePreview({
      descriptor: descriptor(),
      playerElementId: "player",
      timelineElementId: "timeline",
      onChange,
    });
    const player = mocks.instances[0];
    await handle.ready;

    const button = document.querySelector<HTMLElement>("omakase-play-button");
    if (!button) throw new Error("Play control missing");
    button.click();
    expect(player.player.play).toHaveBeenCalledOnce();
    player.mainMediaElement.currentTime = 4;
    player.ranges = [[0, 4.5]];
    await vi.advanceTimersByTimeAsync(100);
    expect(player.paused).toBe(true);
    expect(onChange).toHaveBeenLastCalledWith(
      expect.objectContaining({ phase: "buffering", currentTime: 4 }),
    );
    button.click();
    player.ranges = [[0, 12]];
    await vi.advanceTimersByTimeAsync(100);
    expect(player.player.play).toHaveBeenCalledOnce();
    expect(player.mainMediaElement.currentTime).toBe(4);
    expect(onChange).toHaveBeenLastCalledWith(
      expect.objectContaining({ phase: "paused" }),
    );
    handle.destroy();
  });

  it("reports when split tracks reduce the playable window", async () => {
    mocks.plan.trimmed = true;
    const onChange = vi.fn();
    const handle = createOmakasePreview({
      descriptor: descriptor(),
      playerElementId: "player",
      timelineElementId: "timeline",
      onChange,
    });

    await handle.ready;

    expect(onChange).toHaveBeenLastCalledWith({
      phase: "ready",
      currentTime: 0,
      duration: 12,
      warning:
        "Playback is limited to the timerange shared by video and audio tracks.",
    });
    handle.destroy();
  });

  it("loads one standalone MP4 Object directly", async () => {
    mocks.plan.kind = "direct";
    mocks.plan.url = "https://storage.example/object.mp4?signature=secret";
    const onChange = vi.fn();
    const handle = createOmakasePreview({
      descriptor: descriptor(),
      playerElementId: "player",
      timelineElementId: "timeline",
      onChange,
    });

    await handle.ready;

    expect(mocks.instances[0].loadMainMedia).toHaveBeenCalledWith(
      mocks.plan.url,
      expect.objectContaining({
        fileFormatType: "MP4",
        mainMediaType: "MP4",
      }),
    );
    expect(onChange).toHaveBeenLastCalledWith({
      phase: "ready",
      currentTime: 0,
      duration: 12,
    });
    expect(JSON.stringify(onChange.mock.calls)).not.toContain(
      "signature=secret",
    );
    handle.destroy();
  });

  it("hands the manifest compiler a half-open playback window", async () => {
    const inclusive = descriptor();
    const handle = createOmakasePreview({
      descriptor: { ...inclusive, initialTimerange: "[100:0_106:0]" },
      playerElementId: "player",
      timelineElementId: "timeline",
      onChange: vi.fn(),
    });

    await handle.ready;

    expect(compilePlaybackPlan).toHaveBeenCalledWith({
      tracks: inclusive.tracks,
      initialTimerange: "[100:0_106:1)",
    });
    handle.destroy();
  });

  it("refuses a window with no playable duration", () => {
    expect(() =>
      createOmakasePreview({
        descriptor: { ...descriptor(), initialTimerange: "[100:0]" },
        playerElementId: "player",
        timelineElementId: "timeline",
        onChange: vi.fn(),
      }),
    ).toThrowError(expect.objectContaining({ code: "no-playable-media" }));
    expect(compilePlaybackPlan).not.toHaveBeenCalled();
  });

  it("reports no switchable renditions for a single main track", async () => {
    const handle = createOmakasePreview({
      descriptor: descriptor(),
      playerElementId: "player",
      timelineElementId: "timeline",
      onChange: vi.fn(),
    });

    expect(handle.audioTracks).toEqual([]);
    await handle.ready;
    handle.destroy();
  });

  it("loads a single HLS master and switches native audio renditions", async () => {
    document.body.innerHTML = '<div id="player"></div>';
    mocks.plan.audioTracks = [
      {
        flowId: "audio-1",
        label: "Programme",
      },
      {
        flowId: "audio-2",
        label: "Commentary",
      },
    ];
    const handle = createOmakasePreview({
      descriptor: descriptor(),
      playerElementId: "player",
      timelineElementId: "timeline",
      onChange: vi.fn(),
    });

    await handle.ready;

    expect(handle.audioTracks).toEqual([
      { flowId: "audio-1", label: "Programme" },
      { flowId: "audio-2", label: "Commentary" },
    ]);
    expect(mocks.instances[0].loadMainMedia).toHaveBeenCalledWith(
      "blob:master",
      expect.any(Object),
    );
    expect(document.querySelectorAll("audio")).toHaveLength(0);

    handle.selectAudioTrack("audio-2");
    expect(mocks.instances[0].hls.audioTrack).toBe(1);
    handle.selectAudioTrack("unknown");
    expect(mocks.instances[0].hls.audioTrack).toBe(1);
    handle.destroy();
    expect(mocks.instances[0].hlsEvents.size).toBe(0);
  });

  it("queues an early play until eight contiguous seconds are buffered", async () => {
    vi.useFakeTimers();
    const onChange = vi.fn();
    const handle = createOmakasePreview({
      descriptor: descriptor(),
      playerElementId: "player",
      timelineElementId: "timeline",
      onChange,
    });

    const player = mocks.instances[0];
    player.ranges = [
      [0, 2],
      [3, 12],
    ];
    document.querySelector<HTMLElement>("omakase-play-button")?.click();
    await vi.advanceTimersByTimeAsync(100);
    expect(player.player.play).not.toHaveBeenCalled();
    expect(onChange).not.toHaveBeenCalledWith(
      expect.objectContaining({ phase: "ready" }),
    );

    player.ranges = [[0, 8]];
    await vi.advanceTimersByTimeAsync(100);
    await handle.ready;
    expect(player.player.play).toHaveBeenCalledOnce();
    expect(onChange).toHaveBeenLastCalledWith(
      expect.objectContaining({ phase: "playing" }),
    );
    handle.destroy();
  });

  it("fails closed on fatal native HLS errors", async () => {
    const onChange = vi.fn();
    const handle = createOmakasePreview({
      descriptor: descriptor(),
      playerElementId: "player",
      timelineElementId: "timeline",
      onChange,
    });
    await handle.ready;

    mocks.instances[0].hlsEvents.get(Events.ERROR)?.(Events.ERROR, {
      fatal: true,
    });

    expect(onChange).toHaveBeenLastCalledWith({
      phase: "error",
      currentTime: 0,
      duration: 12,
      message: "Omakase could not load the selected media window.",
    });
    await vi.waitFor(() => {
      expect(mocks.instances[0].destroy).toHaveBeenCalledOnce();
      expect(mocks.plan.dispose).toHaveBeenCalledOnce();
    });
  });

  it("recovers at the same position only after rebuilding the reserve", async () => {
    vi.useFakeTimers();
    const handle = createOmakasePreview({
      descriptor: descriptor(),
      playerElementId: "player",
      timelineElementId: "timeline",
      onChange: vi.fn(),
    });
    const player = mocks.instances[0];
    await handle.ready;
    document.querySelector<HTMLElement>("omakase-play-button")?.click();
    player.mainMediaElement.currentTime = 4;
    player.ranges = [[0, 4.4]];
    await vi.advanceTimersByTimeAsync(100);
    expect(player.paused).toBe(true);
    player.ranges = [[0, 8]];
    await vi.advanceTimersByTimeAsync(100);
    expect(player.paused).toBe(true);
    player.ranges = [[0, 12]];
    await vi.advanceTimersByTimeAsync(100);
    expect(player.paused).toBe(false);
    expect(player.mainMediaElement.currentTime).toBe(4);
    expect(player.player.play).toHaveBeenCalledTimes(2);
    handle.destroy();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("waits for native audio switching to finish before resuming", async () => {
    vi.useFakeTimers();
    const onChange = vi.fn();
    const handle = createOmakasePreview({
      descriptor: descriptor(),
      playerElementId: "player",
      timelineElementId: "timeline",
      onChange,
    });
    const player = mocks.instances[0];
    await handle.ready;
    document.querySelector<HTMLElement>("omakase-play-button")?.click();
    player.hlsEvents.get(Events.AUDIO_TRACK_SWITCHING)?.();
    expect(player.paused).toBe(true);
    await vi.advanceTimersByTimeAsync(100);
    expect(player.paused).toBe(true);
    player.hlsEvents.get(Events.AUDIO_TRACK_SWITCHED)?.();
    expect(player.paused).toBe(false);
    handle.destroy();
  });

  it("uses the remaining duration after a seek and respects playback rate", async () => {
    vi.useFakeTimers();
    const handle = createOmakasePreview({
      descriptor: descriptor(),
      playerElementId: "player",
      timelineElementId: "timeline",
      onChange: vi.fn(),
    });
    const player = mocks.instances[0];
    await handle.ready;
    player.mainMediaElement.playbackRate = 2;
    player.ranges = [[0, 8]];
    document.querySelector<HTMLElement>("omakase-play-button")?.click();
    expect(player.player.play).not.toHaveBeenCalled();
    player.mainMediaElement.currentTime = 10;
    player.seeking = true;
    player.ranges = [[10, 12]];
    await vi.advanceTimersByTimeAsync(100);
    expect(player.player.play).not.toHaveBeenCalled();
    player.seeking = false;
    await vi.advanceTimersByTimeAsync(100);
    expect(player.player.play).toHaveBeenCalledOnce();
    expect(player.mainMediaElement.currentTime).toBe(10);
    handle.destroy();
  });

  it("supports keyboard play/pause and cancels queued early play", async () => {
    vi.useFakeTimers();
    const handle = createOmakasePreview({
      descriptor: descriptor(),
      playerElementId: "player",
      timelineElementId: "timeline",
      onChange: vi.fn(),
    });
    const player = mocks.instances[0];
    player.ranges = [];
    const button = document.querySelector<HTMLElement>("omakase-play-button");
    if (!button) throw new Error("Play control missing");
    button.dispatchEvent(
      new KeyboardEvent("keyup", { key: " ", bubbles: true }),
    );
    button.dispatchEvent(
      new KeyboardEvent("keyup", { key: " ", bubbles: true }),
    );
    player.ranges = [[0, 12]];
    await vi.advanceTimersByTimeAsync(100);
    await handle.ready;
    expect(player.player.play).not.toHaveBeenCalled();
    handle.destroy();
  });

  it("bounds recovery failures and releases pending operations on navigation", async () => {
    vi.useFakeTimers();
    const onChange = vi.fn();
    const handle = createOmakasePreview({
      descriptor: descriptor(),
      playerElementId: "player",
      timelineElementId: "timeline",
      onChange,
    });
    const player = mocks.instances[0];
    await handle.ready;
    document.querySelector<HTMLElement>("omakase-play-button")?.click();
    player.ranges = [];
    await vi.advanceTimersByTimeAsync(30_100);
    expect(onChange).toHaveBeenLastCalledWith(
      expect.objectContaining({ phase: "error" }),
    );
    expect(player.destroy).toHaveBeenCalledOnce();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("does not resume a delayed play operation after the user pauses", async () => {
    const handle = createOmakasePreview({
      descriptor: descriptor(),
      playerElementId: "player",
      timelineElementId: "timeline",
      onChange: vi.fn(),
    });
    const player = mocks.instances[0];
    await handle.ready;
    let finishPlay: (() => void) | undefined;
    player.player.play.mockImplementationOnce(() => ({
      subscribe: (observer) => {
        finishPlay = () => {
          player.paused = false;
          observer.complete?.();
        };
        return { unsubscribe: vi.fn() };
      },
    }));
    const button = document.querySelector<HTMLElement>("omakase-play-button");
    if (!button) throw new Error("Play control missing");
    button.click();
    button.click();
    finishPlay?.();
    expect(player.paused).toBe(true);
    handle.destroy();
  });

  it("still revokes the playback plan if Omakase teardown throws", async () => {
    const handle = createOmakasePreview({
      descriptor: descriptor(),
      playerElementId: "player",
      timelineElementId: "timeline",
      onChange: vi.fn(),
    });
    await handle.ready;
    mocks.instances[0].destroy.mockImplementationOnce(() => {
      throw new Error("Omakase DOM already removed");
    });

    expect(() => handle.destroy()).not.toThrow();
    await vi.waitFor(() => expect(mocks.plan.dispose).toHaveBeenCalledOnce());
  });

  it("pauses the main media element before destroying Omakase", async () => {
    const handle = createOmakasePreview({
      descriptor: descriptor(),
      playerElementId: "player",
      timelineElementId: "timeline",
      onChange: vi.fn(),
    });
    await handle.ready;
    const player = mocks.instances[0];

    handle.destroy();

    expect(player.mainMediaElement.pause).toHaveBeenCalledOnce();
    await vi.waitFor(() => expect(player.destroy).toHaveBeenCalledOnce());
    expect(player.mainMediaElement.pause).toHaveBeenCalledBefore(
      player.destroy,
    );
  });
});
