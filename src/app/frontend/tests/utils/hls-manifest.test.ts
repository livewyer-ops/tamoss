import { describe, it, expect } from "vitest";
import {
  parseTimerange,
  computeSegmentsDuration,
  buildMediaPlaylistContent,
  buildHlsManifest,
  buildMultiFlowManifest,
} from "@/utils/hls-manifest";

describe("parseTimerange", () => {
  it("parses [0:0_6:0)", () => {
    const r = parseTimerange("[0:0_6:0)");
    expect(r.start).toBe(0);
    expect(r.end).toBe(6);
    expect(r.duration).toBe(6);
  });

  it("parses [6:0_12:0)", () => {
    const r = parseTimerange("[6:0_12:0)");
    expect(r.start).toBe(6);
    expect(r.end).toBe(12);
    expect(r.duration).toBe(6);
  });

  it("parses [138:0_144:0)", () => {
    const r = parseTimerange("[138:0_144:0)");
    expect(r.start).toBe(138);
    expect(r.end).toBe(144);
    expect(r.duration).toBe(6);
  });

  it("handles nanosecond precision", () => {
    const r = parseTimerange("[0:500000000_1:0)");
    expect(r.start).toBeCloseTo(0.5);
    expect(r.end).toBe(1);
    expect(r.duration).toBeCloseTo(0.5);
  });

  it("returns zero for unbounded range _", () => {
    const r = parseTimerange("_");
    expect(r).toEqual({ start: 0, end: 0, duration: 0 });
  });

  it("returns zero for empty range ()", () => {
    const r = parseTimerange("()");
    expect(r).toEqual({ start: 0, end: 0, duration: 0 });
  });

  it("returns zero for empty string", () => {
    const r = parseTimerange("");
    expect(r).toEqual({ start: 0, end: 0, duration: 0 });
  });

  it("parses instantaneous timeranges", () => {
    const r = parseTimerange("[5:0]");
    expect(r.start).toBe(5);
    expect(r.end).toBe(5);
    expect(r.duration).toBe(0);
  });
});

describe("computeSegmentsDuration", () => {
  it("returns 0 for empty segments", () => {
    expect(computeSegmentsDuration([])).toBe(0);
  });

  it("computes span from earliest start to latest end", () => {
    const segments = [
      { object_id: "1", timerange: "[0:0_6:0)", get_urls: [{ url: "u" }] },
      { object_id: "2", timerange: "[6:0_12:0)", get_urls: [{ url: "u" }] },
    ];
    expect(computeSegmentsDuration(segments)).toBe(12);
  });

  it("handles non-zero start offset", () => {
    const segments = [
      { object_id: "1", timerange: "[100:0_106:0)", get_urls: [{ url: "u" }] },
      { object_id: "2", timerange: "[106:0_112:0)", get_urls: [{ url: "u" }] },
    ];
    expect(computeSegmentsDuration(segments)).toBe(12);
  });

  it("handles segments with gaps", () => {
    const segments = [
      { object_id: "1", timerange: "[0:0_6:0)", get_urls: [{ url: "u" }] },
      { object_id: "2", timerange: "[12:0_18:0)", get_urls: [{ url: "u" }] },
    ];
    // Span is 0 → 18 = 18s (includes gap)
    expect(computeSegmentsDuration(segments)).toBe(18);
  });
});

