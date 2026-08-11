import { beforeEach, describe, expect, it, vi } from "vitest";
import type { MediaPreviewDescriptor, PreviewTrack } from "@/player/descriptor";

const mocks = vi.hoisted(() => {
  const instances: MockPlayer[] = [];
  const plan = {
    kind: "hls" as const,
    url: "blob:master",
    mainUrl: "blob:video",
    audioSidecars: [] as Array<{
      flowId: string;
      label: string;
      offsetSeconds: number;
      url: string;
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
    failTimeline = false;
    eventObserver?: Observer;
    player = {
      getDuration: vi.fn(() => 12),
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
      this.mainMediaElement.pause = vi.fn();
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

  const createAudioSidecar = vi.fn((_options: unknown) => ({
    ready: Promise.resolve(),
    setEnabled: vi.fn(),
    destroy: vi.fn(),
  }));

  return { MockPlayer, createAudioSidecar, instances, plan };
});

vi.mock("@byomakase/omakase-player/dist/omakase-player.es.js", () => ({
  ChromingTheme: { DEFAULT: "DEFAULT" },
  FileFormatType: { HLS: "HLS", MP4: "MP4", MP4_AUDIO: "MP4_AUDIO" },
  MainMediaType: { HLS: "HLS", MP4: "MP4", AUDIO_FILE: "AUDIO_FILE" },
  OmakasePlayer: mocks.MockPlayer,
  PlayerAudioMode: { SINGLE: "SINGLE" },
  PlayerEventType: {
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

vi.mock("@/player/audio-sidecar", () => ({
  createSynchronizedAudioSidecar: mocks.createAudioSidecar,
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
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.replaceChildren();
    mocks.instances.length = 0;
    mocks.plan.audioSidecars = [];
    mocks.plan.trimmed = false;
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

    expect(player.config).toEqual({
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
      warning: "Timeline visualization is unavailable for this media.",
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

  it("derives buffering and paused states from playback snapshots", async () => {
    const onChange = vi.fn();
    const handle = createOmakasePreview({
      descriptor: descriptor(),
      playerElementId: "player",
      timelineElementId: "timeline",
      onChange,
    });
    const player = mocks.instances[0];
    await handle.ready;

    player.emitEvent(playbackEvent({ buffering: true, currentTime: 4 }));
    expect(onChange).toHaveBeenLastCalledWith({
      phase: "buffering",
      currentTime: 4,
      duration: 12,
    });

    player.emitEvent(playbackEvent({ paused: true, currentTime: 5 }));
    expect(onChange).toHaveBeenLastCalledWith({
      phase: "paused",
      currentTime: 5,
      duration: 12,
    });

    player.emitEvent(playbackEvent({ playing: true, currentTime: 6 }));
    expect(onChange).toHaveBeenLastCalledWith({
      phase: "playing",
      currentTime: 6,
      duration: 12,
    });

    player.emitEvent(playbackEvent({ ended: true, currentTime: 12 }));
    expect(onChange).toHaveBeenLastCalledWith({
      phase: "ended",
      currentTime: 12,
      duration: 12,
    });
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

  it("reports no switchable renditions when playback has no audio sidecar", async () => {
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

  it("loads synchronized audio and switches renditions", async () => {
    document.body.innerHTML = '<div id="player"></div>';
    mocks.plan.audioSidecars = [
      {
        flowId: "audio-1",
        label: "Programme",
        offsetSeconds: 2,
        url: "blob:audio-1",
      },
      {
        flowId: "audio-2",
        label: "Commentary",
        offsetSeconds: 3,
        url: "blob:audio-2",
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
    expect(mocks.createAudioSidecar).toHaveBeenCalledTimes(2);
    expect(mocks.createAudioSidecar).toHaveBeenNthCalledWith(
      1,
      expect.objectContaining({
        enabled: true,
        label: "Programme",
        offsetSeconds: 2,
        playlistUrl: "blob:audio-1",
      }),
    );
    expect(mocks.createAudioSidecar).toHaveBeenNthCalledWith(
      2,
      expect.objectContaining({
        enabled: false,
        label: "Commentary",
        offsetSeconds: 3,
        playlistUrl: "blob:audio-2",
      }),
    );

    handle.selectAudioTrack("audio-2");
    const first = mocks.createAudioSidecar.mock.results[0].value;
    const second = mocks.createAudioSidecar.mock.results[1].value;
    expect(first.setEnabled).toHaveBeenCalledWith(false);
    expect(second.setEnabled).toHaveBeenCalledWith(true);
    handle.destroy();
    expect(first.destroy).toHaveBeenCalledOnce();
    expect(second.destroy).toHaveBeenCalledOnce();
  });

  it("does not report ready until every declared audio rendition is ready", async () => {
    document.body.innerHTML = '<div id="player"></div>';
    mocks.plan.audioSidecars = [
      {
        flowId: "audio-1",
        label: "Programme",
        offsetSeconds: 2,
        url: "blob:audio-1",
      },
    ];
    let resolveAudio: (() => void) | undefined;
    mocks.createAudioSidecar.mockImplementationOnce(() => ({
      ready: new Promise<void>((resolve) => {
        resolveAudio = resolve;
      }),
      setEnabled: vi.fn(),
      destroy: vi.fn(),
    }));
    const onChange = vi.fn();
    const handle = createOmakasePreview({
      descriptor: descriptor(),
      playerElementId: "player",
      timelineElementId: "timeline",
      onChange,
    });

    await vi.waitFor(() => {
      expect(mocks.createAudioSidecar).toHaveBeenCalledOnce();
    });
    expect(onChange).not.toHaveBeenCalledWith(
      expect.objectContaining({ phase: "ready" }),
    );

    resolveAudio?.();
    await handle.ready;

    expect(onChange).toHaveBeenCalledWith(
      expect.objectContaining({ phase: "ready" }),
    );
    handle.destroy();
  });

  it("fails closed when synchronized audio fails after readiness", async () => {
    document.body.innerHTML = '<div id="player"></div>';
    mocks.plan.audioSidecars = [
      {
        flowId: "audio-1",
        label: "Programme",
        offsetSeconds: 2,
        url: "blob:audio-1",
      },
    ];
    const onChange = vi.fn();
    const handle = createOmakasePreview({
      descriptor: descriptor(),
      playerElementId: "player",
      timelineElementId: "timeline",
      onChange,
    });
    await handle.ready;

    const options = mocks.createAudioSidecar.mock.calls[0][0] as {
      onError?: (error: Error) => void;
    };
    options.onError?.(new Error("signed URL expired"));

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

function playbackEvent(
  overrides: Partial<{
    buffering: boolean;
    currentTime: number;
    ended: boolean;
    paused: boolean;
    pausing: boolean;
    playing: boolean;
    waiting: boolean;
    waitingSyncedMedia: boolean;
  }>,
) {
  return {
    type: "PLAYER_PLAYBACK_CHANGE",
    data: {
      playerPlayback: {
        buffering: false,
        currentTime: 0,
        ended: false,
        paused: false,
        pausing: false,
        playing: false,
        waiting: false,
        waitingSyncedMedia: false,
        ...overrides,
      },
    },
  };
}
