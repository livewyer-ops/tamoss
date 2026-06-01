import { act, renderHook, waitFor } from "@testing-library/react";
import { useIngestSession } from "@/hooks/useIngestSession";

const { apiMock, ffmpegServiceMock } = vi.hoisted(() => ({
  apiMock: {
    addFlowSegments: vi.fn(),
    allocateStorage: vi.fn(),
    createFlow: vi.fn(),
    deleteObjectInstance: vi.fn(),
    getSource: vi.fn(),
    setFlowCollection: vi.fn(),
    updateFlowTag: vi.fn(),
    updateSourceDescription: vi.fn(),
    updateSourceLabel: vi.fn(),
    updateSourceTag: vi.fn(),
    uploadRaw: vi.fn(),
  },
  ffmpegServiceMock: {
    probe: vi.fn(),
    probeSegment: vi.fn(),
    segment: vi.fn(),
  },
}));

vi.mock("@/contexts/ApiContext", () => ({
  useApi: () => apiMock,
}));

vi.mock("@/services/ffmpeg-service", () => ({
  ffmpegService: ffmpegServiceMock,
}));

const ids = {
  audioFlow: "00000000-0000-4000-8000-000000000005",
  audioObject: "00000000-0000-4000-8000-000000000006",
  audioSource: "00000000-0000-4000-8000-000000000003",
  file: "00000000-0000-4000-8000-000000000001",
  multiFlow: "00000000-0000-4000-8000-000000000007",
  parentSource: "00000000-0000-4000-8000-000000000000",
  videoFlow: "00000000-0000-4000-8000-000000000004",
  videoSource: "00000000-0000-4000-8000-000000000002",
  videoObject: "00000000-0000-4000-8000-000000000008",
};

function segmentProbe(
  durationNanoseconds: bigint,
  startTimeNanoseconds: bigint = 1_400_000_000n,
) {
  return {
    duration: Number(durationNanoseconds) / 1_000_000_000,
    durationNanoseconds,
    hasAudio: true,
    hasVideo: false,
    startTimeNanoseconds,
  };
}

function stubUuid(sequence: string[]) {
  vi.spyOn(crypto, "randomUUID").mockImplementation(
    () =>
      sequence.shift() as `${string}-${string}-${string}-${string}-${string}`,
  );
}

