import { describe, expect, it, type Mock, vi } from "vitest";
import type { PreviewTrack } from "@/player/descriptor";
import {
  type BlobUrlApi,
  BlobUrlRegistry,
  compileHlsMediaManifest,
  compilePlaybackPlan,
  type PlaybackPlanError,
  parseTamsTimerange,
} from "@/player/hls-manifest";

function track(
  kind: PreviewTrack["kind"],
  options: {
    id?: string;
    label?: string;
    role?: string;
    container?: string;
    segments?: Array<{
      object_id: string;
      timerange: string;
      object_timerange?: string;
      ts_offset?: string;
      get_urls?: Array<{ url: string }>;
      init_object?: {
        object_id: string;
        get_urls: Array<{ url: string }>;
      };
    }>;
  } = {},
): PreviewTrack {
  return {
    kind,
    ...(options.role ? { role: options.role } : {}),
    flow: {
      id: options.id ?? `${kind}-flow`,
      source_id: "source-id",
      format:
        kind === "audio"
          ? "urn:x-nmos:format:audio"
          : "urn:x-nmos:format:video",
      container: options.container ?? "video/mp2t",
      ...(options.label ? { label: options.label } : {}),
      avg_bit_rate: kind === "audio" ? 128 : 2_000,
    },
    segments: (options.segments ?? [
      {
        object_id: `${kind}-object`,
        timerange: "[100:0_106:0)",
        get_urls: [
          { url: `https://media.example/${kind}.ts?signature=secret` },
        ],
      },
    ]) as PreviewTrack["segments"],
    truncated: false,
    rejectedUrlCount: 0,
  };
}

function blobApi(): BlobUrlApi & {
  createObjectURL: Mock<(blob: Blob) => string>;
  revokeObjectURL: Mock<(url: string) => void>;
} {
  let nextId = 0;
  return {
    createObjectURL: vi.fn<(blob: Blob) => string>(
      () => `blob:manifest-${++nextId}`,
    ),
    revokeObjectURL: vi.fn<(url: string) => void>(),
  };
}

describe("TAMS timeranges", () => {
  it("parses nanosecond timestamps without losing integer precision", () => {
    expect(
      parseTamsTimerange("[1783762858:661461830_1783763359:994794846)"),
    ).toEqual({
      startNanoseconds: 1_783_762_858_661_461_830n,
      endNanoseconds: 1_783_763_359_994_794_846n,
      durationNanoseconds: 501_333_333_016n,
    });
  });

  it.each(["_", "()", "[1:0]", "[1:0_1:0)", "[1:1000000000_2:0)"])(
    "rejects timerange %s",
    (timerange) => {
      expect(() => parseTamsTimerange(timerange)).toThrowError(
        expect.objectContaining({ code: "invalid-timerange" }),
      );
    },
  );
});

