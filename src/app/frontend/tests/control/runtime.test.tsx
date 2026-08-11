import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useRuntime } from "@/control/runtime";

vi.mock("@/config", () => ({
  config: { controlApiUrl: "/ui-api/v1" },
}));

const eventSource = vi.fn();

class FakeEventSource {
  close = vi.fn();
  addEventListener = vi.fn();

  constructor(url: string) {
    eventSource(url);
  }
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
  return render(<Probe />, { wrapper: Wrapper });
}

describe("runtime stream", () => {
  beforeEach(() => {
    vi.clearAllMocks();
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
});
