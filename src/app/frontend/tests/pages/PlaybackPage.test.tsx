import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import PlaybackPage from "@/pages/PlaybackPage";
import type { Flow, FlowSegment } from "@/types/tams";

const mocks = vi.hoisted(() => ({
  api: {
    getFlows: vi.fn(),
    getFlow: vi.fn(),
    getFlowCollection: vi.fn(),
    getFlowSegments: vi.fn(),
  },
  hls: {
    attachMedia: vi.fn(),
    destroy: vi.fn(),
    loadSource: vi.fn(),
    on: vi.fn(),
  },
}));

vi.mock("@/contexts/ApiContext", () => ({
  useApi: () => mocks.api,
}));

vi.mock("hls.js", () => {
  class MockHls {
    static Events = {
      ERROR: "hlsError",
      MANIFEST_PARSED: "manifestParsed",
    };

    static isSupported() {
      return true;
    }

    attachMedia = mocks.hls.attachMedia;
    destroy = mocks.hls.destroy;
    loadSource = mocks.hls.loadSource;

    on(event: string, callback: () => void) {
      mocks.hls.on(event, callback);
      if (event === MockHls.Events.MANIFEST_PARSED) {
        queueMicrotask(callback);
      }
    }
  }

  return { default: MockHls };
});

const videoFlow: Flow = {
  id: "11111111-1111-4111-8111-111111111111",
  source_id: "source-1",
  label: "Synthetic video",
  format: "urn:x-nmos:format:video",
  codec: "video/h264",
  container: "video/mp2t",
  timerange: "[0:0_6:0)",
  essence_parameters: {
    frame_width: 1920,
    frame_height: 1080,
    frame_rate: { numerator: 25, denominator: 1 },
  },
};

const audioFlow: Flow = {
  id: "22222222-2222-4222-8222-222222222222",
  source_id: "source-1",
  label: "Synthetic audio",
  format: "urn:x-nmos:format:audio",
  codec: "audio/aac",
  container: "video/mp2t",
};

const multiFlow: Flow = {
  id: "33333333-3333-4333-8333-333333333333",
  source_id: "source-1",
  label: "Synthetic multi",
  format: "urn:x-nmos:format:multi",
};

const playableSegment: FlowSegment = {
  object_id: "segment-1.ts",
  timerange: "[0:0_6:0)",
  get_urls: [{ url: "https://media.example.test/segment-1.ts" }],
};

const playableAudioSegment: FlowSegment = {
  object_id: "segment-1-audio.ts",
  timerange: "[0:0_6:0)",
  get_urls: [{ url: "https://media.example.test/segment-1-audio.ts" }],
};

