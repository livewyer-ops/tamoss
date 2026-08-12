import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import App from "@/App";
import { apiQueryClient } from "@/api/query";
import type { RuntimeSnapshot } from "@/control/runtime";

const mocks = vi.hoisted(() => {
  const source = {
    id: "source-1",
    label: "Camera A",
    format: "urn:x-nmos:format:video",
    tags: { site: "studio" },
  };
  const flow = {
    id: "flow-1",
    source_id: "source-1",
    label: "Programme video",
    format: "urn:x-nmos:format:video",
    codec: "video/h264",
    profile_id: "00000000-0000-4000-8000-000000000101",
    status: "ingesting",
    timerange: "[0:0_6:0)",
  };
  const profile = {
    id: "00000000-0000-4000-8000-000000000101",
    label: "HD production video",
    description: "Production video profile",
    created: "2026-08-10T12:00:00Z",
    tags: { purpose: ["editorial", "archive"] },
    flow_metadata: {
      format: "urn:x-nmos:format:video",
      codec: "video/h264",
      container: "video/mp2t",
      avg_bit_rate: 8_000,
      segment_duration: { numerator: 6, denominator: 1 },
      essence_parameters: {
        frame_width: 1920,
        frame_height: 1080,
        frame_rate: { numerator: 25, denominator: 1 },
      },
    },
  };
  const mediaObject = {
    id: "object-1.ts",
    referenced_by_flows: ["flow-1"],
    first_referenced_by_flow: "flow-1",
    timerange: "[0:0_6:0)",
    get_urls: [{ url: "https://media.example.test/object-1.ts" }],
  };
  return {
    api: {
      getHealth: vi.fn(),
      getService: vi.fn(),
      getSources: vi.fn(),
      getSource: vi.fn(),
      getFlows: vi.fn(),
      getFlow: vi.fn(),
      getFlowSegments: vi.fn(),
      getProfiles: vi.fn(),
      getProfile: vi.fn(),
      getStorageBackends: vi.fn(),
      getObject: vi.fn(),
      getWebhooks: vi.fn(),
      getDeletionRequests: vi.fn(),
    },
    flow,
    mediaObject,
    profile,
    source,
  };
});

vi.mock("@/contexts/ApiContext", () => ({
  ApiProvider: ({ children }: { children: ReactNode }) => children,
  useApi: () => mocks.api,
}));

vi.mock("@/config", () => ({
  config: {
    apiUrl: "/api",
    controlApiUrl: "/ui-api/v1",
  },
}));

function primeApi() {
  mocks.api.getHealth.mockResolvedValue("ok");
  mocks.api.getService.mockResolvedValue({
    name: "TAMOSS",
    type: "urn:x-tamoss:service",
    api_version: "8.2",
    service_version: "1.0.0",
  });
  mocks.api.getSources.mockResolvedValue({ data: [mocks.source] });
  mocks.api.getSource.mockResolvedValue(mocks.source);
  mocks.api.getFlows.mockResolvedValue({ data: [mocks.flow] });
  mocks.api.getFlow.mockResolvedValue(mocks.flow);
  mocks.api.getFlowSegments.mockResolvedValue({ data: [] });
  mocks.api.getProfiles.mockResolvedValue({ data: [mocks.profile] });
  mocks.api.getProfile.mockResolvedValue(mocks.profile);
  mocks.api.getStorageBackends.mockResolvedValue([]);
  mocks.api.getObject.mockResolvedValue({ data: mocks.mediaObject });
  mocks.api.getWebhooks.mockResolvedValue({ data: [] });
  mocks.api.getDeletionRequests.mockResolvedValue({ data: [] });
}

function renderRoute(route: string) {
  window.history.pushState({}, "TAMOSS test route", route);
  return render(<App />);
}

