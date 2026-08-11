import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { MediaPreviewDescriptor } from "@/player/descriptor";
import MediaPreview from "@/player/MediaPreview";
import { renderWithQueryClient } from "../testUtils";

const mocks = vi.hoisted(() => ({
  api: {},
  buildDescriptor: vi.fn(),
  createPreview: vi.fn(),
  destroy: vi.fn(),
  selectAudioTrack: vi.fn(),
}));

vi.mock("@/contexts/ApiContext", () => ({
  useApi: () => mocks.api,
}));

vi.mock("@/player/descriptor", async (importOriginal) => {
  const original = await importOriginal<typeof import("@/player/descriptor")>();
  return {
    ...original,
    buildMediaPreviewDescriptor: mocks.buildDescriptor,
  };
});

vi.mock("@/player/OmakaseAdapter", () => ({
  createOmakasePreview: mocks.createPreview,
}));

function descriptor(
  overrides: Partial<MediaPreviewDescriptor> = {},
): MediaPreviewDescriptor {
  const video = {
    kind: "video" as const,
    role: "programme",
    flow: {
      id: "video-1",
      source_id: "source-1",
      label: "Programme video",
      format: "urn:x-nmos:format:video",
      codec: "video/h264",
      container: "video/mp2t",
      timerange: "[100:0_110:0)",
    },
    segments: [
      {
        object_id: "object-1.ts",
        timerange: "[100:0_105:0)",
        get_urls: [
          {
            url: "https://storage.example/object-1.ts?signature=secret",
            credentials: "omit" as const,
            presigned: true,
            label: "primary",
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
    initialTimerange: "[100:0_110:0)",
    segmentCount: 1,
    truncated: false,
    flowsSegments: new Map([[video.flow.id, video.segments]]),
    ...overrides,
  };
}

describe("MediaPreview", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.buildDescriptor.mockResolvedValue(descriptor());
    mocks.createPreview.mockImplementation(({ onChange }) => {
      onChange({ phase: "ready", currentTime: 0, duration: 10 });
      return {
        ready: Promise.resolve(),
        audioTracks: [],
        destroy: mocks.destroy,
        selectAudioTrack: mocks.selectAudioTrack,
      };
    });
  });

  afterEach(() => cleanup());

  it("mounts Omakase and keeps signed media URLs out of the DOM", async () => {
    const view = renderWithQueryClient(<MediaPreview flowId="video-1" />);

    expect(
      await screen.findByRole("heading", { name: "Programme video" }),
    ).toBeInTheDocument();
    expect(screen.getAllByText("programme")).toHaveLength(2);
    expect(screen.getByText("object-1.ts", { exact: false })).not.toBeNull();
    expect(document.body.textContent).not.toContain("signature=secret");
    await waitFor(() => expect(mocks.createPreview).toHaveBeenCalledOnce());
    expect(screen.getByRole("status")).toHaveTextContent("Ready");

    view.unmount();
    expect(mocks.destroy).toHaveBeenCalledOnce();
  });

  it("shows metadata without mounting a player for non-media tracks", async () => {
    const dataOnly = descriptor();
    const dataTrack = {
      ...dataOnly.tracks[0],
      kind: "data" as const,
      flow: {
        ...dataOnly.tracks[0].flow,
        id: "captions-1",
        format: "urn:x-nmos:format:data",
        container: "text/vtt",
      },
    };
    mocks.buildDescriptor.mockResolvedValue({
      ...dataOnly,
      rootFlow: dataTrack.flow,
      tracks: [dataTrack],
      video: undefined,
      data: [dataTrack],
      flowsSegments: new Map([[dataTrack.flow.id, dataTrack.segments]]),
    });

    renderWithQueryClient(<MediaPreview flowId="captions-1" />);

    expect(
      await screen.findByText("No playable media in this window"),
    ).toBeInTheDocument();
    expect(screen.getByText("captions-1")).toBeInTheDocument();
    expect(mocks.createPreview).not.toHaveBeenCalled();
  });

  it("offers a bounded retry when descriptor loading fails", async () => {
    mocks.buildDescriptor.mockRejectedValue(
      new Error("The preview Flow collection is empty."),
    );

    renderWithQueryClient(<MediaPreview flowId="multi-1" />);

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "The preview Flow collection is empty.",
    );
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });

  it("reports an instantaneous window as empty rather than as a failure", async () => {
    mocks.buildDescriptor.mockResolvedValue({
      ...descriptor(),
      initialTimerange: "[100:0]",
    });

    renderWithQueryClient(<MediaPreview flowId="video-1" />);

    expect(
      await screen.findByText("No playable media in this window"),
    ).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(mocks.createPreview).not.toHaveBeenCalled();
  });

  it("selects the default split-audio track and switches renditions", async () => {
    mocks.buildDescriptor.mockResolvedValue(multiAudioDescriptor());
    mocks.createPreview.mockImplementation(({ onChange }) => {
      onChange({ phase: "ready", currentTime: 0, duration: 10 });
      return {
        ready: Promise.resolve(),
        audioTracks: [
          { flowId: "audio-1", label: "Programme" },
          { flowId: "audio-2", label: "Commentary" },
        ],
        destroy: mocks.destroy,
        selectAudioTrack: mocks.selectAudioTrack,
      };
    });
    renderWithQueryClient(<MediaPreview flowId="multi-1" />);

    const selector = await screen.findByRole("combobox", { name: "Audio" });
    expect(selector).toHaveValue("audio-1");
    await waitFor(() =>
      expect(mocks.selectAudioTrack).toHaveBeenCalledWith("audio-1"),
    );
    fireEvent.change(selector, { target: { value: "audio-2" } });
    expect(mocks.selectAudioTrack).toHaveBeenLastCalledWith("audio-2");
  });

  it("omits the audio selector when the player cannot switch renditions", async () => {
    const base = multiAudioDescriptor();
    mocks.buildDescriptor.mockResolvedValue({
      ...base,
      tracks: base.audio,
      video: undefined,
    });
    renderWithQueryClient(<MediaPreview flowId="multi-1" />);

    await waitFor(() => expect(mocks.createPreview).toHaveBeenCalledOnce());
    expect(
      screen.queryByRole("combobox", { name: "Audio" }),
    ).not.toBeInTheDocument();
    expect(mocks.selectAudioTrack).not.toHaveBeenCalled();
    // The inventory still lists every audio track that the window contains.
    expect(screen.getByText("audio-1")).toBeInTheDocument();
    expect(screen.getByText("audio-2")).toBeInTheDocument();
  });
});

function multiAudioDescriptor(): MediaPreviewDescriptor {
  const base = descriptor();
  const audio = ["Programme", "Commentary"].map((role, index) => ({
    ...base.tracks[0],
    kind: "audio" as const,
    role,
    flow: {
      ...base.tracks[0].flow,
      id: `audio-${index + 1}`,
      format: "urn:x-nmos:format:audio",
      codec: "audio/aac",
    },
  }));
  return { ...base, tracks: [...base.tracks, ...audio], audio };
}