describe("buildMediaPlaylistContent", () => {
  it("does not stretch segment durations to cover gaps", () => {
    const content = buildMediaPlaylistContent([
      {
        object_id: "1",
        timerange: "[0:0_6:0)",
        get_urls: [{ url: "http://s3/obj1" }],
      },
      {
        object_id: "2",
        timerange: "[12:0_18:0)",
        get_urls: [{ url: "http://s3/obj2" }],
      },
    ]);

    expect(content).toContain("#EXT-X-DISCONTINUITY");
    expect(content?.match(/#EXTINF:6\.000000,/g)).toHaveLength(2);
    expect(content).not.toContain("#EXTINF:12.000000,");
  });

  it("marks discontinuity between independently encoded objects", () => {
    const content = buildMediaPlaylistContent([
      {
        object_id: "1",
        timerange: "[0:0_6:0)",
        get_urls: [{ url: "http://s3/obj1" }],
      },
      {
        object_id: "2",
        timerange: "[6:0_12:0)",
        get_urls: [{ url: "http://s3/obj2" }],
      },
    ]);

    expect(content?.split("\n")).toContain("#EXT-X-DISCONTINUITY");
  });
});

describe("buildHlsManifest", () => {
  it("returns null for empty segments", () => {
    expect(buildHlsManifest([])).toBeNull();
  });

  it("returns null for segments without URLs", () => {
    const segments = [{ object_id: "1", timerange: "[0:0_6:0)" }];
    expect(buildHlsManifest(segments)).toBeNull();
  });

  it("returns a blob URL for valid segments", () => {
    const segments = [
      {
        object_id: "1",
        timerange: "[0:0_6:0)",
        get_urls: [{ url: "http://localhost:9000/tams/obj1?sig=abc" }],
      },
      {
        object_id: "2",
        timerange: "[6:0_12:0)",
        get_urls: [{ url: "http://localhost:9000/tams/obj2?sig=def" }],
      },
    ];
    const url = buildHlsManifest(segments);
    expect(url).toBeTruthy();
    expect(url).toMatch(/^blob:/);
  });

  it("sorts segments by start time", () => {
    const segments = [
      {
        object_id: "2",
        timerange: "[6:0_12:0)",
        get_urls: [{ url: "http://localhost:9000/tams/obj2" }],
      },
      {
        object_id: "1",
        timerange: "[0:0_6:0)",
        get_urls: [{ url: "http://localhost:9000/tams/obj1" }],
      },
    ];
    const url = buildHlsManifest(segments);
    expect(url).toBeTruthy();
  });
});

describe("buildMultiFlowManifest", () => {
  it("returns null when both streams are empty", () => {
    expect(buildMultiFlowManifest([], [])).toBeNull();
  });

  it("returns simple manifest for video-only", () => {
    const video = [
      {
        object_id: "v1",
        timerange: "[0:0_6:0)",
        get_urls: [{ url: "http://s3/v1" }],
      },
    ];
    const result = buildMultiFlowManifest(video, []);
    expect(result).not.toBeNull();
    expect(result!.primaryUrl).toMatch(/^blob:/);
    expect(result!.audioUrl).toBeNull();
    expect(result!.urls).toHaveLength(1);
  });

  it("returns simple manifest for audio-only", () => {
    const audio = [
      {
        object_id: "a1",
        timerange: "[0:0_6:0)",
        get_urls: [{ url: "http://s3/a1" }],
      },
    ];
    const result = buildMultiFlowManifest([], audio);
    expect(result).not.toBeNull();
    expect(result!.primaryUrl).toMatch(/^blob:/);
    expect(result!.audioUrl).toBeNull();
    expect(result!.urls).toHaveLength(1);
  });

  it("returns separate video and audio URLs for video+audio", () => {
    const video = [
      {
        object_id: "v1",
        timerange: "[0:0_6:0)",
        get_urls: [{ url: "http://s3/v1" }],
      },
    ];
    const audio = [
      {
        object_id: "a1",
        timerange: "[0:0_6:0)",
        get_urls: [{ url: "http://s3/a1" }],
      },
    ];
    const result = buildMultiFlowManifest(video, audio);
    expect(result).not.toBeNull();
    expect(result!.primaryUrl).toMatch(/^blob:/);
    expect(result!.audioUrl).toMatch(/^blob:/);
    expect(result!.urls).toEqual([result!.primaryUrl, result!.audioUrl]);
  });
});
