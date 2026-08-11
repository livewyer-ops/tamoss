import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import type { FlowSegmentParams } from "@/api/client";
import type { ApiRequestOptions } from "@/api/transport";
import {
  buildMediaPreviewDescriptor,
  type MediaPreviewDescriptor,
  type PreviewDescriptorApi,
} from "@/player/descriptor";
import { compilePlaybackPlan } from "@/player/hls-manifest";
import type {
  Flow,
  FlowCollectionItem,
  FlowSegment,
  PaginatedResponse,
} from "@/types/tams";
import { halfOpenTimerange } from "@/utils/tams-time";

const mocks = vi.hoisted(() => {
  interface Observer {
    next?: (value: unknown) => void;
    error?: (error: unknown) => void;
  }
  const instances: MockPlayer[] = [];

  class MockPlayer {
    destroy = vi.fn();
    mainMediaElement = document.createElement("video");
    player = {
      getDuration: vi.fn(() => 12),
      htmlMediaElement: this.mainMediaElement,
      onEvent$: { subscribe: vi.fn(() => ({ unsubscribe: vi.fn() })) },
    };
    loadMainMedia = vi.fn(() => ({
      subscribe: ({ next }: Observer) => {
        queueMicrotask(() => next?.({}));
        return { unsubscribe: vi.fn() };
      },
    }));
    createTimeline = vi.fn(() => ({
      subscribe: ({ next }: Observer) => {
        queueMicrotask(() => next?.({}));
        return { unsubscribe: vi.fn() };
      },
    }));

    constructor(config: unknown) {
      this.mainMediaElement.pause = vi.fn();
      instances.push(this);
      const playerId = (config as { playerHtmlElementId?: string })
        .playerHtmlElementId;
      if (playerId) {
        document.getElementById(playerId)?.append(this.mainMediaElement);
      }
    }
  }

  return { MockPlayer, instances };
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

vi.mock("@/player/audio-sidecar", () => ({
  createSynchronizedAudioSidecar: vi.fn(() => ({
    ready: Promise.resolve(),
    setEnabled: vi.fn(),
    destroy: vi.fn(),
  })),
}));

import { createOmakasePreview } from "@/player/OmakaseAdapter";

const VIDEO = "urn:x-nmos:format:video";
const SEGMENT_TIMERANGE = "[1400:123456789_1406:123456789)";

function fakeApi(flow: Flow, segments: FlowSegment[]): PreviewDescriptorApi {
  return {
    getFlow: vi.fn(async () => flow),
    getFlowCollection: vi.fn(async (): Promise<FlowCollectionItem[]> => []),
    getFlowSegments: vi.fn(
      async (
        _flowId: string,
        _params?: FlowSegmentParams,
        _options?: ApiRequestOptions,
      ): Promise<PaginatedResponse<FlowSegment>> => ({ data: segments }),
    ),
  };
}

async function previewDescriptor(
  timerange: string,
): Promise<MediaPreviewDescriptor> {
  const flow: Flow = {
    id: "video-1",
    source_id: "source-1",
    format: VIDEO,
    container: "video/mp2t",
    timerange,
  };
  const segments: FlowSegment[] = [
    {
      object_id: "clip.ts",
      timerange: SEGMENT_TIMERANGE,
      get_urls: [{ url: "https://storage.example/clip.ts", presigned: true }],
    },
  ];
  return buildMediaPreviewDescriptor(fakeApi(flow, segments), flow.id, {
    locationOrigin: "https://app.example",
  });
}

function mountPreview(descriptor: MediaPreviewDescriptor) {
  const player = document.createElement("div");
  player.id = "player";
  const timeline = document.createElement("div");
  timeline.id = "timeline";
  document.body.append(player, timeline);
  return createOmakasePreview({
    descriptor,
    playerElementId: "player",
    timelineElementId: "timeline",
    onChange: vi.fn(),
  });
}

describe("descriptor to playback plan", () => {
  beforeAll(() => {
    let created = 0;
    Object.defineProperty(URL, "createObjectURL", {
      configurable: true,
      value: vi.fn(() => `blob:manifest-${++created}`),
    });
    Object.defineProperty(URL, "revokeObjectURL", {
      configurable: true,
      value: vi.fn(),
    });
  });

  beforeEach(() => {
    document.body.replaceChildren();
    mocks.instances.length = 0;
  });

  it("compiles a half-open Flow timerange unchanged", async () => {
    const descriptor = await previewDescriptor("[1000:0_2000:0)");

    expect(descriptor.initialTimerange).toBe("[1400:0_2000:0)");
    expect(halfOpenTimerange(descriptor.initialTimerange)?.timerange).toBe(
      "[1400:0_2000:0)",
    );

    const handle = mountPreview(descriptor);
    await handle.ready;
    expect(mocks.instances[0].loadMainMedia).toHaveBeenCalledWith(
      "blob:manifest-1",
      expect.objectContaining({ fileFormatType: "HLS" }),
    );
    handle.destroy();
  });

  it("compiles an inclusive Flow timerange end as one more nanosecond", async () => {
    const descriptor = await previewDescriptor("[1000:0_2000:123456789]");

    // The descriptor keeps the TAMS form the media store returned.
    expect(descriptor.initialTimerange).toBe("[1400:123456789_2000:123456789]");
    // The manifest compiler accepts only half-open timeranges, so the raw
    // descriptor value must never be handed to it directly.
    expect(() =>
      compilePlaybackPlan({
        tracks: descriptor.tracks,
        initialTimerange: descriptor.initialTimerange,
      }),
    ).toThrowError(expect.objectContaining({ code: "invalid-timerange" }));

    const window = halfOpenTimerange(descriptor.initialTimerange);
    expect(window).toEqual({
      startNanoseconds: 1_400_123_456_789n,
      endNanoseconds: 2_000_123_456_790n,
      timerange: "[1400:123456789_2000:123456790)",
    });

    const plan = compilePlaybackPlan({
      tracks: descriptor.tracks,
      initialTimerange: window?.timerange as string,
    });
    expect(plan.kind).toBe("hls");
    if (plan.kind !== "hls") return;
    // The window start is unchanged, so the synthetic clock still begins at
    // the first segment.
    expect(plan.mediaManifests.get("video-1")).toContain(
      "#EXT-X-PROGRAM-DATE-TIME:2000-01-01T00:00:00.000000000Z",
    );
    plan.dispose();

    const handle = mountPreview(descriptor);
    await expect(handle.ready).resolves.toBeUndefined();
    expect(mocks.instances[0].loadMainMedia).toHaveBeenCalledOnce();
    handle.destroy();
  });

  it("reports an instantaneous Flow timerange as unplayable, not invalid", async () => {
    const descriptor = await previewDescriptor("[1500:123456789]");

    expect(descriptor.initialTimerange).toBe("[1500:123456789]");
    expect(halfOpenTimerange(descriptor.initialTimerange)).toBeUndefined();
    expect(() => mountPreview(descriptor)).toThrowError(
      expect.objectContaining({ code: "no-playable-media" }),
    );
    expect(mocks.instances).toHaveLength(0);
  });
});