function hibernatedRuntimeSnapshot(stale = false): RuntimeSnapshot {
  return {
    schemaVersion: "1.0",
    observedAt: "2026-08-09T10:00:00Z",
    stale,
    instance: {
      name: "tamoss-kind",
      namespace: "tamoss",
      uid: "instance-uid",
      generation: 2,
      observedGeneration: 2,
      phase: "Hibernated",
      conditions: [],
    },
    workloads: ["api", "worker", "ui", "console"].map((component) => ({
      kind: "Deployment",
      name: `tamoss-${component}`,
      component,
      status: "scaledDown",
      generation: 2,
      observedGeneration: 2,
      desiredReplicas: 0,
      readyReplicas: 0,
      availableReplicas: 0,
      updatedReplicas: 0,
      conditions: [],
    })),
    services: [
      {
        name: "tamoss-api",
        component: "api",
        type: "ClusterIP",
        selectorComponent: "api",
        ports: [
          {
            name: "http",
            protocol: "TCP",
            port: 8000,
            targetPort: "http",
          },
        ],
      },
      {
        name: "tamoss-authentik-outpost",
        component: "authentik-outpost",
        type: "ExternalName",
        ports: [
          {
            name: "http",
            protocol: "TCP",
            port: 9000,
            targetPort: "9000",
          },
        ],
      },
    ],
    endpointSlices: [
      {
        name: "tamoss-api-empty",
        serviceName: "tamoss-api",
        component: "api",
        addressType: "IPv4",
        ports: [{ name: "http", protocol: "TCP", port: 8000 }],
        totalEndpoints: 0,
        readyEndpoints: 0,
        notReadyEndpoints: 0,
        terminatingEndpoints: 0,
      },
    ],
    pods: [],
    jobs: [],
    events: [],
  };
}

function diagnosticRuntimeSnapshot(): RuntimeSnapshot {
  return {
    schemaVersion: "1.0",
    observedAt: "2026-08-09T10:00:00Z",
    stale: false,
    instance: {
      name: "tamoss-kind",
      namespace: "tamoss",
      uid: "instance-uid",
      generation: 3,
      observedGeneration: 3,
      phase: "Degraded",
      conditions: [
        {
          type: "Ready",
          status: "False",
          reason: "DatabaseUnavailable",
          lastTransitionTime: "2026-08-09T09:58:00Z",
        },
      ],
    },
    workloads: [
      {
        kind: "Deployment",
        name: "tamoss-api",
        component: "api",
        status: "unavailable",
        generation: 3,
        observedGeneration: 3,
        desiredReplicas: 1,
        readyReplicas: 0,
        availableReplicas: 0,
        updatedReplicas: 1,
        conditions: [
          {
            type: "Available",
            status: "False",
            reason: "MinimumReplicasUnavailable",
          },
        ],
      },
    ],
    services: [
      {
        name: "tamoss-api",
        component: "api",
        type: "ClusterIP",
        selectorComponent: "api",
        ports: [
          {
            name: "http",
            protocol: "TCP",
            port: 8000,
            targetPort: "http",
          },
        ],
      },
      {
        name: "tamoss-authentik-outpost",
        component: "authentik-outpost",
        type: "ExternalName",
        ports: [
          {
            name: "http",
            protocol: "TCP",
            port: 9000,
            targetPort: "9000",
          },
        ],
      },
    ],
    endpointSlices: [
      {
        name: "tamoss-api-x7k2z",
        serviceName: "tamoss-api",
        component: "api",
        addressType: "IPv4",
        ports: [{ name: "http", protocol: "TCP", port: 8000 }],
        totalEndpoints: 1,
        readyEndpoints: 0,
        notReadyEndpoints: 1,
        terminatingEndpoints: 0,
      },
    ],
    pods: [
      {
        name: "tamoss-api-abc",
        component: "api",
        phase: "Running",
        ready: false,
        restarts: 4,
        reason: "CrashLoopBackOff",
        deleting: false,
      },
    ],
    jobs: [
      {
        name: "tamsin-ingest-1",
        component: "tamsin",
        status: "failed",
        active: 0,
        succeeded: 0,
        failed: 1,
        conditions: [
          {
            type: "Failed",
            status: "True",
            reason: "BackoffLimitExceeded",
          },
        ],
      },
    ],
    events: [
      {
        type: "Warning",
        reason: "Unhealthy",
        regarding: { kind: "Pod", name: "tamoss-api-abc" },
        count: 3,
        lastObservedAt: "2026-08-09T09:59:00Z",
      },
    ],
  };
}