describe("HLS manifest compilation", () => {
  it("links alternate audio to the video variant and escapes labels", () => {
    const urls = blobApi();
    const plan = compilePlaybackPlan(
      {
        initialTimerange: "[90:0_120:0)",
        tracks: [
          track("video", { id: "video" }),
          track("audio", {
            id: "audio",
            role: 'English\nmain "mix"',
          }),
        ],
      },
      urls,
    );

    expect(plan.kind).toBe("hls");
    if (plan.kind !== "hls") return;
    expect(plan.masterManifest).toContain(
      '#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID="audio",NAME="English main \'mix\'",DEFAULT=YES,AUTOSELECT=YES,URI="blob:manifest-2"',
    );
    expect(plan.masterManifest).toContain(
      '#EXT-X-STREAM-INF:BANDWIDTH=2128000,AUDIO="audio"\nblob:manifest-1',
    );
    expect(plan.trimmed).toBe(false);
    expect(plan.mainUrl).toBe("blob:manifest-1");
    expect(plan.audioSidecars).toEqual([
      {
        flowId: "audio",
        label: "English main 'mix'",
        offsetSeconds: 0,
        url: "blob:manifest-2",
      },
    ]);
    expect(plan.masterManifest).not.toContain('\nmain "mix"');
  });

  it("keeps independently starting tracks aligned to one synthetic clock", () => {
    const video = compileHlsMediaManifest(
      track("video", {
        segments: [
          {
            object_id: "video",
            timerange: "[100:250000000_106:250000000)",
            get_urls: [{ url: "https://media.example/video.ts" }],
          },
        ],
      }),
      "[90:0_120:0)",
    );
    const audio = compileHlsMediaManifest(
      track("audio", {
        segments: [
          {
            object_id: "audio",
            timerange: "[105:750000000_111:750000000)",
            get_urls: [{ url: "https://media.example/audio.ts" }],
          },
        ],
      }),
      "[90:0_120:0)",
    );

    expect(video).toContain(
      "#EXT-X-PROGRAM-DATE-TIME:2000-01-01T00:00:10.250000000Z",
    );
    expect(audio).toContain(
      "#EXT-X-PROGRAM-DATE-TIME:2000-01-01T00:00:15.750000000Z",
    );
    expect(video).toContain("#EXT-X-TARGETDURATION:6");
    expect(video).toContain("#EXTINF:6,");
    expect(video).toContain("#EXT-X-PLAYLIST-TYPE:VOD");
    expect(video).toMatch(/#EXT-X-ENDLIST\n$/u);
  });

  it("ignores valid Object clusivity that is not used for playback timing", () => {
    const inclusive = track("video", {
      segments: [
        {
          object_id: "inclusive",
          timerange: "[100:0_106:0)",
          object_timerange: "[0:0_6:0]",
          get_urls: [{ url: "https://media.example/inclusive.ts" }],
        },
        {
          object_id: "point",
          timerange: "[106:0_112:0)",
          object_timerange: "[6:0]",
          get_urls: [{ url: "https://media.example/point.ts" }],
        },
      ],
    });

    expect(compileHlsMediaManifest(inclusive, "[90:0_120:0)")).toContain(
      "https://media.example/point.ts",
    );
  });

  it("starts split playback inside the timerange shared by video and audio", () => {
    const plan = compilePlaybackPlan(
      {
        initialTimerange: "[90:0_125:0)",
        tracks: [
          track("video", {
            id: "video",
            segments: [
              {
                object_id: "video-early",
                timerange: "[100:0_105:0)",
                get_urls: [{ url: "https://media.example/video-early.ts" }],
              },
              {
                object_id: "video-shared-1",
                timerange: "[110:0_115:0)",
                get_urls: [{ url: "https://media.example/video-shared-1.ts" }],
              },
              {
                object_id: "video-shared-2",
                timerange: "[115:0_120:0)",
                get_urls: [{ url: "https://media.example/video-shared-2.ts" }],
              },
            ],
          }),
          track("audio", {
            id: "audio",
            segments: [
              {
                object_id: "audio-shared-1",
                timerange: "[108:0_113:0)",
                get_urls: [{ url: "https://media.example/audio-shared-1.ts" }],
              },
              {
                object_id: "audio-shared-2",
                timerange: "[113:0_118:0)",
                get_urls: [{ url: "https://media.example/audio-shared-2.ts" }],
              },
            ],
          }),
        ],
      },
      blobApi(),
    );

    expect(plan.kind).toBe("hls");
    if (plan.kind !== "hls") return;
    expect(plan.trimmed).toBe(true);
    expect(plan.mainUrl).toBe("blob:manifest-1");
    expect(plan.audioSidecars).toEqual([
      {
        flowId: "audio",
        label: "Audio 1",
        offsetSeconds: 2,
        url: "blob:manifest-2",
      },
    ]);
    expect(plan.mediaManifests.get("video")).not.toContain("video-early.ts");
    expect(plan.mediaManifests.get("video")).toContain("video-shared-1.ts");
    expect(plan.mediaManifests.get("video")).not.toContain("video-shared-2.ts");
    expect(plan.mediaManifests.get("audio")).toContain("audio-shared-1.ts");
  });

  it("rejects split playback when audio has a gap inside the video window", () => {
    expect(() =>
      compilePlaybackPlan(
        {
          initialTimerange: "[90:0_125:0)",
          tracks: [
            track("video", {
              segments: [
                {
                  object_id: "video-1",
                  timerange: "[110:0_115:0)",
                  get_urls: [{ url: "https://media.example/video-1.ts" }],
                },
                {
                  object_id: "video-2",
                  timerange: "[115:0_120:0)",
                  get_urls: [{ url: "https://media.example/video-2.ts" }],
                },
              ],
            }),
            track("audio", {
              segments: [
                {
                  object_id: "audio-1",
                  timerange: "[108:0_113:0)",
                  get_urls: [{ url: "https://media.example/audio-1.ts" }],
                },
                {
                  object_id: "audio-2",
                  timerange: "[114:0_120:0)",
                  get_urls: [{ url: "https://media.example/audio-2.ts" }],
                },
              ],
            }),
          ],
        },
        blobApi(),
      ),
    ).toThrowError(expect.objectContaining({ code: "no-common-media-window" }));
  });

  it("marks gaps and timestamp mapping changes as discontinuities", () => {
    const manifest = compileHlsMediaManifest(
      track("video", {
        segments: [
          {
            object_id: "one",
            timerange: "[100:0_102:0)",
            get_urls: [{ url: "https://media.example/one.ts" }],
          },
          {
            object_id: "two",
            timerange: "[103:0_105:0)",
            ts_offset: "103:0",
            get_urls: [{ url: "https://media.example/two.ts" }],
          },
        ],
      }),
      "[90:0_120:0)",
    );

    expect(manifest.match(/#EXT-X-DISCONTINUITY/gu)).toHaveLength(1);
    expect(manifest.match(/#EXT-X-PROGRAM-DATE-TIME/gu)).toHaveLength(2);
  });

  it("returns an HLS audio-only plan", () => {
    const plan = compilePlaybackPlan(
      {
        initialTimerange: "[90:0_120:0)",
        tracks: [track("audio")],
      },
      blobApi(),
    );

    expect(plan.kind).toBe("hls");
    if (plan.kind !== "hls") return;
    expect(plan.trimmed).toBe(false);
    expect(plan.mainUrl).toBe("blob:manifest-1");
    expect(plan.audioSidecars).toEqual([]);
    expect(plan.masterManifest).toContain("#EXT-X-STREAM-INF:BANDWIDTH=128000");
    expect(plan.masterManifest).not.toContain("#EXT-X-MEDIA:TYPE=AUDIO");
  });
});

describe("playback plans", () => {
  it("uses a direct URL for one MP4 media object", () => {
    const plan = compilePlaybackPlan({
      initialTimerange: "[90:0_120:0)",
      tracks: [
        track("muxed", {
          container: "video/mp4",
          segments: [
            {
              object_id: "mp4",
              timerange: "[100:0_106:0)",
              get_urls: [
                { url: "https://media.example/media.mp4?token=secret" },
              ],
            },
          ],
        }),
      ],
    });

    expect(plan).toMatchObject({
      kind: "direct",
      mediaKind: "video",
      mimeType: "video/mp4",
      url: "https://media.example/media.mp4?token=secret",
    });
  });

  it("builds fragmented MP4 playlists with initialisation Objects", () => {
    const fragmented = track("video", {
      container: "video/mp4",
      segments: [
        {
          object_id: "one",
          timerange: "[100:0_106:0)",
          get_urls: [{ url: "https://media.example/one.m4s" }],
          init_object: {
            object_id: "init-one",
            get_urls: [{ url: "https://media.example/init-one.mp4" }],
          },
        },
        {
          object_id: "two",
          timerange: "[106:0_112:0)",
          get_urls: [{ url: "https://media.example/two.m4s" }],
          init_object: {
            object_id: "init-two",
            get_urls: [{ url: "https://media.example/init-two.mp4" }],
          },
        },
      ],
    });
    const plan = compilePlaybackPlan(
      { initialTimerange: "[90:0_120:0)", tracks: [fragmented] },
      blobApi(),
    );

    expect(plan.kind).toBe("hls");
    if (plan.kind !== "hls") return;
    expect(plan.mediaManifests.get("video-flow")).toContain("#EXT-X-VERSION:7");
    expect(plan.mediaManifests.get("video-flow")).toContain(
      '#EXT-X-MAP:URI="https://media.example/init-one.mp4"',
    );
    expect(plan.mediaManifests.get("video-flow")).toContain(
      '#EXT-X-MAP:URI="https://media.example/init-two.mp4"',
    );
    expect(plan.mediaManifests.get("video-flow")).toContain(
      "https://media.example/two.m4s",
    );
  });

  it("rejects fragmented MP4 without init Objects and unsupported containers", () => {
    const multiObject = track("video", {
      container: "video/mp4",
      segments: [
        {
          object_id: "one",
          timerange: "[100:0_106:0)",
          get_urls: [{ url: "https://media.example/one.mp4" }],
        },
        {
          object_id: "two",
          timerange: "[106:0_112:0)",
          get_urls: [{ url: "https://media.example/two.mp4" }],
        },
      ],
    });
    expect(() =>
      compilePlaybackPlan({
        initialTimerange: "[90:0_120:0)",
        tracks: [multiObject],
      }),
    ).toThrowError(expect.objectContaining({ code: "missing-init-object" }));
    expect(() =>
      compilePlaybackPlan({
        initialTimerange: "[90:0_120:0)",
        tracks: [track("video", { container: "application/mxf" })],
      }),
    ).toThrowError(expect.objectContaining({ code: "unsupported-container" }));
  });

  it("does not expose a signed URL through validation errors", () => {
    const signedUrl = "https://media.example/asset.ts?X-Amz-Signature=private";
    const invalid = track("video", {
      segments: [
        {
          object_id: "bad",
          timerange: "invalid",
          get_urls: [{ url: signedUrl }],
        },
      ],
    });
    let error: PlaybackPlanError | undefined;
    try {
      compilePlaybackPlan({
        initialTimerange: "[90:0_120:0)",
        tracks: [invalid],
      });
    } catch (caught) {
      error = caught as PlaybackPlanError;
    }
    expect(error?.code).toBe("invalid-timerange");
    expect(error?.message).not.toContain(signedUrl);
    expect(error?.stack).not.toContain(signedUrl);
  });

  it("rejects a media segment without a URL", () => {
    expect(() =>
      compilePlaybackPlan({
        initialTimerange: "[90:0_120:0)",
        tracks: [
          track("video", {
            segments: [
              {
                object_id: "missing",
                timerange: "[100:0_106:0)",
                get_urls: [],
              },
            ],
          }),
        ],
      }),
    ).toThrowError(expect.objectContaining({ code: "missing-url" }));
  });

  it("revokes every generated Blob URL exactly once", () => {
    const urls = blobApi();
    const plan = compilePlaybackPlan(
      {
        initialTimerange: "[90:0_120:0)",
        tracks: [track("video"), track("audio")],
      },
      urls,
    );
    expect(urls.createObjectURL).toHaveBeenCalledTimes(3);

    plan.dispose();
    plan.dispose();

    expect(urls.revokeObjectURL.mock.calls).toEqual([
      ["blob:manifest-1"],
      ["blob:manifest-2"],
      ["blob:manifest-3"],
    ]);
  });

  it("supports explicit registry cleanup", () => {
    const urls = blobApi();
    const registry = new BlobUrlRegistry(urls);
    registry.create("first");
    registry.create("second");
    registry.revokeAll();
    registry.revokeAll();
    expect(urls.revokeObjectURL).toHaveBeenCalledTimes(2);
  });
});
