import { describe, expect, it, vi } from "vitest";
import type { FlowSegmentParams } from "@/api/client";
import type { ApiRequestOptions } from "@/api/transport";
import {
  buildMediaPreviewDescriptor,
  descriptorMediaUrls,
  type PreviewDescriptorApi,
  PreviewDescriptorError,
} from "@/player/descriptor";
import type {
  Flow,
  FlowCollectionItem,
  FlowSegment,
  PaginatedResponse,
} from "@/types/tams";

const VIDEO = "urn:x-nmos:format:video";
const AUDIO = "urn:x-nmos:format:audio";
const DATA = "urn:x-nmos:format:data";
const IMAGE = "urn:x-tam:format:image";
const MULTI = "urn:x-nmos:format:multi";

function flow(id: string, format: string, overrides: Partial<Flow> = {}): Flow {
  return {
    id,
    source_id: "source-1",
    format,
    timerange: "[1000:0_2000:0)",
    ...overrides,
  };
}

function segment(
  objectId: string,
  url = `https://storage.example/${objectId}`,
): FlowSegment {
  return {
    object_id: objectId,
    timerange: "[1400:0_1401:0)",
    object_timerange: "[0:0_1:0)",
    get_urls: [{ url, presigned: true }],
  };
}

function fakeApi({
  flows,
  collection = [],
  segments = {},
  nextKeys = {},
}: {
  flows: Flow[];
  collection?: FlowCollectionItem[];
  segments?: Record<string, FlowSegment[]>;
  nextKeys?: Record<string, string>;
}) {
  const flowById = new Map(flows.map((item) => [item.id, item]));
  const getFlow = vi.fn(async (flowId: string) => {
    const result = flowById.get(flowId);
    if (!result) throw new Error("Flow fixture not found");
    return result;
  });
  const getFlowCollection = vi.fn(async () => collection);
  const getFlowSegments = vi.fn(
    async (
      flowId: string,
      _params?: FlowSegmentParams,
      _options?: ApiRequestOptions,
    ): Promise<PaginatedResponse<FlowSegment>> => ({
      data: segments[flowId] ?? [],
      ...(nextKeys[flowId] ? { nextKey: nextKeys[flowId] } : {}),
    }),
  );
  const api: PreviewDescriptorApi = {
    getFlow,
    getFlowCollection,
    getFlowSegments,
  };
  return { api, getFlow, getFlowCollection, getFlowSegments };
}

