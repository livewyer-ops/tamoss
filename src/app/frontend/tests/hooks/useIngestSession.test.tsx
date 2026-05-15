import { act, renderHook, waitFor } from "@testing-library/react";
import { useIngestSession } from "@/hooks/useIngestSession";

const { apiMock, ffmpegServiceMock } = vi.hoisted(() => ({
  apiMock: {
    addFlowSegments: vi.fn(),
    allocateStorage: vi.fn(),
    createFlow: vi.fn(),
    getSource: vi.fn(),
    setFlowCollection: vi.fn(),
    updateSourceDescription: vi.fn(),
    updateSourceLabel: vi.fn(),
    uploadRaw: vi.fn(),
  },
  ffmpegServiceMock: {
    probe: vi.fn(),
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
    apiMock.allocateStorage.mockImplementation(
      (_flowId: string, objectIds: string[]) =>
        Promise.resolve({
          media_objects: objectIds.map((objectId) => ({
            object_id: objectId,
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
    apiMock.updateSourceDescription.mockResolvedValue(undefined);
    apiMock.updateSourceLabel.mockResolvedValue(undefined);

    ffmpegServiceMock.probe.mockResolvedValue({
      audioCodec: "aac",
      channels: 2,
      duration: 6,
      hasAudio: true,
      hasVideo: true,
      height: 1080,
      frameRate: { numerator: 25, denominator: 1 },
      sampleRate: 48000,
      videoCodec: "h264",
      width: 1920,
    });
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
      ids.videoObject,
      ids.audioFlow,
      ids.audioObject,
      ids.multiFlow,
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
        source_id: ids.parentSource,
      }),
    );
    expect(apiMock.setFlowCollection).toHaveBeenCalledWith(ids.multiFlow, [
      { id: ids.videoFlow, role: "video" },
      { id: ids.audioFlow, role: "audio" },
    ]);
    expect(apiMock.uploadRaw).toHaveBeenCalledWith(
      {
        url: `https://upload.example/${ids.videoObject}`,
        headers: { "Content-Type": "video/mp2t" },
      },
      expect.any(Blob),
    );
  });
});
