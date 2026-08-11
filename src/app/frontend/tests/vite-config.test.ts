import type { ProxyOptions, UserConfig } from "vite";
import { afterEach, describe, expect, it, vi } from "vitest";

async function loadApiProxy(): Promise<ProxyOptions> {
  vi.resetModules();
  const module = await import("../vite.config");
  const config = module.default as UserConfig;
  const proxy = config.server?.proxy;
  if (!proxy || Array.isArray(proxy)) {
    throw new Error("Vite API proxy is not configured");
  }
  return proxy["/api"] as ProxyOptions;
}

afterEach(() => {
  vi.unstubAllEnvs();
  vi.resetModules();
});

describe("Vite API proxy credential", () => {
  it("adds the server-only development token upstream", async () => {
    vi.stubEnv("TAMOSS_DEV_API_TOKEN", "server-secret");

    const proxy = await loadApiProxy();

    expect(proxy.headers).toEqual({ Authorization: "Bearer server-secret" });
  });

  it("ignores the legacy browser-visible VITE_API_TOKEN", async () => {
    vi.stubEnv("TAMOSS_DEV_API_TOKEN", "");
    vi.stubEnv("VITE_API_TOKEN", "browser-secret");

    const proxy = await loadApiProxy();

    expect(proxy.headers).toBeUndefined();
  });

  it("rejects a token that cannot be represented safely as a header", async () => {
    vi.stubEnv("TAMOSS_DEV_API_TOKEN", "server secret");

    await expect(loadApiProxy()).rejects.toThrow(
      "TAMOSS_DEV_API_TOKEN is not a valid HTTP bearer token",
    );
  });
});
