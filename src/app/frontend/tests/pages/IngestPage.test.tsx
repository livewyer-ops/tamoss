import { fireEvent, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import IngestPage from "@/pages/IngestPage";
import { renderWithQueryClient } from "../testUtils";

const mocks = vi.hoisted(() => ({
  api: {
    getSources: vi.fn(),
    getStorageBackends: vi.fn(),
  },
  addFiles: vi.fn(),
  removeFile: vi.fn(),
  reset: vi.fn(),
  setSegmentDuration: vi.fn(),
  setSourceId: vi.fn(),
  startIngest: vi.fn(),
  session: {} as {
    sourceId: string | null;
    segmentDuration: number;
    files: Array<{
      file: File;
      id: string;
      status: "pending";
      tracks: { hasVideo: boolean; hasAudio: boolean };
      progress: number;
    }>;
    running: boolean;
  },
}));

vi.mock("@/contexts/ApiContext", () => ({
  ApiProvider: ({ children }: { children: ReactNode }) => children,
  useApi: () => mocks.api,
}));

vi.mock("@/hooks/useIngestSession", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@/hooks/useIngestSession")>();
  return {
    ...actual,
    useIngestSession: () => ({
      session: mocks.session,
      addFiles: mocks.addFiles,
      removeFile: mocks.removeFile,
      setSourceId: mocks.setSourceId,
      setSegmentDuration: mocks.setSegmentDuration,
      startIngest: mocks.startIngest,
      reset: mocks.reset,
    }),
  };
});

function renderPage() {
  return renderWithQueryClient(
    <MemoryRouter>
      <IngestPage />
    </MemoryRouter>,
  );
}

describe("IngestPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.spyOn(crypto, "randomUUID").mockReturnValue(
      "00000000-0000-4000-8000-000000000123",
    );
    mocks.session = {
      sourceId: null,
      segmentDuration: 6,
      files: [
        {
          file: new File(["media"], "clip.mp4", { type: "video/mp4" }),
          id: "file-1",
          status: "pending",
          tracks: { hasVideo: false, hasAudio: false },
          progress: 0,
        },
      ],
      running: false,
    };
    mocks.api.getSources.mockResolvedValue({
      data: [
        {
          id: "source-1",
          format: "urn:x-nmos:format:video",
          label: "Camera A",
        },
      ],
      nextKey: undefined,
    });
    mocks.api.getStorageBackends.mockResolvedValue([
      {
        id: "storage-1",
        label: "RustFS",
        provider: "tamoss",
        store_type: "http_object_store",
        store_product: "s3",
        default_storage: true,
      },
    ]);
    mocks.startIngest.mockResolvedValue(undefined);
  });

  it("starts ingest with a source draft when Create source is selected", async () => {
    renderPage();
    await screen.findByRole("option", { name: "Camera A" });

    fireEvent.click(screen.getByRole("button", { name: "Create source" }));
    fireEvent.change(
      screen.getByRole("textbox", { name: /New source label/ }),
      { target: { value: "New ingest source" } },
    );
    fireEvent.click(
      screen.getByRole("button", { name: "Create Source & Start Ingest" }),
    );

    await waitFor(() =>
      expect(mocks.startIngest).toHaveBeenCalledWith(
        {
          id: "00000000-0000-4000-8000-000000000123",
          format: "urn:x-nmos:format:multi",
          label: "New ingest source",
          description: undefined,
        },
        undefined,
      ),
    );
    expect(mocks.setSourceId).toHaveBeenCalledWith(
      "00000000-0000-4000-8000-000000000123",
    );
  });

  it("marks create-source required fields", async () => {
    renderPage();
    await screen.findByRole("option", { name: "Camera A" });

    fireEvent.click(screen.getByRole("button", { name: "Create source" }));

    expect(
      screen.getByText("*", { selector: "label[for='new-source-label'] span" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText("*", {
        selector: "label[for='new-source-format'] span",
      }),
    ).toBeInTheDocument();
    expect(
      screen.getByText("(optional)", {
        selector: "label[for='new-source-description'] span",
      }),
    ).toBeInTheDocument();
  });

  it("starts ingest against the selected existing source", async () => {
    mocks.session.sourceId = "source-1";
    renderPage();

    fireEvent.click(screen.getByRole("button", { name: "Start Ingest" }));

    await waitFor(() =>
      expect(mocks.startIngest).toHaveBeenCalledWith("source-1", undefined),
    );
  });
});
