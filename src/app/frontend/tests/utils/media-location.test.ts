import { describe, expect, it } from "vitest";
import { displayMediaLocation, displaySafeUrl } from "@/utils/media-location";

describe("displayMediaLocation", () => {
  it("removes URL credentials, query parameters, and fragments", () => {
    expect(
      displayMediaLocation(
        "https://user:password@media.example.test/object.ts?signature=secret#fragment",
      ),
    ).toBe("https://media.example.test/object.ts");
  });

  it("does not echo an invalid location", () => {
    expect(displayMediaLocation("not a URL?token=secret")).toBe(
      "Unparseable location",
    );
  });
});

describe("displaySafeUrl", () => {
  it("redacts credentials from non-media endpoints", () => {
    expect(
      displaySafeUrl(
        "https://hook-user:hook-password@example.test/callback?token=secret#fragment",
      ),
    ).toBe("https://example.test/callback");
  });

  it("does not echo an invalid URL", () => {
    expect(displaySafeUrl("hook?token=secret")).toBe("Unparseable URL");
  });
});
