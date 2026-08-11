import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useCursorPage } from "@/hooks/useCursorPage";

interface PendingRequest {
  cursor?: string;
  resolve: (value: { data: string[] }) => void;
  signal: AbortSignal;
}

function pendingCatalogLoad(requests: PendingRequest[]) {
  return vi.fn((cursor: string | undefined, signal: AbortSignal) => {
    return new Promise<{ data: string[] }>((resolve) => {
      requests.push({ cursor, resolve, signal });
    });
  });
}

describe("useCursorPage", () => {
  it("resets Previous history after an external cursor change", async () => {
    const load = vi.fn(async (cursor?: string) => ({
      data: [cursor ?? "first"],
      nextKey: cursor === "page-a" ? "page-b" : "page-a",
    }));
    const onCursorChange = vi.fn();
    const { result, rerender } = renderHook(
      ({ cursor }: { cursor?: string }) =>
        useCursorPage<string>({ cursor, load, onCursorChange }),
      { initialProps: { cursor: undefined as string | undefined } },
    );

    await waitFor(() => expect(result.current.hasNext).toBe(true));
    act(() => result.current.next());
    expect(onCursorChange).toHaveBeenLastCalledWith("page-a");

    rerender({ cursor: "page-a" });
    await waitFor(() => expect(result.current.data).toEqual(["page-a"]));
    expect(result.current.hasPrevious).toBe(true);

    rerender({ cursor: undefined });
    await waitFor(() => expect(result.current.data).toEqual(["first"]));
    expect(result.current.hasPrevious).toBe(false);
  });

  it("preserves Previous history for in-app navigation", async () => {
    const load = vi.fn(async (cursor?: string) => ({
      data: [cursor ?? "first"],
      nextKey: "page-a",
    }));
    const onCursorChange = vi.fn();
    const { result, rerender } = renderHook(
      ({ cursor }: { cursor?: string }) =>
        useCursorPage<string>({ cursor, load, onCursorChange }),
      { initialProps: { cursor: undefined as string | undefined } },
    );

    await waitFor(() => expect(result.current.hasNext).toBe(true));
    act(() => result.current.next());
    rerender({ cursor: "page-a" });
    await waitFor(() => expect(result.current.hasPrevious).toBe(true));

    act(() => result.current.previous());
    expect(onCursorChange).toHaveBeenLastCalledWith(undefined);
    rerender({ cursor: undefined });
    await waitFor(() => expect(result.current.data).toEqual(["first"]));
    expect(result.current.hasPrevious).toBe(false);
  });

  it("keeps Previous available across a deep forward traversal", async () => {
    const depth = 12;
    const load = vi.fn(async (cursor?: string) => ({
      data: [cursor ?? "page-0"],
      nextKey: `page-${Number(cursor?.replace("page-", "") ?? 0) + 1}`,
    }));
    const onCursorChange = vi.fn();
    const { result, rerender } = renderHook(
      ({ cursor }: { cursor?: string }) =>
        useCursorPage<string>({ cursor, load, onCursorChange }),
      { initialProps: { cursor: undefined as string | undefined } },
    );

    await waitFor(() => expect(result.current.hasNext).toBe(true));
    for (let page = 1; page <= depth; page += 1) {
      act(() => result.current.next());
      rerender({ cursor: `page-${page}` });
      await waitFor(() =>
        expect(result.current.data).toEqual([`page-${page}`]),
      );
      expect(result.current.hasPrevious).toBe(true);
    }

    for (let page = depth - 1; page >= 1; page -= 1) {
      act(() => result.current.previous());
      expect(onCursorChange).toHaveBeenLastCalledWith(`page-${page}`);
      rerender({ cursor: `page-${page}` });
      await waitFor(() =>
        expect(result.current.data).toEqual([`page-${page}`]),
      );
      expect(result.current.hasPrevious).toBe(true);
    }

    act(() => result.current.previous());
    expect(onCursorChange).toHaveBeenLastCalledWith(undefined);
    rerender({ cursor: undefined });
    await waitFor(() => expect(result.current.data).toEqual(["page-0"]));
    expect(result.current.hasPrevious).toBe(false);
  });

  it("aborts superseded loads and only publishes the newest page", async () => {
    const requests: PendingRequest[] = [];
    const load = pendingCatalogLoad(requests);
    const onCursorChange = vi.fn();
    const { result, rerender, unmount } = renderHook(
      ({ cursor }: { cursor?: string }) =>
        useCursorPage<string>({ cursor, load, onCursorChange }),
      { initialProps: { cursor: "page-a" } },
    );

    await waitFor(() => expect(requests).toHaveLength(1));
    rerender({ cursor: "page-b" });
    await waitFor(() => expect(requests).toHaveLength(2));
    expect(requests[0].signal.aborted).toBe(true);

    rerender({ cursor: "page-c" });
    await waitFor(() => expect(requests).toHaveLength(3));
    expect(requests[1].signal.aborted).toBe(true);
    expect(requests[2].signal.aborted).toBe(false);

    await act(async () => {
      requests[2].resolve({ data: ["current"] });
    });
    expect(result.current.data).toEqual(["current"]);
    expect(result.current.loading).toBe(false);

    await act(async () => {
      requests[0].resolve({ data: ["stale-a"] });
      requests[1].resolve({ data: ["stale-b"] });
    });
    expect(result.current.data).toEqual(["current"]);

    unmount();
    expect(requests[2].signal.aborted).toBe(true);
  });

  it("does not surface an abort rejection as a catalog error", async () => {
    const signals: AbortSignal[] = [];
    const load = vi.fn((_cursor: string | undefined, signal: AbortSignal) => {
      signals.push(signal);
      return new Promise<{ data: string[] }>((_resolve, reject) => {
        signal.addEventListener(
          "abort",
          () => reject(new DOMException("Aborted", "AbortError")),
          { once: true },
        );
      });
    });
    const { result, rerender } = renderHook(
      ({ cursor }: { cursor?: string }) =>
        useCursorPage<string>({ cursor, load, onCursorChange: vi.fn() }),
      { initialProps: { cursor: "page-a" } },
    );

    await waitFor(() => expect(signals).toHaveLength(1));
    rerender({ cursor: "page-b" });
    await waitFor(() => expect(signals).toHaveLength(2));

    expect(signals[0].aborted).toBe(true);
    expect(result.current.error).toBeNull();
    expect(result.current.loading).toBe(true);
  });
});
