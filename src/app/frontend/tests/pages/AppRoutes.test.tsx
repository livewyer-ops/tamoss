import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import App from "@/App";

const mocks = vi.hoisted(() => {
  const paginated = <T,>(data: T[]) => ({ data, nextKey: undefined });
  const source = {
    id: "source-1",
    label: "Camera A",
    format: "urn:x-nmos:format:video",
    tags: { site: "studio" },
  };
  const flow = {
    id: "flow-1",
    source_id: "source-1",
    label: "Synthetic video",
    format: "urn:x-nmos:format:video",
    codec: "video/h264",
    container: "video/mp2t",
    tags: { purpose: "smoke" },
    timerange: "[0:0_6:0)",
    essence_parameters: {
      frame_width: 1920,
      frame_height: 1080,
      frame_rate: { numerator: 25, denominator: 1 },
    },
  };
  const segment = {
    object_id: "object-1.ts",
    timerange: "[0:0_6:0)",
    get_urls: [{ url: "https://media.example.test/object-1.ts" }],
  };
  const mediaObject = {
    id: "object-1.ts",
    referenced_by_flows: ["flow-1"],
    first_referenced_by_flow: "flow-1",
    timerange: "[0:0_6:0)",
    get_urls: [{ url: "https://media.example.test/object-1.ts" }],
  };
  const storageBackend = {
    id: "storage-1",
    label: "RustFS",
    store_type: "http_object_store",
    store_product: "s3",
    provider: "tamoss",
    region: "us-east-1",
    default_storage: true,
  };
  const storageBackend2 = {
    id: "storage-2",
    label: "Archive",
    store_type: "http_object_store",
    store_product: "s3",
    provider: "rustfs",
    region: "eu-west-2",
    default_storage: false,
  };
  const service = {
    name: "TAMOSS",
    type: "urn:x-tamoss:service",
    api_version: "8.0",
    service_version: "1.0.0",
    event_stream_mechanisms: [{ name: "webhooks" }],
  };

  return {
    api: {
      getHealth: vi.fn(),
      getService: vi.fn(),
      getRootPaths: vi.fn(),
      getSources: vi.fn(),
      getSource: vi.fn(),
      getFlows: vi.fn(),
      getFlow: vi.fn(),
      getFlowSegments: vi.fn(),
      getFlowCollection: vi.fn(),
      getStorageBackends: vi.fn(),
      getObject: vi.fn(),
      getWebhooks: vi.fn(),
      getDeletionRequests: vi.fn(),
      getDeletionRequest: vi.fn(),
      updateServiceInfo: vi.fn(),
      updateSourceLabel: vi.fn(),
      updateSourceDescription: vi.fn(),
      updateSourceTag: vi.fn(),
      deleteSourceTag: vi.fn(),
      updateFlowLabel: vi.fn(),
      updateFlowDescription: vi.fn(),
      updateFlowAvgBitRate: vi.fn(),
      updateFlowMaxBitRate: vi.fn(),
      updateFlowTag: vi.fn(),
      deleteFlowTag: vi.fn(),
      deleteFlow: vi.fn(),
      setFlowReadOnly: vi.fn(),
      createFlow: vi.fn(),
      addFlowSegments: vi.fn(),
      setFlowCollection: vi.fn(),
      allocateStorageByCount: vi.fn(),
      deleteObjectInstance: vi.fn(),
      deleteFlowSegments: vi.fn(),
      createWebhook: vi.fn(),
      updateWebhook: vi.fn(),
      deleteWebhook: vi.fn(),
    },
    fixtures: {
      flow,
      mediaObject,
      paginated,
      service,
      source,
      storageBackend,
      storageBackend2,
      segment,
    },
  };
});

vi.mock("@/contexts/ApiContext", () => ({
  ApiProvider: ({ children }: { children: ReactNode }) => children,
  useApi: () => mocks.api,
}));

