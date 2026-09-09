import { afterEach, describe, expect, it, vi } from "vitest";
import { installSensitiveConsoleErrorRedaction } from "@/player/console-redaction";

afterEach(() => {
  vi.restoreAllMocks();
});

describe("Omakase console redaction", () => {
  it("replaces nested and circular signed-URL error data", () => {
    const output = vi.spyOn(console, "error").mockImplementation(() => {});
    const signedUrl = "https://media.example/object.ts?X-Amz-Signature=private";
    const release = installSensitiveConsoleErrorRedaction([signedUrl]);
    const errorData: Record<string, unknown> = {
      response: { url: signedUrl },
    };
    errorData.self = errorData;

    console.error("hlsError", errorData);

    expect(output).toHaveBeenCalledWith(
      "Omakase media error details were redacted by TAMOSS.",
    );
    expect(JSON.stringify(output.mock.calls)).not.toContain("X-Amz-Signature");
    release();
  });

  it("forwards unrelated errors unchanged and restores the console", () => {
    const output = vi.spyOn(console, "error").mockImplementation(() => {});
    const originalConsoleError = console.error;
    const release = installSensitiveConsoleErrorRedaction([
      "https://media.example/private.ts?token=private",
    ]);

    console.error("ordinary failure", { code: "offline" });
    expect(output).toHaveBeenCalledWith("ordinary failure", {
      code: "offline",
    });

    release();
    expect(console.error).toBe(originalConsoleError);
    console.error("after preview");
    expect(output).toHaveBeenLastCalledWith("after preview");
  });

  it("keeps concurrent preview values protected until their owner releases them", () => {
    const output = vi.spyOn(console, "error").mockImplementation(() => {});
    const firstUrl = "https://media.example/first.ts?token=first-secret";
    const secondUrl = "https://media.example/second.ts?token=second-secret";
    const releaseFirst = installSensitiveConsoleErrorRedaction([firstUrl]);
    const releaseSecond = installSensitiveConsoleErrorRedaction([secondUrl]);

    releaseFirst();
    console.error(new Error(secondUrl));
    expect(output).toHaveBeenLastCalledWith(
      "Omakase media error details were redacted by TAMOSS.",
    );

    releaseSecond();
    console.error("restored");
    expect(output).toHaveBeenLastCalledWith("restored");
  });
});