function ingestRunSummary(name = "ingest-20260809") {
  return {
    name,
    uid: `${name}-uid`,
    revision: "17",
    phase: "Running",
    profile: "Editorial",
    sizeClass: "Standard",
    desiredState: "Running",
    attempt: 1,
    createdAt: "2026-08-09T10:00:00Z",
    startedAt: "2026-08-09T10:00:03Z",
    progress: {
      inputsTotal: 12,
      inputsCompleted: 5,
      inputsSucceeded: 4,
      inputsFailed: 1,
      bytesUploaded: 4096,
    },
    cancellable: true,
  };
}

function ingestRunDetail(name = "ingest-20260809") {
  return {
    ...ingestRunSummary(name),
    generation: 2,
    observedGeneration: 2,
    inputKind: "ApprovedS3",
    options: {
      storageBackend: "archive",
      verify: true,
      dryRun: false,
      maxInputs: 1000,
      concurrency: 4,
      tamsFlowProfiles: [
        {
          format: "video",
          index: 0,
          profileRef: "hd-avc",
          resolvedProfileID: "60d9df18-6d9d-4b86-84bf-d1dcf14b3a28",
        },
      ],
    },
    outputIntent: {
      flowMetadata: {
        label: "8.2 ingest test",
        description: "Acceptance ingest",
        tags: { editorial_purpose: ["testing"] },
      },
    },
    job: { name: `${name}-job`, uid: `${name}-job-uid` },
    tamsinRunId: "tamsin-run-1",
    result: { present: false, verified: false },
    output: {
      rootFlowID: "713d391c-828a-513e-9929-65e1bab9c35b",
      sourceID: "6148f737-4536-5442-8897-c20b647e8836",
      memberFlows: [
        {
          id: "69d2a402-b8db-5faa-a970-0aed4f2acfc2",
          format: "urn:x-nmos:format:video",
          role: "video",
        },
      ],
    },
    conditions: [
      {
        type: "Ready",
        status: "False",
        reason: "IngestRunning",
        lastTransitionTime: "2026-08-09T10:00:03Z",
      },
    ],
  };
}

function consoleSession(cancelAllowed: boolean) {
  return {
    schemaVersion: "1.0",
    identity: {
      subject: "user-1",
      username: "operator",
      method: "forward-auth",
      roles: cancelAllowed ? ["viewer", "operator"] : ["viewer"],
    },
    capabilities: {
      ingestRuns: {
        read: { available: true, allowed: true },
        create: {
          available: false,
          allowed: false,
          reason: "ingest_creation_kubernetes_only",
        },
        cancel: { available: true, allowed: cancelAllowed },
        retry: {
          available: false,
          allowed: cancelAllowed,
          reason: "ingest_retry_unavailable",
        },
      },
    },
  };
}

function jsonResponse(body: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: () => Promise.resolve(body),
  };
}