vi.mock("@/config", () => ({
  config: {
    apiToken: "",
    apiUrl: "https://api.example.test/",
  },
}));

function primeApiMocks() {
  const { api, fixtures } = mocks;
  api.getHealth.mockResolvedValue("ok");
  api.getService.mockResolvedValue(fixtures.service);
  api.getRootPaths.mockResolvedValue(["service", "sources", "flows"]);
  api.getSources.mockResolvedValue(fixtures.paginated([fixtures.source]));
  api.getSource.mockResolvedValue(fixtures.source);
  api.getFlows.mockResolvedValue(fixtures.paginated([fixtures.flow]));
  api.getFlow.mockResolvedValue(fixtures.flow);
  api.getFlowSegments.mockResolvedValue(fixtures.paginated([fixtures.segment]));
  api.getFlowCollection.mockResolvedValue([]);
  api.getStorageBackends.mockResolvedValue([
    fixtures.storageBackend,
    fixtures.storageBackend2,
  ]);
  api.getObject.mockResolvedValue({
    data: fixtures.mediaObject,
    nextKey: undefined,
    limit: 50,
  });
  api.getWebhooks.mockResolvedValue(fixtures.paginated([]));
  api.getDeletionRequests.mockResolvedValue([]);
  api.getDeletionRequest.mockResolvedValue(null);
  api.allocateStorageByCount.mockResolvedValue({ media_objects: [] });
  api.deleteObjectInstance.mockResolvedValue(undefined);
}

function renderRoute(route: string) {
  window.history.pushState({}, "TAMOSS test route", route);
  return render(<App />);
}