function renderPlayback(route = "/playback") {
  return render(
    <MemoryRouter initialEntries={[route]}>
      <Routes>
        <Route path="/playback" element={<PlaybackPage />} />
        <Route path="/flows/:flowId" element={<div>Flow detail</div>} />
        <Route path="/objects/:objectId" element={<div>Object detail</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("PlaybackPage", () => {
  beforeEach(() => {
    mocks.api.getFlows.mockReset();
    mocks.api.getFlow.mockReset();
    mocks.api.getFlowCollection.mockReset();
    mocks.api.getFlowSegments.mockReset();
    mocks.hls.attachMedia.mockReset();
    mocks.hls.destroy.mockReset();
    mocks.hls.loadSource.mockReset();
    mocks.hls.on.mockReset();

    mocks.api.getFlows.mockResolvedValue({
      data: [videoFlow],
      nextKey: undefined,
    });
    mocks.api.getFlowCollection.mockResolvedValue([]);
  });

  it("starts as a scoped preview surface when no flow is selected", async () => {
    renderPlayback();

    expect(await screen.findByText("Playback Preview")).toBeInTheDocument();
    expect(screen.getByText("Select a flow to preview.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Load Preview" })).toBeDisabled();
    expect(mocks.api.getFlows).toHaveBeenCalledWith(
      expect.objectContaining({ limit: "300" }),
    );
    expect(mocks.api.getFlow).not.toHaveBeenCalled();
  });

  it("builds an HLS preview from TAMS segment get_urls", async () => {
    mocks.api.getFlow.mockResolvedValue(videoFlow);
    mocks.api.getFlowSegments.mockResolvedValue({
      data: [playableSegment],
      nextKey: undefined,
    });

    renderPlayback(`/playback?flow=${videoFlow.id}`);

    expect(await screen.findByText("Preview ready")).toBeInTheDocument();
    expect(screen.getByText("1 playable segment")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Inspect Flow" })).toHaveAttribute(
      "href",
      `/flows/${videoFlow.id}`,
    );
    expect(mocks.api.getFlowSegments).toHaveBeenCalledWith(videoFlow.id, {
      include_object_timerange: true,
      limit: "300",
      presigned: "true",
    });
    await waitFor(() => {
      expect(mocks.hls.loadSource).toHaveBeenCalledWith(
        expect.stringMatching(/^blob:/),
      );
      expect(mocks.hls.attachMedia).toHaveBeenCalled();
    });
  });

  it("builds an HLS preview from a multi-flow collection", async () => {
    mocks.api.getFlows.mockResolvedValue({
      data: [multiFlow, videoFlow, audioFlow],
      nextKey: undefined,
    });
    mocks.api.getFlow.mockImplementation(async (flowId: string) => {
      if (flowId === multiFlow.id) return multiFlow;
      if (flowId === videoFlow.id) return videoFlow;
      if (flowId === audioFlow.id) return audioFlow;
      throw new Error("missing flow");
    });
    mocks.api.getFlowCollection.mockResolvedValue([
      { id: videoFlow.id, role: "video" },
      { id: audioFlow.id, role: "audio" },
    ]);
    mocks.api.getFlowSegments.mockImplementation(async (flowId: string) => ({
      data:
        flowId === audioFlow.id
          ? [playableAudioSegment]
          : flowId === videoFlow.id
            ? [playableSegment]
            : [],
      nextKey: undefined,
    }));

    renderPlayback(`/playback?flow=${multiFlow.id}`);

    expect(await screen.findByText("Preview ready")).toBeInTheDocument();
    expect(screen.getByText("2 playable segments")).toBeInTheDocument();
    expect(screen.getByText("2 child flows")).toBeInTheDocument();
    expect(mocks.api.getFlowCollection).toHaveBeenCalledWith(multiFlow.id);
    expect(mocks.api.getFlowSegments).not.toHaveBeenCalledWith(
      multiFlow.id,
      expect.anything(),
    );
    expect(mocks.api.getFlowSegments).toHaveBeenCalledWith(videoFlow.id, {
      include_object_timerange: true,
      limit: "300",
      presigned: "true",
    });
    expect(mocks.api.getFlowSegments).toHaveBeenCalledWith(audioFlow.id, {
      include_object_timerange: true,
      limit: "300",
      presigned: "true",
    });
    await waitFor(() => {
      expect(mocks.hls.loadSource).toHaveBeenCalledWith(
        expect.stringMatching(/^blob:/),
      );
      expect(mocks.hls.attachMedia).toHaveBeenCalled();
    });
  });

  it("does not pretend playback is available without segment URLs", async () => {
    mocks.api.getFlow.mockResolvedValue(videoFlow);
    mocks.api.getFlowSegments.mockResolvedValue({
      data: [{ object_id: "segment-without-url.ts", timerange: "[0:0_6:0)" }],
      nextKey: undefined,
    });

    renderPlayback(`/playback?flow=${videoFlow.id}`);

    expect(
      await screen.findByText("No playable segment URLs"),
    ).toBeInTheDocument();
    expect(screen.getByText("0 playable segments")).toBeInTheDocument();
    expect(mocks.hls.loadSource).not.toHaveBeenCalled();
  });
});