describe("useIngestSession", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
    vi.clearAllMocks();

    apiMock.getSource.mockResolvedValue({
      id: ids.parentSource,
      format: "urn:x-nmos:format:multi",
      label: "Programme",
    });
    apiMock.createFlow.mockResolvedValue(undefined);
    apiMock.deleteObjectInstance.mockResolvedValue(undefined);
    apiMock.allocateStorage.mockImplementation(
      (_flowId: string, objectIds: string[]) =>
        Promise.resolve({
          media_objects: objectIds.map((objectId) => ({
            object_id: objectId,
            storage_id: "storage-1",
            put_url: {
              url: `https://upload.example/${objectId}`,
              headers: { "Content-Type": "video/mp2t" },
            },
          })),
        }),
    );
    apiMock.uploadRaw.mockResolvedValue(undefined);
    apiMock.addFlowSegments.mockResolvedValue(undefined);
    apiMock.setFlowCollection.mockResolvedValue(undefined);
    apiMock.updateFlowTag.mockResolvedValue(undefined);
    apiMock.updateSourceDescription.mockResolvedValue(undefined);
    apiMock.updateSourceLabel.mockResolvedValue(undefined);
    apiMock.updateSourceTag.mockResolvedValue(undefined);

    const sourceProbe = {
      audioCodec: "aac",
      channels: 2,
      duration: 6,
      durationNanoseconds: 6_000_000_000n,
      hasAudio: true,
      hasVideo: true,
      height: 1080,
      frameRate: { numerator: 25, denominator: 1 },
      sampleRate: 48000,
      videoCodec: "h264",
      width: 1920,
    };
    ffmpegServiceMock.probe.mockResolvedValueOnce(sourceProbe);
    ffmpegServiceMock.probeSegment.mockResolvedValue(
      segmentProbe(6_000_000_000n),
    );
    ffmpegServiceMock.segment.mockResolvedValue([
      new Blob(["segment"], { type: "video/mp2t" }),
    ]);
  });

  it("creates mono child sources through flow registration for a multi source", async () => {
    stubUuid([
      ids.file,
      ids.videoSource,
      ids.audioSource,
      ids.videoFlow,
      ids.audioFlow,
      ids.multiFlow,
      ids.videoObject,
      ids.audioObject,
    ]);

    const file = new File(["media"], "clip.mp4", { type: "video/mp4" });
    const { result } = renderHook(() => useIngestSession());

    act(() => result.current.addFiles([file]));
    await waitFor(() => expect(result.current.session.files).toHaveLength(1));

    await act(async () => {
      await result.current.startIngest(ids.parentSource);
    });

    expect(apiMock.createFlow).toHaveBeenCalledWith(
      ids.videoFlow,
      expect.objectContaining({
        codec: "video/h264",
        container: "video/mp2t",
        format: "urn:x-nmos:format:video",
        label: "Programme (video)",
        source_id: ids.videoSource,
        essence_parameters: expect.objectContaining({
          frame_height: 1080,
          frame_rate: { numerator: 25, denominator: 1 },
          frame_width: 1920,
        }),
      }),
    );
    expect(apiMock.updateSourceLabel).toHaveBeenCalledWith(
      ids.videoSource,
      "Programme (video)",
    );
    expect(apiMock.createFlow).toHaveBeenCalledWith(
      ids.audioFlow,
      expect.objectContaining({
        codec: "audio/aac",
        container: "video/mp2t",
        format: "urn:x-nmos:format:audio",
        label: "Programme (audio)",
        source_id: ids.audioSource,
      }),
    );
    expect(apiMock.updateSourceLabel).toHaveBeenCalledWith(
      ids.audioSource,
      "Programme (audio)",
    );
    expect(apiMock.updateSourceDescription).not.toHaveBeenCalled();
    expect(apiMock.createFlow).toHaveBeenCalledWith(
      ids.multiFlow,
      expect.objectContaining({
        format: "urn:x-nmos:format:multi",
        label: "clip.mp4 (multi)",
        source_id: ids.parentSource,
        tags: {
          "tamoss-ingest": "managed-browser",
          "tamoss-ingest-state": "pending",
        },
      }),
    );
    expect(apiMock.setFlowCollection).toHaveBeenCalledWith(ids.multiFlow, [
      { id: ids.videoFlow, role: "video" },
      { id: ids.audioFlow, role: "audio" },
    ]);
    expect(apiMock.createFlow.mock.calls.map(([flowId]) => flowId)).toEqual([
      ids.videoFlow,
      ids.audioFlow,
      ids.multiFlow,
    ]);
    expect(apiMock.setFlowCollection.mock.invocationCallOrder[0]).toBeLessThan(
      apiMock.addFlowSegments.mock.invocationCallOrder[0],
    );
    expect(apiMock.uploadRaw).toHaveBeenCalledWith(
      {
        url: `https://upload.example/${ids.videoObject}`,
        headers: { "Content-Type": "video/mp2t" },
      },
      expect.any(Blob),
    );
    expect(apiMock.addFlowSegments).toHaveBeenCalledWith(ids.videoFlow, [
      {
        object_id: ids.videoObject,
        timerange: "[0:0_6:0)",
        object_timerange: "[1:400000000_7:400000000)",
        ts_offset: "-1:400000000",
        last_duration: "0:40000000",
      },
    ]);
    expect(apiMock.addFlowSegments).toHaveBeenCalledWith(ids.audioFlow, [
      {
        object_id: ids.audioObject,
        timerange: "[0:0_6:0)",
        object_timerange: "[1:400000000_7:400000000)",
        ts_offset: "-1:400000000",
      },
    ]);
    expect(apiMock.updateFlowTag).toHaveBeenCalledWith(
      ids.multiFlow,
      "tamoss-ingest-state",
      "complete",
    );
    expect(apiMock.updateSourceTag).toHaveBeenCalledWith(
      ids.parentSource,
      "tamoss-ingest-state",
      "complete",
    );
    const segmentCallOrder = apiMock.addFlowSegments.mock.invocationCallOrder;
    expect(segmentCallOrder[segmentCallOrder.length - 1]).toBeLessThan(
      apiMock.updateFlowTag.mock.invocationCallOrder[0],
    );
    expect(apiMock.updateFlowTag.mock.invocationCallOrder[0]).toBeLessThan(
      apiMock.updateSourceTag.mock.invocationCallOrder[0],
    );
    expect(ffmpegServiceMock.probe).toHaveBeenCalledTimes(1);
    expect(ffmpegServiceMock.probeSegment).toHaveBeenCalledTimes(2);
  });

  it("registers segment timing from measured media-object timing", async () => {
    stubUuid([ids.file, ids.videoFlow, ids.videoObject, ids.audioObject]);
    ffmpegServiceMock.probe.mockReset();
    ffmpegServiceMock.probe.mockResolvedValueOnce({
      duration: 0,
      hasAudio: false,
      hasVideo: true,
      height: 1080,
      frameRate: { numerator: 25, denominator: 1 },
      videoCodec: "h264",
      width: 1920,
    });
    ffmpegServiceMock.probeSegment.mockReset();
    ffmpegServiceMock.probeSegment
      .mockResolvedValueOnce(segmentProbe(4_000_000_000n, 1_500_000_000n))
      .mockResolvedValueOnce(segmentProbe(8_500_000_000n, 1_500_000_000n));
    ffmpegServiceMock.segment.mockResolvedValue([
      new Blob(["segment-1"], { type: "video/mp2t" }),
      new Blob(["segment-2"], { type: "video/mp2t" }),
    ]);

    const file = new File(["media"], "clip.mp4", { type: "video/mp4" });
    const { result } = renderHook(() => useIngestSession());

    act(() => result.current.addFiles([file]));
    await waitFor(() => expect(result.current.session.files).toHaveLength(1));

    await act(async () => {
      await result.current.startIngest({
        id: ids.parentSource,
        format: "urn:x-nmos:format:video",
        label: "Programme",
      });
    });

    expect(apiMock.addFlowSegments).toHaveBeenCalledWith(ids.videoFlow, [
      {
        object_id: ids.videoObject,
        timerange: "[0:0_4:0)",
        object_timerange: "[1:500000000_5:500000000)",
        ts_offset: "-1:500000000",
        last_duration: "0:40000000",
      },
      {
        object_id: ids.audioObject,
        timerange: "[4:0_12:500000000)",
        object_timerange: "[1:500000000_10:0)",
        ts_offset: "2:500000000",
        last_duration: "0:40000000",
      },
    ]);
    expect(apiMock.createFlow).toHaveBeenCalledWith(
      ids.videoFlow,
      expect.objectContaining({
        label: "Programme",
      }),
    );
    expect(result.current.session.files[0].status).toBe("done");
  });

  it("uses draft source labels in initial flow creation events", async () => {
    stubUuid([
      ids.file,
      ids.videoSource,
      ids.audioSource,
      ids.videoFlow,
      ids.audioFlow,
      ids.multiFlow,
      ids.videoObject,
      ids.audioObject,
    ]);

    const file = new File(["media"], "clip.mp4", { type: "video/mp4" });
    const { result } = renderHook(() => useIngestSession());

    act(() => result.current.addFiles([file]));
    await waitFor(() => expect(result.current.session.files).toHaveLength(1));

    await act(async () => {
      await result.current.startIngest({
        id: ids.parentSource,
        format: "urn:x-nmos:format:multi",
        label: "Programme",
      });
    });

    expect(apiMock.createFlow).toHaveBeenCalledWith(
      ids.videoFlow,
      expect.objectContaining({
        label: "Programme (video)",
        source_id: ids.videoSource,
      }),
    );
    expect(apiMock.createFlow).toHaveBeenCalledWith(
      ids.audioFlow,
      expect.objectContaining({
        label: "Programme (audio)",
        source_id: ids.audioSource,
      }),
    );
    expect(apiMock.createFlow).toHaveBeenCalledWith(
      ids.multiFlow,
      expect.objectContaining({
        label: "Programme",
        source_id: ids.parentSource,
      }),
    );
  });

  it("falls back to source timing when segment duration probing is unavailable", async () => {
    stubUuid([ids.file, ids.videoFlow, ids.videoObject, ids.audioObject]);
    ffmpegServiceMock.probe.mockReset();
    ffmpegServiceMock.probe.mockResolvedValueOnce({
      duration: 12,
      durationNanoseconds: 12_000_000_000n,
      hasAudio: false,
      hasVideo: true,
      height: 1080,
      frameRate: { numerator: 25, denominator: 1 },
      videoCodec: "h264",
      width: 1920,
    });
    ffmpegServiceMock.probeSegment.mockReset();
    ffmpegServiceMock.probeSegment.mockResolvedValue({
      duration: 0,
      hasAudio: false,
      hasVideo: true,
      startTimeNanoseconds: 1_600_000_000n,
    });
    ffmpegServiceMock.segment.mockResolvedValue([
      new Blob(["segment-1"], { type: "video/mp2t" }),
      new Blob(["segment-2"], { type: "video/mp2t" }),
    ]);

    const file = new File(["media"], "clip.mp4", { type: "video/mp4" });
    const { result } = renderHook(() => useIngestSession());

    act(() => result.current.addFiles([file]));
    await waitFor(() => expect(result.current.session.files).toHaveLength(1));

    await act(async () => {
      await result.current.startIngest({
        id: ids.parentSource,
        format: "urn:x-nmos:format:video",
        label: "Programme",
      });
    });

    expect(apiMock.addFlowSegments).toHaveBeenCalledWith(ids.videoFlow, [
      {
        object_id: ids.videoObject,
        timerange: "[0:0_6:0)",
        object_timerange: "[1:600000000_7:600000000)",
        ts_offset: "-1:600000000",
        last_duration: "0:40000000",
      },
      {
        object_id: ids.audioObject,
        timerange: "[6:0_12:0)",
        object_timerange: "[1:600000000_7:600000000)",
        ts_offset: "4:400000000",
        last_duration: "0:40000000",
      },
    ]);
    expect(result.current.session.files[0].status).toBe("done");
  });

  it("records cleanup for allocated objects when ingest fails after allocation", async () => {
    stubUuid([
      ids.file,
      ids.videoSource,
      ids.videoFlow,
      ids.multiFlow,
      ids.videoObject,
    ]);
    ffmpegServiceMock.probe.mockReset();
    ffmpegServiceMock.probe.mockResolvedValueOnce({
      duration: 6,
      durationNanoseconds: 6_000_000_000n,
      hasAudio: false,
      hasVideo: true,
      height: 1080,
      frameRate: { numerator: 25, denominator: 1 },
      videoCodec: "h264",
      width: 1920,
    });
    apiMock.uploadRaw.mockRejectedValueOnce(new Error("upload failed"));

    const file = new File(["media"], "clip.mp4", { type: "video/mp4" });
    const { result } = renderHook(() => useIngestSession());

    act(() => result.current.addFiles([file]));
    await waitFor(() => expect(result.current.session.files).toHaveLength(1));

    await act(async () => {
      await result.current.startIngest(ids.parentSource);
    });

    expect(apiMock.deleteObjectInstance).toHaveBeenCalledWith(ids.videoObject, {
      storage_id: "storage-1",
    });
    expect(apiMock.updateFlowTag).not.toHaveBeenCalled();
    expect(apiMock.updateSourceTag).not.toHaveBeenCalled();
    expect(result.current.session.files[0].status).toBe("error");
  });
});
