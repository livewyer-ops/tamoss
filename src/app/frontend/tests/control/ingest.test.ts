import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  type ConsoleApiError,
  cancelIngestRun,
  getConsoleSession,
  getIngestRun,
  getIngestRuns,
} from "@/control/ingest";

vi.mock("@/config", () => ({
  config: { controlApiUrl: "/ui-api/v1" },
}));

describe("Console ingest client", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("forwards bounded filters, cursor and cancellation signals", async () => {
    const controller = new AbortController();
    const fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () =>
        Promise.resolve({
          schemaVersion: "1.0",
          items: [],
          page: { limit: 50, nextCursor: "opaque-next" },
        }),
    });
    vi.stubGlobal("fetch", fetch);

    const result = await getIngestRuns(
      { limit: 50, phase: "Running", cursor: "opaque-current" },
      { signal: controller.signal },
    );
    const [requested, init] = fetch.mock.calls[0] as [string, RequestInit];
    const url = new URL(requested);
    expect(url.pathname).toBe("/ui-api/v1/ingest-runs");
    expect(Object.fromEntries(url.searchParams)).toEqual({
      cursor: "opaque-current",
      limit: "50",
      phase: "Running",
    });
    expect(init).toEqual(
      expect.objectContaining({
        credentials: "same-origin",
        signal: controller.signal,
      }),
    );
    expect(result.page.nextCursor).toBe("opaque-next");
  });

  it("uses encoded detail paths and maps projected errors", async () => {
    const fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 404,
      json: () =>
        Promise.resolve({
          code: "not_found",
          error: "ingest run was not found",
        }),
    });
    vi.stubGlobal("fetch", fetch);

    await expect(getIngestRun("run.with-dots")).rejects.toEqual(
      expect.objectContaining<Partial<ConsoleApiError>>({
        status: 404,
        code: "not_found",
        message: "ingest run was not found",
      }),
    );
    expect(fetch.mock.calls[0]?.[0]).toContain(
      "/ui-api/v1/ingest-runs/run.with-dots",
    );
  });

  it("posts only the typed cancellation preconditions", async () => {
    const controller = new AbortController();
    const fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ replayed: false, run: { name: "run" } }),
    });
    vi.stubGlobal("fetch", fetch);

    await cancelIngestRun(
      "run",
      { uid: "uid-1", revision: "7" },
      { signal: controller.signal },
    );
    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining("/ui-api/v1/ingest-runs/run/cancel"),
      expect.objectContaining({
        method: "POST",
        credentials: "same-origin",
        body: JSON.stringify({ uid: "uid-1", revision: "7" }),
        signal: controller.signal,
        headers: {
          Accept: "application/json",
          "Content-Type": "application/json",
        },
      }),
    );
  });

  it("reads session capabilities without mutation credentials", async () => {
    const fetch = vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ schemaVersion: "1.0", capabilities: {} }),
    });
    vi.stubGlobal("fetch", fetch);

    await getConsoleSession();
    expect(fetch).toHaveBeenCalledWith(
      expect.stringContaining("/ui-api/v1/session"),
      expect.objectContaining({
        credentials: "same-origin",
        headers: { Accept: "application/json" },
      }),
    );
  });
});
