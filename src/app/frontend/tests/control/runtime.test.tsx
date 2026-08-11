import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useRuntime } from "@/control/runtime";

vi.mock("@/config", () => ({
  config: { controlApiUrl: "/ui-api/v1" },
}));

const eventSource = vi.fn();
let runtimeListener: ((event: MessageEvent<string>) => void) | undefined;

class FakeEventSource {
  close = vi.fn();
  addEventListener = vi.fn(
    (type: string, listener: (event: MessageEvent<string>) => void) => {
      if (type === "runtime") runtimeListener = listener;
    },
  );

  constructor(url: string) {
    eventSource(url);
  }
}

function CollectionsProbe() {
  const runtime = useRuntime();
  if (!runtime.data) return <span>loading</span>;
  return (
    <span>
      {[
        runtime.data.instance.conditions.length,
        runtime.data.workloads.length,
        runtime.data.services.length,
        runtime.data.endpointSlices.length,
        runtime.data.pods.length,
        runtime.data.jobs.length,
        runtime.data.events.length,
      ].join("/")}
    </span>
  );
}

function Probe() {
  const runtime = useRuntime();
  return (
    <span>
      {runtime.data?.instance.name ?? runtime.error?.message ?? "loading"}
    </span>
  );
}

function renderProbe() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const Wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  return { client, ...render(<Probe />, { wrapper: Wrapper }) };
}

function snapshot(observedAt: string, name: string) {
  return {
    schemaVersion: "1.0",
    observedAt,
    stale: false,
    instance: {
      name,
      namespace: "tamoss",
      uid: "instance-uid",
      generation: 1,
      observedGeneration: 1,
      phase: "Ready",
      conditions: [],
    },
    workloads: [],
    services: [],
    endpointSlices: [],
    pods: [],
    jobs: [],
    events: [],
  };
}

function jsonResponse(body: unknown) {
  return { ok: true, json: () => Promise.resolve(body) };
}

describe("runtime stream", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    runtimeListener = undefined;
    vi.stubGlobal("EventSource", FakeEventSource);
  });

  it("does not reconnect when the runtime endpoint is unavailable", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({ ok: false, status: 404 }),
    );
    renderProbe();
    expect(
      await screen.findByText("Runtime status is unavailable."),
    ).toBeInTheDocument();
    expect(eventSource).not.toHaveBeenCalled();
  });

  it("opens the event stream after the initial snapshot", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        json: () =>
          Promise.resolve({
            schemaVersion: "1.0",
            observedAt: "2026-08-09T10:00:00Z",
            stale: false,
            instance: {
              name: "tamoss-kind",
              namespace: "tamoss",
              uid: "instance-uid",
              generation: 1,
              observedGeneration: 1,
              phase: "Ready",
              conditions: [],
            },
            workloads: [],
            services: [],
            endpointSlices: [],
            pods: [],
            jobs: [],
            events: [],
          }),
      }),
    );
    renderProbe();
    expect(await screen.findByText("tamoss-kind")).toBeInTheDocument();
    await waitFor(() =>
      expect(eventSource).toHaveBeenCalledWith(
        expect.stringContaining("/ui-api/v1/runtime/events"),
      ),
    );
  });

  it("normalizes null collections from snapshots and stream updates", async () => {
    const snapshot = {
      schemaVersion: "1.0",
      observedAt: "2026-08-09T10:00:00Z",
      stale: false,
      instance: {
        name: "tamoss-kind",
        namespace: "tamoss",
        uid: "instance-uid",
        generation: 1,
        observedGeneration: 1,
        phase: "Ready",
        conditions: null,
      },
      workloads: null,
      services: null,
      endpointSlices: null,
      pods: null,
      jobs: null,
      events: null,
    };
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        json: () => Promise.resolve(snapshot),
      }),
    );

    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    render(
      <QueryClientProvider client={client}>
        <CollectionsProbe />
      </QueryClientProvider>,
    );

    expect(await screen.findByText("0/0/0/0/0/0/0")).toBeInTheDocument();
    await waitFor(() => expect(runtimeListener).toBeDefined());
    runtimeListener?.(
      new MessageEvent("runtime", { data: JSON.stringify(snapshot) }),
    );
    await waitFor(() =>
      expect(screen.getByText("0/0/0/0/0/0/0")).toBeInTheDocument(),
    );
  });

  it("keeps the newest observation when a slow poll answers last", async () => {
    let answerPoll: (response: unknown) => void = () => undefined;
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(
        jsonResponse(snapshot("2026-08-09T10:00:00Z", "first")),
      )
      .mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            answerPoll = resolve;
          }),
      );
    vi.stubGlobal("fetch", fetchMock);

    const { client } = renderProbe();
    expect(await screen.findByText("first")).toBeInTheDocument();
    await waitFor(() => expect(runtimeListener).toBeDefined());

    const polled = client.refetchQueries({ queryKey: ["control", "runtime"] });
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(2));

    runtimeListener?.(
      new MessageEvent("runtime", {
        data: JSON.stringify(snapshot("2026-08-09T10:00:10Z", "streamed")),
      }),
    );
    expect(await screen.findByText("streamed")).toBeInTheDocument();

    answerPoll(jsonResponse(snapshot("2026-08-09T10:00:05Z", "stale-poll")));
    await polled;

    expect(client.getQueryData(["control", "runtime"])).toMatchObject({
      observedAt: "2026-08-09T10:00:10Z",
      instance: { name: "streamed" },
    });
  });

  it("ignores a stream update older than the cached snapshot", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          jsonResponse(snapshot("2026-08-09T10:00:10Z", "polled")),
        ),
    );

    const { client } = renderProbe();
    expect(await screen.findByText("polled")).toBeInTheDocument();
    await waitFor(() => expect(runtimeListener).toBeDefined());

    runtimeListener?.(
      new MessageEvent("runtime", {
        data: JSON.stringify(snapshot("2026-08-09T10:00:00Z", "stale-stream")),
      }),
    );

    expect(client.getQueryData(["control", "runtime"])).toMatchObject({
      observedAt: "2026-08-09T10:00:10Z",
      instance: { name: "polled" },
    });
    expect(screen.queryByText("stale-stream")).not.toBeInTheDocument();
  });

  it("accepts a newer stream update", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          jsonResponse(snapshot("2026-08-09T10:00:00Z", "polled")),
        ),
    );

    renderProbe();
    expect(await screen.findByText("polled")).toBeInTheDocument();
    await waitFor(() => expect(runtimeListener).toBeDefined());

    runtimeListener?.(
      new MessageEvent("runtime", {
        data: JSON.stringify(snapshot("2026-08-09T10:00:10Z", "streamed")),
      }),
    );

    expect(await screen.findByText("streamed")).toBeInTheDocument();
  });
});
