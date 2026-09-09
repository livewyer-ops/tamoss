import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  installStaleAssetRecovery,
  isStaleAssetError,
  RouteLoadBoundary,
  reloadStaleAssetsOnce,
} from "@/components/RouteLoadBoundary";

afterEach(() => {
  sessionStorage.clear();
});

describe("isStaleAssetError", () => {
  it.each([
    "ChunkLoadError: Loading chunk 4 failed",
    "Failed to fetch dynamically imported module",
    "Importing a module script failed",
  ])("recognises stale deployment assets: %s", (message) => {
    expect(isStaleAssetError(new TypeError(message))).toBe(true);
  });

  it("does not reload for an ordinary render error", () => {
    expect(isStaleAssetError(new Error("invalid response"))).toBe(false);
  });

  it("does not describe an ordinary render failure as a console update", () => {
    function BrokenView(): never {
      throw new Error("invalid response");
    }

    const report = vi.spyOn(console, "error").mockImplementation(() => {});
    try {
      render(
        <RouteLoadBoundary>
          <BrokenView />
        </RouteLoadBoundary>,
      );

      expect(
        screen.getByRole("heading", { name: "View unavailable" }),
      ).toBeInTheDocument();
      expect(
        screen.queryByText("Console update required"),
      ).not.toBeInTheDocument();
    } finally {
      report.mockRestore();
    }
  });

  it("reloads once within the stale-asset recovery window", () => {
    const reload = vi.fn();

    expect(reloadStaleAssetsOnce({ now: 100_000, reload })).toBe(true);
    expect(reloadStaleAssetsOnce({ now: 120_000, reload })).toBe(false);
    expect(reload).toHaveBeenCalledTimes(1);

    expect(reloadStaleAssetsOnce({ now: 160_001, reload })).toBe(true);
    expect(reload).toHaveBeenCalledTimes(2);
  });

  it("prevents the failed preload and invokes recovery", () => {
    const recover = vi.fn(() => true);
    const uninstall = installStaleAssetRecovery(recover);
    const event = new Event("vite:preloadError", { cancelable: true });

    expect(window.dispatchEvent(event)).toBe(false);
    expect(event.defaultPrevented).toBe(true);
    expect(recover).toHaveBeenCalledOnce();

    uninstall();
  });
});
