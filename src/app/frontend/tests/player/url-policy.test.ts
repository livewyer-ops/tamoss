import { describe, expect, it } from "vitest";
import { sanitizeMediaUrl } from "@/player/url-policy";
import type { ObjectUrl } from "@/types/tams";

function mediaUrl(url: string, overrides: Partial<ObjectUrl> = {}): ObjectUrl {
  return { url, ...overrides };
}

describe("media URL policy", () => {
  it("allows unsigned same-origin media but rejects same-origin signed media", () => {
    expect(
      sanitizeMediaUrl(
        mediaUrl("/media/clip.ts", {
          label: "primary",
          storage_id: "storage-1",
        }),
        "https://app.example",
      ),
    ).toEqual({
      url: "https://app.example/media/clip.ts",
      credentials: "same-origin",
      presigned: false,
      label: "primary",
      storageId: "storage-1",
    });

    expect(
      sanitizeMediaUrl(
        mediaUrl("https://app.example/media/signed.ts?token=secret", {
          presigned: true,
        }),
        "https://app.example",
      ),
    ).toBeUndefined();
  });

  it("accepts external HTTPS media without credentials", () => {
    expect(
      sanitizeMediaUrl(
        mediaUrl("https://storage.example/clip.ts?signature=secret", {
          presigned: true,
        }),
        "https://app.example",
      ),
    ).toEqual({
      url: "https://storage.example/clip.ts?signature=secret",
      credentials: "omit",
      presigned: true,
    });
  });

  it("limits plain HTTP to the same origin and loopback hosts", () => {
    expect(
      sanitizeMediaUrl(
        mediaUrl("http://app.example/media/clip.ts"),
        "http://app.example",
      ),
    ).toBeDefined();
    expect(
      sanitizeMediaUrl(
        mediaUrl("http://127.0.0.1:9000/media/clip.ts"),
        "http://localhost:5173",
      ),
    ).toBeDefined();
    expect(
      sanitizeMediaUrl(
        mediaUrl("http://storage.example/media/clip.ts"),
        "https://app.example",
      ),
    ).toBeUndefined();
  });

  it.each([
    "data:video/mp2t;base64,AAAA",
    "javascript:alert(1)",
    "https://user:password@storage.example/clip.ts",
    "https://storage.example/clip.ts#fragment",
    "https://storage.example/clip%0a.ts",
    "https://storage.example/clip\n.ts",
    "https://storage.example/clip%C2%85.ts",
    "https://storage.example/clip%7f.ts",
    "https://storage.example/clip%80.ts",
    "https://storage.example/clip%GG.ts",
    "https://storage.example/clip%E6%97.ts",
  ])("rejects an unsafe URL without returning it: %s", (url) => {
    expect(
      sanitizeMediaUrl(mediaUrl(url), "https://app.example"),
    ).toBeUndefined();
  });

  it("preserves UTF-8 paths and the original signed query encoding", () => {
    const url =
      "https://storage.example/%E6%97%A5%E6%9C%AC%E8%AA%9E.mp4?X-Signature=a%2fb%2Bc&name=%C3%89t%C3%A9&part=2&part=1";
    expect(
      sanitizeMediaUrl(
        mediaUrl(url, { presigned: true }),
        "https://app.example",
      ),
    ).toEqual({
      url,
      credentials: "omit",
      presigned: true,
    });
  });

  it("does not include candidate URL values in policy errors", () => {
    const candidate = "https://storage.example/secret.ts?token=do-not-log";
    expect(() =>
      sanitizeMediaUrl(mediaUrl(candidate), "not an origin"),
    ).toThrow("The media URL policy origin is invalid.");
    try {
      sanitizeMediaUrl(mediaUrl(candidate), "not an origin");
    } catch (error) {
      expect(String(error)).not.toContain(candidate);
      expect(String(error)).not.toContain("do-not-log");
    }
  });
});
