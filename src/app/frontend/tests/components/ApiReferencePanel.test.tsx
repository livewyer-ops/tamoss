import { describe, expect, it } from "vitest";
import { buildApiReferenceUrl } from "@/components/apiReferenceUrl";

describe("buildApiReferenceUrl", () => {
  it("resolves relative API bases against the app origin", () => {
    expect(
      buildApiReferenceUrl(
        "/sources/source-1",
        "/api",
        "https://app.example.test",
      ),
    ).toBe("https://app.example.test/api/sources/source-1");
  });

  it("preserves absolute API bases", () => {
    expect(
      buildApiReferenceUrl(
        "/sources/source-1",
        "https://api.example.test",
        "https://app.example.test",
      ),
    ).toBe("https://api.example.test/sources/source-1");
  });
});