describe("buildMediaPreviewDescriptor", () => {
  it("loads a container-bearing root and derives a recent exact timerange", async () => {
    const root = flow("video-1", VIDEO, {
      container: "video/mp2t",
      timerange: "[9007199254740000:123456789_9007199254741593:987654321)",
    });
    const { api, getFlow, getFlowCollection, getFlowSegments } = fakeApi({
      flows: [root],
      segments: { "video-1": [segment("clip.ts")] },
    });
    const controller = new AbortController();

    const descriptor = await buildMediaPreviewDescriptor(api, root.id, {
      locationOrigin: "https://app.example",
      signal: controller.signal,
    });

    expect(descriptor.initialTimerange).toBe(
      "[9007199254740993:987654321_9007199254741593:987654321)",
    );
    expect(descriptor.video?.flow.id).toBe(root.id);
    expect(descriptor.segmentCount).toBe(1);
    expect(descriptor.flowsSegments.get(root.id)).toEqual(
      descriptor.video?.segments,
    );
    expect(descriptor.video?.segments[0].get_urls[0]).toMatchObject({
      url: "https://storage.example/clip.ts",
      credentials: "omit",
    });
    expect(getFlowCollection).not.toHaveBeenCalled();
    expect(getFlow).toHaveBeenCalledWith(
      root.id,
      { include_timerange: true },
      { signal: controller.signal },
    );
    expect(getFlowSegments).toHaveBeenCalledWith(
      root.id,
      {
        include_object_timerange: true,
        limit: 300,
        presigned: true,
        timerange: descriptor.initialTimerange,
        verbose_storage: true,
      },
      { signal: controller.signal },
    );
  });

  it("treats a container-bearing Multi Flow as the segment owner", async () => {
    const root = flow("muxed-1", MULTI, {
      container: "video/mp4",
      flow_collection: [{ id: "child-ignored", role: "video" }],
    });
    const { api, getFlowCollection, getFlowSegments } = fakeApi({
      flows: [root],
      segments: { "muxed-1": [segment("muxed.mp4")] },
    });

    const descriptor = await buildMediaPreviewDescriptor(api, root.id, {
      locationOrigin: "https://app.example",
    });

    expect(descriptor.muxed?.flow.id).toBe(root.id);
    expect(descriptor.tracks).toHaveLength(1);
    expect(getFlowCollection).not.toHaveBeenCalled();
    expect(getFlowSegments).toHaveBeenCalledTimes(1);
  });

  it("preserves instantaneous and inclusive timerange ends", async () => {
    const instant = flow("data-instant", DATA, {
      container: "application/json",
      timerange: "[9007199254740993:123456789]",
    });
    const inclusive = flow("data-range", DATA, {
      container: "application/json",
      timerange: "[1000:0_2000:123456789]",
    });

    await expect(
      buildMediaPreviewDescriptor(
        fakeApi({ flows: [instant] }).api,
        instant.id,
        {
          locationOrigin: "https://app.example",
        },
      ),
    ).resolves.toMatchObject({
      initialTimerange: "[9007199254740993:123456789]",
    });
    await expect(
      buildMediaPreviewDescriptor(
        fakeApi({ flows: [inclusive] }).api,
        inclusive.id,
        { locationOrigin: "https://app.example" },
      ),
    ).resolves.toMatchObject({
      initialTimerange: "[1400:123456789_2000:123456789]",
    });
  });

  it("resolves one collection level and classifies tracks by Flow format", async () => {
    const root = flow("multi-1", MULTI);
    const video = flow("video-1", VIDEO, { container: "video/mp2t" });
    const audio = flow("audio-1", AUDIO, { container: "video/mp2t" });
    const data = flow("data-1", DATA, { container: "application/json" });
    const image = flow("image-1", IMAGE, { container: "image/jpeg" });
    const collection = [
      { id: video.id, role: "audio-role-must-not-classify" },
      { id: audio.id, role: "video-role-must-not-classify" },
      { id: data.id, role: "captions" },
      { id: image.id, role: "thumbnails" },
    ];
    const { api, getFlow, getFlowSegments } = fakeApi({
      flows: [root, video, audio, data, image],
      collection,
      segments: {
        [video.id]: [segment("video.ts")],
        [audio.id]: [segment("audio.ts")],
        [data.id]: [segment("captions.vtt")],
        [image.id]: [segment("thumb.jpg")],
      },
    });

    const descriptor = await buildMediaPreviewDescriptor(api, root.id, {
      locationOrigin: "https://app.example",
    });

    expect(descriptor.video?.flow.id).toBe(video.id);
    expect(descriptor.video?.role).toBe("audio-role-must-not-classify");
    expect(descriptor.audio.map((track) => track.flow.id)).toEqual([audio.id]);
    expect(descriptor.data.map((track) => track.flow.id)).toEqual([data.id]);
    expect(descriptor.images.map((track) => track.flow.id)).toEqual([image.id]);
    expect(getFlow).toHaveBeenCalledTimes(5);
    expect(getFlowSegments).toHaveBeenCalledTimes(4);
  });

  it("rejects multiple video children before requesting segments", async () => {
    const root = flow("multi-1", MULTI);
    const first = flow("video-1", VIDEO, { container: "video/mp2t" });
    const second = flow("video-2", VIDEO, { container: "video/mp2t" });
    const { api, getFlowSegments } = fakeApi({
      flows: [root, first, second],
      collection: [
        { id: first.id, role: "main" },
        { id: second.id, role: "alternate" },
      ],
    });

    await expect(
      buildMediaPreviewDescriptor(api, root.id, {
        locationOrigin: "https://app.example",
      }),
    ).rejects.toMatchObject({ code: "too-many-video-tracks" });
    expect(getFlowSegments).not.toHaveBeenCalled();
  });

  it("rejects collection cycles and nested collections explicitly", async () => {
    const root = flow("multi-1", MULTI);
    const nested = flow("nested-1", MULTI, {
      flow_collection: [{ id: "nested-child", role: "nested" }],
    });
    const cycleApi = fakeApi({
      flows: [root],
      collection: [{ id: root.id, role: "self" }],
    });
    const nestedApi = fakeApi({
      flows: [root, nested],
      collection: [{ id: nested.id, role: "nested" }],
    });

    await expect(
      buildMediaPreviewDescriptor(cycleApi.api, root.id, {
        locationOrigin: "https://app.example",
      }),
    ).rejects.toMatchObject({ code: "collection-cycle" });
    await expect(
      buildMediaPreviewDescriptor(nestedApi.api, root.id, {
        locationOrigin: "https://app.example",
      }),
    ).rejects.toMatchObject({ code: "nested-collection" });
  });

  it("enforces the collection and global segment budgets", async () => {
    const root = flow("multi-1", MULTI);
    const children = Array.from({ length: 16 }, (_, index) =>
      flow(`audio-${index}`, AUDIO, { container: "video/mp2t" }),
    );
    const segments = Object.fromEntries(
      children.map((child) => [
        child.id,
        Array.from({ length: 130 }, (_, index) =>
          segment(`${child.id}-${index}.ts`),
        ),
      ]),
    );
    const { api, getFlowSegments } = fakeApi({
      flows: [root, ...children],
      collection: children.map((child) => ({ id: child.id, role: "audio" })),
      segments,
    });

    const descriptor = await buildMediaPreviewDescriptor(api, root.id, {
      locationOrigin: "https://app.example",
    });

    expect(descriptor.segmentCount).toBe(2_000);
    expect(descriptor.truncated).toBe(true);
    expect(
      descriptor.tracks.every((track) => track.segments.length === 125),
    ).toBe(true);
    expect(
      getFlowSegments.mock.calls.every((call) => call[1]?.limit === 125),
    ).toBe(true);

    const oversized = fakeApi({
      flows: [root],
      collection: Array.from({ length: 17 }, (_, index) => ({
        id: `flow-${index}`,
        role: "track",
      })),
    });
    await expect(
      buildMediaPreviewDescriptor(oversized.api, root.id, {
        locationOrigin: "https://app.example",
      }),
    ).rejects.toMatchObject({ code: "collection-too-large" });
  });

  it("removes rejected URL alternatives without exposing them in errors", async () => {
    const root = flow("video-1", VIDEO, { container: "video/mp2t" });
    const unsafeUrl =
      "https://user:do-not-log@storage.example/clip.ts?token=do-not-log";
    const validSegment = segment("valid.ts");
    validSegment.get_urls = [
      { url: unsafeUrl, presigned: true },
      { url: "https://app.example/media/unsigned.ts", presigned: false },
      { url: "https://storage.example/valid.ts", presigned: true },
    ];
    validSegment.init_object = {
      object_id: "init.mp4",
      get_urls: [
        { url: unsafeUrl, presigned: true },
        { url: "https://storage.example/init.mp4", presigned: true },
      ],
    };
    const validApi = fakeApi({
      flows: [root],
      segments: { [root.id]: [validSegment] },
    });

    const descriptor = await buildMediaPreviewDescriptor(
      validApi.api,
      root.id,
      { locationOrigin: "https://app.example" },
    );
    expect(descriptor.video?.rejectedUrlCount).toBe(3);
    expect(descriptor.video?.segments[0].get_urls).toHaveLength(1);
    expect(descriptor.video?.segments[0].init_object?.get_urls).toHaveLength(1);
    expect(descriptorMediaUrls(descriptor)).toEqual([
      "https://storage.example/valid.ts",
      "https://storage.example/init.mp4",
    ]);

    const unsafeSegment = segment("unsafe.ts", unsafeUrl);
    const unsafeApi = fakeApi({
      flows: [root],
      segments: { [root.id]: [unsafeSegment] },
    });
    try {
      await buildMediaPreviewDescriptor(unsafeApi.api, root.id, {
        locationOrigin: "https://app.example",
      });
      expect.fail("Expected unsafe media URL rejection");
    } catch (error) {
      expect(error).toBeInstanceOf(PreviewDescriptorError);
      expect(error).toMatchObject({ code: "missing-media-url" });
      expect(String(error)).not.toContain(unsafeUrl);
      expect(String(error)).not.toContain("do-not-log");
    }

    const unsafeInitSegment = segment("media.mp4");
    unsafeInitSegment.init_object = {
      object_id: "unsafe-init.mp4",
      get_urls: [{ url: unsafeUrl, presigned: true }],
    };
    await expect(
      buildMediaPreviewDescriptor(
        fakeApi({
          flows: [root],
          segments: { [root.id]: [unsafeInitSegment] },
        }).api,
        root.id,
        { locationOrigin: "https://app.example" },
      ),
    ).rejects.toMatchObject({
      code: "missing-media-url",
      context: { objectId: "unsafe-init.mp4" },
    });
  });

  it("rejects invalid availability and missing track containers", async () => {
    const invalidRange = flow("video-invalid", VIDEO, {
      container: "video/mp2t",
      timerange: "[1000:0_)",
    });
    const missingContainer = flow("audio-no-container", AUDIO);

    await expect(
      buildMediaPreviewDescriptor(
        fakeApi({ flows: [invalidRange] }).api,
        invalidRange.id,
        { locationOrigin: "https://app.example" },
      ),
    ).rejects.toMatchObject({ code: "invalid-timerange" });
    await expect(
      buildMediaPreviewDescriptor(
        fakeApi({ flows: [missingContainer] }).api,
        missingContainer.id,
        { locationOrigin: "https://app.example" },
      ),
    ).rejects.toMatchObject({ code: "missing-container" });
  });
});