describe("App routes", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    primeApiMocks();
  });

  it.each([
    ["/", "Dashboard", "Dashboard · TAMOSS"],
    ["/service", "Service", "Service · TAMOSS"],
    ["/sources", "Sources", "Sources · TAMOSS"],
    ["/sources/source-1", "Source Overview", "Source · TAMOSS"],
    ["/flows", "Flows", "Flows · TAMOSS"],
    ["/flows/flow-1", "Flow Overview", "Flow · TAMOSS"],
    ["/objects", "Objects", "Objects · TAMOSS"],
    ["/objects/object-1.ts", "Media Object", "Media Object · TAMOSS"],
    ["/playback", "Playback Preview", "Playback · TAMOSS"],
    ["/webhooks", "Webhooks", "Webhooks · TAMOSS"],
    ["/ingest", "Ingest Media", "Ingest · TAMOSS"],
    ["/deletions", "Deletion Requests", "Deletion Requests · TAMOSS"],
  ])("renders %s", async (route, heading, title) => {
    renderRoute(route);

    expect(
      await screen.findByRole("heading", { name: heading }),
    ).toBeInTheDocument();
    expect(document.title).toBe(title);
  });

  it.each([
    ["/sources", "Sources", mocks.api.getSources],
    ["/flows", "Flows", mocks.api.getFlows],
    ["/webhooks", "Webhooks", mocks.api.getWebhooks],
  ])(
    "requests a bounded first page for %s",
    async (route, heading, request) => {
      renderRoute(route);

      await screen.findByRole("heading", { name: heading });

      await waitFor(() => {
        expect(request).toHaveBeenCalledWith(
          expect.objectContaining({ limit: "50" }),
        );
      });
    },
  );

  it("submits webhook API-key value and writable status", async () => {
    mocks.api.createWebhook.mockResolvedValue({
      id: "webhook-2",
      url: "https://hooks.example.test/tams",
      events: ["flows/created"],
      status: "disabled",
    });
    renderRoute("/webhooks");

    await screen.findByRole("heading", { name: "Webhooks" });
    fireEvent.click(screen.getByRole("button", { name: "New webhook" }));
    fireEvent.change(screen.getByLabelText("Webhook URL"), {
      target: { value: "https://hooks.example.test/tams" },
    });
    fireEvent.change(screen.getByLabelText("API key header"), {
      target: { value: "X-TAMOSS-Webhook-Key" },
    });
    fireEvent.change(screen.getByLabelText("API key value"), {
      target: { value: "shared-secret" },
    });
    fireEvent.change(screen.getByLabelText("Status"), {
      target: { value: "disabled" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create webhook" }));

    await waitFor(() => {
      expect(mocks.api.createWebhook).toHaveBeenCalledWith(
        expect.objectContaining({
          url: "https://hooks.example.test/tams",
          api_key_name: "X-TAMOSS-Webhook-Key",
          api_key_value: "shared-secret",
          events: ["flows/created"],
          status: "disabled",
        }),
      );
    });
  });

  it("requests bounded collection summaries on the dashboard", async () => {
    renderRoute("/");

    await screen.findByRole("heading", { name: "Dashboard" });

    await waitFor(() => {
      expect(mocks.api.getSources).toHaveBeenCalledWith(
        expect.objectContaining({ limit: "50" }),
      );
      expect(mocks.api.getFlows).toHaveBeenCalledWith(
        expect.objectContaining({ limit: "50" }),
      );
      expect(mocks.api.getWebhooks).toHaveBeenCalledWith(
        expect.objectContaining({ limit: "50" }),
      );
    });
  });

  it("shows the default storage backend on the service page", async () => {
    renderRoute("/service");

    await screen.findByRole("heading", { name: "Service" });

    expect(screen.getByText("Default")).toBeInTheDocument();
  });

  it("refreshes every service-page query", async () => {
    renderRoute("/service");

    await screen.findByRole("heading", { name: "Service" });
    vi.clearAllMocks();
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));

    await waitFor(() => {
      expect(mocks.api.getHealth).toHaveBeenCalled();
      expect(mocks.api.getService).toHaveBeenCalled();
      expect(mocks.api.getStorageBackends).toHaveBeenCalled();
      expect(mocks.api.getRootPaths).toHaveBeenCalled();
    });
  });

  it("requests a bounded downstream flow page on source detail", async () => {
    renderRoute("/sources/source-1");

    await screen.findByRole("heading", { name: "Source Overview" });

    await waitFor(() => {
      expect(mocks.api.getFlows).toHaveBeenCalledWith(
        expect.objectContaining({
          limit: "300",
          source_id: "source-1",
        }),
      );
    });
  });

  it("requests a bounded segment page on flow detail", async () => {
    renderRoute("/flows/flow-1");

    await screen.findByRole("heading", { name: "Flow Overview" });

    await waitFor(() => {
      expect(mocks.api.getFlowSegments).toHaveBeenCalledWith(
        "flow-1",
        expect.objectContaining({
          include_object_timerange: true,
          limit: "300",
        }),
      );
    });
  });

  it("requests a bounded object-detail page", async () => {
    renderRoute("/objects/object-1.ts");

    await screen.findByRole("heading", { name: "Media Object" });

    await waitFor(() => {
      expect(mocks.api.getObject).toHaveBeenCalledWith(
        "object-1.ts",
        expect.objectContaining({ limit: "50" }),
      );
    });
  });

  it("filters object URLs by storage backend", async () => {
    renderRoute("/objects/object-1.ts");

    await screen.findByRole("heading", { name: "Media Object" });
    await screen.findAllByRole("option", { name: /Archive/ });
    fireEvent.change(screen.getByLabelText("Storage backend"), {
      target: { value: "storage-2" },
    });

    await waitFor(() => {
      expect(mocks.api.getObject).toHaveBeenCalledWith(
        "object-1.ts",
        expect.objectContaining({
          accept_storage_ids: "storage-2",
          verbose_storage: true,
        }),
      );
    });
  });

  it("allocates flow storage on a selected backend", async () => {
    renderRoute("/flows/flow-1");

    await screen.findByRole("heading", { name: "Flow Overview" });
    await screen.findAllByRole("option", { name: /Archive/ });
    fireEvent.change(screen.getByLabelText("Allocation backend"), {
      target: { value: "storage-2" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Allocate storage" }));

    await waitFor(() => {
      expect(mocks.api.allocateStorageByCount).toHaveBeenCalledWith(
        "flow-1",
        1,
        { storageId: "storage-2" },
      );
    });
  });

  it("disables destructive flow controls for read-only flows", async () => {
    mocks.api.getFlow.mockResolvedValue({
      ...mocks.fixtures.flow,
      read_only: true,
    });

    renderRoute("/flows/flow-1");

    await screen.findByRole("heading", { name: "Flow Overview" });

    expect(screen.getByRole("button", { name: "Delete" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Add Segments" })).toBeDisabled();
  });

  it("requests bounded candidates when editing a flow collection", async () => {
    mocks.api.getFlow.mockResolvedValue({
      ...mocks.fixtures.flow,
      format: "urn:x-nmos:format:multi",
    });

    renderRoute("/flows/flow-1");

    await screen.findByRole("heading", { name: "Flow Overview" });
    fireEvent.click(screen.getByRole("button", { name: "Edit Collection" }));

    await screen.findByRole("heading", { name: "Edit Flow Collection" });
    await waitFor(() => {
      expect(mocks.api.getFlows).toHaveBeenCalledWith(
        expect.objectContaining({ limit: "300" }),
      );
    });
  });

  it("requests bounded compatible flows and segment pages in the add-segments dialog", async () => {
    const compatibleFlow = {
      ...mocks.fixtures.flow,
      id: "copy-flow-1",
      label: "Copy flow",
    };
    mocks.api.getFlows.mockResolvedValueOnce(
      mocks.fixtures.paginated([compatibleFlow]),
    );

    renderRoute("/flows/flow-1");

    await screen.findByRole("heading", { name: "Flow Overview" });
    fireEvent.click(screen.getByRole("button", { name: "Add Segments" }));

    await screen.findByRole("heading", { name: "Add Segments" });
    await waitFor(() => {
      expect(mocks.api.getFlows).toHaveBeenCalledWith(
        expect.objectContaining({
          format: "urn:x-nmos:format:video",
          limit: "300",
          source_id: "source-1",
        }),
      );
    });

    fireEvent.change(screen.getByLabelText(/Source flow/), {
      target: { value: "copy-flow-1" },
    });

    await waitFor(() => {
      expect(mocks.api.getFlowSegments).toHaveBeenCalledWith(
        "copy-flow-1",
        expect.objectContaining({
          include_object_timerange: true,
          limit: "300",
        }),
      );
    });
  });

  it("creates a flow by copying an existing flow", async () => {
    renderRoute("/flows");

    await screen.findByRole("heading", { name: "Flows" });
    fireEvent.click(screen.getByRole("button", { name: "Create Flow" }));

    await screen.findByRole("heading", { name: "Create New Flow" });
    fireEvent.change(screen.getByLabelText(/Copy properties from/), {
      target: { value: "flow-1" },
    });
    fireEvent.change(screen.getByLabelText(/Label/), {
      target: { value: "Copied flow" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));

    await waitFor(() => {
      expect(mocks.api.getFlowSegments).toHaveBeenCalledWith(
        "flow-1",
        expect.objectContaining({
          include_object_timerange: true,
          limit: "300",
        }),
      );
      expect(mocks.api.createFlow).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          source_id: "source-1",
          format: "urn:x-nmos:format:video",
          label: "Copied flow",
          codec: "video/h264",
          container: "video/mp2t",
          essence_parameters: mocks.fixtures.flow.essence_parameters,
        }),
      );
    });
  });

  it("shows flow collection load errors", async () => {
    mocks.api.getFlow.mockResolvedValue({
      ...mocks.fixtures.flow,
      format: "urn:x-nmos:format:multi",
    });
    mocks.api.getFlowCollection.mockRejectedValue(
      new Error("collection route failed"),
    );

    renderRoute("/flows/flow-1");

    await screen.findByRole("heading", { name: "Flow Overview" });

    expect(
      await screen.findByText("collection route failed"),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("No flow collection items."),
    ).not.toBeInTheDocument();
  });

  it("shows child segment load errors", async () => {
    mocks.api.getFlow.mockResolvedValue({
      ...mocks.fixtures.flow,
      format: "urn:x-nmos:format:multi",
    });
    mocks.api.getFlowCollection.mockResolvedValue([
      { id: "child-flow-1", role: "video" },
    ]);
    mocks.api.getFlowSegments.mockImplementation(async (flowId: string) => {
      if (flowId === "child-flow-1")
        throw new Error("child segments unavailable");
      return mocks.fixtures.paginated([mocks.fixtures.segment]);
    });

    renderRoute("/flows/flow-1");

    await screen.findByRole("heading", { name: "Flow Overview" });

    expect(
      await screen.findByText("child segments unavailable"),
    ).toBeInTheDocument();
  });

  it("shows deletion request error summaries", async () => {
    const deletionRequest = {
      id: "delete-1",
      flow_id: "flow-1",
      timerange_to_delete: "[0:0_10:0)",
      delete_flow: false,
      status: "error" as const,
      created: "2026-01-01T00:00:00Z",
      updated: "2026-01-01T00:01:00Z",
      error: {
        type: "RuntimeError",
        summary: "Delete failed",
        time: "2026-01-01T00:01:00Z",
      },
    };
    mocks.api.getDeletionRequests.mockResolvedValue([deletionRequest]);
    mocks.api.getDeletionRequest.mockResolvedValue(deletionRequest);

    renderRoute("/deletions?request=delete-1");

    expect(await screen.findAllByText("Delete failed")).not.toHaveLength(0);
    expect(screen.queryByText("Unknown error")).not.toBeInTheDocument();
  });

  it("refreshes selected deletion request detail", async () => {
    const deletionRequest = {
      id: "delete-1",
      flow_id: "flow-1",
      timerange_to_delete: "[0:0_10:0)",
      delete_flow: false,
      status: "created" as const,
      created: "2026-01-01T00:00:00Z",
    };
    mocks.api.getDeletionRequests.mockResolvedValue([deletionRequest]);
    mocks.api.getDeletionRequest.mockResolvedValue(deletionRequest);

    renderRoute("/deletions?request=delete-1");

    await screen.findByRole("heading", { name: "Deletion Requests" });
    vi.clearAllMocks();
    fireEvent.click(
      screen.getByRole("button", { name: "Refresh deletion requests" }),
    );

    await waitFor(() => {
      expect(mocks.api.getDeletionRequests).toHaveBeenCalled();
      expect(mocks.api.getDeletionRequest).toHaveBeenCalledWith("delete-1");
    });
  });

  it("opens the accepted deletion request after background flow deletion", async () => {
    const deletionRequest = {
      id: "delete-1",
      flow_id: "flow-1",
      timerange_to_delete: "[0:0_6:0)",
      delete_flow: true,
      status: "created" as const,
      created: "2026-01-01T00:00:00Z",
    };
    mocks.api.deleteFlow.mockResolvedValue(deletionRequest);
    mocks.api.getDeletionRequests.mockResolvedValue([deletionRequest]);
    mocks.api.getDeletionRequest.mockResolvedValue(deletionRequest);

    renderRoute("/flows/flow-1");

    await screen.findByRole("heading", { name: "Flow Overview" });
    fireEvent.click(screen.getByRole("button", { name: "Delete" }));
    fireEvent.click(
      await screen.findByRole("button", { name: "Confirm Delete" }),
    );

    expect(mocks.api.deleteFlow).toHaveBeenCalledWith("flow-1");
    expect(
      await screen.findByRole("heading", { name: "Deletion Requests" }),
    ).toBeInTheDocument();
    expect(window.location.pathname).toBe("/deletions");
    expect(window.location.search).toBe("?request=delete-1");
  });
});