describe("operational routes", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    apiQueryClient.clear();
    primeApi();
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: false, status: 404 }),
    );
  });

  it.each([
    ["/", "Overview", "Overview · TAMOSS"],
    ["/service", "TAMS service", "TAMS service · TAMOSS"],
    ["/sources", "Sources", "Sources · TAMOSS"],
    ["/sources/source-1", "Camera A", "Source · TAMOSS"],
    ["/flows", "Flows", "Flows · TAMOSS"],
    ["/flows/flow-1", "Programme video", "Flow · TAMOSS"],
    ["/profiles", "Profiles", "Profiles · TAMOSS"],
    [
      "/profiles/00000000-0000-4000-8000-000000000101",
      "HD production video",
      "Profile · TAMOSS",
    ],
    ["/objects", "Object lookup", "Object lookup · TAMOSS"],
    ["/objects/object-1.ts", "Media object", "Media object · TAMOSS"],
    ["/playback", "Preview", "Preview · TAMOSS"],
    ["/ingest", "Ingest runs", "Ingest runs · TAMOSS"],
    ["/deletions", "Deletion requests", "Deletion requests · TAMOSS"],
    ["/webhooks", "Webhooks", "Webhooks · TAMOSS"],
    ["/system", "Runtime", "Runtime · TAMOSS"],
  ])("renders %s", async (route, heading, title) => {
    renderRoute(route);
    expect(
      await screen.findByRole("heading", { name: heading }),
    ).toBeInTheDocument();
    expect(document.title).toBe(title);
  });

  it.each([
    ["/sources", "Sources", mocks.api.getSources, true],
    ["/flows", "Flows", mocks.api.getFlows, true],
    ["/profiles", "Profiles", mocks.api.getProfiles, true],
    ["/deletions", "Deletion requests", mocks.api.getDeletionRequests, true],
    ["/webhooks", "Webhooks", mocks.api.getWebhooks, false],
  ])(
    "requests a bounded first page for %s",
    async (route, heading, request, forwardsSignal) => {
      renderRoute(route);
      await screen.findByRole("heading", { name: heading });
      await waitFor(() => {
        expect(request).toHaveBeenCalled();
        expect(request.mock.lastCall?.[0]).toEqual(
          expect.objectContaining({ limit: "50" }),
        );
        if (forwardsSignal) {
          expect(request.mock.lastCall?.[1]).toEqual(
            expect.objectContaining({ signal: expect.any(AbortSignal) }),
          );
        }
      });
    },
  );

  it("replaces the current catalog page instead of accumulating results", async () => {
    mocks.api.getSources
      .mockResolvedValueOnce({ data: [mocks.source], nextKey: "next-page" })
      .mockResolvedValueOnce({
        data: [{ ...mocks.source, id: "source-2", label: "Camera B" }],
      });
    renderRoute("/sources");
    expect(await screen.findByText("Camera A")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Next/ }));
    expect(await screen.findByText("Camera B")).toBeInTheDocument();
    expect(screen.queryByText("Camera A")).not.toBeInTheDocument();
    expect(mocks.api.getSources).toHaveBeenLastCalledWith(
      expect.objectContaining({ page: "next-page" }),
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
  });

  it("pages deletion requests without accumulating earlier results", async () => {
    const deletion = {
      id: "00000000-0000-4000-8000-000000000201",
      flow_id: "flow-1",
      timerange_to_delete: "[0:0_6:0)",
      delete_flow: false,
      status: "started",
    };
    mocks.api.getDeletionRequests
      .mockResolvedValueOnce({ data: [deletion], nextKey: "next-deletion" })
      .mockResolvedValueOnce({
        data: [
          {
            ...deletion,
            id: "00000000-0000-4000-8000-000000000202",
            status: "done",
          },
        ],
      });

    renderRoute("/deletions");
    expect(await screen.findByText(deletion.id)).toBeVisible();
    fireEvent.click(screen.getByRole("button", { name: /Next/ }));
    expect(
      await screen.findByText("00000000-0000-4000-8000-000000000202"),
    ).toBeVisible();
    expect(screen.queryByText(deletion.id)).not.toBeInTheDocument();
    expect(mocks.api.getDeletionRequests).toHaveBeenLastCalledWith(
      expect.objectContaining({
        page: "next-deletion",
        reverse_order: true,
        sort_by: "created",
      }),
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
  });

  it("applies a source filter and clears the active cursor atomically", async () => {
    renderRoute("/sources?cursor=stale-page");
    await screen.findByText("Camera A");

    fireEvent.change(screen.getByPlaceholderText("Exact label"), {
      target: { value: "Camera A" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Apply" }));

    await waitFor(() =>
      expect(mocks.api.getSources).toHaveBeenLastCalledWith(
        expect.objectContaining({
          label: "Camera A",
          page: undefined,
        }),
        expect.objectContaining({ signal: expect.any(AbortSignal) }),
      ),
    );
    expect(window.location.search).toContain("label=Camera+A");
    expect(window.location.search).not.toContain("cursor=");
  });

  it("pages Object references without requesting presigned URLs", async () => {
    mocks.api.getObject
      .mockResolvedValueOnce({ data: mocks.mediaObject, nextKey: "next-ref" })
      .mockResolvedValueOnce({
        data: { ...mocks.mediaObject, referenced_by_flows: ["flow-2"] },
      });

    renderRoute("/objects/object-1.ts");
    expect(await screen.findAllByText("flow-1")).not.toHaveLength(0);
    expect(mocks.api.getObject).toHaveBeenLastCalledWith(
      "object-1.ts",
      expect.objectContaining({
        limit: "50",
        page: undefined,
        presigned: false,
      }),
    );

    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(await screen.findByText("flow-2")).toBeInTheDocument();
    expect(mocks.api.getObject).toHaveBeenLastCalledWith(
      "object-1.ts",
      expect.objectContaining({ page: "next-ref", presigned: false }),
    );
  });

  it("does not mint presigned URLs for the Flow segment table", async () => {
    renderRoute("/flows/flow-1");
    await screen.findByText("Programme video");
    expect(mocks.api.getFlowSegments).toHaveBeenCalledWith(
      "flow-1",
      expect.objectContaining({ presigned: false }),
    );
  });

  it("filters Flows by 8.2 status and Profile", async () => {
    renderRoute(
      "/flows?status=replication_in_progress&profile_id=00000000-0000-4000-8000-000000000101",
    );
    await screen.findByText("Programme video");
    expect(mocks.api.getFlows).toHaveBeenLastCalledWith(
      expect.objectContaining({
        status: "replication_in_progress",
        profile_id: "00000000-0000-4000-8000-000000000101",
      }),
      expect.objectContaining({ signal: expect.any(AbortSignal) }),
    );
  });

  it("shows Flow status, Profile provenance and initialisation Objects", async () => {
    mocks.api.getFlowSegments.mockResolvedValue({
      data: [
        {
          object_id: "media-1.m4s",
          timerange: "[0:0_6:0)",
          object_timerange: "[0:0_6:0)",
          init_object: {
            object_id: "init-1.mp4",
            get_urls: [{ url: "https://media.example.test/init-1.mp4" }],
          },
        },
      ],
    });

    renderRoute("/flows/flow-1");
    expect(await screen.findByText("Ingesting")).toBeVisible();
    expect(
      screen.getByRole("link", {
        name: "00000000-0000-4000-8000-000000000101",
      }),
    ).toHaveAttribute("href", "/profiles/00000000-0000-4000-8000-000000000101");
    expect(
      await screen.findByRole("link", { name: "init-1.mp4" }),
    ).toHaveAttribute("href", "/objects/init-1.mp4");
  });

  it("shows a Media Object's initialisation Object relationship", async () => {
    mocks.api.getObject.mockResolvedValue({
      data: {
        ...mocks.mediaObject,
        init_object: {
          id: "init-1.mp4",
          get_urls: [{ url: "https://media.example.test/init-1.mp4" }],
        },
      },
    });

    renderRoute("/objects/object-1.ts");
    expect(
      await screen.findByRole("link", { name: "init-1.mp4" }),
    ).toHaveAttribute("href", "/objects/init-1.mp4");
  });

  it("degrades clearly when the Console API is unavailable", async () => {
    renderRoute("/system");
    expect(
      await screen.findByText("Runtime status is unavailable."),
    ).toBeInTheDocument();
  });

  it("reports an intentionally hibernated instance as paused", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        json: () => Promise.resolve(hibernatedRuntimeSnapshot()),
      }),
    );

    renderRoute("/");
    expect(await screen.findByText("Paused")).toBeInTheDocument();
    expect(screen.getByText("Hibernated")).toBeInTheDocument();
    expect(screen.getByText("No active warnings")).toBeInTheDocument();
    expect(screen.queryByText("Scaled down")).not.toBeInTheDocument();
  });

  it("marks the Dashboard deletion summary as partial", async () => {
    mocks.api.getDeletionRequests.mockResolvedValue({
      data: [
        {
          id: "00000000-0000-4000-8000-000000000201",
          flow_id: "flow-1",
          timerange_to_delete: "[0:0_6:0)",
          delete_flow: false,
          status: "started",
        },
      ],
      nextKey: "more-deletions",
    });
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        json: () => Promise.resolve(hibernatedRuntimeSnapshot()),
      }),
    );

    renderRoute("/");
    expect(await screen.findByText("1+ active request")).toBeVisible();
    expect(screen.getByText("Latest page")).toBeVisible();
  });

  it("does not present a stale hibernated snapshot as current", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        json: () => Promise.resolve(hibernatedRuntimeSnapshot(true)),
      }),
    );

    renderRoute("/");
    expect(await screen.findByText("Paused")).toBeInTheDocument();
    expect(screen.getAllByText("Stale")).not.toHaveLength(0);
    expect(screen.queryByText("No active warnings")).not.toBeInTheDocument();
  });

  it("shows cached runtime data as stale when the Dashboard refresh fails", async () => {
    apiQueryClient.setQueryData(
      ["control", "runtime"],
      hibernatedRuntimeSnapshot(),
    );
    await apiQueryClient.invalidateQueries({
      queryKey: ["control", "runtime"],
    });

    renderRoute("/");
    expect(await screen.findByText("Runtime refresh failed")).toBeVisible();
    expect(screen.getAllByText("Stale")).not.toHaveLength(0);
    expect(screen.queryByText("No active warnings")).not.toBeInTheDocument();
  });

  it("shows cached runtime data as stale when the System refresh fails", async () => {
    apiQueryClient.setQueryData(
      ["control", "runtime"],
      hibernatedRuntimeSnapshot(),
    );
    await apiQueryClient.invalidateQueries({
      queryKey: ["control", "runtime"],
    });

    renderRoute("/system");
    expect(
      await screen.findByText(
        "Runtime data is stale. The last refresh failed.",
      ),
    ).toBeVisible();
    expect(screen.getByText("Hibernated")).toBeVisible();
  });

  it("renders hibernated workloads as paused on the System page", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        json: () => Promise.resolve(hibernatedRuntimeSnapshot()),
      }),
    );

    renderRoute("/system");
    expect(await screen.findByText("Hibernated")).toBeVisible();
    expect(screen.getAllByText("Paused")).toHaveLength(5);
    expect(screen.getByText("Instance hibernated")).toBeVisible();
    expect(screen.queryByText("0/0 ready")).not.toBeInTheDocument();
    expect(screen.queryByText("scaledDown")).not.toBeInTheDocument();
  });

  it("renders runtime diagnostic explanations as accessible table content", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        json: () => Promise.resolve(diagnosticRuntimeSnapshot()),
      }),
    );

    renderRoute("/system");
    expect(
      await screen.findByRole("cell", {
        name: "DatabaseUnavailable",
      }),
    ).toBeVisible();
    expect(screen.getByText("Current")).toBeVisible();
    expect(
      screen.getByRole("columnheader", { name: "Replicas" }),
    ).toBeVisible();
    expect(
      screen.getByRole("columnheader", { name: "Diagnostic" }),
    ).toBeVisible();
    expect(
      screen.queryByRole("columnheader", { name: "Generation" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("columnheader", { name: "Updated" }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByRole("cell", {
        name: /MinimumReplicasUnavailable/,
      }),
    ).toBeVisible();
    expect(
      screen.getByRole("cell", {
        name: "CrashLoopBackOff",
      }),
    ).toBeVisible();
    expect(
      screen.getByRole("cell", {
        name: /BackoffLimitExceeded/,
      }),
    ).toBeVisible();
    expect(screen.getByRole("cell", { name: "Unhealthy" })).toBeVisible();
    expect(
      screen.getByRole("cell", { name: "tamoss-api-x7k2z" }),
    ).toBeVisible();
    expect(
      screen.getByRole("cell", { name: "http 8000/TCP -> http" }),
    ).toBeVisible();
    expect(screen.getByText("0/1 ready")).toBeVisible();
    expect(screen.getByRole("cell", { name: "1 not ready" })).toBeVisible();
  });

  it("shows reconciliation and rollout detail without permanent raw columns", async () => {
    const snapshot = diagnosticRuntimeSnapshot();
    snapshot.instance.generation = 4;
    snapshot.workloads[0].generation = 4;
    snapshot.workloads[0].updatedReplicas = 0;
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        json: () => Promise.resolve(snapshot),
      }),
    );

    renderRoute("/system");
    expect(await screen.findByText("Reconciling")).toBeVisible();
    expect(screen.getByText("0 updated")).toBeVisible();
    expect(screen.getAllByText("Details").length).toBeGreaterThan(0);
  });

  it("makes bounded ingest job projection visible", async () => {
    const snapshot = diagnosticRuntimeSnapshot();
    snapshot.ingestRuntimeTruncated = true;
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        json: () => Promise.resolve(snapshot),
      }),
    );

    renderRoute("/system");
    expect(await screen.findByText("Partial")).toBeVisible();
    expect(
      screen.getByText(
        "Runtime view is partial. Some ingest Jobs, Pods, and Events are not shown.",
      ),
    ).toBeVisible();
  });

  it("pages durable IngestRun history without accumulating prior results", async () => {
    const fetch = vi.fn((input: RequestInfo | URL) => {
      const url = new URL(String(input));
      const cursor = url.searchParams.get("cursor");
      return Promise.resolve(
        jsonResponse({
          schemaVersion: "1.0",
          items: [ingestRunSummary(cursor ? "ingest-second" : "ingest-first")],
          page: {
            limit: 25,
            nextCursor: cursor ? undefined : "opaque-next-page",
          },
        }),
      );
    });
    vi.stubGlobal("fetch", fetch);

    renderRoute("/ingest");
    expect(await screen.findByText("ingest-first")).toBeVisible();
    expect(screen.queryByText("Create run")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /Next/ }));

    expect(await screen.findByText("ingest-second")).toBeVisible();
    expect(screen.queryByText("ingest-first")).not.toBeInTheDocument();
    const pagedURL = new URL(
      String(fetch.mock.calls[fetch.mock.calls.length - 1]?.[0]),
    );
    expect(pagedURL.searchParams.get("cursor")).toBe("opaque-next-page");
    expect(pagedURL.searchParams.get("limit")).toBe("25");
  });

  it("keeps sparse IngestRun pages navigable", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        jsonResponse({
          schemaVersion: "1.0",
          items: [],
          page: { limit: 25, nextCursor: "opaque-sparse-page" },
        }),
      ),
    );

    renderRoute("/ingest?phase=Failed");
    expect(
      await screen.findByText("No matching runs on this page"),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: /Next/ })).toBeEnabled();
  });

  it("requires operator capability and confirmation for one-way cancellation", async () => {
    const run = ingestRunDetail();
    const fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = new URL(String(input));
      if (url.pathname.endsWith("/session")) {
        return Promise.resolve(jsonResponse(consoleSession(true)));
      }
      if (init?.method === "POST") {
        return Promise.resolve(
          jsonResponse({
            run: {
              ...run,
              desiredState: "Cancelled",
              cancellable: false,
              revision: "18",
            },
            replayed: false,
          }),
        );
      }
      return Promise.resolve(jsonResponse(run));
    });
    vi.stubGlobal("fetch", fetch);

    renderRoute("/ingest/ingest-20260809");
    const cancelTrigger = await screen.findByRole("button", {
      name: "Cancel run",
    });
    expect(
      screen.getByText(
        "video:0 FlowProfile hd-avc -> 60d9df18-6d9d-4b86-84bf-d1dcf14b3a28",
      ),
    ).toBeVisible();
    expect(screen.getByText("8.2 ingest test")).toBeVisible();
    expect(
      screen.getByRole("link", {
        name: "713d391c-828a-513e-9929-65e1bab9c35b",
      }),
    ).toHaveAttribute("href", "/flows/713d391c-828a-513e-9929-65e1bab9c35b");
    expect(screen.getByRole("link", { name: "video" })).toHaveAttribute(
      "href",
      "/flows/69d2a402-b8db-5faa-a970-0aed4f2acfc2",
    );
    expect(screen.queryByText("Retry run")).not.toBeInTheDocument();
    fireEvent.click(cancelTrigger);

    const confirmation = screen.getByRole("region", {
      name: "Cancel this ingest run?",
    });
    expect(
      within(confirmation).getByRole("button", { name: "Keep running" }),
    ).toHaveFocus();
    fireEvent.click(
      within(confirmation).getByRole("button", { name: "Cancel run" }),
    );

    expect(await screen.findByText("Cancellation requested.")).toBeVisible();
    const commandCall = fetch.mock.calls.find(
      ([, init]) => init?.method === "POST",
    );
    expect(commandCall?.[1]).toEqual(
      expect.objectContaining({
        credentials: "same-origin",
        body: JSON.stringify({ uid: run.uid, revision: run.revision }),
      }),
    );
  });

  it("does not expose cancellation to a viewer", async () => {
    const run = ingestRunDetail("viewer-run");
    vi.stubGlobal(
      "fetch",
      vi.fn((input: RequestInfo | URL) => {
        const url = new URL(String(input));
        return Promise.resolve(
          jsonResponse(
            url.pathname.endsWith("/session") ? consoleSession(false) : run,
          ),
        );
      }),
    );

    renderRoute("/ingest/viewer-run");
    expect(await screen.findByText("viewer-run-job")).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "Cancel run" }),
    ).not.toBeInTheDocument();
  });

  it("includes unhealthy selector-backed routing but ignores ExternalName services", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        json: () => Promise.resolve(diagnosticRuntimeSnapshot()),
      }),
    );

    renderRoute("/");
    expect(await screen.findByText("1 unhealthy service route")).toBeVisible();
    expect(screen.queryByText("No active warnings")).not.toBeInTheDocument();
  });

  it("does not report a clean Overview when a summary request fails", async () => {
    mocks.api.getSources.mockRejectedValue(
      new Error("source summary unavailable"),
    );
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        json: () => Promise.resolve(hibernatedRuntimeSnapshot()),
      }),
    );

    renderRoute("/");
    expect(
      await screen.findByText("Sources summary", {}, { timeout: 3_000 }),
    ).toBeVisible();
    expect(screen.getByText("Request failed")).toBeVisible();
    expect(screen.queryByText("No active warnings")).not.toBeInTheDocument();
  });

  it("manages focus and Escape for the mobile navigation drawer", async () => {
    renderRoute("/sources");
    await screen.findByRole("heading", { name: "Sources" });
    // CSS media queries are not evaluated by JSDOM, so query the mobile-only
    // control by its accessible label while testing its focus state machine.
    const open = screen.getByLabelText("Open navigation");

    fireEvent.click(open);
    const close = screen
      .getByRole("complementary")
      .querySelector<HTMLButtonElement>('[aria-label="Close navigation"]');
    expect(close).not.toBeNull();
    expect(open).toHaveAttribute("aria-expanded", "true");
    expect(close).toHaveFocus();

    fireEvent.keyDown(window, { key: "Escape" });
    expect(open).toHaveAttribute("aria-expanded", "false");
    expect(open).toHaveFocus();
  });
});
